package mcpv2_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
// through the real auth middleware. Recall and search run twice: across
// every space the agent reaches, and in its V2 space alone (hub_id or
// space_id), which is N2's path (an unscoped call also runs V1's recall
// for spaces not on V2, and waits for it). TestMCPRoundTrips guards the
// round trips each makes, in CI; TestMCPLatency (MEMAX_LATENCY=1) times
// them at 0 and 24 ms of round-trip time.

// counted is an MCP session whose calls can be counted: each request
// carries the netsim.Counter its tag names (env.counters).
type counted struct {
	e   *env
	cs  *mcp.ClientSession
	tag *counterTag
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

// countedClient opens a modern MCP session for token whose calls count.
func (e *env) countedClient(token string) *counted {
	e.t.Helper()
	c := &counted{e: e, tag: &counterTag{}}
	client := mcp.NewClient(&mcp.Implementation{Name: "memax-latency", Version: "1"}, &mcp.ClientOptions{Logger: quiet})
	tr := &mcp.StreamableClientTransport{
		Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Timeout: 30 * time.Second,
			Transport: tagged{tag: c.tag, next: bearer{token: token, next: http.DefaultTransport}}},
		MaxRetries: -1,
	}
	cs, err := client.Connect(context.Background(), tr, &mcp.ClientSessionOptions{ProtocolVersion: modern})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = cs.Close() })
	c.cs = cs
	return c
}

// call makes one call and returns its result, how long it took, and the
// round trips its request made (those its context carries: V2's reads
// and writes, not V1's store under the auth middleware). With fallback,
// trips made with no counter during the call (V1's store) count too; use
// it only while nothing else runs on the pool. It waits for work the call
// left running (a COMMIT sent after the answer) before counting.
func (c *counted) call(t *testing.T, tool string, args map[string]any, fallback bool) (*mcp.CallToolResult, time.Duration, *netsim.Counter) {
	t.Helper()
	cnt := &netsim.Counter{}
	c.tag.set(c.e.counters.Add(cnt))
	if tr := netsim.Of(c.e.pool); fallback && tr != nil {
		tr.SetFallback(cnt)
		defer tr.SetFallback(nil)
	}
	start := time.Now()
	res, err := c.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	took := time.Since(start)
	c.tag.set("")
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("%s: %s", tool, text(res))
	}
	if !netsim.Settle(c.e.pool, 2*time.Second) {
		t.Fatalf("%s: connections still checked out after 2 s", tool)
	}
	return res, took, cnt
}

// rig is one agent's MCP session on a space of seeded, embedded memories
// with a compiled AGENTS.md, a pending proposal of its own session, and
// every call counted.
type rig struct {
	e    *env
	db   *testdb.DB
	sp   space
	c    *counted
	refs []string
	runs int
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

	r := &rig{e: e, db: db, sp: sp, c: e.countedClient(tok), refs: []string{"M-0001", "M-0002", "M-0003"}}
	// One proposal of this session, which recall reads back.
	r.c.call(t, "memax_push", push("Latency rig: a pending idea", sp, map[string]any{"session_ref": rigSessionRef}), false)
	return r
}

// mcpOp is one tool call, its arguments built per run (a new query text
// each time, so the embedding cache never answers).
type mcpOp struct {
	name string
	tool string
	args func(i int) map[string]any
	// partial: the answer says whether it ran out of its budget.
	partial bool
}

const (
	opRecall      = "MCP memax_recall (query, hybrid)"
	opRecallSpace = "MCP memax_recall (query, in the space)"
	opDigest      = "MCP memax_recall (digest)"
	opSearch      = "MCP memax_search"
	opSearchSpace = "MCP memax_search (in the space)"
	opGet         = "MCP memax_get"
)

