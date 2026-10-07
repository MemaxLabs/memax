package ledger

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Follow-up work goes out through River in the command's own transaction
// (plan 25 §5.1, §5.3 step 6): River is the outbox. A command that dirties
// targets inserts their compile_target jobs before it commits, so a
// committed Keep always has its compile queued, and a rolled-back one never
// does.
//
// # The role switch around InsertTx
//
// Ledger transactions run as memax_v2, which has no privileges on River's
// tables, on purpose: river_job has no row-level security and holds every
// space's job arguments, so a grant there would undo the isolation memax_v2
// exists for. So the ledger switches back to the transaction's login role
// for exactly the River insert, and to memax_v2 straight after:
//
//	set_config('role', <the role the transaction began with>, true)
//	river InsertManyTx(tx, …)
//	set_config('role', 'memax_v2', true)
//
// Nothing else runs in that window, and both switches are SET LOCAL, so
// they end with the transaction whatever happens. River v0.49 has no
// savepoints in its transactional helpers: if the insert fails, Postgres
// has aborted the transaction, and the whole command rolls back with it
// (TestRiverFailureRollsBackTheCommand). The enqueue is the last statement
// before COMMIT, so the deferred receipt checks run as memax_v2.

// QueueCompile is the River queue compile jobs run on.
const QueueCompile = "compile"

// CompileTargetArgs is the River job that compiles one target (§5.7).
//
// It is unique by args over every unfinished state, so a target has at
// most one compile job waiting or running: a Keep that lands while the
// job runs bumps dirty_gen, and the running job notices and compiles
// again (the generation counter). It doesn't need a job of its own.
type CompileTargetArgs struct {
	TargetID uuid.UUID `json:"target_id"`
	SpaceID  uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (CompileTargetArgs) Kind() string { return "compile_target" }

// CompileUniqueStates are the states a compile_target job is unique over:
// available, pending, running, scheduled (snoozed) and retryable.
var CompileUniqueStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
	rivertype.JobStateScheduled, rivertype.JobStateRetryable,
}

// InsertOpts implements river.JobArgsWithInsertOpts.
func (CompileTargetArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueCompile,
		// Transient failures (the compile service restarting) retry with
		// River's backoff; after that the sweeper takes over, every 30 s.
		MaxAttempts: 5,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: slices.Clone(CompileUniqueStates)},
	}
}

// Jobs inserts River jobs in a transaction: (*river.Client[pgx.Tx]).InsertManyTx.
type Jobs interface {
	InsertManyTx(ctx context.Context, tx pgx.Tx, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error)
}

// WithJobs makes commands enqueue their follow-up jobs with River's
// InsertTx. Without it, nothing is enqueued and the compile sweeper alone
// notices dirty targets (within 30 s).
func WithJobs(j Jobs) Option { return func(l *Ledger) { l.inserter = j } }

