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

// The protocol eras the tests drive with the official go-sdk client.
const (
	modern = "2026-07-28" // stateless; multi round-trip requests
	legacy = "2025-11-25" // initialize; elicitation/create on a session
	older  = "2025-06-18"
)

// fixture is a person with a project space on V2 and Claude Code connected
// to it over OAuth at the given autonomy.
type fixture struct {
	e     *env
	user  uuid.UUID
	sp    space
	token string
}

func newFixture(t *testing.T, level policy.Autonomy) (*env, fixture) {
	t.Helper()
	e := newEnv(t)
	user := e.user("zz")
	sp := e.space(user, policy.SpaceProject, "memax-v2")
	e.toV2(sp)
	tok, grant := e.grant(user, "claude-code", "memax:read memax:write")
	e.connect(user, grant, ledger.AgentClaudeCode, level, sp, e.personal(user))
	return e, fixture{e: e, user: user, sp: sp, token: tok}
}

func push(statement string, sp space, extra ...map[string]any) map[string]any {
	args := map[string]any{"content": statement, "hub_id": sp.id.String()}
	for _, x := range extra {
		for k, v := range x {
			args[k] = v
		}
	}
	return args
}

// A Propose agent whose client can't elicit proposes: the memory waits in
// Review, and the result carries its display ID and the Review link.
func TestProposeAgentProposes(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	for _, version := range []string{older, legacy, modern} {
		t.Run(version, func(t *testing.T) {
			cs := e.connectClient(f.token, "/mcp", version, nil)
			res := call(t, cs, "memax_push", push("Deploys go out on Fridays only ("+version+")", f.sp))
			if res.IsError {
				t.Fatalf("push: %s", text(res))
			}
			validates(t, "agent", "memax_push", res)
			out := structured[handler.MCPPushOutput](t, res)
			if out.Status != handler.MCPPushProposed || !strings.Contains(out.ReviewURL, "/"+f.sp.slug+"/review?ref="+out.ID) {
				t.Errorf("push = %+v", out)
			}
			mustContain(t, text(res), "Proposed "+out.ID, "waits in Review")
			if lc, _, _ := e.memory(f.sp, out.ID); lc != "proposed" {
				t.Errorf("lifecycle = %s", lc)
			}
			rc := e.receipts(f.sp, out.ID)
			if len(rc) != 1 || rc[0].Action != "proposed" || rc[0].ActorKind != "agent" || rc[0].Agent != "claude-code" || rc[0].Via != "mcp" {
				t.Errorf("receipts = %+v", rc)
			}
		})
	}
}

// In-agent Keep: the person accepts the elicitation, over multi round-trip
// requests (2026-07-28) or elicitation/create (a legacy session), and the
// memory is kept by the person via the agent, client-attested.
func TestElicitationAcceptKeeps(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	for _, version := range []string{modern, legacy} {
		t.Run(version, func(t *testing.T) {
			el := &elicitor{answer: accept("keep", "")}
			cs := e.connectClient(f.token, "/mcp", version, el)
			res := call(t, cs, "memax_push", push("MCP write tools ask for confirmation when a person is present ("+version+")", f.sp))
			if res.IsError {
				t.Fatalf("push: %s", text(res))
			}
			if len(el.asked) != 1 {
				t.Fatalf("elicitations = %d, want 1", len(el.asked))
			}
			mustContain(t, el.asked[0].Message, "Keep this in memax-v2?")
			out := structured[handler.MCPPushOutput](t, res)
			if out.Status != handler.MCPPushKept || out.Assurance != "client_attested" {
				t.Errorf("push = %+v", out)
			}
			mustContain(t, text(res), "Kept by you via Claude Code · "+out.ID)
			if lc, _, _ := e.memory(f.sp, out.ID); lc != "kept" {
				t.Errorf("lifecycle = %s", lc)
			}
			rc := e.receipts(f.sp, out.ID)
			if len(rc) != 2 || rc[0].Action != "proposed" || rc[0].ActorKind != "agent" {
				t.Fatalf("receipts = %+v", rc)
			}
			if k := rc[1]; k.Action != "kept" || k.ActorKind != "person" || k.Agent != "claude-code" || k.Via != "mcp" || k.Assurance != "client_attested" {
				t.Errorf("keep receipt = %+v", k)
			}
		})
	}
}

