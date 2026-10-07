// Package v2eval scores V2 recall and search (internal/v2recall, plan 25
// §5.11, §11) on a graded corpus of kept statements, with the retrieval
// harness's metrics (package eval): lexical only, hybrid on a
// deterministic fake embedder, and, with keys, hybrid on Voyage.
//
//	go test ./eval/v2/ -v                                        # lexical and fake hybrid (CI)
//	V2_EVAL_LIVE=1 VOYAGE_API_KEY=… go test ./eval/v2/ -v          # also Voyage: the models, rerank-3-lite and the floor
//
// The fake embedder is a bag of words with the corpus's synonym table, so
// its hybrid numbers check the plumbing (the vector lane's candidates
// survive fusion, the floor keeps nonsense out, isolation holds), not
// retrieval quality. The live run is the eval gate for the models (§5.11:
// they ship only if retrieval doesn't regress): the default pair
// (V2_EMBED_QUERY_MODEL voyage-4-lite → V2_EMBED_MODEL voyage-4) must not
// score below lexical or below V1's model (voyage-code-3) on the same
// corpus, and nothing harmful may reach the top 10. Results are in
// RESULTS.md.
//
// In the live run the quality modes share one embedding per query and wait
// up to 2 s for it, and the reranker runs without its 150 ms deadline, so
// their numbers don't depend on this machine's distance from Voyage. The
// "production deadlines" mode then runs the real 120 ms and 150 ms
// deadlines from here, and the latency report says how often each would be
// missed.
package v2eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/eval"
	"github.com/MemaxLabs/memax/packages/server/eval/livemeter"
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
	qids := map[string]bool{}
	for _, q := range c.Queries {
		if qids[q.ID] {
			t.Errorf("query %s twice", q.ID)
		}
		qids[q.ID] = true
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

// scored is one mode's run over the queries.
type scored struct {
	agg      eval.MetricSet
	slices   []eval.SliceReport
	results  []eval.QueryResult
	fellBack int
	rerank   map[string]int // rerank stage → queries
}

// cachedEmbedder embeds each text once per input type, so the quality
// modes share one embedding per query whatever the network does, and
// records how long each single-text embedding took the first time.
type cachedEmbedder struct {
	inner embed.Embedder
	mu    sync.Mutex
	vecs  map[string][]float64
	took  []time.Duration
}

func newCached(e embed.Embedder) *cachedEmbedder {
	return &cachedEmbedder{inner: e, vecs: map[string][]float64{}}
}

func (c *cachedEmbedder) Embed(texts []string, inputType string) ([][]float64, error) {
	return c.EmbedContext(context.Background(), texts, inputType)
}

func (c *cachedEmbedder) Dimensions() int { return c.inner.Dimensions() }

func (c *cachedEmbedder) EmbedContext(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	var missing []int
	c.mu.Lock()
	for i, t := range texts {
		if v, ok := c.vecs[inputType+"\x00"+t]; ok {
			out[i] = v
		} else {
			missing = append(missing, i)
		}
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	batch := make([]string, len(missing))
	for j, i := range missing {
		batch[j] = texts[i]
	}
	start := time.Now()
	vecs, err := c.inner.EmbedContext(ctx, batch, inputType)
	if err != nil {
		return nil, err
	}
	took := time.Since(start)
	c.mu.Lock()
	defer c.mu.Unlock()
	for j, i := range missing {
		out[i] = vecs[j]
		c.vecs[inputType+"\x00"+texts[i]] = vecs[j]
	}
	if len(batch) == 1 {
		c.took = append(c.took, took)
	}
	return out, nil
}

// noDeadline runs the reranker without the caller's deadline (the query
// path's 150 ms), so the rerank's quality can be scored from a machine far
// from Voyage. Its own HTTP timeout still applies.
type noDeadline struct{ rerank.Reranker }

func (r noDeadline) Rerank(ctx context.Context, query string, docs []rerank.Document) ([]rerank.Result, error) {
	return r.Reranker.Rerank(context.WithoutCancel(ctx), query, docs)
}

// timedReranker records each rerank's latency.
type timedReranker struct {
	rerank.Reranker
	mu   sync.Mutex
	took []time.Duration
}

func (r *timedReranker) Rerank(ctx context.Context, query string, docs []rerank.Document) ([]rerank.Result, error) {
	start := time.Now()
	res, err := r.Reranker.Rerank(ctx, query, docs)
	if err == nil {
		r.mu.Lock()
		r.took = append(r.took, time.Since(start))
		r.mu.Unlock()
	}
	return res, err
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
	scope, err := l.UserScope(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	searchAll := func(s *v2recall.Searcher) scored {
		sc := scored{rerank: map[string]int{}}
		for _, q := range c.Queries {
			start := time.Now()
			res, err := s.Search(ctx, scope, v2recall.Query{Text: q.Query, Filter: v2recall.Filter{Spaces: []uuid.UUID{spaces["mine"], spaces["theirs"]}}, Limit: 10})
			took := time.Since(start)
			if err != nil {
				t.Fatalf("%s: %v", q.ID, err)
			}
			if res.Retrieval.Vector == v2recall.StageTimeout || res.Retrieval.Vector == v2recall.StageError {
				sc.fellBack++
			}
			sc.rerank[res.Retrieval.Rerank]++
			qr := eval.QueryResult{QueryID: q.ID, Query: q.Query, QueryType: q.Type, Slices: []string{q.Type},
				Relevance: q.Relevance, Negative: q.Type == "negative", LatencyMs: took.Milliseconds()}
			for i, h := range res.Hits {
				qr.Results = append(qr.Results, eval.RankedResult{FixtureID: fixtureOf[h.ID], Rank: i + 1, Score: h.Score})
			}
			sc.results = append(sc.results, qr)
		}
		sc.agg, sc.slices = eval.AggregateMetrics(sc.results), eval.AggregateBySlice(sc.results)
		return sc
	}

	live := os.Getenv("V2_EVAL_LIVE") == "1"
	var lv *liveModels
	if live {
		lv = setupLive(t, ctx, c, index)
		modes = append(modes, lv.modes(l)...)
	}

	all := map[string]scored{}
	for _, m := range modes {
		all[m.name] = searchAll(m.search)
	}
	t.Log(modesReport(c, modes, all))

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
	// The floor keeps the vector lane's nonsense out: on a query nothing
	// answers, hybrid returns no more than the lexical lanes alone (which
	// can match a stray word, such as "push" in "push notifications").
	fake := all["hybrid, fake embedder"]
	for i, r := range fake.results {
		if lex := lexical.results[i]; r.Negative && len(r.Results) > len(lex.Results) {
			t.Errorf("fake hybrid: negative query %s returned %d results, lexical %d; the floor should keep nonsense out",
				r.QueryID, len(r.Results), len(lex.Results))
		}
	}
	if fake.fellBack != 0 {
		t.Errorf("fake hybrid fell back to lexical on %d queries", fake.fellBack)
	}
	if !live {
		return
	}

	// The ship gate (§5.11): the default pair doesn't regress against V1's
	// model on the same corpus.
	def, v1 := all[lv.defaultName], all[lv.v1Name]
	if def.agg.NDCG5 < v1.agg.NDCG5-0.01 || def.agg.MRR10 < v1.agg.MRR10-0.01 {
		t.Errorf("%s scores below %s: nDCG@5 %.3f vs %.3f, MRR@10 %.3f vs %.3f", lv.defaultName, lv.v1Name,
			def.agg.NDCG5, v1.agg.NDCG5, def.agg.MRR10, v1.agg.MRR10)
	}
	if lv.rerankName != "" {
		rr := all[lv.rerankName]
		if rr.agg.NDCG5 < def.agg.NDCG5-0.01 || rr.agg.MRR10 < def.agg.MRR10-0.01 {
			t.Errorf("%s scores below %s: nDCG@5 %.3f vs %.3f, MRR@10 %.3f vs %.3f", lv.rerankName, lv.defaultName,
				rr.agg.NDCG5, def.agg.NDCG5, rr.agg.MRR10, def.agg.MRR10)
		}
	}
	t.Log(lv.floorSweep(t, l, searchAll, lexical))
	t.Log(lv.similarityReport(t, ctx, c))
	t.Log(lv.latencyReport(all))
}

// liveModels are the Voyage models and the modes built on them.
type liveModels struct {
	cfg                  v2index.Config
	index, v1, query     embed.Embedder // the index model, V1's model, the query model
	cIndex, cV1, cQuery  *cachedEmbedder
	reranker             rerank.Reranker
	timed                *timedReranker
	defaultName, v1Name  string
	rerankName, prodName string
	vectorCfg            v2recall.VectorConfig
	rerankModel          string
}

const v1Model = "voyage-code-3"

func setupLive(t *testing.T, ctx context.Context, c corpus, index func(embed.Embedder, string)) *liveModels {
	t.Helper()
	cfg := v2index.ConfigFromEnv(os.LookupEnv)
	if !cfg.Enabled() {
		t.Fatal("V2_EVAL_LIVE=1 needs VOYAGE_API_KEY")
	}
	lv := &liveModels{cfg: cfg, index: cfg.IndexEmbedder(), query: cfg.QueryEmbedder(),
		v1: embed.NewVoyage(embed.VoyageConfig{APIKey: cfg.APIKey, Model: v1Model, OutputDimension: ledger.EmbeddingDimensions,
			MaxAttempts: 2, Timeout: 30 * time.Second})}
	for _, m := range []struct {
		e     embed.Embedder
		model string
	}{{lv.index, cfg.IndexModel}, {lv.v1, v1Model}} {
		start := time.Now()
		index(m.e, m.model)
		t.Logf("indexed %d statements with %s in %v", len(c.Memories), m.model, time.Since(start).Round(time.Millisecond))
	}
	// Each query embedded once per model, one request each as on the
	// query path, so the cache's timings are the real per-query latency.
	lv.cIndex, lv.cV1, lv.cQuery = newCached(lv.index), newCached(lv.v1), newCached(lv.query)
	for _, q := range c.Queries {
		for _, e := range []*cachedEmbedder{lv.cQuery, lv.cIndex, lv.cV1} {
			if _, err := e.EmbedContext(ctx, []string{q.Query}, "query"); err != nil {
				t.Fatalf("embed %s: %v", q.ID, err)
			}
		}
	}
	lv.vectorCfg = v2recall.VectorConfigFromEnv(os.LookupEnv, cfg.IndexModel)
	lv.vectorCfg.Log = quiet
	if rr := rerank.NewVoyage(rerank.VoyageConfig{APIKey: cfg.APIKey, Model: os.Getenv("V2_RERANK_MODEL")}); rr != nil {
		lv.reranker, lv.rerankModel = rr, rr.Model()
		lv.timed = &timedReranker{Reranker: rr}
	}
	return lv
}

// quality is the vector config for the quality modes: a 2 s query
// deadline (the embeddings are cached anyway).
func (lv *liveModels) quality(model string, floor float64) v2recall.VectorConfig {
	vc := lv.vectorCfg
	vc.Model, vc.QueryDeadline = model, 2*time.Second
	if floor != 0 {
		vc.Floor = floor
	}
	return vc
}

func (lv *liveModels) modes(l *ledger.Ledger) []mode {
	cfg := lv.cfg
	lv.v1Name = "hybrid, " + v1Model + " (V1's model)"
	lv.defaultName = "hybrid, " + cfg.QueryModel + " → " + cfg.IndexModel
	out := []mode{
		{lv.v1Name, v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cV1, lv.cV1, lv.quality(v1Model, 0)))},
		{"hybrid, " + cfg.IndexModel + " → " + cfg.IndexModel, v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cIndex, lv.cIndex,
			lv.quality(cfg.IndexModel, 0)))},
		{lv.defaultName, v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cQuery, lv.cIndex, lv.quality(cfg.IndexModel, 0)))},
	}
	if lv.reranker != nil {
		lv.rerankName = lv.defaultName + " + " + lv.rerankModel
		out = append(out, mode{lv.rerankName, v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cQuery, lv.cIndex,
			lv.quality(cfg.IndexModel, 0))).WithReranker(noDeadline{lv.timed})})
		// The real deadlines from this machine: uncached query embeddings
		// (a fresh cache) within 120 ms, the rerank within 150 ms.
		lv.prodName = lv.rerankName + ", production deadlines from here"
		prod := lv.vectorCfg
		out = append(out, mode{lv.prodName, v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.query, lv.index, prod)).WithReranker(lv.reranker)})
	}
	return out
}

