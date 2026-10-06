package v2recall_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const model = "voyage-4"

// synonyms give the Words embedder the meaning the lexical lanes can't
// see: "queue" is River here, a "task" is a job, "async" is background.
var synonyms = [][]string{{"river", "queue"}, {"job", "task", "worker"}, {"background", "async"},
	{"postgres", "database", "pg"}, {"deploy", "ship", "release"}}

type fixture struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	l     *ledger.Ledger
	user  uuid.UUID
	space uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, ctx: context.Background(), pool: pool, l: ledger.New(pool, ledger.WithLogger(quiet))}
	f.user = f.newUser("zz")
	f.space = f.newSpace(f.user, "memax-v2")
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) newUser(name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

func (f *fixture) newSpace(owner uuid.UUID, name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
		id, name, id.String(), owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (f *fixture) scope(user uuid.UUID) ledger.Scope {
	f.t.Helper()
	s, err := f.l.UserScope(f.ctx, user)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) keep(user, space uuid.UUID, statement string) *ledger.Memory {
	f.t.Helper()
	res, err := f.l.Apply(f.ctx, &ledger.Remember{
		Meta:      ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user, Name: "zz"}, Scope: f.scope(user), Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: space, Statement: statement, Section: ledger.SectionConventions},
	})
	if err != nil || res.Memory == nil {
		f.t.Fatalf("remember: %v %+v", err, res.Policy)
	}
	return res.Memory
}

// index embeds every waiting version of the space, as the index jobs do.
func (f *fixture) index(e embed.Embedder, space uuid.UUID) {
	f.t.Helper()
	ix := v2index.New(f.l, e, model, 64, quiet)
	for {
		n, err := ix.Index(f.ctx, ledger.IndexArgs{SpaceID: space})
		if err != nil {
			f.t.Fatalf("index: %v", err)
		}
		if n == 0 {
			return
		}
	}
}

func (f *fixture) search(s *v2recall.Searcher, user uuid.UUID, text string, spaces ...uuid.UUID) v2recall.Result {
	f.t.Helper()
	if len(spaces) == 0 {
		spaces = []uuid.UUID{f.space}
	}
	res, err := s.Search(f.ctx, f.scope(user), v2recall.Query{Text: text, Filter: v2recall.Filter{Spaces: spaces}, Limit: 5})
	if err != nil {
		f.t.Fatalf("search %q: %v", text, err)
	}
	return res
}

func refs(res v2recall.Result) []string {
	out := make([]string, len(res.Hits))
	for i, h := range res.Hits {
		out[i] = h.Ref
	}
	return out
}

func vectorsFor(f *fixture, e embed.Embedder) *v2recall.Vectors {
	return v2recall.NewVectors(f.l, e, e, v2recall.VectorConfig{Model: model, Log: quiet})
}

// The vector lane finds by meaning what the lexical lanes can't: no word
// of the query is in the statement. Without vectors (nil embedder) the
// same query finds nothing and says it is lexical; a query both find
// keeps working either way.
func TestHybridFindsByMeaning(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	e := mockembed.NewWords(synonyms...)
	river := f.keep(f.user, f.space, "Background jobs run on River.")
	f.keep(f.user, f.space, "The web app is a Next.js 16 project.")
	f.keep(f.user, f.space, "Reviews happen in the morning.")
	f.index(e, f.space)

	lexical := v2recall.New(f.l)
	hybrid := v2recall.New(f.l).WithVectors(vectorsFor(f, e))
	query := "Which queue handles async tasks?"
	if res := f.search(lexical, f.user, query); len(res.Hits) != 0 || !res.LexicalOnly || res.Retrieval.Vector != v2recall.StageOff {
		t.Fatalf("lexical: %v lexical_only=%v vector=%s", refs(res), res.LexicalOnly, res.Retrieval.Vector)
	}
	res := f.search(hybrid, f.user, query)
	if len(res.Hits) != 1 || res.Hits[0].ID != river.ID || res.LexicalOnly || res.Retrieval.Vector != v2recall.StageOK {
		t.Fatalf("hybrid: %v lexical_only=%v vector=%s", refs(res), res.LexicalOnly, res.Retrieval.Vector)
	}
	// A query the words find: both agree, and the vector lane adds no noise
	// below its floor.
	for _, s := range []*v2recall.Searcher{lexical, hybrid} {
		if res := f.search(s, f.user, "Next.js web app"); len(res.Hits) != 1 || res.Hits[0].Statement != "The web app is a Next.js 16 project." {
			t.Errorf("both find the web app: %v", refs(res))
		}
	}
	// Nonsense matches nothing: the floor keeps the nearest memories out.
	if res := f.search(hybrid, f.user, "zebra xylophone"); len(res.Hits) != 0 {
		t.Errorf("nonsense matched %v", refs(res))
	}
}

