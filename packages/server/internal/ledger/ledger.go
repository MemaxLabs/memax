package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	l := &Ledger{pool: pool, now: time.Now, lockTimeout: DefaultLockTimeout, log: slog.Default()}
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
	if err := validateMeta(m, l.now()); err != nil {
		return Result{}, err
	}
	if err := validateCommand(cmd); err != nil {
		return Result{}, err
	}
	hash, err := requestHash(cmd)
	if err != nil {
		return Result{}, err
	}

	tx, err := l.begin(ctx, m.Scope, pgx.ReadWrite)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	w := &writer{tx: tx, meta: m, command: cmd.Name(), hash: hash}
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
	}
	if err != nil {
		return Result{}, mapDBError(err)
	}
	// Refusals and replays wrote nothing worth keeping: the deferred
	// rollback discards the idempotency claim, so a refused command is
	// decided afresh when retried.
	if res.Outcome == OutcomeRefused || res.Replayed {
		return res, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, mapDBError(fmt.Errorf("ledger: commit %s: %w", cmd.Name(), err))
	}
	if res.Memory != nil {
		l.log.Info("ledger: applied",
			"command", string(cmd.Name()), "outcome", string(res.Outcome), "policy", res.Policy.Code,
			"memory", res.Memory.Ref, "space_id", res.Memory.SpaceID.String(),
			"actor_kind", string(m.Actor.Kind), "via", string(m.Via), "receipts", len(res.Receipts))
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
func (l *Ledger) Read(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	if l == nil {
		return ErrDisabled
	}
	tx, err := l.begin(ctx, scope, pgx.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return mapDBError(err)
	}
	return tx.Commit(ctx)
}

// begin opens a transaction, switches it to memax_v2 and sets the scope.
// set_config(..., true) is SET LOCAL: it ends with the transaction, so
// pooled connections (including Neon's transaction pooling) never carry
// a scope into the next transaction.
func (l *Ledger) begin(ctx context.Context, scope Scope, mode pgx.TxAccessMode) (pgx.Tx, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return nil, fmt.Errorf("ledger: begin: %w", err)
	}
	person := ""
	if scope.PersonID != uuid.Nil {
		person = scope.PersonID.String()
	}
	_, err = tx.Exec(ctx,
		`SELECT set_config('role', $1, true),
		        set_config('app.space_ids', $2, true),
		        set_config('app.tenant_ids', $3, true),
		        set_config('app.person_id', $4, true),
		        set_config('lock_timeout', $5, true)`,
		DBRole, uuidArray(scope.SpaceIDs()), uuidArray(scope.TenantIDs()), person,
		fmt.Sprintf("%dms", l.lockTimeout.Milliseconds()))
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("ledger: switch to %s: %w", DBRole, err)
	}
	return tx, nil
}

// errNoRows reports whether err is pgx's "no rows".
func errNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
