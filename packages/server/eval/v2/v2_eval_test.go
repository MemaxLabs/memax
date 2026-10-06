// Package v2eval scores V2 recall and search (internal/v2recall, plan 25
// §5.11, §11) on a small graded corpus of kept statements, with the
// retrieval harness's metrics (package eval): lexical only, hybrid on a
// deterministic fake embedder, and, with keys, hybrid on Voyage.
//
//	go test ./eval/v2/ -v                                        # lexical and fake hybrid (CI)
//	V2_EVAL_LIVE=1 VOYAGE_API_KEY=… go test ./eval/v2/ -v          # also voyage-4 / voyage-4-lite, and rerank-3-lite
//
// The fake embedder is a bag of words with the corpus's synonym table, so
// its hybrid numbers check the plumbing (the vector lane's candidates
// survive fusion, the floor keeps nonsense out, isolation holds), not
// retrieval quality. The live run is the eval gate for the models: hybrid
// must not score below lexical, and nothing harmful may reach the top 10.
package v2eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/eval"
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

type corpus struct {
	Version     int        `json:"version"`
	Description string     `json:"description"`
	Synonyms    [][]string `json:"synonyms"`
	Memories    []struct {
		ID        string `json:"id"`
		Space     string `json:"space"`
		Section   string `json:"section"`
		Kind      string `json:"kind"`
		Statement string `json:"statement"`
	} `json:"memories"`
	Queries []struct {
		ID        string         `json:"id"`
		Type      string         `json:"type"`
		Query     string         `json:"query"`
		Relevance map[string]int `json:"relevance"`
	} `json:"queries"`
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()
	raw, err := os.ReadFile("corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c corpus
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("corpus.json: %v", err)
	}
	return c
}

// The corpus is well formed: unique ids, every graded id exists, every
// query type is covered, and the harmful items live in the other space.
func TestCorpusLoads(t *testing.T) {
	t.Parallel()
	c := loadCorpus(t)
	ids := map[string]string{}
	for _, m := range c.Memories {
		if _, dup := ids[m.ID]; dup {
			t.Errorf("memory %s twice", m.ID)
		}
		if m.Space != "mine" && m.Space != "theirs" {
			t.Errorf("%s: space %q", m.ID, m.Space)
		}
		if !ledger.Section(m.Section).Valid() {
			t.Errorf("%s: section %q", m.ID, m.Section)
		}
		ids[m.ID] = m.Space
	}
	types := map[string]int{}
	for _, q := range c.Queries {
		types[q.Type]++
		for id, g := range q.Relevance {
			space, ok := ids[id]
			if !ok {
				t.Errorf("%s grades unknown memory %s", q.ID, id)
			}
			if (g < 0) != (space == "theirs") {
				t.Errorf("%s: %s is graded %d; harmful means the other space's", q.ID, id, g)
			}
		}
	}
	for _, want := range []string{"lexical", "paraphrase", "typo", "negative", "isolation"} {
		if types[want] == 0 {
			t.Errorf("no %s queries", want)
		}
	}
}

type mode struct {
	name   string
	search *v2recall.Searcher
}

