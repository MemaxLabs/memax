package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBRole is the Postgres role every V2 transaction runs as. It has no
// BYPASSRLS and only the grants the ledger needs (migration 026).
const DBRole = "memax_v2"

// DefaultLockTimeout bounds how long a command waits for a memory
// another transaction holds.
const DefaultLockTimeout = 5 * time.Second

// Ledger applies commands to the V2 record and reads it back, always
// inside the caller's scope.
type Ledger struct {
	pool        *pgxpool.Pool
	now         func() time.Time
	lockTimeout time.Duration
	log         *slog.Logger
	// inserter enqueues follow-up jobs in the command's transaction
	// (WithJobs); nil enqueues nothing.
	inserter Jobs
	// indexJobs enqueues index_memory with every new version
	// (WithIndexJobs, embeddings.go).
	indexJobs bool
	// The undo windows (WithUndoWindows).
	undoWindow      time.Duration
	judgeUndoWindow time.Duration
	// returnWindow bounds the judge's return of a Write agent's write to
	// Review (WithReturnWindow, judge.go).
	returnWindow time.Duration
	// readMonths are the months whose reads partitions this process has
	// ensured (reads.go).
	readMonths sync.Map
	// loginRole is the role the pool's connections log in as, learned from
	// the first transaction's opening (tx.go): River's insert switches
	// back to it.
	loginRole atomic.Pointer[string]
	// honesty is what tombstones say about copies Memax can't reach
	// (WithForgetHonesty).
	honesty ForgetHonesty
}

// Option configures a Ledger.
type Option func(*Ledger)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(l *Ledger) { l.now = now } }

// WithLockTimeout replaces DefaultLockTimeout.
func WithLockTimeout(d time.Duration) Option { return func(l *Ledger) { l.lockTimeout = d } }

// WithLogger replaces slog.Default().
func WithLogger(log *slog.Logger) Option { return func(l *Ledger) { l.log = log } }