func modesReport(c corpus, modes []mode, all map[string]scored) string {
	var b strings.Builder
	fmt.Fprintf(&b, "V2 retrieval over %d statements, %d queries (search limit 10)\n", len(c.Memories), len(c.Queries))
	fmt.Fprintf(&b, "%-62s %7s %7s %7s %8s %8s %7s %8s\n", "mode", "nDCG@5", "nDCG@10", "MRR@10", "Recall20", "Harmful", "p50 ms", "fallback")
	for _, m := range modes {
		s := all[m.name]
		lat := make([]int64, len(s.results))
		for i, r := range s.results {
			lat[i] = r.LatencyMs
		}
		slices.Sort(lat)
		fmt.Fprintf(&b, "%-62s %7.3f %7.3f %7.3f %8.3f %8d %7d %8d\n", m.name, s.agg.NDCG5, s.agg.NDCG10, s.agg.MRR10, s.agg.Recall20,
			s.agg.Harmful10, lat[len(lat)/2], s.fellBack)
	}
	b.WriteString("\nnDCG@5 by query type:\n")
	for _, m := range modes {
		fmt.Fprintf(&b, "%-62s", m.name)
		for _, sl := range all[m.name].slices {
			fmt.Fprintf(&b, " %s %.2f (n=%d)", sl.Slice, sl.Metrics.NDCG5, sl.Count)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nrerank stage (queries):\n")
	for _, m := range modes {
		if st := all[m.name].rerank; st[v2recall.StageOff] != len(all[m.name].results) {
			fmt.Fprintf(&b, "%-62s %v\n", m.name, st)
		}
	}
	b.WriteString("\nnegative queries, results returned:\n")
	for _, m := range modes {
		fmt.Fprintf(&b, "%-62s", m.name)
		for _, r := range all[m.name].results {
			if r.Negative {
				fmt.Fprintf(&b, " %s=%d", strings.TrimPrefix(r.QueryID, "neg-"), len(r.Results))
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
		fmt.Fprintf(&b, "%-62s %v\n", m.name, missed)
	}
	return b.String()
}

// floorSweep runs the default pair at each recall floor
// (V2_RECALL_VECTOR_FLOOR): quality, and what the vector lane adds to
// queries nothing answers.
func (lv *liveModels) floorSweep(t *testing.T, l *ledger.Ledger, searchAll func(*v2recall.Searcher) scored, lexical scored) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "recall floor sweep (%s): nDCG@5 / MRR@10 / Recall@20, without and with the reranker; on the negative queries, "+
		"results the vector lane added to lexical's\n", lv.defaultName)
	for _, floor := range []float64{0.2, 0.25, 0.3, 0.33, 0.35, 0.37, 0.4, 0.45, 0.5} {
		s := searchAll(v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cQuery, lv.cIndex, lv.quality(lv.cfg.IndexModel, floor))))
		var added, hitQueries, negs int
		for i, r := range s.results {
			if !r.Negative {
				continue
			}
			negs++
			if d := len(r.Results) - len(lexical.results[i].Results); d > 0 {
				added += d
				hitQueries++
			}
		}
		fmt.Fprintf(&b, "  floor %.2f: %.3f / %.3f / %.3f", floor, s.agg.NDCG5, s.agg.MRR10, s.agg.Recall20)
		if lv.timed != nil {
			r := searchAll(v2recall.New(l).WithVectors(v2recall.NewVectors(l, lv.cQuery, lv.cIndex, lv.quality(lv.cfg.IndexModel, floor))).
				WithReranker(noDeadline{lv.timed}))
			fmt.Fprintf(&b, ", reranked %.3f / %.3f / %.3f", r.agg.NDCG5, r.agg.MRR10, r.agg.Recall20)
		}
		fmt.Fprintf(&b, "; negatives: %d results added on %d of %d queries\n", added, hitQueries, negs)
	}
	return b.String()
}

