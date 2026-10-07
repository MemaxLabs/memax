package ledger_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

const testModel = "voyage-4"

// newIndexFixture is newCompileFixture with index jobs on: the ledger the
// API server builds when V2 embeddings are configured.
func newIndexFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, pool: pool, logs: &syncBuffer{},
		l: ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), ledger.WithJobs(client), ledger.WithIndexJobs())}
}

// indexJobs lists the index_memory jobs waiting, as "memory@version".
func (f *fixture) indexJobs() []ledger.IndexArgs {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT args FROM river_job WHERE kind = 'index_memory' ORDER BY id`)
	if err != nil {
		f.t.Fatal(err)
	}
	raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		f.t.Fatal(err)
	}
	out := make([]ledger.IndexArgs, len(raws))
	for i, raw := range raws {
		if err := json.Unmarshal(raw, &out[i]); err != nil {
			f.t.Fatal(err)
		}
	}
	return out
}

// unit is a unit vector along dimension i, tilted towards j by w: two
// unit(i, …) vectors are as similar as their tilts make them.
func unit(i, j int, w float64) []float32 {
	v := make([]float32, ledger.EmbeddingDimensions)
	v[i] = float32(math.Sqrt(1 - w*w))
	v[j] += float32(w)
	return v
}

func (f *fixture) store(scope ledger.Scope, m *ledger.Memory, vec []float32) int {
	f.t.Helper()
	n, err := f.l.StoreEmbeddings(context.Background(), scope, testModel,
		[]ledger.Embedding{{MemoryID: m.ID, SpaceID: m.SpaceID, Version: m.Version, Vector: vec}})
	if err != nil {
		f.t.Fatalf("StoreEmbeddings %s: %v", m.Ref, err)
	}
	return n
}

// Every command that writes a searchable version queues its index job in
// its own transaction: a proposal, a person's remember, an edit. A
// refused command and a rolled-back one queue nothing; a ledger without
// index jobs queues none.
func TestIndexJobsGoOutWithTheCommand(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "Background jobs run on River.")}).Memory
	k := f.remember(zz, space, "Deploy the API to Fly machines.")
	e := f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: k.Ref, ExpectedVersion: 1,
		Statement: "Deploy the API and the worker to Fly machines."}).Memory
	want := []ledger.IndexArgs{{MemoryID: p.ID, SpaceID: space, Version: 1}, {MemoryID: k.ID, SpaceID: space, Version: 1},
		{MemoryID: e.ID, SpaceID: space, Version: 2}}
	if got := f.indexJobs(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("index jobs = %+v, want %+v", got, want)
	}

	// Refused (a Read agent writes nothing): no job.
	res := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyRead), scope, policy.ViaMCP), NewMemory: fact(space, "Refused.")})
	if res.Outcome != ledger.OutcomeRefused {
		t.Fatalf("read agent: %s", res.Outcome)
	}
	// Rolled back (River is down): no job, no memory.
	f.exec(`ALTER TABLE river_job RENAME TO river_job_gone`)
	if _, err := f.l.Apply(ctx, &ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
		NewMemory: fact(space, "Never written.")}); err == nil || !strings.Contains(err.Error(), "enqueue") {
		t.Fatalf("propose with River down: %v", err)
	}
	f.exec(`ALTER TABLE river_job_gone RENAME TO river_job`)
	if n := len(f.indexJobs()); n != 3 {
		t.Errorf("%d index jobs after a refusal and a rollback, want 3", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions v WHERE v.statement = 'Never written.'`); n != 0 {
		t.Errorf("the rolled-back proposal was written")
	}

	// Without WithIndexJobs nothing is queued.
	client, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	plain := ledger.New(f.pool, ledger.WithJobs(client))
	if _, err := plain.Apply(ctx, &ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: fact(space, "No index job.")}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.indexJobs()); n != 3 {
		t.Errorf("a ledger without index jobs queued one (%d)", n)
	}
}

