package compile_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fixture is one database with a ledger (enqueuing through River), an
// in-memory object store and a compiler (the fake, or the real service).
type fixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	l      *ledger.Ledger
	store  *mockobjectstore.Store
	fake   *compiletest.Fake
	svc    *compile.Service
	client *river.Client[pgx.Tx]
}

type fixtureOpts struct {
	compiler compile.Compiler
	cfg      compile.Config
	// workers runs compile jobs in-process (a River client with the
	// compile queue).
	workers bool
}

func newFixture(t *testing.T, o fixtureOpts) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, pool: pool, store: mockobjectstore.New(), fake: &compiletest.Fake{}}
	var compiler compile.Compiler = f.fake
	if o.compiler != nil {
		compiler = o.compiler
	}
	cfg := o.cfg
	if cfg.Log == nil {
		cfg.Log = quiet
	}
	// The ledger and the coordinator are built before the River client
	// that runs them, as in the worker; the ledger's inserter is set once
	// the client exists.
	jobs := &lateJobs{}
	f.l = ledger.New(pool, ledger.WithLogger(quiet), ledger.WithJobs(jobs))
	f.svc = compile.New(f.l, compiler, f.store, cfg)
	rc := &river.Config{Logger: quiet}
	if o.workers {
		workers := river.NewWorkers()
		compile.AddWorkers(workers, f.l, f.svc)
		// Proposals enqueue judge jobs too; nothing works them here.
		judge.AddWorkers(workers, nil)
		rc.Workers = workers
		rc.Queues = map[string]river.QueueConfig{ledger.QueueCompile: {MaxWorkers: compile.MaxWorkers}}
	}
	client, err := river.NewClient(riverpgxv5.New(pool), rc)
	if err != nil {
		t.Fatal(err)
	}
	jobs.client = client
	f.client = client
	if o.workers {
		ctx, cancel := context.WithCancel(context.Background())
		if err := client.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			stop, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			_ = client.Stop(stop)
			cancel()
		})
	}
	return f
}

// lateJobs lets the ledger be built before the River client.
type lateJobs struct{ client *river.Client[pgx.Tx] }

func (j *lateJobs) InsertManyTx(ctx context.Context, tx pgx.Tx, p []river.InsertManyParams) ([]*rivertype.JobInsertResult, error) {
	return j.client.InsertManyTx(ctx, tx, p)
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) user(name string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

func (f *fixture) space(owner uuid.UUID, slug string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
		id, "Memax V2", slug+"-"+id.String()[:6], owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (f *fixture) scope(user uuid.UUID) ledger.Scope {
	f.t.Helper()
	s, err := f.l.UserScope(context.Background(), user)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) meta(user uuid.UUID, via policy.Via) ledger.Meta {
	return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: f.scope(user), Via: via,
		IdempotencyKey: uuid.NewString()}
}

func (f *fixture) apply(cmd ledger.Command) ledger.Result {
	f.t.Helper()
	res, err := f.l.Apply(context.Background(), cmd)
	if err != nil {
		f.t.Fatalf("Apply %s: %v", cmd.Name(), err)
	}
	if res.Outcome == ledger.OutcomeRefused {
		f.t.Fatalf("Apply %s refused: %s", cmd.Name(), res.Policy.Message)
	}
	return res
}

func (f *fixture) remember(user, space uuid.UUID, statement string, section ledger.Section) *ledger.Memory {
	f.t.Helper()
	return f.apply(&ledger.Remember{Meta: f.meta(user, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: statement, Section: section}}).Memory
}

func (f *fixture) target(user, space uuid.UUID, kind ledger.TargetKind) *ledger.Target {
	f.t.Helper()
	return f.apply(&ledger.ConfigureTarget{Meta: f.meta(user, policy.ViaWeb), SpaceID: space, Kind: kind}).Target
}

func (f *fixture) get(user, id uuid.UUID) *ledger.Target {
	f.t.Helper()
	t, err := f.l.GetTarget(context.Background(), f.scope(user), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return t
}

// run compiles a target now (no quiet window), as the job would.
func (f *fixture) run(t *ledger.Target) compile.Outcome {
	f.t.Helper()
	out, err := f.svc.Run(context.Background(), ledger.CompileTargetArgs{TargetID: t.ID, SpaceID: t.SpaceID}, compile.RunOptions{NoWait: true})
	if err != nil {
		f.t.Fatalf("Run %s: %v", t.Kind, err)
	}
	return out
}

func (f *fixture) runs(t *ledger.Target) []ledger.CompileRun {
	f.t.Helper()
	page, err := f.l.ListCompileRuns(context.Background(), mustScope(f, t.SpaceID), ledger.CompileRunQuery{TargetID: t.ID, Limit: 200})
	if err != nil {
		f.t.Fatal(err)
	}
	return page.Runs
}

func mustScope(f *fixture, space uuid.UUID) ledger.Scope {
	f.t.Helper()
	s, err := f.l.SpaceScope(context.Background(), space)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}
