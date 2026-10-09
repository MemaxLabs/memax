package mcpv2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/mcpv2"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

const embedModel = "voyage-4"

var synonyms = [][]string{{"river", "queue"}, {"job", "task"}, {"background", "async"}}

// withVectors serves recall and search with the hybrid searcher on query
// (the query embedder) over embeddings of embedModel.
func withVectors(query embed.Embedder) envOption {
	return func(e *env, o *mcpv2.Options) {
		v := v2recall.NewVectors(e.ledger, query, query, v2recall.VectorConfig{Model: embedModel, Log: quiet})
		o.Search = v2recall.New(e.ledger).WithVectors(v)
	}
}

// newVectorFixture is newFixture on a server with the hybrid searcher.
func newVectorFixture(t *testing.T, query embed.Embedder) (*env, fixture) {
	t.Helper()
	e := buildEnv(t, true, withVectors(query))
	user := e.user("zz")
	sp := e.space(user, policy.SpaceProject, "memax-v2")
	e.toV2(sp)
	tok, grant := e.grant(user, "claude-code", "memax:read memax:write")
	e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, sp, e.personal(user))
	return e, fixture{e: e, user: user, sp: sp, token: tok}
}

// index embeds every waiting version of the space, as the index jobs do.
func (e *env) index(emb embed.Embedder, sp space) {
	e.t.Helper()
	ix := v2index.New(e.ledger, emb, embedModel, 512, quiet)
	for {
		n, err := ix.Index(context.Background(), ledger.IndexArgs{SpaceID: sp.id})
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// retrievalMeta is a result's _meta["app.memax/retrieval"].
func retrievalMeta(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	r, ok := retrievalOf(t, res)
	if !ok {
		b, _ := json.Marshal(res.Meta)
		t.Fatalf("no %s in _meta %s", mcpv2.MetaRetrieval, b)
	}
	return r
}

// recallBudget is the server's deadline on a recall's V2 part (search has
// twice it): the least a read that ran out of time took.
const recallBudget = 250 * time.Millisecond

// retrievalOf is res's retrieval, if it has one.
func retrievalOf(t *testing.T, res *mcp.CallToolResult) (map[string]any, bool) {
	t.Helper()
	b, _ := json.Marshal(res.Meta)
	var meta map[string]map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("_meta %s: %v", b, err)
	}
	r, ok := meta[mcpv2.MetaRetrieval]
	return r, ok
}

// Recall and search find by meaning over MCP, say so (lexical_only false,
// _meta vector ok), and still match their output schemas.
func TestRecallByMeaning(t *testing.T) {
	words := mockembed.NewWords(synonyms...)
	e, f := newVectorFixture(t, words)
	m := e.keep(f.user, f.sp, "Background jobs run on River.", ledger.SectionDecisions)
	e.keep(f.user, f.sp, "The web app is a Next.js project.", ledger.SectionConventions)
	e.index(words, f.sp)
	cs := e.connectClient(f.token, "/mcp", modern, nil)
	for _, tool := range []string{"memax_recall", "memax_search"} {
		args := map[string]any{"query": "Which queue handles async tasks?", "hub_id": f.sp.id.String(), "space_id": f.sp.id.String()}
		res := call(t, cs, tool, args)
		validates(t, "agent", tool, res)
		out := structured[handler.MCPRecallOutput](t, res)
		if len(out.Results) != 1 || out.Results[0].Ref != m.Ref || out.LexicalOnly {
			t.Fatalf("%s: %+v (lexical_only %v)\n%s", tool, out.Results, out.LexicalOnly, text(res))
		}
		if r := retrievalMeta(t, res); r["vector"] != v2recall.StageOK || r["lexical_only"] != false || r["rerank"] != v2recall.StageOff {
			t.Errorf("%s _meta = %v", tool, r)
		}
	}
	// The digest (no query) carries no retrieval: nothing was searched.
	res := call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})
	if _, ok := res.Meta[mcpv2.MetaRetrieval]; ok {
		t.Errorf("the digest carries a retrieval: %v", res.Meta)
	}
}

// A query embedding past its 120 ms deadline answers lexically, on time,
// flagged in _meta and in lexical_only; the answer isn't partial.
func TestRecallFallsBackLexically(t *testing.T) {
	slow := mockembed.New()
	slow.SetDelay(time.Second)
	e, f := newVectorFixture(t, slow)
	m := e.keep(f.user, f.sp, "Deploy the API to Fly machines.", ledger.SectionDecisions)
	cs := e.connectClient(f.token, "/mcp", modern, nil)
	start := time.Now()
	res := call(t, cs, "memax_recall", map[string]any{"query": "deploy API", "hub_id": f.sp.id.String()})
	took := time.Since(start)
	out := structured[handler.MCPRecallOutput](t, res)
	if len(out.Results) != 1 || out.Results[0].Ref != m.Ref || !out.LexicalOnly || out.Partial {
		t.Fatalf("results %+v lexical_only %v partial %v", out.Results, out.LexicalOnly, out.Partial)
	}
	if r := retrievalMeta(t, res); r["vector"] != v2recall.StageTimeout || r["lexical_only"] != true {
		t.Errorf("_meta = %v", r)
	}
	if took > 300*time.Millisecond {
		t.Errorf("recall took %v with a 1 s embedder; the deadline is 120 ms", took)
	}
}