// StoreEmbeddings stores once per version and model, refuses the wrong
// width, and skips a version that is no longer current. IndexWork lists
// the job's own memory first, then the rest of the space, and nothing
// already embedded.
func TestStoreEmbeddingsIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	a := f.remember(zz, space, "Deploy the API to Fly machines.")
	b := f.remember(zz, space, "Background jobs run on River.")
	c := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "Use pnpm catalogs.")}).Memory

	work, err := f.l.IndexWork(ctx, scope, space, testModel, b.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 3 || work[0].MemoryID != b.ID || work[0].Statement != b.Statement {
		t.Fatalf("index work = %+v; the job's own memory comes first", work)
	}
	if work, _ := f.l.IndexWork(ctx, scope, space, testModel, b.ID, 2); len(work) != 2 {
		t.Errorf("limit 2: %d tasks", len(work))
	}
	if n := f.store(scope, a, unit(1, 2, 0)); n != 1 {
		t.Fatalf("first store: %d", n)
	}
	if n := f.store(scope, a, unit(1, 2, 0)); n != 0 {
		t.Errorf("second store of the same version stored %d", n)
	}
	if n := f.store(scope, c, unit(3, 4, 0)); n != 1 {
		t.Errorf("a proposal is embedded too: stored %d", n)
	}
	work, _ = f.l.IndexWork(ctx, scope, space, testModel, a.ID, 10)
	if len(work) != 1 || work[0].MemoryID != b.ID {
		t.Errorf("index work after storing a and c = %+v", work)
	}
	if work, _ := f.l.IndexWork(ctx, scope, space, "voyage-5", a.ID, 10); len(work) != 3 {
		t.Errorf("another model needs all three: %d", len(work))
	}

	// An edit moves a past version 1: storing version 1 now does nothing.
	f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: b.Ref, ExpectedVersion: 1, Statement: "Background jobs run on River, in Postgres."})
	if n := f.store(scope, b, unit(5, 6, 0)); n != 0 {
		t.Errorf("stored an embedding for a replaced version (%d)", n)
	}
	if _, err := f.l.StoreEmbeddings(ctx, scope, testModel, []ledger.Embedding{{MemoryID: a.ID, SpaceID: space, Version: 1, Vector: make([]float32, 3)}}); err == nil {
		t.Error("a 3-dimensional embedding was accepted")
	}
	vec, ok, err := f.l.StoredEmbedding(ctx, scope, a.ID, testModel)
	if err != nil || !ok || len(vec) != ledger.EmbeddingDimensions || vec[1] != 1 {
		t.Errorf("stored embedding = %v %v %v", ok, err, len(vec))
	}
	if _, ok, _ := f.l.StoredEmbedding(ctx, scope, b.ID, testModel); ok {
		t.Error("b's current version has no embedding yet")
	}
}

// Rule 13 for vectors: the same statement, with the same vector, in two
// spaces of two tenants. A search scoped to one space never returns the
// other's memory, even when the query names both spaces: row-level
// security holds it out, not the Go filter.
func TestNearestNeverCrossesSpaces(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz, mallory := f.user("zz"), f.user("mallory")
	mine := f.space(zz, policy.SpaceProject, "memax-v2")
	theirs := f.space(mallory, policy.SpaceProject, "other")
	statement := "The staging database lives in Neon us-west-2."
	a := f.remember(zz, mine, statement)
	b := f.remember(mallory, theirs, statement+" ")
	vec := unit(7, 8, 0.1)
	f.store(f.scope(zz), a, vec)
	f.store(f.scope(mallory), b, vec)

	for _, c := range []struct {
		name   string
		scope  ledger.Scope
		spaces []uuid.UUID
		want   []uuid.UUID
	}{
		{"mine", f.scope(zz), []uuid.UUID{mine}, []uuid.UUID{a.ID}},
		{"mine, asking for both", f.scope(zz), []uuid.UUID{mine, theirs}, []uuid.UUID{a.ID}},
		{"theirs", f.scope(mallory), []uuid.UUID{mine, theirs}, []uuid.UUID{b.ID}},
		{"no scope", ledger.Scope{}, []uuid.UUID{mine, theirs}, nil},
	} {
		var got []ledger.Neighbor
		err := f.l.Read(ctx, c.scope, func(tx pgx.Tx) error {
			var err error
			got, err = ledger.Nearest(ctx, tx, ledger.NearestQuery{Spaces: c.spaces, Model: testModel, Vector: vec, K: 10})
			return err
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != len(c.want) || (len(got) == 1 && got[0].ID != c.want[0]) {
			t.Errorf("%s: %+v, want %v", c.name, got, c.want)
		}
		if len(got) == 1 && math.Abs(got[0].Similarity-1) > 1e-3 {
			t.Errorf("%s: similarity %v, want 1", c.name, got[0].Similarity)
		}
	}
	// The raw table under memax_v2: a scope of one space sees one row.
	var n int
	if err := f.asV2([]uuid.UUID{mine}, []uuid.UUID{a.TenantID}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.memory_embeddings`).Scan(&n)
	}); err != nil || n != 1 {
		t.Errorf("memax_v2 sees %d embeddings (%v), want 1", n, err)
	}
	// And can't write into a space outside its scope. The guard trigger
	// runs before the policy's WITH CHECK, and under row-level security it
	// can't see the other space's memory either, so either refusal will do.
	err := f.asV2([]uuid.UUID{mine}, []uuid.UUID{a.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO v2.memory_embeddings (memory_id, version, model, space_id, embedding)
			VALUES ($1, 1, 'other-model', $2, $3::halfvec(1024))`, b.ID, theirs, mustLiteral(t, vec))
		return err
	})
	if code := sqlstate(err); code != "42501" && code != "MXE01" {
		t.Errorf("insert into another space: %v, want a refusal (42501 or MXE01)", err)
	}
}