func TestV2Retrieval(t *testing.T) {
	if testing.Short() {
		t.Skip("v2 retrieval eval: skipped in -short")
	}
	c := loadCorpus(t)
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	l := ledger.New(pool, ledger.WithLogger(quiet))
	newUser := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newSpace := func(owner uuid.UUID, name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
			id, name, id.String(), owner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	me, them := newUser("zz"), newUser("other")
	spaces := map[string]uuid.UUID{"mine": newSpace(me, "memax-v2"), "theirs": newSpace(them, "other-team")}
	owners := map[string]uuid.UUID{"mine": me, "theirs": them}
	fixtureOf := map[uuid.UUID]string{}
	for _, m := range c.Memories {
		scope, err := l.UserScope(ctx, owners[m.Space])
		if err != nil {
			t.Fatal(err)
		}
		nm := ledger.NewMemory{SpaceID: spaces[m.Space], Statement: m.Statement, Section: ledger.Section(m.Section), Kind: ledger.KindFact}
		if m.Kind == "decision" {
			nm.Kind = ledger.KindDecision
		}
		res, err := l.Apply(ctx, &ledger.Remember{Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: owners[m.Space], Name: "zz"},
			Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}, NewMemory: nm})
		if err != nil || res.Memory == nil {
			t.Fatalf("%s: %v %+v", m.ID, err, res.Policy)
		}
		fixtureOf[res.Memory.ID] = m.ID
	}
	index := func(e embed.Embedder, model string) {
		ix := v2index.New(l, e, model, 128, quiet)
		for _, sp := range spaces {
			for {
				n, err := ix.Index(ctx, ledger.IndexArgs{SpaceID: sp})
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
			}
		}
	}
	words := mockembed.NewWords(c.Synonyms...)
	index(words, "fake-words")
	modes := []mode{
		{"lexical", v2recall.New(l)},
		{"hybrid, fake embedder", v2recall.New(l).WithVectors(v2recall.NewVectors(l, words, words,
			v2recall.VectorConfig{Model: "fake-words", Log: quiet}))},
	}
	live := os.Getenv("V2_EVAL_LIVE") == "1"
	if live {
		cfg := v2index.ConfigFromEnv(os.LookupEnv)
		if !cfg.Enabled() {
			t.Fatal("V2_EVAL_LIVE=1 needs VOYAGE_API_KEY")
		}
		start := time.Now()
		index(cfg.IndexEmbedder(), cfg.IndexModel)
		t.Logf("indexed %d statements with %s in %v", len(c.Memories), cfg.IndexModel, time.Since(start).Round(time.Millisecond))
		vectors := v2recall.NewVectors(l, cfg.QueryEmbedder(), cfg.IndexEmbedder(),
			v2recall.VectorConfigFromEnv(os.LookupEnv, cfg.IndexModel))
		modes = append(modes, mode{"hybrid, " + cfg.QueryModel + " → " + cfg.IndexModel, v2recall.New(l).WithVectors(vectors)})
		if rr := rerank.NewVoyage(rerank.VoyageConfig{APIKey: cfg.APIKey, Model: os.Getenv("V2_RERANK_MODEL")}); rr != nil {
			modes = append(modes, mode{"hybrid + " + rr.Model(), v2recall.New(l).WithVectors(vectors).WithReranker(rr)})
		}
	}

	scope, err := l.UserScope(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	type scored struct {
		agg      eval.MetricSet
		slices   []eval.SliceReport
		results  []eval.QueryResult
		fellBack int
	}
	all := map[string]scored{}
	for _, m := range modes {
		var s scored
		for _, q := range c.Queries {
			start := time.Now()
			res, err := m.search.Search(ctx, scope, v2recall.Query{Text: q.Query, Filter: v2recall.Filter{Spaces: []uuid.UUID{spaces["mine"], spaces["theirs"]}}, Limit: 10})
			took := time.Since(start)
			if err != nil {
				t.Fatalf("%s %s: %v", m.name, q.ID, err)
			}
			if res.Retrieval.Vector == v2recall.StageTimeout || res.Retrieval.Vector == v2recall.StageError {
				s.fellBack++
			}
			qr := eval.QueryResult{QueryID: q.ID, Query: q.Query, QueryType: q.Type, Slices: []string{q.Type},
				Relevance: q.Relevance, Negative: q.Type == "negative", LatencyMs: took.Milliseconds()}
			for i, h := range res.Hits {
				qr.Results = append(qr.Results, eval.RankedResult{FixtureID: fixtureOf[h.ID], Rank: i + 1, Score: h.Score})
			}
			s.results = append(s.results, qr)
		}
		s.agg, s.slices = eval.AggregateMetrics(s.results), eval.AggregateBySlice(s.results)
		all[m.name] = s
	}

	var b strings.Builder
	fmt.Fprintf(&b, "V2 retrieval over %d statements, %d queries (search limit 10)\n", len(c.Memories), len(c.Queries))
	fmt.Fprintf(&b, "%-36s %7s %7s %7s %8s %8s %7s %8s\n", "mode", "nDCG@5", "nDCG@10", "MRR@10", "Recall20", "Harmful", "p50 ms", "fallback")
	for _, m := range modes {
		s := all[m.name]
		lat := make([]int64, len(s.results))
		for i, r := range s.results {
			lat[i] = r.LatencyMs
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		fmt.Fprintf(&b, "%-36s %7.3f %7.3f %7.3f %8.3f %8d %7d %8d\n", m.name, s.agg.NDCG5, s.agg.NDCG10, s.agg.MRR10, s.agg.Recall20,
			s.agg.Harmful10, lat[len(lat)/2], s.fellBack)
	}
	b.WriteString("\nnDCG@5 by query type:\n")
	for _, m := range modes {
		fmt.Fprintf(&b, "%-36s", m.name)
		for _, sl := range all[m.name].slices {
			fmt.Fprintf(&b, " %s %.2f (n=%d)", sl.Slice, sl.Metrics.NDCG5, sl.Count)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nnegative queries, results returned:\n")
	for _, m := range modes {
		fmt.Fprintf(&b, "%-36s", m.name)
		for _, r := range all[m.name].results {
			if r.Negative {
				fmt.Fprintf(&b, " %s=%d", r.QueryID, len(r.Results))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\nmissed (no result graded 2 or more in the top 10):\n")
	for _, m := range modes {
		var missed []string
		for _, r := range all[m.name].results {
			if r.Negative || r.QueryType == "isolation" {
				continue
			}
			hit := false
			for _, x := range r.Results {
				hit = hit || r.Relevance[x.FixtureID] >= 2
			}
			if !hit {
				missed = append(missed, r.QueryID)
			}
		}
		fmt.Fprintf(&b, "%-36s %v\n", m.name, missed)
	}
	t.Log(b.String())

	lexical := all["lexical"]
	for _, m := range modes {
		s := all[m.name]
		if s.agg.Harmful10 != 0 {
			t.Errorf("%s: %d harmful results (another space's memory) in a top 10", m.name, s.agg.Harmful10)
		}
		if m.name == "lexical" {
			continue
		}
		if s.agg.NDCG5 < lexical.agg.NDCG5-0.01 || s.agg.MRR10 < lexical.agg.MRR10-0.01 {
			t.Errorf("%s scores below lexical: nDCG@5 %.3f vs %.3f, MRR@10 %.3f vs %.3f", m.name,
				s.agg.NDCG5, lexical.agg.NDCG5, s.agg.MRR10, lexical.agg.MRR10)
		}
	}
	fake := all["hybrid, fake embedder"]
	for _, r := range fake.results {
		if r.Negative && len(r.Results) > 0 {
			t.Errorf("fake hybrid: negative query %s returned %d results; the floor should keep nonsense out", r.QueryID, len(r.Results))
		}
	}
	if fake.fellBack != 0 {
		t.Errorf("fake hybrid fell back to lexical on %d queries", fake.fellBack)
	}
}
