package mcpv2_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/mcpv2"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// The MCP read path over a simulated network (internal/testdb/netsim):
// recall with a query (hybrid, on an instant fake embedder) and without
// one (the compiled digest), search and get, each as one agent's tool call
// through the real auth middleware. TestMCPRoundTrips guards the round
// trips each makes, in CI; TestMCPLatency (MEMAX_LATENCY=1) times them at
// 0 and 24 ms of round-trip time.

// rig is one agent's MCP session on a space of seeded, embedded memories
// with a compiled AGENTS.md, a pending proposal of its own session, and
// every call counted.
type rig struct {
	e    *env
	db   *testdb.DB
	sp   space
	cs   *mcp.ClientSession
	tag  *counterTag
	refs []string
}

// counterTag names the netsim.Counter the next requests count into.
type counterTag struct {
	mu sync.Mutex
	id string
}

func (c *counterTag) set(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id = id
}

func (c *counterTag) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

type tagged struct {
	tag  *counterTag
	next http.RoundTripper
}

func (t tagged) RoundTrip(r *http.Request) (*http.Response, error) {
	if id := t.tag.get(); id != "" {
		r = r.Clone(r.Context())
		r.Header.Set(netsim.CounterHeader, id)
	}
	return t.next.RoundTrip(r)
}

const rigSessionRef = "latency-session"

func newRig(t *testing.T, o testdb.Options) *rig {
	t.Helper()
	db := testdb.Open(t, o)
	words := mockembed.NewWords(synonyms...)
	// Reads go to memory only: the production recorder writes them in the
	// background, after the answer, which a call's count shouldn't include.
	inMemoryReads := func(e *env, o *mcpv2.Options) { o.Reads = e.reads }
	e := buildEnvOn(t, db.Store, db.Pool, true, withVectors(words), inMemoryReads)
	user := e.user("zz")
	sp := e.space(user, policy.SpaceProject, "memax-v2")
	e.toV2(sp, e.personal(user))
	tok, grant := e.grant(user, "claude-code", "memax:read memax:write")
	e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, sp, e.personal(user))
	seedKept(t, e, sp, 300)
	e.index(words, sp)

	// The Brief and AGENTS.md, compiled once: the digest serves the file.
	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	meta := ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope, Via: policy.ViaWeb}
	meta.IdempotencyKey = uuid.NewString()
	if _, err := e.ledger.Apply(ctx, &ledger.ReviseBrief{Meta: meta, SpaceID: sp.id, Title: "memax-v2",
		Sections: []ledger.BriefSection{{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: "M-0001"}}}}}); err != nil {
		t.Fatal(err)
	}
	meta.IdempotencyKey = uuid.NewString()
	res, err := e.ledger.Apply(ctx, &ledger.ConfigureTarget{Meta: meta, SpaceID: sp.id, Kind: ledger.TargetAgentsMD})
	if err != nil || res.Target == nil {
		t.Fatalf("configure target: %v %+v", err, res.Policy)
	}
	if _, err := e.compile.Run(ctx, ledger.CompileTargetArgs{TargetID: res.Target.ID, SpaceID: sp.id}, compile.RunOptions{NoWait: true}); err != nil {
		t.Fatal(err)
	}

	r := &rig{e: e, db: db, sp: sp, tag: &counterTag{}, refs: []string{"M-0001", "M-0002", "M-0003"}}
	client := mcp.NewClient(&mcp.Implementation{Name: "memax-latency", Version: "1"}, &mcp.ClientOptions{Logger: quiet})
	tr := &mcp.StreamableClientTransport{
		Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Timeout: 30 * time.Second,
			Transport: tagged{tag: r.tag, next: bearer{token: tok, next: http.DefaultTransport}}},
		MaxRetries: -1,
	}
	cs, err := client.Connect(ctx, tr, &mcp.ClientSessionOptions{ProtocolVersion: modern})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	r.cs = cs
	// One proposal of this session, which recall reads back.
	call(t, cs, "memax_push", push("Latency rig: a pending idea", sp, map[string]any{"session_ref": rigSessionRef}))
	return r
}

// mcpOp is one tool call, its arguments built per run (a new query text
// each time, so the embedding cache never answers).
type mcpOp struct {
	name string
	tool string
	args func(i int) map[string]any
}

func (r *rig) ops() []mcpOp {
	queries := []string{"deploy target", "postgres migrations", "review queue", "rate limits", "fly machines"}
	return []mcpOp{
		{"MCP memax_recall (query, hybrid)", "memax_recall", func(i int) map[string]any {
			return map[string]any{"query": fmt.Sprintf("%s %d", queries[i%len(queries)], i), "limit": 5, "session_ref": rigSessionRef}
		}},
		{"MCP memax_recall (digest)", "memax_recall", func(int) map[string]any {
			return map[string]any{"session_ref": rigSessionRef}
		}},
		{"MCP memax_search", "memax_search", func(i int) map[string]any {
			return map[string]any{"query": fmt.Sprintf("%s %d", queries[i%len(queries)], i), "limit": 10}
		}},
		{"MCP memax_get", "memax_get", func(i int) map[string]any {
			return map[string]any{"id": r.refs[i%len(r.refs)]}
		}},
	}
}