func mustLiteral(t *testing.T, v []float32) string {
	t.Helper()
	s, err := ledger.VectorLiteral(v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Nearest is exact and filtered: it orders by similarity, honours the
// floor, the lifecycles, the kind, the exception and superseded
// decisions, and reads only the current version's vector.
func TestNearestFilters(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	q := unit(10, 11, 0)
	close := f.remember(zz, space, "Close.")
	f.store(scope, close, unit(10, 11, 0.3)) // similarity ≈ 0.95
	far := f.remember(zz, space, "Far.")
	f.store(scope, far, unit(10, 11, 0.9)) // ≈ 0.44
	prop := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "Pending.")}).Memory
	f.store(scope, prop, unit(10, 11, 0.1))
	edited := f.remember(zz, space, "Old words.")
	f.store(scope, edited, unit(10, 11, 0)) // version 1 is the query itself
	f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: edited.Ref, ExpectedVersion: 1, Statement: "New words."})

	ids := func(ns []ledger.Neighbor) []uuid.UUID {
		out := make([]uuid.UUID, len(ns))
		for i, n := range ns {
			out[i] = n.ID
		}
		return out
	}
	for _, c := range []struct {
		name string
		q    ledger.NearestQuery
		want []uuid.UUID
	}{
		{"kept, best first", ledger.NearestQuery{}, []uuid.UUID{close.ID, far.ID}},
		{"floor", ledger.NearestQuery{Floor: 0.8}, []uuid.UUID{close.ID}},
		{"kept and proposed", ledger.NearestQuery{Lifecycles: []lifecycle.Lifecycle{lifecycle.Kept, lifecycle.Proposed}}, []uuid.UUID{prop.ID, close.ID, far.ID}},
		{"except", ledger.NearestQuery{Except: close.ID}, []uuid.UUID{far.ID}},
		{"k", ledger.NearestQuery{K: 1}, []uuid.UUID{close.ID}},
		{"decisions only", ledger.NearestQuery{Kind: ledger.KindDecision}, []uuid.UUID{}},
	} {
		c.q.Spaces, c.q.Model, c.q.Vector = []uuid.UUID{space}, testModel, q
		var got []ledger.Neighbor
		if err := f.l.Read(ctx, scope, func(tx pgx.Tx) error {
			var err error
			got, err = ledger.Nearest(ctx, tx, c.q)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if g := ids(got); len(g) != len(c.want) || (len(g) > 0 && !equalIDs(g, c.want)) {
			t.Errorf("%s: %v, want %v", c.name, g, c.want)
		}
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Forget purges the vectors in the same transaction as the words, by
// trigger, whichever way it purges them: the memory moving to forgotten,
// or a statement version set to NULL. Afterwards an index job can't put a
// vector back (MXE01, and StoreEmbeddings stores nothing).
func TestForgetPurgesEmbeddings(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	m := f.remember(zz, space, "The launch date is the 14th.")
	other := f.remember(zz, space, "Keep this one.")
	f.store(scope, m, unit(1, 2, 0))
	f.store(scope, other, unit(3, 4, 0))
	f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1, Statement: "The launch date is the 21st."})
	m2, err := f.l.GetMemory(ctx, scope, m.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	f.store(scope, m2, unit(1, 2, 0.2))
	if n := f.count(`SELECT count(*) FROM v2.memory_embeddings WHERE memory_id = $1`, m.ID); n != 2 {
		t.Fatalf("%d embeddings of m, want 2 (both versions)", n)
	}

	forget := func(tx pgx.Tx, version int, whole bool) error {
		rc := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'forgot', 'person', $6, 'web', now(), $4, $7)`,
			rc, m.TenantID, space, m.ID, m.Ref, zz, 10+version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE v2.memory_versions SET statement = NULL, last_receipt_id = $3 WHERE memory_id = $1 AND version = $2`,
			m.ID, version, rc); err != nil {
			return err
		}
		if !whole {
			return nil
		}
		// Every version's words go, and the tombstone is written (043).
		if _, err := tx.Exec(ctx, `UPDATE v2.memory_versions SET statement = NULL, last_receipt_id = $2 WHERE memory_id = $1`, m.ID, rc); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'forgotten', search = NULL, content_sha256 = NULL,
			minhash_bands = NULL, last_receipt_id = $2 WHERE id = $1`, m.ID, rc); err != nil {
			return err
		}
		return insertTombstoneSQL(tx, m, zz, rc)
	}
	// Purge version 1's words only: its vector goes, version 2's stays.
	if err := f.asV2([]uuid.UUID{space}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error { return forget(tx, 1, false) }); err != nil {
		t.Fatalf("purge version 1: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_embeddings WHERE memory_id = $1`, m.ID); n != 1 {
		t.Errorf("%d embeddings after purging version 1, want 1", n)
	}
	// Forget the memory: every vector goes, in that transaction.
	if err := f.asV2([]uuid.UUID{space}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
		if err := forget(tx, 2, true); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.memory_embeddings WHERE memory_id = $1`, m.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("inside the forget's transaction: %d embeddings (%v)", n, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_embeddings`); n != 1 {
		t.Errorf("%d embeddings left, want only the other memory's", n)
	}
	// A late index job can't bring it back.
	if n := f.store(scope, m2, unit(1, 2, 0.2)); n != 0 {
		t.Errorf("stored an embedding for a forgotten memory")
	}
	err = f.asV2([]uuid.UUID{space}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO v2.memory_embeddings (memory_id, version, model, space_id, embedding)
			VALUES ($1, 2, 'voyage-5', $2, $3::halfvec(1024))`, m.ID, space, mustLiteral(t, unit(1, 2, 0)))
		return err
	})
	if sqlstate(err) != "MXE01" {
		t.Errorf("raw insert for a forgotten memory: %v, want MXE01", err)
	}
	// memax_v2 can't delete embeddings itself.
	err = f.asV2([]uuid.UUID{space}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM v2.memory_embeddings`)
		return err
	})
	if sqlstate(err) != "42501" {
		t.Errorf("delete as memax_v2: %v, want permission denied", err)
	}
}

