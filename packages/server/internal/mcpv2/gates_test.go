package mcpv2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func decision(sp space) map[string]any {
	return map[string]any{
		"question": "Which deploy target should the v2 API use?",
		"options":  []string{"Fly.io", "Railway", "Decide later"},
		"context":  "M-0174 (Railway) and M-0431 (Fly.io) disagree.",
		"space_id": sp.id.String(),
	}
}

// answerAs answers a gate as the person, on the web.
func (e *env) answerAs(user uuid.UUID, sp space, ref string, option int) *ledger.Memory {
	e.t.Helper()
	ctx := context.Background()
	scope, err := e.ledger.UserScope(ctx, user)
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.ledger.Apply(ctx, &ledger.AnswerGate{
		Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: scope.Narrow(sp.id), Via: policy.ViaWeb,
			IdempotencyKey: uuid.NewString()},
		Gate: ref, Option: option,
	})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		e.t.Fatalf("answer %s: %v %+v", ref, err, res.Policy)
	}
	return res.Memory
}

func gateOf(t *testing.T, res *mcp.CallToolResult) handler.MCPGate {
	t.Helper()
	if res.IsError {
		t.Fatalf("memax_request_decision: %s", text(res))
	}
	g := structured[handler.MCPGate](t, res)
	if _, _, ok := ledger.ParseRef(g.ID); !ok || !strings.HasPrefix(g.ID, "G-") {
		t.Fatalf("returned %q, not a gate ID: %s", g.ID, text(res))
	}
	return g
}