// similarityReport is the query → statement cosine similarity (query
// model to index model, as the vector lane compares them), by grade, and
// for the negative queries their best match.
func (lv *liveModels) similarityReport(t *testing.T, ctx context.Context, c corpus) string {
	t.Helper()
	var texts []string
	var ids []string
	for _, m := range c.Memories {
		if m.Space == "mine" {
			texts, ids = append(texts, m.Statement), append(ids, m.ID)
		}
	}
	docs, err := lv.index.EmbedContext(ctx, texts, "document")
	if err != nil {
		t.Fatal(err)
	}
	byGrade := map[string][]float64{}
	var negBest []float64
	for _, q := range c.Queries {
		qv, err := lv.cQuery.EmbedContext(ctx, []string{q.Query}, "query")
		if err != nil {
			t.Fatal(err)
		}
		best := -1.0
		for i, d := range docs {
			s := cosine(qv[0], d)
			best = math.Max(best, s)
			if q.Type == "negative" {
				continue
			}
			switch g := q.Relevance[ids[i]]; {
			case g >= 3:
				byGrade["3 (ideal)"] = append(byGrade["3 (ideal)"], s)
			case g == 2:
				byGrade["2"] = append(byGrade["2"], s)
			case g == 1:
				byGrade["1"] = append(byGrade["1"], s)
			default:
				byGrade["0 (ungraded)"] = append(byGrade["0 (ungraded)"], s)
			}
		}
		if q.Type == "negative" {
			negBest = append(negBest, best)
		}
	}
	byGrade["negative query, best match"] = negBest
	var b strings.Builder
	fmt.Fprintf(&b, "query → statement cosine (%s → %s): min / p10 / p25 / median / p75 / p90 / max\n", lv.cfg.QueryModel, lv.cfg.IndexModel)
	for _, k := range []string{"3 (ideal)", "2", "1", "0 (ungraded)", "negative query, best match"} {
		s := slices.Clone(byGrade[k])
		sort.Float64s(s)
		q := func(f float64) float64 { return s[min(len(s)-1, int(f*float64(len(s))))] }
		fmt.Fprintf(&b, "  %-28s n=%4d  %.2f / %.2f / %.2f / %.2f / %.2f / %.2f / %.2f\n", k, len(s), s[0], q(0.1), q(0.25), q(0.5), q(0.75), q(0.9), s[len(s)-1])
	}
	for _, floor := range []float64{0.25, 0.3, 0.35, 0.4, 0.45, 0.5} {
		var ideal, ungraded, negs int
		for _, x := range byGrade["3 (ideal)"] {
			if x >= floor {
				ideal++
			}
		}
		for _, x := range byGrade["0 (ungraded)"] {
			if x >= floor {
				ungraded++
			}
		}
		for _, x := range negBest {
			if x >= floor {
				negs++
			}
		}
		fmt.Fprintf(&b, "  floor %.2f keeps %d/%d ideal matches, %d/%d ungraded pairs, and lets %d/%d negative queries reach a statement\n", floor,
			ideal, len(byGrade["3 (ideal)"]), ungraded, len(byGrade["0 (ungraded)"]), negs, len(negBest))
	}
	return b.String()
}