// The sweep finds versions with no embedding of the model in every space,
// across tenants, ids only.
func TestUnindexedVersions(t *testing.T) {
	t.Parallel()
	f := newIndexFixture(t)
	ctx := context.Background()
	zz, mallory := f.user("zz"), f.user("mallory")
	mine := f.space(zz, policy.SpaceProject, "memax-v2")
	theirs := f.space(mallory, policy.SpacePersonal, "other")
	a := f.remember(zz, mine, "One.")
	b := f.remember(mallory, theirs, "Two.")
	f.remember(zz, mine, "Three.")
	rej := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP), NewMemory: fact(mine, "Rejected.")}).Memory
	f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: rej.Ref})
	f.store(f.scope(zz), a, unit(1, 2, 0))

	got, err := f.l.UnindexedVersions(ctx, testModel, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unindexed = %+v, want two (not the embedded one, not the rejected one)", got)
	}
	seen := map[uuid.UUID]bool{}
	for _, g := range got {
		seen[g.MemoryID] = true
	}
	if !seen[b.ID] || seen[a.ID] || seen[rej.ID] {
		t.Errorf("unindexed = %+v", got)
	}
	if got, _ := f.l.UnindexedVersions(ctx, testModel, 1); len(got) != 1 {
		t.Errorf("limit 1: %d", len(got))
	}
}

func TestVectorLiteral(t *testing.T) {
	t.Parallel()
	v := unit(0, 1, 0.5)
	s, err := ledger.VectorLiteral(v)
	if err != nil || !strings.HasPrefix(s, "[0.8660254,0.5,0,") || !strings.HasSuffix(s, ",0]") {
		t.Errorf("literal = %.40s… %v", s, err)
	}
	for name, bad := range map[string][]float32{
		"short": make([]float32, 3),
		"nan":   func() []float32 { x := unit(0, 1, 0); x[5] = float32(math.NaN()); return x }(),
		"huge":  func() []float32 { x := unit(0, 1, 0); x[5] = 1e6; return x }(),
	} {
		if _, err := ledger.VectorLiteral(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