// New returns a Ledger on pool, or nil when pool is nil (nil means
// disabled; every method on a nil *Ledger returns ErrDisabled).
func New(pool *pgxpool.Pool, opts ...Option) *Ledger {
	if pool == nil {
		return nil
	}
	l := &Ledger{pool: pool, now: time.Now, lockTimeout: DefaultLockTimeout, log: slog.Default(),
		undoWindow: DefaultUndoWindow, judgeUndoWindow: DefaultJudgeUndoWindow, returnWindow: DefaultReturnWindow,
		honesty: ForgetHonesty{BackupDays: DefaultBackupDays}}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Apply runs one command in one transaction and returns what happened.
// A policy refusal is a Result with OutcomeRefused, not an error; errors
// are for malformed commands (ErrInvalid), missing memories
// (ErrNotFound), edit clashes (ErrEditClash), disallowed transitions
// (ErrInvalidTransition), reused idempotency keys and database failures.
func (l *Ledger) Apply(ctx context.Context, cmd Command) (Result, error) {
	if l == nil {
		return Result{}, ErrDisabled
	}
	if cmd == nil {
		return Result{}, invalid("command", "is missing")
	}
	m := cmd.envelope()
	now := l.now()
	if err := validateMeta(m, now); err != nil {
		return Result{}, err
	}
	if err := validateCommand(cmd); err != nil {
		return Result{}, err
	}
	hash, err := requestHash(cmd)
	if err != nil {
		return Result{}, err
	}

	tx, loginRole, err := l.begin(ctx, m.Scope, pgx.ReadWrite)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	w := &writer{tx: tx, meta: m, command: cmd.Name(), hash: hash, inserter: l.inserter, loginRole: loginRole,
		undoWindow: l.undoWindow, judgeUndoWindow: l.judgeUndoWindow, returnWindow: l.returnWindow, now: now,
		indexJobs: l.indexJobs, forgetHonesty: l.honesty}
	var res Result
	switch c := cmd.(type) {
	case *Remember:
		res, err = w.write(ctx, c.NewMemory, false)
	case *Propose:
		res, err = w.write(ctx, c.NewMemory, true)
	case *Keep:
		res, err = w.review(ctx, c.Memory, c.ExpectedVersion, CommandKeep)
	case *Reject:
		res, err = w.review(ctx, c.Memory, c.ExpectedVersion, CommandReject)
	case *Edit:
		res, err = w.edit(ctx, c)
	case *ConnectAgent:
		res, err = w.connect(ctx, c)
	case *SetAutonomy:
		res, err = w.setAutonomy(ctx, c)
	case *PauseAgent:
		res, err = w.changeConnection(ctx, c.Connection, CommandPauseAgent)
	case *ResumeAgent:
		res, err = w.changeConnection(ctx, c.Connection, CommandResumeAgent)
	case *DisconnectAgent:
		res, err = w.changeConnection(ctx, c.Connection, CommandDisconnectAgent)
	case *ReviseBrief:
		res, err = w.reviseBrief(ctx, c)
	case *ConfigureTarget:
		res, err = w.configureTarget(ctx, c)
	case *RequestCompile:
		res, err = w.requestCompile(ctx, c)
	case *RecordCompile:
		res, err = w.recordCompile(ctx, c)
	case *RecordDelivery:
		res, err = w.recordDelivery(ctx, c)
	case *RecordObservation:
		res, err = w.recordObservation(ctx, c)
	case *ResolveDrift:
		res, err = w.resolveDrift(ctx, c)
	case *RecordVerdict:
		res, err = w.recordVerdict(ctx, c)
	case *ResolveConflict:
		res, err = w.resolveConflict(ctx, c)
	case *Undo:
		res, err = w.undoCommand(ctx, c)
	case *RequestDecision:
		res, err = w.requestDecision(ctx, c)
	case *AnswerGate:
		res, err = w.answerGate(ctx, c)
	case *WithdrawGate:
		res, err = w.withdrawGate(ctx, c)
	case *Forget:
		res, err = w.forget(ctx, c)
	case *ForgetSpace:
		res, err = w.forgetSpace(ctx, c)
	case *RequestForget:
		res, err = w.requestForget(ctx, c)
	case *DeclineForget:
		res, err = w.declineForget(ctx, c)
	case *ReapplyForget:
		res, err = w.reapplyForget(ctx, c)
	case *ImportStatement:
		res, err = w.importStatement(ctx, c)
	case *RecordImportCheck:
		res, err = w.recordImportCheck(ctx, c)
	case *SettleImportConflict:
		res, err = w.settleImportConflict(ctx, c)
	case *Export:
		res, err = w.export(ctx, c)
	case *ForgetNote:
		res, err = w.forgetNote(ctx, c)
	}
	if err != nil {
		return Result{}, mapDBError(err)
	}
	// Refusals, replays and no-ops wrote nothing worth keeping: the
	// deferred rollback discards the idempotency claim, so a refused
	// command is decided afresh when retried.
	if res.Outcome == OutcomeRefused || res.Replayed || res.Unchanged {
		return res, nil
	}
	// The follow-up jobs go out last, right before COMMIT (jobs.go).
	if err := w.flush(ctx); err != nil {
		return Result{}, mapDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, mapDBError(fmt.Errorf("ledger: commit %s: %w", cmd.Name(), err))
	}
	switch {
	case res.Memory != nil:
		l.log.Info("ledger: applied",
			"command", string(cmd.Name()), "outcome", string(res.Outcome), "policy", res.Policy.Code,
			"memory", res.Memory.Ref, "space_id", res.Memory.SpaceID.String(),
			"actor_kind", string(m.Actor.Kind), "via", string(m.Via), "receipts", len(res.Receipts),
			"jobs", len(w.jobs))
	case len(res.Receipts) > 0:
		rc := res.Receipts[0]
		l.log.Info("ledger: applied",
			"command", string(cmd.Name()), "outcome", string(res.Outcome), "policy", res.Policy.Code,
			"object", rc.ObjectRef, "object_kind", rc.ObjectKind, "space_id", rc.SpaceID.String(),
			"actor_kind", string(m.Actor.Kind), "via", string(m.Via), "receipts", len(res.Receipts),
			"jobs", len(w.jobs))
	}
	if res.Connection != nil {
		l.log.Info("ledger: applied",
			"command", string(cmd.Name()), "outcome", string(res.Outcome), "connection", res.Connection.ID.String(),
			"agent", string(res.Connection.Agent), "state", string(res.Connection.State),
			"actor_kind", string(m.Actor.Kind), "via", string(m.Via), "receipts", len(res.Receipts))
	}
	return res, nil
}

func validateCommand(cmd Command) error {
	switch c := cmd.(type) {
	case *ReviseBrief:
		return c.validate()
	case *ConfigureTarget:
		return c.validate()
	case *RequestCompile:
		return c.validate()
	case *RecordCompile:
		return c.validate()
	case *RecordDelivery:
		return c.validate()
	case *RecordObservation:
		return c.validate()
	case *ResolveDrift:
		return c.validate()
	case *RecordVerdict:
		return c.validate()
	case *ResolveConflict:
		return c.validate()
	case *Undo:
		if c.Receipt == uuid.Nil {
			return invalid("receipt", "say which receipt to undo")
		}
		return nil
	case *RequestDecision:
		return c.validate()
	case *AnswerGate:
		return validateGateTarget(c.Gate, c.ExpectedVersion)
	case *WithdrawGate:
		return validateGateTarget(c.Gate, c.ExpectedVersion)
	case *Forget:
		return c.validate()
	case *ForgetSpace:
		if c.SpaceID == uuid.Nil {
			return invalid("space", "say which space to forget")
		}
		return nil
	case *RequestForget:
		return validateTarget(c.Memory, 0, false)
	case *DeclineForget:
		return validateTarget(c.Memory, 0, false)
	case *ReapplyForget:
		return c.Op.validate()
	case *ImportStatement:
		return c.validate()
	case *RecordImportCheck:
		return c.validate()
	case *SettleImportConflict:
		return c.validate()
	case *Export:
		if c.SpaceID == uuid.Nil {
			return invalid("space", "say which space to export")
		}
		return nil
	case *ForgetNote:
		return c.validate()
	case *Remember:
		return c.NewMemory.validate()
	case *Propose:
		return c.NewMemory.validate()
	case *Keep:
		return validateTarget(c.Memory, c.ExpectedVersion, false)
	case *Reject:
		return validateTarget(c.Memory, c.ExpectedVersion, false)
	case *Edit:
		if err := validateTarget(c.Memory, c.ExpectedVersion, true); err != nil {
			return err
		}
		if c.Section != "" && !c.Section.Valid() {
			return invalid("section", "use decisions, conventions, preferences or open_question")
		}
		c.Statement = strings.TrimSpace(c.Statement)
		return checkText("statement", c.Statement, MaxStatementRunes, true)
	case *ConnectAgent:
		return c.validate()
	case *SetAutonomy:
		if err := validateConnectionTarget(c.Connection); err != nil {
			return err
		}
		if c.SpaceID == uuid.Nil {
			return invalid("space", "say which space")
		}
		if !c.Autonomy.Valid() {
			return invalid("autonomy", "use read, propose or write")
		}
		return nil
	case *PauseAgent:
		return validateConnectionTarget(c.Connection)
	case *ResumeAgent:
		return validateConnectionTarget(c.Connection)
	case *DisconnectAgent:
		return validateConnectionTarget(c.Connection)
	}
	return invalid("command", "unknown command %T", cmd)
}

// Read runs fn in a read-only transaction as memax_v2, scoped to the
// given spaces. Use it for any read of v2 tables outside this package
// (retrieval, export), so row-level security applies to it too.
//
// The transaction's BEGIN and scope go out with fn's first statement, and
// its COMMIT after Read returns (tx.go): a read costs one round trip per
// statement that waits on another's result. Send independent statements
// together (tx.SendBatch, or ReadBatch).
func (l *Ledger) Read(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	if l == nil {
		return ErrDisabled
	}
	return l.read(ctx, scope, pgx.ReadCommitted, fn)
}

// readSnapshot is Read at REPEATABLE READ: every query in fn sees the same
// snapshot of the record (a compile input built from one moment).
func (l *Ledger) readSnapshot(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	if l == nil {
		return ErrDisabled
	}
	return l.read(ctx, scope, pgx.RepeatableRead, fn)
}

func (l *Ledger) read(ctx context.Context, scope Scope, iso pgx.TxIsoLevel, fn func(pgx.Tx) error) error {
	tx, _, err := l.beginTx(ctx, scope, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: iso})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return mapDBError(err)
	}
	return tx.Commit(ctx)
}