func (lv *liveModels) latencyReport(all map[string]scored) string {
	var b strings.Builder
	b.WriteString("latency from this machine (the API runs in sjc; Voyage's region decides the real numbers):\n")
	over := func(ds []time.Duration, limit time.Duration) int {
		n := 0
		for _, d := range ds {
			if d > limit {
				n++
			}
		}
		return n
	}
	for _, e := range []struct {
		name string
		c    *cachedEmbedder
	}{{lv.cfg.QueryModel, lv.cQuery}, {lv.cfg.IndexModel, lv.cIndex}, {v1Model, lv.cV1}} {
		fmt.Fprintf(&b, "  query embedding, %s: %s; over the 120 ms deadline %d/%d\n", e.name, livemeter.QuantilesOf(e.c.took),
			over(e.c.took, 120*time.Millisecond), len(e.c.took))
	}
	if lv.timed != nil {
		fmt.Fprintf(&b, "  rerank (%s, no deadline): %s; over the 150 ms deadline %d/%d\n", lv.rerankModel,
			livemeter.QuantilesOf(lv.timed.took), over(lv.timed.took, v2recall.RerankTimeout), len(lv.timed.took))
		p := all[lv.prodName]
		fmt.Fprintf(&b, "  production deadlines from here: %d of %d queries fell back to lexical; rerank stages %v\n", p.fellBack, len(p.results), p.rerank)
	}
	return b.String()
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