// markDirty bumps the generation of every target of the space (or only
// the given targets) that isn't off, and queues their compile jobs. It
// runs in the command's transaction; the jobs are inserted by flush.
func (w *writer) markDirty(ctx context.Context, spaceID uuid.UUID, targets ...uuid.UUID) error {
	var only []uuid.UUID
	if len(targets) > 0 {
		only = targets
	}
	rows, err := w.tx.Query(ctx, `
		UPDATE v2.targets
		   SET dirty_gen = dirty_gen + 1, dirty_at = now(), updated_at = now(),
		       sync_state = CASE WHEN sync_state IN ('in_sync', 'pending_delivery') THEN 'compiling' ELSE sync_state END
		 WHERE space_id = $1 AND sync_state <> 'off' AND ($2::uuid[] IS NULL OR id = ANY ($2))
		RETURNING id`, spaceID, only)
	if err != nil {
		return fmt.Errorf("ledger: mark targets dirty: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("ledger: mark targets dirty: %w", err)
	}
	for _, id := range ids {
		args := CompileTargetArgs{TargetID: id, SpaceID: spaceID}
		if !slices.ContainsFunc(w.jobs, func(p river.InsertManyParams) bool { return p.Args == args }) {
			w.jobs = append(w.jobs, river.InsertManyParams{Args: args})
		}
	}
	return nil
}

// Finisher runs in a command's transaction, as the login role, right
// before COMMIT, alongside the River inserts. The compile job passes one
// that completes the job itself (river.JobCompleteTx), so the job is
// finished in the same commit that records its generation. Without that,
// a Keep landing after the commit but before River marked the job
// completed would have its InsertTx deduplicated against the still
// "running" job, and its generation would wait for the sweeper.
type Finisher func(ctx context.Context, tx pgx.Tx) error

// flush inserts the command's jobs and runs its finishers, switching to
// the login role for them only (see the package note above).
func (w *writer) flush(ctx context.Context) error {
	if (w.inserter == nil || len(w.jobs) == 0) && len(w.finishers) == 0 {
		return nil
	}
	return asLoginRole(ctx, w.tx, w.loginRole, func() error {
		if w.inserter != nil && len(w.jobs) > 0 {
			if _, err := w.inserter.InsertManyTx(ctx, w.tx, w.jobs); err != nil {
				return fmt.Errorf("ledger: enqueue %d job(s): %w", len(w.jobs), err)
			}
		}
		for _, f := range w.finishers {
			if err := f(ctx, w.tx); err != nil {
				return fmt.Errorf("ledger: finish: %w", err)
			}
		}
		return nil
	})
}

// asLoginRole runs fn as the transaction's login role, then switches back
// to memax_v2. If fn fails, the transaction is aborted anyway.
func asLoginRole(ctx context.Context, tx pgx.Tx, loginRole string, fn func() error) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, loginRole); err != nil {
		return fmt.Errorf("ledger: switch to %s for River: %w", loginRole, err)
	}
	if err := fn(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, DBRole); err != nil {
		return fmt.Errorf("ledger: switch back to %s: %w", DBRole, err)
	}
	return nil
}

// CompileSweeperRole is the role the compile sweeper's cross-space read
// runs as (migration 040): it alone may execute v2.dirty_targets, and the
// policy that admits other spaces' dirty targets applies to it only, so
// memax_v2 can't borrow it by setting app.sweep.
const CompileSweeperRole = "memax_v2_compile_sweeper"

// DirtyTargets lists targets whose latest compile is behind (dirty_gen >
// compiled_gen), across every space, oldest change first: the compile
// sweeper's read (§5.7 step 4). It returns ids only, through
// v2.dirty_targets, as CompileSweeperRole.
func (l *Ledger) DirtyTargets(ctx context.Context, limit int) ([]TargetRef, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	tx, _, err := l.beginRole(ctx, CompileSweeperRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT target_id, space_id FROM v2.dirty_targets($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: dirty targets: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TargetRef, error) {
		var t TargetRef
		err := r.Scan(&t.TargetID, &t.SpaceID)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: dirty targets: %w", err)
	}
	return out, tx.Commit(ctx)
}

// SpaceScope is the scope a system actor (Memax compiling, the
// repository) acts in on one space: the space, with no membership. Like
// ResolveUserScope it reads public.hubs as the login role, before any
// ledger transaction.
func (l *Ledger) SpaceScope(ctx context.Context, spaceID uuid.UUID) (Scope, error) {
	if l == nil {
		return Scope{}, ErrDisabled
	}
	var g SpaceGrant
	var kind string
	err := l.pool.QueryRow(ctx, `SELECT id, tenant_id, space_kind FROM public.hubs WHERE id = $1`, spaceID).
		Scan(&g.SpaceID, &g.TenantID, &kind)
	if errNoRows(err) {
		// A retired space: its hub is gone, its receipts and seals stay
		// (migration 042), and the sealer and the verifier still need its
		// tenant.
		var retired *time.Time
		err = l.pool.QueryRow(ctx, `SELECT tenant_id, retired_at FROM v2.space_ledger($1)`, spaceID).Scan(&g.TenantID, &retired)
		if errNoRows(err) || (err == nil && retired == nil) {
			return Scope{}, ErrNotFound
		}
		if err != nil {
			return Scope{}, fmt.Errorf("ledger: space scope: %w", err)
		}
		g.SpaceID = spaceID
		return Scope{Spaces: []SpaceGrant{g}}, nil
	}
	if err != nil {
		return Scope{}, fmt.Errorf("ledger: space scope: %w", err)
	}
	g.Kind = policy.SpaceKind(kind)
	return Scope{Spaces: []SpaceGrant{g}}, nil
}
