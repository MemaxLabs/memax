package v2index_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const model = "voyage-4"

type fixture struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	l     *ledger.Ledger
	owner uuid.UUID
	space uuid.UUID
	scope ledger.Scope
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: context.Background(), pool: pool,
		l: ledger.New(pool, ledger.WithLogger(quiet), ledger.WithJobs(client), ledger.WithIndexJobs())}
	f.owner = uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, f.owner, f.owner.String()[:8]+"@zz.test")
	f.space = f.newSpace("memax-v2")
	f.scope = f.scopeOf()
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) newSpace(name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
		id, name, id.String(), f.owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, f.owner)
	return id
}

func (f *fixture) scopeOf() ledger.Scope {
	s, err := f.l.UserScope(f.ctx, f.owner)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) propose(space uuid.UUID, statement string) *ledger.Memory {
	f.t.Helper()
	res, err := f.l.Apply(f.ctx, &ledger.Propose{
		Meta:      ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: policy.AutonomyPropose}, Scope: f.scopeOf(), Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: space, Statement: statement, Section: ledger.SectionConventions},
	})
	if err != nil || res.Memory == nil {
		f.t.Fatalf("propose: %v %+v", err, res.Policy)
	}
	return res.Memory
}

func (f *fixture) embedded() int {
	var n int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM v2.memory_embeddings`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// One job embeds its space's burst in one request; the burst's other jobs
// find their versions done and call nothing.
func TestIndexBatchesASpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var ms []*ledger.Memory
	for i := range 5 {
		ms = append(ms, f.propose(f.space, fmt.Sprintf("Statement number %d about deploys.", i)))
	}
	other := f.propose(f.newSpace("other"), "In another space.")
	e := mockembed.New()
	ix := v2index.New(f.l, e, model, 64, quiet)
	stored, err := ix.Index(f.ctx, ledger.IndexArgs{MemoryID: ms[2].ID, SpaceID: f.space, Version: 1})
	if err != nil || stored != 5 {
		t.Fatalf("index: stored %d, %v", stored, err)
	}
	if e.RequestCount() != 1 || e.CallCount() != 5 {
		t.Errorf("embedder: %d requests, %d texts; want one request of 5", e.RequestCount(), e.CallCount())
	}
	for _, m := range ms[1:] {
		if n, err := ix.Index(f.ctx, ledger.IndexArgs{MemoryID: m.ID, SpaceID: f.space, Version: 1}); err != nil || n != 0 {
			t.Errorf("index %s again: %d %v", m.Ref, n, err)
		}
	}
	if e.RequestCount() != 1 {
		t.Errorf("%d requests after the burst's other jobs, want 1", e.RequestCount())
	}
	if f.embedded() != 5 {
		t.Errorf("%d embeddings, want 5 (the other space waits for its own job)", f.embedded())
	}
	if n, _ := ix.Index(f.ctx, ledger.IndexArgs{MemoryID: other.ID, SpaceID: other.SpaceID, Version: 1}); n != 1 {
		t.Errorf("the other space's job stored %d", n)
	}
	// A small batch splits the work.
	small := v2index.New(f.l, mockembed.New(), "voyage-4-large", 2, quiet)
	if n, _ := small.Index(f.ctx, ledger.IndexArgs{MemoryID: ms[0].ID, SpaceID: f.space, Version: 1}); n != 2 {
		t.Errorf("batch 2 stored %d", n)
	}
}

// rateLimited answers 429 the way the Voyage embedder reports it.
type rateLimited struct{ mockembed.Embedder }

func (*rateLimited) EmbedContext(context.Context, []string, string) ([][]float64, error) {
	return nil, &embed.RateLimitError{RetryAfter: 45 * time.Second, Err: errors.New("voyage API error 429")}
}

// wrongWidth answers with vectors V2 can't store.
type wrongWidth struct{ mockembed.Embedder }

func (*wrongWidth) EmbedContext(_ context.Context, texts []string, _ string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range out {
		out[i] = make([]float64, 512)
	}
	return out, nil
}

// The worker: a rate limit snoozes for Voyage's Retry-After (no attempt
// spent), the wrong width is an error that names the setting, and with
// embeddings off the job cancels with the reason.
func TestWorkerOutcomes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	m := f.propose(f.space, "Background jobs run on River.")
	args := ledger.IndexArgs{MemoryID: m.ID, SpaceID: f.space, Version: 1}
	work := func(ix *v2index.Indexer) error {
		return (&v2index.Worker{Indexer: ix}).Work(f.ctx, &river.Job[ledger.IndexArgs]{Args: args})
	}
	var snooze *river.JobSnoozeError
	if err := work(v2index.New(f.l, &rateLimited{}, model, 64, quiet)); !errors.As(err, &snooze) || snooze.Duration < 45*time.Second {
		t.Errorf("rate limited: %v, want a snooze of at least 45 s", err)
	}
	if err := work(v2index.New(f.l, &wrongWidth{}, model, 64, quiet)); err == nil || !strings.Contains(err.Error(), "V2_EMBED_MODEL") {
		t.Errorf("wrong width: %v", err)
	}
	var cancel *river.JobCancelError
	if err := work(nil); !errors.As(err, &cancel) {
		t.Errorf("embeddings off: %v, want a cancel", err)
	}
	if err := work(v2index.New(f.l, mockembed.New(), model, 64, quiet)); err != nil || f.embedded() != 1 {
		t.Errorf("working embedder: %v, %d stored", err, f.embedded())
	}
}

// The sweep queues an index job for every version without an embedding,
// across spaces, and nothing once they are all embedded. With embeddings
// off it does nothing.
func TestSweep(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	a := f.propose(f.space, "One.")
	f.propose(f.newSpace("other"), "Two.")
	// Clear the jobs the commands queued, as if they had been discarded.
	f.exec(`DELETE FROM river_job WHERE kind = 'index_memory'`)
	ix := v2index.New(f.l, mockembed.New(), model, 64, quiet)
	if _, err := ix.Index(f.ctx, ledger.IndexArgs{MemoryID: a.ID, SpaceID: f.space, Version: 1}); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	sweep := func(m string) int {
		w := &v2index.SweepWorker{Ledger: f.l, Model: m, Jobs: client}
		if err := w.Work(f.ctx, &river.Job[v2index.SweepArgs]{}); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		var n int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM river_job WHERE kind = 'index_memory' AND state = 'available'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := sweep(""); n != 0 {
		t.Errorf("sweep with embeddings off queued %d", n)
	}
	if n := sweep(model); n != 1 {
		t.Errorf("sweep queued %d, want 1 (the other space's)", n)
	}
	if n := sweep(model); n != 1 {
		t.Errorf("a second sweep queued a duplicate (%d)", n)
	}
	// A new model needs both versions: one new job, and the one waiting.
	if n := sweep("voyage-5"); n != 2 {
		t.Errorf("a new model: %d queued, want 2", n)
	}
}

// The configuration: the plan's models by default, "off" disables, and
// nothing works without a key.
func TestConfigFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv ...string) func(string) (string, bool) {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	c := v2index.ConfigFromEnv(env("VOYAGE_API_KEY", "k"))
	if !c.Enabled() || c.IndexModel != "voyage-4" || c.QueryModel != "voyage-4-lite" || c.Batch != 64 || c.SweepInterval != 5*time.Minute {
		t.Errorf("defaults = %+v", c)
	}
	if c.IndexEmbedder() == nil || c.QueryEmbedder() == nil {
		t.Error("embedders missing with a key")
	}
	if c := v2index.ConfigFromEnv(env()); c.Enabled() || c.IndexEmbedder() != nil || len(v2index.PeriodicJobs(c)) != 0 {
		t.Errorf("no key: %+v", c)
	}
	if c := v2index.ConfigFromEnv(env("VOYAGE_API_KEY", "k", "V2_EMBED_MODEL", "off")); c.Enabled() {
		t.Error("V2_EMBED_MODEL=off is enabled")
	}
	c = v2index.ConfigFromEnv(env("VOYAGE_API_KEY", "k", "V2_EMBED_MODEL", "voyage-4-large", "V2_EMBED_QUERY_MODEL", "voyage-4",
		"V2_EMBED_BATCH", "16", "V2_INDEX_SWEEP_SECONDS", "60"))
	if c.IndexModel != "voyage-4-large" || c.QueryModel != "voyage-4" || c.Batch != 16 || c.SweepInterval != time.Minute {
		t.Errorf("explicit = %+v", c)
	}
	if v2index.New(nil, mockembed.New(), model, 0, nil) != nil || v2index.New(&ledger.Ledger{}, nil, model, 0, nil) != nil {
		t.Error("New without a ledger or an embedder must be nil")
	}
}