// A query embedding that misses its 120 ms deadline leaves the answer
// lexical, flagged, and on time; an embedder error does the same.
func TestQueryEmbeddingDeadline(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	words := mockembed.NewWords(synonyms...)
	f.keep(f.user, f.space, "Background jobs run on River.")
	f.keep(f.user, f.space, "Deploy the API to Fly machines.")
	f.index(words, f.space)

	slow := mockembed.New()
	slow.SetDelay(400 * time.Millisecond)
	s := v2recall.New(f.l).WithVectors(v2recall.NewVectors(f.l, slow, nil, v2recall.VectorConfig{Model: model, Log: quiet}))
	start := time.Now()
	res := f.search(s, f.user, "deploy API")
	took := time.Since(start)
	if res.Retrieval.Vector != v2recall.StageTimeout || !res.LexicalOnly || len(res.Hits) != 1 {
		t.Fatalf("slow embedder: vector=%s lexical_only=%v hits=%v", res.Retrieval.Vector, res.LexicalOnly, refs(res))
	}
	if took > 250*time.Millisecond {
		t.Errorf("answered after %v; the embedding deadline is 120 ms", took)
	}
	if res.Retrieval.EmbedMS < 100 {
		t.Errorf("embed_ms %d, want about the deadline", res.Retrieval.EmbedMS)
	}

	broken := mockembed.New()
	broken.SetError(io.ErrUnexpectedEOF)
	s = v2recall.New(f.l).WithVectors(v2recall.NewVectors(f.l, broken, nil, v2recall.VectorConfig{Model: model, Log: quiet}))
	if res := f.search(s, f.user, "deploy API"); res.Retrieval.Vector != v2recall.StageError || len(res.Hits) != 1 {
		t.Errorf("broken embedder: vector=%s hits=%v", res.Retrieval.Vector, refs(res))
	}
}

