package ledger_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// fixture is one isolated, migrated database with a ledger on it.
type fixture struct {
	t    *testing.T
	pool *pgxpool.Pool
	l    *ledger.Ledger
	logs *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	logs := &syncBuffer{}
	return &fixture{
		t: t, pool: pool, logs: logs,
		l: ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.MultiWriter(logs), nil)))),
	}
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec %q: %v", firstLine(sql), err)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

func (f *fixture) user(name string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

// space creates a hub of the given V2 kind owned by owner, with the
// owner's membership row, as V1 does.
func (f *fixture) space(owner uuid.UUID, kind policy.SpaceKind, name string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	hubType := "team"
	if kind == policy.SpacePersonal {
		hubType = "personal"
	}
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, name, id.String(), hubType, owner, string(kind))
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (f *fixture) join(space, user uuid.UUID, v1Role string) {
	f.t.Helper()
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3)`, space, user, v1Role)
}

func (f *fixture) setRules(space uuid.UUID, rules string) {
	f.t.Helper()
	f.exec(`UPDATE hubs SET rules = $2::jsonb WHERE id = $1`, space, rules)
}

func (f *fixture) scope(user uuid.UUID) ledger.Scope {
	f.t.Helper()
	s, err := f.l.UserScope(context.Background(), user)
	if err != nil {
		f.t.Fatalf("UserScope: %v", err)
	}
	return s
}

func person(id uuid.UUID) ledger.Actor {
	return ledger.Actor{Kind: policy.ActorPerson, ID: id, Name: "Ziyang"}
}

// agentFor is an agent connection working for a person.
func agentFor(level policy.Autonomy) ledger.Actor {
	return ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: level}
}

func meta(actor ledger.Actor, scope ledger.Scope, via policy.Via) ledger.Meta {
	return ledger.Meta{Actor: actor, Scope: scope, Via: via, IdempotencyKey: uuid.NewString()}
}

func fact(space uuid.UUID, statement string) ledger.NewMemory {
	return ledger.NewMemory{SpaceID: space, Statement: statement, Section: ledger.SectionConventions}
}

func (f *fixture) apply(cmd ledger.Command) ledger.Result {
	f.t.Helper()
	res, err := f.l.Apply(context.Background(), cmd)
	if err != nil {
		f.t.Fatalf("Apply %s: %v", cmd.Name(), err)
	}
	return res
}

// remember writes a kept memory as the space owner (via the web).
func (f *fixture) remember(owner, space uuid.UUID, statement string) *ledger.Memory {
	f.t.Helper()
	res := f.apply(&ledger.Remember{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb), NewMemory: fact(space, statement)})
	if res.Outcome != ledger.OutcomeApplied {
		f.t.Fatalf("remember: outcome %s (%s)", res.Outcome, res.Policy.Message)
	}
	return res.Memory
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count %q: %v", firstLine(sql), err)
	}
	return n
}

// asV2 runs fn in a transaction switched to memax_v2 with the given
// spaces and tenants in scope, the way the ledger does, and returns the
// commit error. Raw SQL inside fn is how the tests prove the database
// (not the Go code) enforces the rules.
func (f *fixture) asV2(spaces, tenants []uuid.UUID, fn func(tx pgx.Tx) error) error {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE memax_v2`); err != nil {
		f.t.Fatalf("set role: %v", err)
	}
	if spaces != nil {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.space_ids', $1, true), set_config('app.tenant_ids', $2, true)`,
			arrayLiteral(spaces), arrayLiteral(tenants)); err != nil {
			f.t.Fatalf("set scope: %v", err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func arrayLiteral(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}