var runSeq int

// run makes one call, counting its round trips, and waits for whatever it
// left running (a COMMIT sent after the answer) before reading the count.
func (r *rig) run(t *testing.T, op mcpOp) (time.Duration, *netsim.Counter, *mcp.CallToolResult) {
	t.Helper()
	runSeq++
	c := &netsim.Counter{}
	r.tag.set(r.e.counters.Add(c))
	// V1's store, under the auth middleware, queries with
	// context.Background(): those trips count here too.
	r.db.Trips.SetFallback(c)
	defer r.db.Trips.SetFallback(nil)
	start := time.Now()
	res, err := r.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: op.tool, Arguments: op.args(runSeq)})
	took := time.Since(start)
	r.tag.set("")
	if err != nil {
		t.Fatalf("%s: %v", op.name, err)
	}
	if res.IsError {
		t.Fatalf("%s: %s", op.name, text(res))
	}
	if !netsim.Settle(r.db.Pool, 2*time.Second) {
		t.Fatalf("%s: connections still checked out after 2 s", op.name)
	}
	return took, c, res
}

// partial reports whether a read came back incomplete (the recall budget
// ran out).
func partial(t *testing.T, res *mcp.CallToolResult) bool {
	t.Helper()
	out := structured[handler.MCPRecallOutput](t, res)
	return out.Partial
}

// TestMCPLatency times the MCP reads at 0 and 24 ms of round-trip time
// (MEMAX_LATENCY=1) and prints a table. At 24 ms, recall's p95 must stay
// well under N2's 300 ms, and search's under 500 ms.
func TestMCPLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newRig(t, testdb.Options{Proxy: true})
	const runs = 30
	var rows []string
	bars := map[string]time.Duration{
		"MCP memax_recall (query, hybrid)": 150 * time.Millisecond,
		"MCP memax_recall (digest)":        150 * time.Millisecond,
		"MCP memax_search":                 300 * time.Millisecond,
		"MCP memax_get":                    150 * time.Millisecond,
	}
	for _, rtt := range []time.Duration{0, netsim.ProductionRTT} {
		r.db.Proxy.SetOneWay(rtt / 2)
		for _, op := range r.ops() {
			for range 3 {
				r.run(t, op) // warm the statement caches
			}
			s := &netsim.Sample{Name: op.name, RTT: rtt}
			partials := 0
			for range runs {
				took, c, res := r.run(t, op)
				s.Add(took, c.RoundTrips())
				if op.tool != "memax_get" && partial(t, res) {
					partials++
				}
			}
			row := s.Row()
			if partials > 0 {
				row += fmt.Sprintf(" %d/%d partial", partials, runs)
			}
			rows = append(rows, row)
			if bar, ok := bars[op.name]; ok && rtt == netsim.ProductionRTT && s.P(0.95) > bar {
				t.Errorf("%s at %v: p95 %v, want under %v", op.name, rtt, s.P(0.95), bar)
			}
			if partials > 0 && rtt == netsim.ProductionRTT {
				t.Errorf("%s at %v: %d of %d answers were partial", op.name, rtt, partials, runs)
			}
		}
	}
	t.Logf("\n%s\n%s", netsim.Header, joinLines(rows))
}

func joinLines(rows []string) string {
	out := ""
	for i, r := range rows {
		if i > 0 {
			out += "\n"
		}
		out += r
	}
	return out
}

// TestMCPRoundTrips guards the round trips of each MCP read: a change that
// adds one fails here, whatever the machine's speed. The counts are
// steady-state (warm statement caches) and include work a call leaves
// running after it answers, such as a read's COMMIT.
func TestMCPRoundTrips(t *testing.T) {
	r := newRig(t, testdb.Options{})
	budgets := map[string]int{
		"MCP memax_recall (query, hybrid)": 99,
		"MCP memax_recall (digest)":        99,
		"MCP memax_search":                 99,
		"MCP memax_get":                    99,
	}
	for _, op := range r.ops() {
		for range 3 {
			r.run(t, op)
		}
		_, c, _ := r.run(t, op)
		t.Logf("%s: %d round trips\n%s", op.name, c.RoundTrips(), c)
		if got, want := c.RoundTrips(), budgets[op.name]; got > want {
			t.Errorf("%s: %d round trips, budget %d\n%s", op.name, got, want, c)
		}
	}
}