func (r *rig) ops() []mcpOp {
	queries := []string{"deploy target", "postgres migrations", "review queue", "rate limits", "fly machines"}
	q := func(i int) string { return fmt.Sprintf("%s %d", queries[i%len(queries)], i) }
	in := r.sp.id.String()
	return []mcpOp{
		{opRecall, "memax_recall", func(i int) map[string]any {
			return map[string]any{"query": q(i), "limit": 5, "session_ref": rigSessionRef}
		}, true},
		{opRecallSpace, "memax_recall", func(i int) map[string]any {
			return map[string]any{"query": q(i), "limit": 5, "session_ref": rigSessionRef, "hub_id": in}
		}, true},
		{opDigest, "memax_recall", func(int) map[string]any {
			return map[string]any{"session_ref": rigSessionRef}
		}, true},
		{opSearch, "memax_search", func(i int) map[string]any {
			return map[string]any{"query": q(i), "limit": 10}
		}, true},
		{opSearchSpace, "memax_search", func(i int) map[string]any {
			return map[string]any{"query": q(i), "limit": 10, "space_id": in}
		}, true},
		{opGet, "memax_get", func(i int) map[string]any {
			return map[string]any{"id": r.refs[i%len(r.refs)]}
		}, false},
	}
}

// run makes one call, counting every round trip in its window (V1's store
// under the auth middleware included).
func (r *rig) run(t *testing.T, op mcpOp) (time.Duration, *netsim.Counter, *mcp.CallToolResult) {
	t.Helper()
	r.runs++
	res, took, cnt := r.c.call(t, op.tool, op.args(r.runs), true)
	return took, cnt, res
}

// partial reports whether a read came back incomplete (the recall budget
// ran out).
func partial(t *testing.T, res *mcp.CallToolResult) bool {
	t.Helper()
	out := structured[handler.MCPRecallOutput](t, res)
	return out.Partial
}

// TestMCPLatency times the MCP reads at 0 and 24 ms of round-trip time
// (MEMAX_LATENCY=1) and prints a table. At 24 ms, recall in a space on V2
// (N2's path) must stay well under N2's 300 ms p95, and search under its
// 500 ms; recall's aim of 150 ms is reported, not enforced (it needs the
// API and the database closer than 24 ms; AGENTS.md, "Round trips").
func TestMCPLatency(t *testing.T) {
	if !netsim.LatencyOn() {
		t.Skipf("wall-clock latency: set %s=1", netsim.LatencyEnv)
	}
	r := newRig(t, testdb.Options{Proxy: true})
	const runs = 30
	var rows []string
	bars := map[string]time.Duration{
		opRecallSpace: 275 * time.Millisecond,
		opDigest:      300 * time.Millisecond,
		opSearchSpace: 500 * time.Millisecond,
		opGet:         300 * time.Millisecond,
	}
	aims := map[string]time.Duration{opRecallSpace: 150 * time.Millisecond}
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
				if op.partial && partial(t, res) {
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
			if aim, ok := aims[op.name]; ok && rtt == netsim.ProductionRTT && s.P(0.95) > aim {
				t.Logf("%s at %v: p95 %v misses the %v aim", op.name, rtt, s.P(0.95), aim)
			}
			if partials > 0 && rtt == netsim.ProductionRTT {
				t.Errorf("%s at %v: %d of %d answers were partial", op.name, rtt, partials, runs)
			}
		}
	}
	t.Logf("\n%s\n%s", netsim.Header, strings.Join(rows, "\n"))
}

// TestMCPRoundTrips guards the round trips of each MCP read: a change that
// adds one fails here, whatever the machine's speed. A count is every
// round trip in the call's window (V1's, under the auth middleware and
// beside an unscoped recall, included), steady-state (warm statement
// caches), with what the call left running after it answered (a read's
// COMMIT). The wire audit checks every statement the reads send: none
// touches a v2 table before its transaction's scope.
func TestMCPRoundTrips(t *testing.T) {
	audit := netsim.NewAudit(ledger.DBRole)
	r := newRig(t, testdb.Options{Watch: audit.Observe})
	audit.Arm()
	defer audit.Require(t)
	// Before the pipelined ledger (Oct 7, 2026) these were 46, 39, 42, 30,
	// 23 and 23.
	budgets := map[string]int{
		opRecall:      24,
		opRecallSpace: 17,
		opDigest:      19,
		opSearch:      21,
		opSearchSpace: 14,
		opGet:         13,
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