// Rule 13 for the vector lane: the same statement in two spaces of two
// tenants. A search in one never returns the other's memory, whether the
// filter names both spaces (row-level security holds it out) or the scope
// holds both and the filter one.
func TestVectorLaneStaysInItsSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	e := mockembed.NewWords(synonyms...)
	mallory := f.newUser("mallory")
	theirs := f.newSpace(mallory, "theirs")
	mine := f.keep(f.user, f.space, "Background jobs run on River.")
	other := f.keep(mallory, theirs, "Background jobs run on River!")
	f.index(e, f.space)
	f.index(e, theirs)
	s := v2recall.New(f.l).WithVectors(vectorsFor(f, e))
	query := "Which queue handles async tasks?" // vector-only: no shared words

	if res := f.search(s, f.user, query, f.space, theirs); len(res.Hits) != 1 || res.Hits[0].ID != mine.ID {
		t.Errorf("mine, filter naming both: %v", refs(res))
	}
	if res := f.search(s, mallory, query, f.space, theirs); len(res.Hits) != 1 || res.Hits[0].ID != other.ID {
		t.Errorf("theirs: %v", refs(res))
	}
	// A person in both spaces, filtering to one.
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'member')`, theirs, f.user)
	if res := f.search(s, f.user, query, f.space); len(res.Hits) != 1 || res.Hits[0].ID != mine.ID {
		t.Errorf("both in scope, filter one: %v", refs(res))
	}
	if res := f.search(s, f.user, query, f.space, theirs); len(res.Hits) != 2 {
		t.Errorf("both in scope and filter: %v", refs(res))
	}
}

// slowRerank takes longer than the 150 ms budget.
type slowRerank struct{}

func (slowRerank) TopN() int { return 20 }
func (slowRerank) Rerank(ctx context.Context, _ string, _ []rerank.Document) ([]rerank.Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return nil, nil
	}
}

// reverseRerank puts the fused order upside down.
type reverseRerank struct{ calls int }

func (r *reverseRerank) TopN() int { return 20 }
func (r *reverseRerank) Rerank(_ context.Context, _ string, docs []rerank.Document) ([]rerank.Result, error) {
	r.calls++
	out := make([]rerank.Result, len(docs))
	for i := range docs {
		out[i] = rerank.Result{ID: docs[len(docs)-1-i].ID, Score: float64(i), Rank: i}
	}
	return out, nil
}

// The reranker runs on more than 8 candidates and reorders them; one that
// misses its 150 ms leaves the RRF order, on time.
func TestRerankOnTheQueryPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 12 {
		f.keep(f.user, f.space, "Deploy step "+string(rune('a'+i))+" runs on Fly machines.")
	}
	f.keep(f.user, f.space, "Rollbacks need a person.")
	plain := f.search(v2recall.New(f.l), f.user, "deploy Fly machines")
	if len(plain.Hits) != 5 {
		t.Fatalf("plain: %v", refs(plain))
	}
	rev := &reverseRerank{}
	got := f.search(v2recall.New(f.l).WithReranker(rev), f.user, "deploy Fly machines")
	if rev.calls != 1 || got.Retrieval.Rerank != v2recall.StageOK || got.Hits[0].ID == plain.Hits[0].ID || len(got.Hits) != 5 {
		t.Errorf("reranked: calls %d, %s, %v vs %v", rev.calls, got.Retrieval.Rerank, refs(got), refs(plain))
	}
	start := time.Now()
	got = f.search(v2recall.New(f.l).WithReranker(slowRerank{}), f.user, "deploy Fly machines")
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("a slow reranker held the answer for %v", took)
	}
	if got.Retrieval.Rerank != v2recall.StageTimeout || refs(got)[0] != refs(plain)[0] {
		t.Errorf("slow reranker: %s %v, want the RRF order %v", got.Retrieval.Rerank, refs(got), refs(plain))
	}
	// Few candidates: the reranker isn't called.
	rev = &reverseRerank{}
	got = f.search(v2recall.New(f.l).WithReranker(rev), f.user, "rollbacks")
	if rev.calls != 0 || got.Retrieval.Rerank != v2recall.StageSkipped {
		t.Errorf("few candidates: calls %d, %s", rev.calls, got.Retrieval.Rerank)
	}
	var none *rerank.Voyage
	if got := f.search(v2recall.New(f.l).WithReranker(none), f.user, "deploy"); got.Retrieval.Rerank != v2recall.StageOff {
		t.Errorf("a nil *Voyage is no reranker: %s", got.Retrieval.Rerank)
	}
}

// The judge's vector candidates come from the proposal's stored vector,
// or its statement when the index job hasn't run, and never include the
// proposal itself.
func TestSimilarForTheJudge(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	e := mockembed.NewWords(synonyms...)
	kept := f.keep(f.user, f.space, "Background jobs run on River.")
	f.keep(f.user, f.space, "The web app is a Next.js 16 project.")
	f.index(e, f.space)
	v := vectorsFor(f, e)
	res, err := f.l.Apply(f.ctx, &ledger.Propose{
		Meta:      ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: policy.AutonomyPropose}, Scope: f.scope(f.user), Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: f.space, Statement: "Async tasks go through the queue.", Section: ledger.SectionConventions},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Memory
	scope := f.scope(f.user)
	before := e.CallCount()
	got, err := v.Similar(f.ctx, scope, f.space, p.ID, p.Statement, 10)
	if err != nil || len(got) == 0 || got[0].ID != kept.ID || got[0].Score < 0.5 {
		t.Fatalf("unindexed proposal: %+v %v", got, err)
	}
	if e.CallCount() != before+1 {
		t.Errorf("the statement wasn't embedded (%d calls)", e.CallCount()-before)
	}
	f.index(e, f.space)
	before = e.CallCount()
	got, _ = v.Similar(f.ctx, scope, f.space, p.ID, p.Statement, 10)
	if e.CallCount() != before || len(got) == 0 || got[0].ID != kept.ID {
		t.Errorf("indexed proposal: %d calls, %+v", e.CallCount()-before, got)
	}
	for _, c := range got {
		if c.ID == p.ID {
			t.Error("the proposal is its own candidate")
		}
	}
}
