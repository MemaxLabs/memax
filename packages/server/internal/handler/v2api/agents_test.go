package v2api_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The wire shape of an agent connection, as the tests read it.
type agentSpace struct {
	SpaceID  uuid.UUID `json:"space_id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Autonomy string    `json:"autonomy"`
	Writes   int       `json:"writes_7d"`
}

type agentConn struct {
	ID          uuid.UUID `json:"id"`
	PersonID    uuid.UUID `json:"person_id"`
	Agent       string    `json:"agent"`
	DisplayName string    `json:"display_name"`
	Surface     string    `json:"surface"`
	Credential  struct {
		Kind   string    `json:"kind"`
		ID     uuid.UUID `json:"id"`
		Active bool      `json:"active"`
	} `json:"credential"`
	MaxAutonomy     string       `json:"max_autonomy"`
	State           string       `json:"state"`
	Spaces          []agentSpace `json:"spaces"`
	Writes          int          `json:"writes_7d"`
	LastSeenAt      *time.Time   `json:"last_seen_at"`
	ConnectedByKind string       `json:"connected_by_kind"`
}

func (c agentConn) autonomyIn(sp space) string {
	for _, s := range c.Spaces {
		if s.SpaceID == sp.id {
			return s.Autonomy
		}
	}
	return ""
}

type agentResult struct {
	Outcome  string    `json:"outcome"`
	Agent    agentConn `json:"agent"`
	Receipts []receipt `json:"receipts"`
}

func agentPath(id uuid.UUID, rest string) string { return "/v2/agents/" + id.String() + rest }

func TestListAndReadAgents(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(jy, policy.SpaceTeam, "acme")
	e.join(team, zz, "contributor")
	codexTok, codexGrant := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "claude-code"})
	codex, claude := e.connection(codexGrant), e.connection(keyID)
	jyTok, jyGrant := e.grant(jy, "cursor", []string{"memory:read", "memory:write"})
	_ = jyTok

	// Codex writes twice in memax-v2, in one session.
	for _, s := range []string{"One.", "Two."} {
		e.do(call{method: "POST", path: memoriesPath(project), token: codexTok,
			body: map[string]any{"statement": s, "section": "conventions", "session_ref": "cx-9f1c"}}).ok(http.StatusCreated, nil)
	}
	e.h.Wait() // let the last-seen update land

	var list page[agentConn]
	e.do(call{method: "GET", path: "/v2/agents", token: e.session(zz)}).ok(http.StatusOK, &list)
	if len(list.Items) != 2 || list.Items[0].ID != codex || list.Items[1].ID != claude {
		t.Fatalf("zz's agents = %+v", list.Items)
	}
	cx := list.Items[0]
	if cx.Agent != "codex" || cx.DisplayName != "Codex" || cx.Surface != "cli" || cx.Credential.Kind != "oauth_grant" ||
		!cx.Credential.Active || cx.MaxAutonomy != "write" || cx.State != "active" || cx.Writes != 2 ||
		cx.LastSeenAt == nil || cx.ConnectedByKind != "memax" || cx.PersonID != zz {
		t.Errorf("codex = %+v", cx)
	}
	if len(cx.Spaces) != 3 || cx.Spaces[0].SpaceID != personal.id || cx.autonomyIn(project) != "propose" || cx.autonomyIn(team) != "propose" {
		t.Errorf("codex spaces = %+v", cx.Spaces)
	}
	if list.Items[1].MaxAutonomy != "propose" || list.Items[1].Credential.Kind != "api_key" {
		t.Errorf("an API key's agent proposes at most: %+v", list.Items[1])
	}

	// A space lists everyone's agents there: jy sees zz's two and their own.
	e.do(call{method: "GET", path: "/v2/spaces/" + team.slug + "/agents", token: e.session(jy)}).ok(http.StatusOK, &list)
	var ids []uuid.UUID
	for _, c := range list.Items {
		ids = append(ids, c.ID)
		if c.ID == codex && (len(c.Spaces) != 1 || c.Spaces[0].SpaceID != team.id) {
			t.Errorf("jy sees codex's other spaces: %+v", c.Spaces)
		}
	}
	if !slices.Equal(ids, []uuid.UUID{codex, claude, e.connection(jyGrant)}) {
		t.Errorf("agents in acme = %v", ids)
	}
	// …but not zz's agents through a space they don't share.
	e.do(call{method: "GET", path: "/v2/spaces/" + project.id.String() + "/agents", token: e.session(jy)}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: agentPath(codex, ""), token: e.session(jy)}).ok(http.StatusOK, nil)
	other := e.user("other")
	e.space(other, policy.SpacePersonal, "Personal")
	e.do(call{method: "GET", path: agentPath(codex, ""), token: e.session(other)}).fails(http.StatusNotFound, "not_found")

	// AgentDetail: the week, recent writes and sessions.
	var detail struct {
		Agent    agentConn `json:"agent"`
		ThisWeek struct {
			Reads, Writes, Proposals, Kept, Rejected, Waiting int
		} `json:"this_week"`
		RecentWrites []receipt `json:"recent_writes"`
		Sessions     []struct {
			SessionRef string `json:"session_ref"`
			Writes     int    `json:"writes"`
		} `json:"sessions"`
	}
	e.do(call{method: "GET", path: agentPath(codex, ""), token: e.session(zz)}).ok(http.StatusOK, &detail)
	w := detail.ThisWeek
	if detail.Agent.ID != codex || w.Writes != 2 || w.Proposals != 2 || w.Waiting != 2 || w.Kept != 0 || w.Reads != 0 {
		t.Errorf("week = %+v", w)
	}
	if len(detail.RecentWrites) != 2 || detail.RecentWrites[0].ActorID == nil || *detail.RecentWrites[0].ActorID != codex {
		t.Errorf("recent writes = %+v", detail.RecentWrites)
	}
	if len(detail.Sessions) != 1 || detail.Sessions[0].SessionRef != "cx-9f1c" || detail.Sessions[0].Writes != 2 {
		t.Errorf("sessions = %+v", detail.Sessions)
	}

	// An agent sees itself and nothing else of its person's.
	e.do(call{method: "GET", path: "/v2/agents", token: keyTok}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != claude {
		t.Errorf("an agent lists %+v", list.Items)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + project.slug + "/agents", token: keyTok}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != claude {
		t.Errorf("an agent lists the space's agents as %+v", list.Items)
	}
	e.do(call{method: "GET", path: agentPath(claude, ""), token: keyTok}).ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: agentPath(codex, ""), token: keyTok}).fails(http.StatusNotFound, "not_found")

	e.do(call{method: "GET", path: agentPath(uuid.New(), ""), token: e.session(zz)}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "GET", path: "/v2/agents/codex", token: e.session(zz), invalid: true}).fails(http.StatusBadRequest, "invalid_request")
}

func TestSetAgentAutonomy(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(jy, policy.SpaceTeam, "acme")
	e.join(team, zz, "contributor")
	codexTok, grant := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	codex := e.connection(grant)
	set := func(token string, sp space, level string, sign func(*http.Request, []byte), header map[string]string) *resp {
		return e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/"+sp.slug), token: token,
			body: map[string]any{"autonomy": level, "reason": "trusted now"}, sign: sign, header: header})
	}

	// Raising from the CLI is refused, so an agent with zz's login can't
	// raise itself; on the web it works, and the receipt says so.
	if code := policyCode(t, set(e.cliSession(zz), project, "write", nil, nil)); code != "autonomy_needs_web" {
		t.Errorf("CLI raise: %s", code)
	}
	var res agentResult
	idem := map[string]string{"Idempotency-Key": uuid.NewString()}
	set(e.webSession(zz), project, "write", webSigned(zz, tamper{}), idem).ok(http.StatusOK, &res)
	if res.Outcome != "applied" || res.Agent.autonomyIn(project) != "write" || len(res.Receipts) != 1 {
		t.Fatalf("web raise: %+v", res)
	}
	rc := res.Receipts[0]
	if rc.Action != "autonomy_changed" || rc.ObjectKind != "agent" || rc.ObjectID != codex || rc.Assurance != "human_web" ||
		rc.Via != "web" || rc.Reason != "trusted now" || rc.SpaceID != project.id {
		t.Errorf("raise receipt = %+v", rc)
	}
	// A retry with the same key is a replay (the proxy signs it afresh).
	r := set(e.webSession(zz), project, "write", webSigned(zz, tamper{}), idem).ok(http.StatusOK, &res)
	if r.header.Get("Idempotent-Replayed") != "true" || len(res.Receipts) != 1 || res.Receipts[0].ID != rc.ID {
		t.Errorf("replay: %v %+v", r.header, res.Receipts)
	}
	// Now Codex keeps what it writes in memax-v2.
	var mem result
	e.do(call{method: "POST", path: memoriesPath(project), token: codexTok, body: remember("Codex keeps this.", "conventions")}).ok(http.StatusCreated, &mem)
	if mem.Outcome != "applied" || mem.Memory.State != "kept" {
		t.Errorf("write agent: %s %s", mem.Outcome, mem.Memory.State)
	}
	// The same level writes nothing; lowering works from the CLI.
	set(e.cliSession(zz), project, "write", nil, nil).ok(http.StatusOK, &res)
	if len(res.Receipts) != 0 {
		t.Errorf("same level wrote %+v", res.Receipts)
	}
	set(e.cliSession(zz), project, "read", nil, nil).ok(http.StatusOK, &res)
	if res.Agent.autonomyIn(project) != "read" || res.Receipts[0].Assurance != "client_attested" {
		t.Errorf("CLI lower: %+v", res)
	}
	if code := policyCode(t, e.do(call{method: "POST", path: memoriesPath(project), token: codexTok, body: remember("Read only.", "conventions")})); code != "read_only" {
		t.Errorf("read agent: %s", code)
	}

	// The team's owner may lower zz's agent in the team, never raise it.
	if code := policyCode(t, set(e.webSession(jy), team, "write", webSigned(jy, tamper{}), nil)); code != "not_your_agent" {
		t.Errorf("owner raises someone else's agent: %s", code)
	}
	set(e.cliSession(jy), team, "read", nil, nil).ok(http.StatusOK, &res)
	if res.Agent.autonomyIn(team) != "read" || len(res.Agent.Spaces) != 1 {
		t.Errorf("owner lowers: %+v", res.Agent)
	}
	// A member who can keep may let their agent write in the team; on the web.
	set(e.webSession(zz), team, "write", webSigned(zz, tamper{}), nil).ok(http.StatusOK, &res)

	// Agents never change autonomy, their own included.
	if code := policyCode(t, set(codexTok, project, "write", nil, nil)); code != "person_must_manage" {
		t.Errorf("agent raises itself: %s", code)
	}
	// An API key's agent can't be set to Write.
	_, keyID := e.apiKey(zz, keyOpts{agent: "claude-code"})
	e.do(call{method: "PATCH", path: agentPath(e.connection(keyID), "/spaces/"+project.slug), token: e.webSession(zz),
		body: map[string]any{"autonomy": "write"}, sign: webSigned(zz, tamper{})}).fails(http.StatusForbidden, "refused")

	// Malformed and unknown.
	e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/"+project.slug), token: e.cliSession(zz), body: map[string]any{"autonomy": "root"}, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/"+project.slug), token: e.cliSession(zz), body: map[string]any{}, invalid: true}).
		fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/"+project.slug), token: e.cliSession(zz), body: map[string]any{"autonomy": "read"}, invalid: true,
		header: map[string]string{"Idempotency-Key": ""}}).fails(http.StatusBadRequest, "idempotency_key_required")
	e.do(call{method: "PATCH", path: agentPath(uuid.New(), "/spaces/"+project.slug), token: e.cliSession(zz), body: map[string]any{"autonomy": "read"}}).
		fails(http.StatusNotFound, "not_found")
	e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/nowhere"), token: e.cliSession(zz), body: map[string]any{"autonomy": "read"}}).
		fails(http.StatusNotFound, "not_found")
}

func TestPauseResumeDisconnectAgents(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(project, jy, "contributor")
	codexTok, grant := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "claude-code"})
	codex, claude := e.connection(grant), e.connection(keyID)
	cmd := func(token string, id uuid.UUID, verb string, sign func(*http.Request, []byte)) *resp {
		return e.do(call{method: "POST", path: agentPath(id, ":"+verb), token: token, sign: sign})
	}

	// Pausing: only its person, from anywhere.
	if code := policyCode(t, cmd(e.webSession(jy), codex, "pause", webSigned(jy, tamper{}))); code != "not_your_agent" {
		t.Errorf("someone else pauses: %s", code)
	}
	if code := policyCode(t, cmd(codexTok, codex, "pause", nil)); code != "person_must_manage" {
		t.Errorf("an agent pauses itself: %s", code)
	}
	var res agentResult
	e.do(call{method: "POST", path: agentPath(codex, ":pause"), token: e.cliSession(zz), body: map[string]any{"reason": "taking a break"}}).ok(http.StatusOK, &res)
	if res.Agent.State != "paused" || res.Receipts[0].Action != "paused" || res.Receipts[0].Reason != "taking a break" {
		t.Errorf("pause: %+v", res)
	}
	got := e.do(call{method: "POST", path: memoriesPath(project), token: codexTok, body: remember("Paused.", "conventions")}).fails(http.StatusForbidden, "refused")
	if got.Details.Policy.Code != "agent_paused" {
		t.Errorf("a paused agent writes: %+v", got.Details.Policy)
	}
	e.do(call{method: "GET", path: memoriesPath(project), token: codexTok}).ok(http.StatusOK, nil)
	cmd(e.cliSession(zz), codex, "pause", nil).fails(http.StatusConflict, "invalid_transition")

	// Resuming is raising: the web only.
	if code := policyCode(t, cmd(e.cliSession(zz), codex, "resume", nil)); code != "autonomy_needs_web" {
		t.Errorf("CLI resume: %s", code)
	}
	cmd(e.webSession(zz), codex, "resume", webSigned(zz, tamper{})).ok(http.StatusOK, &res)
	if res.Agent.State != "active" || res.Receipts[0].Assurance != "human_web" {
		t.Errorf("resume: %+v", res)
	}
	e.do(call{method: "POST", path: memoriesPath(project), token: codexTok, body: remember("Back.", "conventions")}).ok(http.StatusCreated, nil)
	cmd(e.webSession(zz), codex, "resume", webSigned(zz, tamper{})).fails(http.StatusConflict, "invalid_transition")

	// Disconnecting revokes the credential: the token stops working at once.
	cmd(e.cliSession(zz), claude, "disconnect", nil).ok(http.StatusOK, &res)
	if res.Agent.State != "disconnected" || res.Agent.Credential.Active || res.Receipts[0].Action != "disconnected" {
		t.Errorf("disconnect: %+v", res)
	}
	e.do(call{method: "GET", path: "/v2/spaces", token: keyTok}).fails(http.StatusUnauthorized, "unauthorized")
	cmd(e.webSession(zz), codex, "disconnect", webSigned(zz, tamper{})).ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: memoriesPath(project), token: codexTok}).fails(http.StatusUnauthorized, "unauthorized")
	// It is gone from the lists, still readable, and can't come back.
	var list page[agentConn]
	e.do(call{method: "GET", path: "/v2/agents", token: e.session(zz)}).ok(http.StatusOK, &list)
	if len(list.Items) != 0 {
		t.Errorf("disconnected agents listed: %+v", list.Items)
	}
	e.do(call{method: "GET", path: agentPath(codex, ""), token: e.session(zz)}).ok(http.StatusOK, nil)
	cmd(e.webSession(zz), codex, "resume", webSigned(zz, tamper{})).fails(http.StatusConflict, "invalid_transition")
	cmd(e.cliSession(zz), uuid.New(), "pause", nil).fails(http.StatusNotFound, "not_found")
}
