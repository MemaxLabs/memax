package v2api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Cloud agents and their start keys (cloud warm-start design §4): a
// person connects a cloud agent to one space with a key that starts its
// sessions there and does nothing else; Propose only from the web; a new
// key under the same connection; 180 days; and a start key refused on
// every other route.

type cloudAgentResult struct {
	Outcome string    `json:"outcome"`
	Agent   agentConn `json:"agent"`
	Key     *struct {
		Secret    string    `json:"secret"`
		Prefix    string    `json:"prefix"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"key"`
	Receipts []receipt `json:"receipts"`
}

var startKeyShape = regexp.MustCompile(`^mxk_[0-9a-f]{64}$`)

type agentStartWire struct {
	At         time.Time `json:"at"`
	SpaceID    uuid.UUID `json:"space_id"`
	SessionRef string    `json:"session_ref"`
	Compiles   []string  `json:"compiles"`
	Told       []string  `json:"told"`
}

// lastStart is the agent page's last start, or nil when it has none.
func (f *startFixture) lastStart(token string, agent uuid.UUID) *agentStartWire {
	f.t.Helper()
	var d struct {
		LastStart *agentStartWire `json:"last_start"`
	}
	f.do(call{method: "GET", path: agentPath(agent, ""), token: token}).ok(http.StatusOK, &d)
	return d.LastStart
}

func (f *startFixture) cloudAgent(token string, body map[string]any, header map[string]string,
	sign ...func(*http.Request, []byte)) cloudAgentResult {
	f.t.Helper()
	var res cloudAgentResult
	c := call{method: "POST", path: "/v2/cloud-agents", token: token, body: body, header: header}
	if len(sign) > 0 {
		c.sign = sign[0]
	}
	f.do(c).ok(http.StatusCreated, &res)
	return res
}

func TestCloudAgentsAndStartKeys(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	cli := f.cliSession(f.owner)
	idem := uuid.NewString()
	made := f.cloudAgent(cli, map[string]any{"agent": "copilot", "space": f.sp.slug},
		map[string]string{"X-Memax-Via": "cli", "Idempotency-Key": idem})

	a := made.Agent
	if made.Outcome != "applied" || a.Surface != "cloud" || a.Agent != "copilot" || a.Credential.Kind != "api_key" ||
		!a.Credential.Active || a.MaxAutonomy != "read" || a.autonomyIn(f.sp) != "read" || len(a.Spaces) != 1 ||
		!strings.Contains(a.DisplayName, "Copilot cloud agent") {
		t.Fatalf("cloud agent = %+v", a)
	}
	if made.Key == nil || !startKeyShape.MatchString(made.Key.Secret) || made.Key.Prefix != made.Key.Secret[:8] {
		t.Fatalf("key = %+v", made.Key)
	}
	if d := time.Until(made.Key.ExpiresAt); d < 179*24*time.Hour || d > 181*24*time.Hour {
		t.Errorf("expires in %v, want 180 days", d)
	}
	if len(made.Receipts) != 1 || made.Receipts[0].Action != "connected" {
		t.Errorf("receipts = %+v", made.Receipts)
	}
	var perms []string
	if err := f.pool.QueryRow(context.Background(), `SELECT default_permissions FROM api_keys WHERE id = $1`, a.Credential.ID).
		Scan(&perms); err != nil || len(perms) != 1 || perms[0] != "compile:read" {
		t.Fatalf("the key's permissions: %v %v", perms, err)
	}

	// A retry under the same key: the same connection, and no secret.
	var again cloudAgentResult
	r := f.do(call{method: "POST", path: "/v2/cloud-agents", token: cli, body: map[string]any{"agent": "copilot", "space": f.sp.slug},
		header: map[string]string{"X-Memax-Via": "cli", "Idempotency-Key": idem}})
	r.ok(http.StatusCreated, &again)
	if r.header.Get("Idempotent-Replayed") != "true" || again.Key != nil || again.Agent.ID != a.ID {
		t.Errorf("replay = %+v", again)
	}

	// Before its first start, its page has no last start.
	if s := f.lastStart(cli, a.ID); s != nil {
		t.Errorf("last start before any = %+v", s)
	}

	// The key starts sessions in its space, counted under the connection.
	secret := made.Key.Secret
	got := f.start(secret, startBody(agentsFile(f.c1, f.drift1)), nil)
	if len(got.Files) != 1 || len(got.Files[0].Added) != 1 {
		t.Fatalf("start = %+v", got)
	}
	if n := f.loads(a.ID); n != 1 {
		t.Errorf("%d loads under the cloud connection, want 1", n)
	}
	// Its page says when, which compile the files held, and what it was told.
	told := append(refsOf(got.Files[0].Added), refsOf(got.Files[0].Changed)...)
	slices.Sort(told)
	if s := f.lastStart(cli, a.ID); s == nil || s.SpaceID != f.sp.id || !slices.Equal(s.Compiles, []string{f.c1}) ||
		!slices.Equal(s.Told, told) || s.SessionRef != "cp-1" || time.Since(s.At) > time.Minute {
		t.Errorf("last start = %+v, want %s and %v", s, f.c1, told)
	}
	// Another agent's page never has one: only cloud agents start this way.
	f.start(f.key, startBody(agentsFile(f.c1, "")), map[string]string{"Idempotency-Key": uuid.NewString()})
	if s := f.lastStart(f.tok, f.conn); s != nil {
		t.Errorf("a V1 key's connection has a last start: %+v", s)
	}
	// Only there: another space of the person's is not found.
	other := f.space(f.owner, policy.SpaceProject, "other")
	f.do(call{method: "POST", path: startsPath(other), token: secret, body: startBody(agentsFile(f.c1, ""))}).
		fails(http.StatusNotFound, "not_found")

	// Propose only from the web; never Write; never a personal space; only a person.
	f.do(call{method: "POST", path: "/v2/cloud-agents", token: cli,
		body: map[string]any{"agent": "copilot", "space": f.sp.slug, "autonomy": "propose"}}).
		fails(http.StatusForbidden, "refused")
	if code := policyCode(t, f.do(call{method: "POST", path: "/v2/cloud-agents", token: cli,
		body: map[string]any{"agent": "codex", "space": f.sp.slug, "autonomy": "propose"}})); code != "cloud_propose_needs_web" {
		t.Errorf("Propose from the CLI: %s", code)
	}
	web := f.webSession(f.owner)
	proposer := f.cloudAgent(web, map[string]any{"agent": "codex", "space": f.sp.slug, "autonomy": "propose"},
		map[string]string{"Idempotency-Key": uuid.NewString()}, webSigned(f.owner, tamper{}))
	if proposer.Agent.autonomyIn(f.sp) != "propose" || proposer.Receipts[0].Assurance != "human_web" {
		t.Errorf("Propose from the web = %+v", proposer)
	}
	f.do(call{method: "POST", path: "/v2/cloud-agents", token: cli, invalid: true,
		body: map[string]any{"agent": "copilot", "space": f.sp.slug, "autonomy": "write"}}).
		fails(http.StatusBadRequest, "invalid_request")
	personal := f.space(f.owner, policy.SpacePersonal, "Personal")
	f.do(call{method: "POST", path: "/v2/cloud-agents", token: cli, body: map[string]any{"agent": "copilot", "space": personal.slug}}).
		fails(http.StatusBadRequest, "invalid_request")
	if code := policyCode(t, f.do(call{method: "POST", path: "/v2/cloud-agents", token: f.key,
		body: map[string]any{"agent": "copilot", "space": f.sp.slug}})); code != "person_must_manage" {
		t.Errorf("an agent making a cloud agent: %s", code)
	}
	jy := f.user("jy")
	f.do(call{method: "POST", path: "/v2/cloud-agents", token: f.session(jy), body: map[string]any{"agent": "copilot", "space": f.sp.slug}}).
		fails(http.StatusNotFound, "not_found")

	// A new key under the same connection: the old one stops at once.
	var rekeyed cloudAgentResult
	f.do(call{method: "POST", path: agentPath(a.ID, ":rekey"), token: cli}).ok(http.StatusOK, &rekeyed)
	if rekeyed.Agent.ID != a.ID || rekeyed.Key == nil || rekeyed.Key.Secret == secret || len(rekeyed.Receipts) != 1 ||
		rekeyed.Receipts[0].Action != "rekeyed" {
		t.Fatalf("rekey = %+v", rekeyed)
	}
	f.do(call{method: "POST", path: startsPath(f.sp), token: secret, body: startBody(agentsFile(f.c1, ""))}).
		fails(http.StatusUnauthorized, "unauthorized")
	f.start(rekeyed.Key.Secret, startBody(agentsFile(f.c1, "")), map[string]string{"Idempotency-Key": uuid.NewString()})
	if n := f.loads(a.ID); n != 2 {
		t.Errorf("after the rekey, %d loads under the same connection, want 2", n)
	}
	// Only the person's own cloud agents are rekeyed here.
	if code := policyCode(t, f.do(call{method: "POST", path: agentPath(f.conn, ":rekey"), token: cli})); code != "autonomy_not_allowed" {
		t.Errorf("rekeying a V1 key's connection: %s", code)
	}
	f.do(call{method: "POST", path: agentPath(a.ID, ":rekey"), token: f.session(jy)}).fails(http.StatusNotFound, "not_found")

	// Raising it is the web's, and it stays in its one space.
	if code := policyCode(t, f.do(call{method: "PATCH", path: agentPath(a.ID, "/spaces/"+f.sp.slug), token: cli,
		header: map[string]string{"Idempotency-Key": uuid.NewString()},
		body:   map[string]any{"autonomy": "propose"}})); code != "cloud_propose_needs_web" {
		t.Errorf("raising a cloud agent from the CLI: %s", code)
	}
	f.do(call{method: "PATCH", path: agentPath(a.ID, "/spaces/"+other.slug), token: f.webSession(f.owner),
		header: map[string]string{"Idempotency-Key": uuid.NewString()}, sign: webSigned(f.owner, tamper{}),
		body: map[string]any{"autonomy": "read"}}).fails(http.StatusBadRequest, "invalid_request")

	// Disconnect revokes it.
	f.do(call{method: "POST", path: agentPath(a.ID, ":disconnect"), token: cli}).ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: startsPath(f.sp), token: rekeyed.Key.Secret, body: startBody(agentsFile(f.c1, ""))}).
		fails(http.StatusUnauthorized, "unauthorized")
	f.do(call{method: "POST", path: agentPath(a.ID, ":rekey"), token: cli}).fails(http.StatusConflict, "invalid_transition")
}