// Decline, cancel and "leave as proposal" leave the proposal in Review;
// Edit keeps the edited words.
func TestElicitationAnswers(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cases := []struct {
		name      string
		answer    func(*mcp.ElicitParams) *mcp.ElicitResult
		lifecycle string
		statement string
	}{
		{"decline", decline, "proposed", ""},
		{"cancel", func(*mcp.ElicitParams) *mcp.ElicitResult { return &mcp.ElicitResult{Action: "cancel"} }, "proposed", ""},
		{"leave", accept("leave", ""), "proposed", ""},
		{"edit", accept("edit", "Releases go out on Fridays, after the freeze"), "kept", "Releases go out on Fridays, after the freeze"},
	}
	for _, version := range []string{modern, legacy} {
		for _, c := range cases {
			t.Run(version+"/"+c.name, func(t *testing.T) {
				cs := e.connectClient(f.token, "/mcp", version, &elicitor{answer: c.answer})
				res := call(t, cs, "memax_push", push("Releases go out on Fridays ("+version+" "+c.name+")", f.sp))
				if res.IsError {
					t.Fatalf("push: %s", text(res))
				}
				out := structured[handler.MCPPushOutput](t, res)
				lc, version2, statement := e.memory(f.sp, out.ID)
				if lc != c.lifecycle {
					t.Errorf("lifecycle = %s, want %s (%s)", lc, c.lifecycle, text(res))
				}
				if c.statement != "" && (statement != c.statement || version2 != 2) {
					t.Errorf("statement = %q v%d", statement, version2)
				}
				if c.lifecycle == "proposed" {
					mustContain(t, text(res), "Left "+out.ID+" as a proposal")
				}
			})
		}
	}
}

// External content is quarantined at any autonomy: even an agent at Write
// only proposes it, and nobody is asked to keep it in the agent.
func TestExternalSourceIsQuarantined(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyWrite)
	el := &elicitor{answer: accept("keep", "")}
	cs := e.connectClient(f.token, "/mcp", modern, el)
	res := call(t, cs, "memax_push", push("The vendor API rate limit is 100 requests a second", f.sp, map[string]any{
		"sources": []map[string]any{{"kind": "url", "ref": "vendor docs", "uri": "https://vendor.example/limits"}},
	}))
	if res.IsError {
		t.Fatalf("push: %s", text(res))
	}
	out := structured[handler.MCPPushOutput](t, res)
	if out.Status != handler.MCPPushProposed || len(el.asked) != 0 {
		t.Errorf("push = %+v, elicitations = %d", out, len(el.asked))
	}
	mustContain(t, text(res), "quarantined")
	var trust string
	if err := e.pool.QueryRow(context.Background(), `SELECT trust FROM v2.memories WHERE space_id = $1`, f.sp.id).Scan(&trust); err != nil || trust != "external" {
		t.Errorf("trust = %q (%v)", trust, err)
	}
}

// An agent at Write keeps its own first-party statement, and the receipt
// names it.
func TestWriteAgentKeeps(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyWrite)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	res := call(t, cs, "memax_push", push("The API listens on port 8080", f.sp))
	out := structured[handler.MCPPushOutput](t, res)
	if res.IsError || out.Status != handler.MCPPushKept {
		t.Fatalf("push: %s", text(res))
	}
	rc := e.receipts(f.sp, out.ID)
	if len(rc) != 1 || rc[0].Action != "kept" || rc[0].ActorKind != "agent" || rc[0].Agent != "claude-code" {
		t.Errorf("receipts = %+v", rc)
	}
}