// In a space on V2 the agent's question is a G- gate, returned at once
// with where a person answers it; asking again is the same gate.
func TestRequestDecisionAsksAGate(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	for _, version := range []string{older, legacy} {
		t.Run(version, func(t *testing.T) {
			cs := e.connectClient(f.token, "/mcp", version, nil)
			args := decision(f.sp)
			args["question"] = "Which deploy target should the v2 API use (" + version + ")?"
			g := gateOf(t, call(t, cs, "memax_request_decision", args))
			if g.Status != "waiting" || g.SpaceID != f.sp.id.String() || g.URL != "https://memax.test/"+f.sp.slug+"/review?ref="+g.ID {
				t.Errorf("gate = %+v", g)
			}
			res := call(t, cs, "memax_request_decision", args)
			mustContain(t, text(res), "Asked in memax-v2 as "+g.ID, "A person answers it in Review", "next memax_recall")
			if again := gateOf(t, res); again.ID != g.ID {
				t.Errorf("a retried call asked again: %s then %s", g.ID, again.ID)
			}
			rc := e.receipts(f.sp, g.ID)
			if len(rc) != 1 || rc[0].Action != "asked" || rc[0].ActorKind != "agent" || rc[0].Agent != "claude-code" || rc[0].Via != "mcp" {
				t.Errorf("receipts = %+v", rc)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM board_slots`); n != 0 {
		t.Errorf("%d board cards written in a space on V2", n)
	}
	if n := e.count(`SELECT count(*) FROM v2.decision_gates WHERE space_id = $1`, f.sp.id); n != 2 {
		t.Errorf("%d gates, want 2", n)
	}
}

// What a read-only agent, a malformed request and an unknown space get.
func TestRequestDecisionRefusals(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyRead)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	res := call(t, cs, "memax_request_decision", decision(f.sp))
	if !res.IsError {
		t.Fatalf("a read-only agent asked: %s", text(res))
	}
	mustContain(t, text(res), "read-only in memax-v2")
	bad := decision(f.sp)
	bad["options"] = []string{"Only one"}
	if res := call(t, cs, "memax_request_decision", bad); !res.IsError || !strings.Contains(text(res), "2-4 'options'") {
		t.Errorf("one option: %s", text(res))
	}
	unknown := decision(f.sp)
	unknown["space_id"] = uuid.NewString()
	if res := call(t, cs, "memax_request_decision", unknown); !res.IsError || !strings.Contains(text(res), "Space not found") {
		t.Errorf("an unknown space: %s", text(res))
	}
	if n := e.count(`SELECT count(*) FROM v2.decision_gates`); n != 0 {
		t.Errorf("%d gates from refused calls", n)
	}
}

// The answer reaches the connection that asked in its next recall, once;
// a gate still waiting is in each digest; another agent hears nothing.
func TestGateAnswerReachesRecallOnce(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	answered := gateOf(t, call(t, cs, "memax_request_decision", decision(f.sp)))
	waitingArgs := decision(f.sp)
	waitingArgs["question"] = "Should the CLI keep the old import command?"
	waiting := gateOf(t, call(t, cs, "memax_request_decision", waitingArgs))
	m := e.answerAs(f.user, f.sp, answered.ID, 2)

	recall := func(args map[string]any) handler.MCPRecallOutput {
		t.Helper()
		res := call(t, cs, "memax_recall", args)
		validates(t, "agent", "memax_recall", res)
		return structured[handler.MCPRecallOutput](t, res)
	}
	res := call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})
	validates(t, "agent", "memax_recall", res)
	out := structured[handler.MCPRecallOutput](t, res)
	if len(out.Gates) != 2 {
		t.Fatalf("gates = %+v", out.Gates)
	}
	if g := out.Gates[0]; g.ID != answered.ID || g.Status != "answered" || g.Option != 2 || g.Answer != "Railway" || g.Memory != m.Ref {
		t.Errorf("the answer = %+v", g)
	}
	if g := out.Gates[1]; g.ID != waiting.ID || g.Status != "waiting" {
		t.Errorf("the waiting gate = %+v", g)
	}
	mustContain(t, text(res), "Decisions you asked for:", answered.ID+" (memax-v2) answered: Railway. Kept as "+m.Ref, waiting.ID+" (memax-v2) is still waiting")

	// Told once; the waiting one stays in the digest, not in a search-style recall.
	again := recall(map[string]any{"hub_id": f.sp.id.String()})
	if len(again.Gates) != 1 || again.Gates[0].ID != waiting.ID {
		t.Errorf("second digest gates = %+v", again.Gates)
	}
	if q := recall(map[string]any{"query": "deploy target", "hub_id": f.sp.id.String()}); len(q.Gates) != 0 {
		t.Errorf("a query recall lists waiting gates: %+v", q.Gates)
	}
	// The decision is kept, so recall finds it like any kept memory.
	found := recall(map[string]any{"query": "Railway deploy", "hub_id": f.sp.id.String()})
	if len(found.Results) == 0 || found.Results[0].Ref != m.Ref || found.Results[0].Kind != "decision" {
		t.Errorf("recall of the decision = %+v", found.Results)
	}
	// Another agent of the same person doesn't hear about it.
	tok2, grant2 := e.grant(f.user, "codex", "memax:read memax:write")
	e.connect(f.user, grant2, ledger.AgentCodex, policy.AutonomyPropose, f.sp)
	other := e.connectClient(tok2, "/mcp", older, nil)
	if o := structured[handler.MCPRecallOutput](t, call(t, other, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})); len(o.Gates) != 0 {
		t.Errorf("another agent heard %+v", o.Gates)
	}
}

// On 2026-07-28 clients that can elicit, the person answers in the agent:
// the answer is theirs via the agent, client-attested, kept as their
// decision, and not repeated in the next recall.
func TestGateAnsweredInTheAgent(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	el := &elicitor{answer: func(*mcp.ElicitParams) *mcp.ElicitResult {
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "1"}}
	}}
	cs := e.connectClient(f.token, "/mcp", modern, el)
	g := gateOf(t, call(t, cs, "memax_request_decision", decision(f.sp)))
	if len(el.asked) != 1 {
		t.Fatalf("elicitations = %d, want 1", len(el.asked))
	}
	mustContain(t, el.asked[0].Message, "Claude Code asks in memax-v2 ("+g.ID+")", "Which deploy target", "M-0174")
	if g.Status != "answered" || g.Option != 1 || g.Answer != "Fly.io" || g.Memory == "" {
		t.Fatalf("gate = %+v", g)
	}
	lc, _, statement := e.memory(f.sp, g.Memory)
	if lc != "kept" || statement != "Which deploy target should the v2 API use? Fly.io" {
		t.Errorf("decision = %s %q", lc, statement)
	}
	rc := e.receipts(f.sp, g.Memory)
	if len(rc) != 1 || rc[0].Action != "kept" || rc[0].ActorKind != "person" || rc[0].Agent != "claude-code" || rc[0].Via != "mcp" || rc[0].Assurance != "client_attested" {
		t.Errorf("decision receipts = %+v", rc)
	}
	if gr := e.receipts(f.sp, g.ID); len(gr) != 2 || gr[1].Action != "answered" || gr[1].Assurance != "client_attested" {
		t.Errorf("gate receipts = %+v", gr)
	}
	if out := structured[handler.MCPRecallOutput](t, call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})); len(out.Gates) != 0 {
		t.Errorf("the answer was told twice: %+v", out.Gates)
	}
}

// Later, decline or cancel leave the gate waiting for a person in Memax.
func TestGateLeftForLater(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	answers := map[string]func(*mcp.ElicitParams) *mcp.ElicitResult{
		"later": func(*mcp.ElicitParams) *mcp.ElicitResult {
			return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "later"}}
		},
		"decline": decline,
		"cancel":  func(*mcp.ElicitParams) *mcp.ElicitResult { return &mcp.ElicitResult{Action: "cancel"} },
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			el := &elicitor{answer: answer}
			cs := e.connectClient(f.token, "/mcp", modern, el)
			args := decision(f.sp)
			args["question"] = "Which queue for " + name + "?"
			res := call(t, cs, "memax_request_decision", args)
			g := gateOf(t, res)
			if len(el.asked) != 1 || g.Status != "waiting" {
				t.Errorf("asked %d, gate %+v", len(el.asked), g)
			}
			mustContain(t, text(res), "Asked in memax-v2 as "+g.ID)
		})
	}
}

// D15: in a team space the question isn't put to the person in the agent,
// and an API key's agent isn't asked either.
func TestGateIsNotAskedInAgentWhereItCantBeAnswered(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	team := e.space(user, policy.SpaceTeam, "team")
	e.toV2(team)
	tok, grant := e.grant(user, "claude-code", "memax:read memax:write")
	e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, team)
	el := &elicitor{answer: accept("1", "")}
	cs := e.connectClient(tok, "/mcp", modern, el)
	res := call(t, cs, "memax_request_decision", decision(team))
	if g := gateOf(t, res); g.Status != "waiting" || len(el.asked) != 0 {
		t.Fatalf("gate %+v, elicitations %d", g, len(el.asked))
	}
	mustContain(t, text(res), "Decisions in team need a person on the web")
}

// The asking agent's in-agent answer can't be replayed by another call or
// caller, and a stale question reports how the gate stands now.
func TestGateRetryIsBoundToTheCall(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	client := mcp.NewClient(&mcp.Implementation{Name: "raw", Version: "1"}, &mcp.ClientOptions{
		Logger: quiet, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})
	raw, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: f.token, next: http.DefaultTransport}}, MaxRetries: -1},
		&mcp.ClientSessionOptions{ProtocolVersion: modern})
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	args := decision(f.sp)
	first, err := raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_request_decision", Arguments: args})
	if err != nil || !first.NeedsInput() || first.RequestState == "" {
		t.Fatalf("first call: %v %+v", err, first)
	}
	choose := mcp.InputResponseMap{"answer": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "2"}}}
	other := decision(f.sp)
	other["question"] = "Something else entirely?"
	res, err := raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_request_decision", Arguments: other,
		InputResponses: choose, RequestState: first.RequestState})
	if err != nil || !res.IsError {
		t.Fatalf("retry with other arguments: %v %s", err, text(res))
	}
	mustContain(t, text(res), "issued for a different call", "Nothing was answered")
	// A person answers on the web first; the genuine retry then reports that.
	e.answerAs(f.user, f.sp, "G-0001", 3)
	res, _ = raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_request_decision", Arguments: args,
		InputResponses: choose, RequestState: first.RequestState})
	g := gateOf(t, res)
	if g.Status != "answered" || g.Option != 3 || g.Answer != "Decide later" {
		t.Errorf("after a web answer: %+v (%s)", g, text(res))
	}
	if n := e.count(`SELECT count(*) FROM v2.memories WHERE kind = 'decision'`); n != 1 {
		t.Errorf("%d decisions, want the web answer's 1", n)
	}
}

// A read-only OAuth token asking in a space on V2 gets the step-up
// challenge, as a push does.
func TestRequestDecisionStepUp(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	sp := e.space(user, policy.SpaceProject, "on-v2")
	e.toV2(sp)
	tok, _ := e.grant(user, "claude-code", "memax:read")
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "memax_request_decision", "arguments": decision(sp)}})
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	mustContain(t, res.Header.Get("WWW-Authenticate"), `error="insufficient_scope"`, `scope="memax:read memax:propose"`)
}