// A start key reaches the cloud start and nothing else: every other /v2
// operation answers 403 permission_denied before its handler runs.
func TestStartKeysReachOnlyTheCloudStart(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	made := f.cloudAgent(f.cliSession(f.owner), map[string]any{"agent": "claude-code", "space": f.sp.slug}, nil)
	secret := made.Key.Secret
	public := map[string]bool{"unsubscribeDreamEmail": true, "startPasskeySignIn": true, "finishPasskeySignIn": true}
	for _, op := range spec.Operations() {
		if public[op.ID] || op.ID == "startSession" {
			continue
		}
		sample := sampleRequests[op.ID]
		r := httptest.NewRequest(op.Method, sample.path, strings.NewReader(sample.body))
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", uuid.NewString())
		for k, v := range sample.header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		f.raw.ServeHTTP(rec, r)
		if code := errorCode(t, rec.Body.Bytes()); rec.Code != http.StatusForbidden || code != "permission_denied" {
			t.Errorf("%s %s with a start key = %d %s, want 403 permission_denied", op.Method, sample.path, rec.Code, code)
		}
	}
	// And the start itself works.
	f.start(secret, startBody(agentsFile(f.c1, "")), nil)
}

// Rule 8: a Memax key pasted into a memory is refused, not kept.
func TestAMemaxKeyIsNeverRemembered(t *testing.T) {
	t.Parallel()
	f := newStartFixture(t)
	r := f.do(call{method: "POST", path: memoriesPath(f.sp), token: f.tok,
		body: remember("Copilot's start key is mxk_"+strings.Repeat("ab", 32)+", in the repository secrets.", "conventions")})
	if code := policyCode(t, r); code != "secret_detected" {
		t.Errorf("a memory with a Memax key: %s", code)
	}
}