// ReadBatch runs b's statements in one read-only transaction as memax_v2,
// scoped like Read, in one round trip: BEGIN, the scope, b's statements
// and COMMIT go out together. Their results come through b's callbacks
// (QueuedQuery.Query, QueryRow and Exec), so no statement in b may need
// another's result.
func (l *Ledger) ReadBatch(ctx context.Context, scope Scope, b *pgx.Batch) error {
	if l == nil {
		return ErrDisabled
	}
	if b.Len() == 0 {
		return nil
	}
	t, err := l.openTx(ctx, DBRole, scope, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	return mapDBError(t.readBatch(ctx, b))
}

// begin opens a transaction, switches it to memax_v2 and sets the scope.
// set_config(..., true) is SET LOCAL: it ends with the transaction, so
// pooled connections (including Neon's transaction pooling) never carry
// a scope into the next transaction. It also returns the role the
// transaction began with, which the River insert switches back to for
// its one statement (jobs.go).
//
// Nothing is sent yet: the BEGIN and the scope go to the server with the
// transaction's first statement, in the same round trip (tx.go).
func (l *Ledger) begin(ctx context.Context, scope Scope, mode pgx.TxAccessMode) (pgx.Tx, string, error) {
	return l.beginTx(ctx, scope, pgx.TxOptions{AccessMode: mode})
}

func (l *Ledger) beginTx(ctx context.Context, scope Scope, opts pgx.TxOptions) (pgx.Tx, string, error) {
	tx, err := l.openTx(ctx, DBRole, scope, opts)
	if err != nil {
		return nil, "", err
	}
	if known := l.loginRole.Load(); known != nil {
		// Every transaction checks, when its opening is answered, that its
		// connection began as this role too.
		return tx, *known, nil
	}
	// The process's first transaction opens at once, to learn the role
	// the pool's connections log in as.
	if err := tx.flush(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, "", err
	}
	return tx, tx.loginRole, nil
}

// errNoRows reports whether err is pgx's "no rows".
func errNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
