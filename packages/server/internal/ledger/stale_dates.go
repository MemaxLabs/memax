package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The date sweep (the stale-detection design §2.3, migration 068). A kept
// memory whose date to check again (stale_after) has passed is flagged
// stale by Memax within the hour, via system, its source the date, the
// way a failing check flags one: an hourly stale_sweep finds the V2
// spaces with a memory due, as StaleSweeperRole, and queues a stale_space
// job for each, which applies FlagDue in that space's own scope. Dream no
// longer flags dates; its editions list what went stale.

// StaleSweeperRole is the role the date sweep's one cross-space read runs
// as: its policies are keyed on app.sweep = 'stale' and apply to it only.
const StaleSweeperRole = "memax_v2_stale_sweeper"

// QueueStale is the date sweep's River queue: short, database-bound jobs,
// one per space with a memory due.
const QueueStale = "stale"

// CommandFlagDue names FlagDue.
const CommandFlagDue CommandName = "flag_due"

// StaleSpaceArgs is the River job that flags one space's memories whose
// date passed. It is unique by args over every state River keeps, so a
// space is flagged once per slot (the hour) however often the sweep runs.
type StaleSpaceArgs struct {
	SpaceID uuid.UUID `json:"space_id"`
	Slot    time.Time `json:"slot"`
}

// Kind implements river.JobArgs.
func (StaleSpaceArgs) Kind() string { return "stale_space" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (StaleSpaceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueStale, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// dueCond is what makes a kept memory (alias m) due: a date that passed,
// not stale yet, and no person's act on it since the date (they looked at
// it after the date: a Keep of a proposal imported with a passed date, a
// change to its scope), the rule Dream's date flags had.
const dueCond = `m.lifecycle = 'kept' AND m.stale_after IS NOT NULL AND m.stale_after <= $1
	   AND NOT ('stale' = ANY (m.flags))
	   AND NOT EXISTS (SELECT 1 FROM v2.receipts r WHERE r.stream_id = m.id AND r.space_id = m.space_id
	                    AND r.actor_kind = 'person' AND r.recorded_at > m.stale_after AND r.id <> m.created_receipt_id)`

// StaleDueSpaces lists the V2 spaces with a kept memory whose date passed
// by now, as StaleSweeperRole: ids only, at most limit.
func (l *Ledger) StaleDueSpaces(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	tx, err := l.openTx(ctx, StaleSweeperRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tx.deferStatement(`SELECT set_config('app.sweep', 'stale', true)`)
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT m.space_id
		  FROM v2.memories m
		  JOIN public.hubs h ON h.id = m.space_id AND h.v2_enabled_at IS NOT NULL
		 WHERE `+dueCond+`
		 LIMIT $2`, now, max(limit, 1))
	if err != nil {
		return nil, fmt.Errorf("ledger: stale sweep: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("ledger: stale sweep: %w", err)
	}
	return ids, tx.Commit(ctx)
}

// FlagDue marks stale, as Memax, a space's kept memories whose date to
// check again has passed: a `flagged` receipt each, its source the date
// ({kind: date, ref: 2026-10-01}) and Memax's words on it, and every
// target recompiling once. At most MaxStaleFlags a command, the earliest
// dates first: when it flags that many, apply it again. What it finds is
// read again under the lock, so a memory a person verified or edited
// meanwhile (its date moved or went) is left as it is.
type FlagDue struct {
	Meta
	SpaceID uuid.UUID
}

// Name implements Command.
func (*FlagDue) Name() CommandName { return CommandFlagDue }

func (c *FlagDue) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space")
	}
	return nil
}

// flagDue applies FlagDue.
func (w *writer) flagDue(ctx context.Context, c *FlagDue) (Result, error) {
	sp, replay, err := w.spaceAndClaim(ctx, c.SpaceID)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	grant, _ := w.meta.Scope.Grant(sp.ID)
	dec := policy.Decide(w.policyActor(grant), policy.ActionFlagStale, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	rows, err := w.tx.Query(ctx, `
		SELECT m.id FROM v2.memories m
		 WHERE m.space_id = $3 AND `+dueCond+`
		 ORDER BY m.stale_after, m.id LIMIT $2`, w.now, MaxStaleFlags, sp.ID)
	if err != nil {
		return Result{}, fmt.Errorf("ledger: flag due: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return Result{}, fmt.Errorf("ledger: flag due: %w", err)
	}
	if len(ids) == 0 {
		return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
	}
	locked, err := w.lockMemories(ctx, ids)
	if err != nil {
		return Result{}, err
	}
	// Under the lock, the same question again: a command that committed
	// between the read and the lock (a Verify, an edit, a Forget) answered it.
	rows, err = w.tx.Query(ctx, `
		SELECT m.id FROM v2.memories m
		 WHERE m.id = ANY ($2) AND m.space_id = $3 AND `+dueCond, w.now, ids, sp.ID)
	if err != nil {
		return Result{}, fmt.Errorf("ledger: flag due: %w", err)
	}
	still, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return Result{}, fmt.Errorf("ledger: flag due: %w", err)
	}
	due := make(map[uuid.UUID]bool, len(still))
	for _, id := range still {
		due[id] = true
	}
	var flags []pendingFlag
	for _, id := range ids {
		m := locked[id]
		if !due[id] || m == nil || m.SpaceID != sp.ID || m.Lifecycle != lifecycle.Kept ||
			m.Flags.Has(lifecycle.Stale) || m.StaleAfter == nil || m.StaleAfter.After(w.now) {
			continue
		}
		date := m.StaleAfter.UTC()
		flags = append(flags, pendingFlag{mem: m,
			reason: fmt.Sprintf("Its date to check again, %s, has passed. Verify it.", date.Format("Jan 2, 2006")),
			source: &ReceiptSource{Kind: StaleSourceDate, Ref: date.Format(time.DateOnly)}})
	}
	if len(flags) == 0 {
		return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
	}
	b := &pgx.Batch{}
	receipts := w.queueFlags(b, sp, flags, nil, w.meta.Via, ReceiptSource{})
	if err := w.record(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, uuid.Nil); err != nil {
		return Result{}, err
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return Result{}, fmt.Errorf("ledger: flag due: %w", err)
	}
	return Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, nil
}