// Recall and search over 3,000 kept memories, each embedded: lexical only
// (as before vectors), hybrid with an instant fake embedder, with 60 ms of
// embedding latency (about Voyage's), and with an embedder past the
// deadline (the fallback).
//
// Two guards. Every call's round trips to Postgres are counted (netsim),
// and each case has a budget: a change that adds one fails here on any
// machine. Wall-clock p95 has bars tighter than N2 (recall under 300 ms,
// search under 500 ms), held by the best of three rounds: a shared machine
// (CI, or a full suite beside the web tests) has outliers that aren't
// recall's, while a real regression fails all three.
func TestHybridLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("latency: skipped in -short")
	}
	words := mockembed.NewWords(synonyms...)
	delayed := &delayedWords{Words: words}
	e, f := newVectorFixture(t, delayed)
	const n = 3000
	seedKept(t, e, f.sp, n)
	start := time.Now()
	e.index(words, f.sp)
	t.Logf("indexed %d memories in %v", n, time.Since(start).Round(time.Millisecond))
	if got := e.count(`SELECT count(*) FROM v2.memory_embeddings WHERE space_id = $1`, f.sp.id); got != n {
		t.Fatalf("%d embeddings, want %d", got, n)
	}
	lexical := buildEnvSharing(t, e)
	queries := []string{"deploy target", "postgres migrations", "review queue", "rate limits", "lighthouse", "fly machines",
		"session ref", "compile budget", "staging database", "token audience"}

	run := 0
	// round makes 40 calls and returns their p95, failing on any call over
	// the round-trip budget.
	round := func(name string, c *counted, tool string, delay time.Duration, wantVector string, budget int) time.Duration {
		delayed.delay = delay
		run++
		var took []time.Duration
		trips := 0
		for i := range 40 {
			// A new text every call, so the embedding cache never answers.
			q := fmt.Sprintf("%s %d-%d", queries[i%len(queries)], run, i)
			args := map[string]any{"query": q, "limit": 10, "hub_id": f.sp.id.String(), "space_id": f.sp.id.String()}
			res, d, cnt := c.call(t, tool, args, false)
			took = append(took, d)
			trips = max(trips, cnt.RoundTrips())
			if wantVector != "" {
				if r, ok := retrievalOf(t, res); ok {
					if r["vector"] != wantVector {
						t.Fatalf("%s %s: vector %v, want %s", name, tool, r["vector"], wantVector)
					}
				} else if !partial(t, res) || d < recallBudget {
					t.Fatalf("%s %s: no %s in _meta of an answer that didn't run out of time", name, tool, mcpv2.MetaRetrieval)
				}
				// Else the search ran out of the recall budget on a loaded
				// machine and answered partial, without a retrieval: a slow
				// call, which the p95 counts.
			}
			if got := cnt.RoundTrips(); got > budget {
				t.Fatalf("%s %s: %d round trips to Postgres, budget %d\n%s", name, tool, got, budget, cnt)
			}
		}
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		p95 := took[len(took)*95/100]
		t.Logf("%-28s %-12s p50 %6.1f ms  p95 %6.1f ms  max %6.1f ms  %d round trips", name, tool,
			ms(took[len(took)/2]), ms(p95), ms(took[len(took)-1]), trips)
		return p95
	}
	// bestOf3 is the best p95 of up to three rounds, stopping at one under
	// the bar.
	bestOf3 := func(name string, c *counted, tool string, delay time.Duration, wantVector string, budget int, bar time.Duration) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 3 {
			best = min(best, round(name, c, tool, delay, wantVector, budget))
			if best <= bar {
				break
			}
		}
		return best
	}
	lcs := lexical.countedClient(f.token)
	hcs := e.countedClient(f.token)
	for _, c := range []struct {
		name       string
		c          *counted
		delay      time.Duration
		wantVector string
		// The round trips a call makes in the space on V2: the principal
		// (scope, connection), the hub, the lanes, the vector lane with its
		// hits, the session's proposals and notices, the gates' news, and
		// the reads' COMMITs sent after the answer.
		recallTrips, searchTrips int
		recallBar, searchBar     time.Duration
	}{
		{"lexical only (before)", lcs, 0, "", 10, 7, 150 * time.Millisecond, 250 * time.Millisecond},
		{"hybrid, instant embedder", hcs, 0, v2recall.StageOK, 12, 9, 150 * time.Millisecond, 250 * time.Millisecond},
		{"hybrid, 60 ms embedder", hcs, 60 * time.Millisecond, v2recall.StageOK, 12, 9, 250 * time.Millisecond, 400 * time.Millisecond},
		{"hybrid, 400 ms embedder", hcs, 400 * time.Millisecond, v2recall.StageTimeout, 10, 7, 250 * time.Millisecond, 400 * time.Millisecond},
	} {
		if p95 := bestOf3(c.name, c.c, "memax_recall", c.delay, c.wantVector, c.recallTrips, c.recallBar); p95 > c.recallBar {
			t.Errorf("%s: recall p95 %v in its best of three rounds, want under %v (N2: 300 ms)", c.name, p95, c.recallBar)
		}
		if p95 := bestOf3(c.name, c.c, "memax_search", c.delay, c.wantVector, c.searchTrips, c.searchBar); p95 > c.searchBar {
			t.Errorf("%s: search p95 %v in its best of three rounds, want under %v (500 ms)", c.name, p95, c.searchBar)
		}
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// delayedWords is the Words embedder with a settable delay, honouring its
// context as Voyage's client does.
type delayedWords struct {
	*mockembed.Words
	delay time.Duration
}

func (d *delayedWords) EmbedContext(ctx context.Context, texts []string, inputType string) ([][]float64, error) {
	if d.delay > 0 {
		t := time.NewTimer(d.delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	return d.Words.EmbedContext(ctx, texts, inputType)
}

// buildEnvSharing is a second, lexical-only server on e's database, so the
// same data answers without vectors.
func buildEnvSharing(t *testing.T, e *env) *env {
	t.Helper()
	return buildEnvOn(t, e.st, e.pool, true)
}