// A Read agent's writes are refused with a message saying where to change
// it; its capture is refused too.
func TestReadAgentIsRefused(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyRead)
	cs := e.connectClient(f.token, "/mcp", legacy, nil)
	res := call(t, cs, "memax_push", push("Anything", f.sp))
	if !res.IsError {
		t.Fatalf("push by a Read agent: %s", text(res))
	}
	mustContain(t, text(res), "Claude Code is read-only in memax-v2. Change it in Agents.")
	if n := e.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1`, f.sp.id); n != 0 {
		t.Errorf("%d memories written", n)
	}
}

// Agents read only the spaces they're connected to: a space on V2 the
// credential reaches but the connection doesn't is invisible to reads and
// refused for writes.
func TestAgentsReadOnlyConnectedSpaces(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	connected := e.space(user, policy.SpaceProject, "connected")
	other := e.space(user, policy.SpaceProject, "other")
	e.toV2(connected, other)
	e.keep(user, connected, "Connected space uses Postgres seventeen", ledger.SectionConventions)
	secret := e.keep(user, other, "Other space uses Postgres sixteen", ledger.SectionConventions)
	tok, grant := e.grant(user, "codex", "memax:read memax:propose")
	e.connect(user, grant, ledger.AgentCodex, policy.AutonomyPropose, connected)
	cs := e.connectClient(tok, "/mcp", legacy, nil)

	res := call(t, cs, "memax_recall", map[string]any{"query": "postgres"})
	got := text(res)
	mustContain(t, got, "Postgres seventeen")
	if strings.Contains(got, "sixteen") {
		t.Errorf("recall leaked the unconnected space: %s", got)
	}
	out := structured[handler.MCPRecallOutput](t, res)
	for _, it := range out.Results {
		if it.SpaceID == other.id.String() {
			t.Errorf("result from the unconnected space: %+v", it)
		}
	}
	res = call(t, cs, "memax_get", map[string]any{"id": secret.Ref, "space_id": other.id.String()})
	if !res.IsError || strings.Contains(text(res), "sixteen") {
		t.Errorf("get in the unconnected space: %s", text(res))
	}
	mustContain(t, text(res), "isn't connected to other")
	res = call(t, cs, "memax_get", map[string]any{"id": secret.ID.String()})
	if !res.IsError || strings.Contains(text(res), "sixteen") {
		t.Errorf("get by uuid in the unconnected space: %s", text(res))
	}
	res = call(t, cs, "memax_push", push("Write into the other space", other))
	if !res.IsError {
		t.Errorf("push into the unconnected space: %s", text(res))
	}
	mustContain(t, text(res), "isn't connected to other")
	res = call(t, cs, "memax_hubs", nil)
	if strings.Contains(text(res), other.id.String()) {
		t.Errorf("hubs lists the unconnected space: %s", text(res))
	}
	mustContain(t, text(res), connected.id.String(), "on V2", "Codex: propose")
}

// Read-after-write is scoped to the proposing session: the session that
// proposed sees its proposal, marked proposed; another session doesn't.
func TestSessionScopedReadAfterWrite(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cs := e.connectClient(f.token, "/mcp", older, nil)
	res := call(t, cs, "memax_push", push("Staging runs on a single machine", f.sp, map[string]any{"session_ref": "cc-7f3a"}))
	ref := refOf(t, res)

	for session, want := range map[string]bool{"cc-7f3a": true, "cc-other": false, "": false} {
		args := map[string]any{"query": "staging machine"}
		if session != "" {
			args["session_ref"] = session
		}
		res := call(t, cs, "memax_recall", args)
		validates(t, "agent", "memax_recall", res)
		out := structured[handler.MCPRecallOutput](t, res)
		found := false
		for _, p := range out.Proposals {
			found = found || (p.Ref == ref && p.State == "proposed")
		}
		for _, r := range out.Results {
			if r.Ref == ref {
				t.Errorf("session %q: the proposal is among kept results", session)
			}
		}
		if found != want {
			t.Errorf("session %q: proposal visible = %t, want %t (%s)", session, found, want, text(res))
		}
	}
}

// Forget needs a person: the agent's request forgets nothing and says
// where the person forgets it.
func TestForgetNeedsAPerson(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyWrite)
	m := e.keep(f.user, f.sp, "The staging database is shared", ledger.SectionConventions)
	cs := e.connectClient(f.token, "/mcp", modern, &elicitor{answer: accept("keep", "")})
	res := call(t, cs, "memax_forget", map[string]any{"id": m.Ref, "space_id": f.sp.id.String()})
	mustContain(t, text(res), m.Ref+" wasn't forgotten", "Forget needs a person", "https://memax.test/"+f.sp.slug+"/memories/"+m.Ref)
	if lc, _, _ := e.memory(f.sp, m.Ref); lc != "kept" {
		t.Errorf("lifecycle = %s after an agent's forget", lc)
	}
}

// D15: decisions in a team space need a person on the web, so the agent
// isn't asked to keep one and the result says so.
func TestTeamDecisionNeedsTheWeb(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	team := e.space(user, policy.SpaceTeam, "team")
	e.toV2(team)
	tok, grant := e.grant(user, "claude-code", "memax:read memax:write")
	e.connect(user, grant, ledger.AgentClaudeCode, policy.AutonomyPropose, team)
	el := &elicitor{answer: accept("keep", "")}
	cs := e.connectClient(tok, "/mcp", modern, el)
	res := call(t, cs, "memax_push", push("We deploy the v2 API to Fly in sjc", team, map[string]any{"section": "decisions", "hub_reason": "team-wide"}))
	out := structured[handler.MCPPushOutput](t, res)
	if out.Status != handler.MCPPushProposed || len(el.asked) != 0 {
		t.Fatalf("push = %+v, elicitations = %d: %s", out, len(el.asked), text(res))
	}
	mustContain(t, text(res), "Decisions in team need a person on the web")
}

// An OAuth token whose scope only reads gets a 403 insufficient_scope
// challenge for a write in a space on V2, so the client can step up; in a
// V1 space it keeps V1's tool error.
func TestInsufficientScopeStepUp(t *testing.T) {
	e := newEnv(t)
	user := e.user("zz")
	v2sp := e.space(user, policy.SpaceProject, "on-v2")
	v1sp := e.space(user, policy.SpaceProject, "on-v1")
	e.toV2(v2sp)
	tok, _ := e.grant(user, "claude-code", "memax:read")
	body := func(sp space) []byte {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call",
			"params": map[string]any{"name": "memax_push", "arguments": push("x", sp)}})
		return b
	}
	do := func(sp space) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/mcp", bytes.NewReader(body(sp)))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := do(v2sp)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	mustContain(t, res.Header.Get("WWW-Authenticate"), `error="insufficient_scope"`, `scope="memax:read memax:propose"`, "resource_metadata=")
	res = do(v1sp)
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(b), "not allowed to perform that Memax action") {
		t.Errorf("V1 space: %d %s", res.StatusCode, b)
	}
}

// memax_get reads a kept memory with its receipts and sources, by display
// ID; structured output validates against the schema.
func TestGetReturnsReceiptsAndSources(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cs := e.connectClient(f.token, "/mcp", modern, &elicitor{answer: accept("keep", "")})
	res := call(t, cs, "memax_push", push("CI runs the parity check", f.sp, map[string]any{
		"sources": []map[string]any{{"kind": "pr", "ref": "PR #212"}}}))
	ref := refOf(t, res)
	for _, tool := range []struct{ path, name, profile string }{{"/mcp", "memax_get", "agent"}, {"/mcp/chatgpt", "get_memory", "chatgpt"}} {
		c := cs
		if tool.path != "/mcp" {
			c = e.connectClient(f.token, tool.path, legacy, nil)
		}
		res = call(t, c, tool.name, map[string]any{"id": ref, "space_id": f.sp.slug})
		if res.IsError {
			t.Fatalf("%s: %s", tool.name, text(res))
		}
		validates(t, tool.profile, tool.name, res)
		out := structured[handler.MCPGetOutput](t, res)
		if out.Memory.Ref != ref || out.Memory.Record != "v2" || out.Memory.Text != "CI runs the parity check" ||
			len(out.Memory.Receipts) != 2 || len(out.Memory.Sources) != 1 || out.Memory.Sources[0].Ref != "PR #212" {
			t.Errorf("%s = %+v", tool.name, out.Memory)
		}
		mustContain(t, text(res), "## Receipts", "kept by person via claude-code (mcp, client_attested)", "pr: PR #212")
	}
}

// Recall without a query is the digest: each space's kept memories by
// section, and what waits in Review.
func TestRecallDigest(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	e.keep(f.user, f.sp, "We chose Postgres over Mongo", ledger.SectionDecisions)
	e.keep(f.user, f.sp, "Commit messages use the imperative mood", ledger.SectionConventions)
	cs := e.connectClient(f.token, "/mcp", legacy, nil)
	call(t, cs, "memax_push", push("A pending idea", f.sp))
	res := call(t, cs, "memax_recall", map[string]any{"hub_id": f.sp.id.String()})
	validates(t, "agent", "memax_recall", res)
	out := structured[handler.MCPRecallOutput](t, res)
	if len(out.Digest) != 1 || len(out.Digest[0].Sections) != 2 || out.Digest[0].WaitingInReview != 1 {
		t.Fatalf("digest = %+v", out.Digest)
	}
	mustContain(t, text(res), "## memax-v2", "1 waiting in Review", "### Decisions", "We chose Postgres over Mongo", "### Conventions")
	if strings.Contains(text(res), "A pending idea") {
		t.Error("the digest shows a proposal")
	}
}

// memax_capture in a space on V2 writes notes (V1 memories), never
// proposals, and refuses credentials.
func TestCaptureWritesNotes(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cs := e.connectClient(f.token, "/mcp", legacy, nil)
	// The write hub is the personal space unless the client picks one.
	personal := e.personal(f.user)
	e.toV2(personal)
	res := call(t, cs, "memax_capture", map[string]any{"summary": "Paired on the MCP server", "decisions": []string{"go-sdk"}})
	if res.IsError {
		t.Fatalf("capture: %s", text(res))
	}
	mustContain(t, text(res), "Saved as notes in")
	if n := e.count(`SELECT count(*) FROM memories WHERE hub_id = $1`, personal.id); n != 1 {
		t.Errorf("notes = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1`, personal.id); n != 0 {
		t.Errorf("capture wrote %d V2 memories", n)
	}
	res = call(t, cs, "memax_capture", map[string]any{"summary": "token AKIAIOSFODNN7EXAMPLE leaked"})
	if !res.IsError {
		t.Errorf("a capture with a credential was saved: %s", text(res))
	}
}

