package v2recall

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
)

// ids are uuidv7s in creation order: a later one is newer.
func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.Must(uuid.NewV7())
		time.Sleep(time.Millisecond)
	}
	return out
}

// Weighted RRF with V1's constants: a memory two lanes agree on rises, the
// vector lane outweighs full text, trigrams count least, and ties go to
// the newer memory.
func TestFuse(t *testing.T) {
	t.Parallel()
	m := ids(4)
	a, b, c, d := m[0], m[1], m[2], m[3]
	for _, tc := range []struct {
		name  string
		lanes []ranking
		want  []uuid.UUID
	}{
		{"agreement rises", []ranking{{laneVector, []uuid.UUID{a, b}}, {"fts", []uuid.UUID{b, c}}, {"trigram", []uuid.UUID{c}}},
			[]uuid.UUID{b, c, a}},
		{"vector outweighs full text at the same rank", []ranking{{"fts", []uuid.UUID{a}}, {laneVector, []uuid.UUID{b}}},
			[]uuid.UUID{b, a}},
		{"full text outweighs trigrams", []ranking{{"trigram", []uuid.UUID{a}}, {"fts", []uuid.UUID{b}}},
			[]uuid.UUID{b, a}},
		{"ties: newer first", []ranking{{"fts", []uuid.UUID{a}}, {"fts", []uuid.UUID{d}}},
			[]uuid.UUID{d, a}},
		{"one lane keeps its order", []ranking{{laneVector, []uuid.UUID{c, a, d, b}}}, []uuid.UUID{c, a, d, b}},
		{"nothing", nil, []uuid.UUID{}},
	} {
		got, scores := fuse(tc.lanes)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s: %v, want %v (scores %v)", tc.name, got, tc.want, scores)
		}
	}
	_, scores := fuse([]ranking{{laneVector, []uuid.UUID{a}}, {"fts", []uuid.UUID{a}}, {"trigram", []uuid.UUID{a}}})
	if want := (1.5 + 1 + 0.6) / 61; scores[a] < want-1e-12 || scores[a] > want+1e-12 {
		t.Errorf("score = %v, want %v", scores[a], want)
	}
}

// fakeReranker answers with a fixed order, after a delay, or an error.
type fakeReranker struct {
	order []int
	delay time.Duration
	err   error
	calls int
	got   int
}

func (f *fakeReranker) TopN() int { return 12 }

func (f *fakeReranker) Rerank(ctx context.Context, _ string, docs []rerank.Document) ([]rerank.Result, error) {
	f.calls++
	f.got = len(docs)
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	out := make([]rerank.Result, 0, len(f.order))
	for rank, i := range f.order {
		if i < len(docs) {
			out = append(out, rerank.Result{ID: docs[i].ID, Score: 1 - float64(rank)/100, Rank: rank})
		}
	}
	return out, nil
}

func hits(n int) []Hit {
	out := make([]Hit, n)
	for i, id := range ids(n) {
		out[i] = Hit{ID: id, Statement: fmt.Sprintf("statement %d", i)}
	}
	return out
}

// The rerank runs only on more than 8 candidates, sends at most TopN,
// puts what it ranked first and the rest after in fused order, and falls
// back to the fused order on a timeout (150 ms) or an error.
func TestRerankHits(t *testing.T) {
	t.Parallel()
	ten := hits(14)
	for _, tc := range []struct {
		name     string
		r        *fakeReranker
		in       []Hit
		ctxLeft  time.Duration
		want     string
		first    int // index into in of the expected first hit
		maxTook  time.Duration
		sentDocs int
	}{
		{"reorders", &fakeReranker{order: []int{5, 2}}, ten, 0, StageOK, 5, 0, 12},
		{"eight or fewer: skipped", &fakeReranker{order: []int{5}}, ten[:8], 0, StageSkipped, 0, 0, 0},
		{"timeout keeps the RRF order", &fakeReranker{order: []int{5}, delay: time.Second}, ten, 0, StageTimeout, 0, 250 * time.Millisecond, 12},
		{"error keeps the RRF order", &fakeReranker{err: errors.New("boom")}, ten, 0, StageError, 0, 0, 12},
		{"no time left: skipped", &fakeReranker{order: []int{5}}, ten, 15 * time.Millisecond, StageSkipped, 0, 0, 0},
		{"the caller's deadline bounds it", &fakeReranker{order: []int{5}, delay: time.Second}, ten, 80 * time.Millisecond, StageTimeout, 0, 120 * time.Millisecond, 12},
	} {
		ctx := context.Background()
		if tc.ctxLeft > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, tc.ctxLeft)
			defer cancel()
		}
		start := time.Now()
		out, status := rerankHits(ctx, tc.r, "q", tc.in)
		took := time.Since(start)
		if status != tc.want {
			t.Errorf("%s: status %s, want %s", tc.name, status, tc.want)
		}
		if len(out) != len(tc.in) || out[0].ID != tc.in[tc.first].ID {
			t.Errorf("%s: %d hits, first %v, want %v", tc.name, len(out), out[0].ID, tc.in[tc.first].ID)
		}
		if tc.maxTook > 0 && took > tc.maxTook {
			t.Errorf("%s: took %v, want under %v", tc.name, took, tc.maxTook)
		}
		if tc.r.got != tc.sentDocs {
			t.Errorf("%s: sent %d documents, want %d", tc.name, tc.r.got, tc.sentDocs)
		}
	}
	// The rest follow in fused order, each once.
	out, _ := rerankHits(context.Background(), &fakeReranker{order: []int{5, 2}}, "q", ten)
	if out[1].ID != ten[2].ID || out[2].ID != ten[0].ID || out[3].ID != ten[1].ID || out[4].ID != ten[3].ID {
		t.Errorf("order after the reranked two: %v", out[:5])
	}
	seen := map[uuid.UUID]bool{}
	for _, h := range out {
		if seen[h.ID] {
			t.Fatalf("%v twice", h.ID)
		}
		seen[h.ID] = true
	}
	if _, status := rerankHits(context.Background(), nil, "q", ten); status != StageOff {
		t.Errorf("nil reranker: %s", status)
	}
}

// The vector cache keeps the newest entries and forgets the oldest.
func TestVectorCache(t *testing.T) {
	t.Parallel()
	c := newVectorCache(2, time.Minute)
	k := func(s string) [32]byte { return cacheKey("m", "query", s) }
	c.put(k("a"), []float32{1})
	c.put(k("b"), []float32{2})
	c.put(k("c"), []float32{3})
	if _, ok := c.get(k("a")); ok {
		t.Error("the oldest entry survived")
	}
	if v, ok := c.get(k("c")); !ok || v[0] != 3 {
		t.Error("the newest entry is missing")
	}
	expired := newVectorCache(2, time.Nanosecond)
	expired.put(k("a"), []float32{1})
	time.Sleep(time.Millisecond)
	if _, ok := expired.get(k("a")); ok {
		t.Error("an expired entry was served")
	}
	if cacheKey("m", "query", "x") == cacheKey("m", "document", "x") {
		t.Error("query and document embeddings share a key")
	}
}