// A confirmation can't be replayed by another caller or for another call.
func TestTamperedConfirmationIsRefused(t *testing.T) {
	e, f := newFixture(t, policy.AutonomyPropose)
	cs := e.connectClient(f.token, "/mcp", modern, &elicitor{answer: accept("keep", "")})
	// Disable the client's automatic retry to see the input request.
	_ = cs
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
	args := push("Feature flags live in LaunchDarkly", f.sp)
	first, err := raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_push", Arguments: args})
	if err != nil || !first.NeedsInput() || first.RequestState == "" {
		t.Fatalf("first call: %v %+v", err, first)
	}
	answer := mcp.InputResponseMap{"keep": &mcp.ElicitResult{Action: "accept", Content: map[string]any{"choice": "keep"}}}
	// Different arguments: the digest doesn't match.
	other := push("Feature flags live somewhere else", f.sp)
	res, err := raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_push", Arguments: other,
		InputResponses: answer, RequestState: first.RequestState})
	if err != nil || !res.IsError {
		t.Fatalf("retry with other arguments: %v %s", err, text(res))
	}
	mustContain(t, text(res), "issued for a different call")
	// A forged state.
	res, _ = raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_push", Arguments: args,
		InputResponses: answer, RequestState: first.RequestState + "x"})
	if !res.IsError {
		t.Errorf("forged state accepted: %s", text(res))
	}
	if n := e.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, f.sp.id); n != 0 {
		t.Errorf("%d kept after refused confirmations", n)
	}
	// The genuine retry keeps.
	res, _ = raw.CallTool(context.Background(), &mcp.CallToolParams{Name: "memax_push", Arguments: args,
		InputResponses: answer, RequestState: first.RequestState})
	if res.IsError || structured[handler.MCPPushOutput](t, res).Status != handler.MCPPushKept {
		t.Errorf("genuine retry: %s", text(res))
	}
}
