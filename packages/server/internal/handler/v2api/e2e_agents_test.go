package v2api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// TestAgentConnectionsEndToEnd walks epic 1.8 through the real auth chain,
// the /v2 handlers, the ledger and Postgres, the way a person and their
// agents would: V1 credentials connected at Propose, autonomy changed per
// space on the web, quarantine and D15 held by assurance, pause and
// disconnect, another tenant kept out, and every write receipted with the
// agent's name.
func TestAgentConnectionsEndToEnd(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy, mallory := e.user("zz"), e.user("jy"), e.user("mallory")
	e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(zz, policy.SpaceTeam, "acme")
	e.join(team, jy, "contributor")
	e.space(mallory, policy.SpacePersonal, "Personal")
	theirs := e.space(mallory, policy.SpaceProject, "theirs")

	// V1 credentials, connected by the backfill at Propose.
	codexTok, codexGrant := e.grant(zz, "codex", []string{"memory:read", "memory:write"})
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "claude-code"})
	malloryTok, _ := e.grant(mallory, "codex", []string{"memory:read", "memory:write"})
	codex, claude := e.connection(codexGrant), e.connection(keyID)
	web := func(user uuid.UUID) (string, func(*http.Request, []byte)) {
		return e.webSession(user), webSigned(user, tamper{})
	}
	zzWeb, zzSign := web(zz)
	zzCLI := e.cliSession(zz)

	write := func(token string, sp space, body map[string]any) *resp {
		return e.do(call{method: "POST", path: memoriesPath(sp), token: token, body: body, header: map[string]string{"X-Memax-Via": "mcp"}})
	}
	setLevel := func(token string, sign func(*http.Request, []byte), sp space, level string) *resp {
		return e.do(call{method: "PATCH", path: agentPath(codex, "/spaces/"+sp.slug), token: token, body: map[string]any{"autonomy": level}, sign: sign})
	}
	expectPolicy := func(r *resp, code string) {
		t.Helper()
		if got := policyCode(t, r); got != code {
			t.Errorf("policy code %q, want %q", got, code)
		}
	}

	// Propose: a proposal, named for Codex.
	var res result
	write(codexTok, project, remember("Workers must be idempotent.", "conventions")).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || *res.Receipts[0].ActorID != codex || res.Receipts[0].Agent != "codex" {
		t.Errorf("propose: %s %+v", res.Outcome, res.Receipts[0])
	}
	// Read: refused, with where to change it.
	setLevel(zzCLI, nil, project, "read").ok(http.StatusOK, nil)
	expectPolicy(write(codexTok, project, remember("Read agents write nothing.", "conventions")), "read_only")
	// Raising needs the web; Write keeps.
	expectPolicy(setLevel(zzCLI, nil, project, "write"), "autonomy_needs_web")
	setLevel(zzWeb, zzSign, project, "write").ok(http.StatusOK, nil)
	write(codexTok, project, remember("Deploys go through Fly.", "conventions")).ok(http.StatusCreated, &res)
	if res.Outcome != "applied" || res.Memory.State != "kept" {
		t.Errorf("write: %s %s", res.Outcome, res.Memory.State)
	}
	// An external source at Write still proposes, quarantined.
	write(codexTok, project, map[string]any{"statement": "Pin pnpm via corepack.", "section": "conventions",
		"sources": []map[string]any{{"kind": "issue", "ref": "#212"}}}).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || res.Policy.Code != "external_source" || !res.Policy.Quarantine {
		t.Errorf("external at write: %s %s %v", res.Outcome, res.Policy.Code, res.Policy.Quarantine)
	}
	quarantined := res.Memory
	// The person keeps it on the web (human_web), not from the CLI.
	expectPolicy(e.do(call{method: "POST", path: memoryPath(quarantined, ":keep"), token: zzCLI}), "external_needs_review")
	e.do(call{method: "POST", path: memoryPath(quarantined, ":keep"), token: zzWeb, sign: zzSign}).ok(http.StatusOK, &res)
	if rc := res.Receipts[0]; rc.Assurance != "human_web" || rc.Via != "web" || *rc.ActorID != zz {
		t.Errorf("web keep of a quarantined proposal: %+v", rc)
	}

	// D15: in the team space a decision waits for a person on the web, even
	// from an agent at Write.
	setLevel(zzWeb, zzSign, team, "write").ok(http.StatusOK, nil)
	write(codexTok, team, map[string]any{"statement": "We release on Tuesdays.", "section": "decisions", "kind": "decision"}).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || res.Policy.Code != "decision_needs_web" {
		t.Errorf("team decision at write: %s %s", res.Outcome, res.Policy.Code)
	}
	decision := res.Memory
	expectPolicy(e.do(call{method: "POST", path: memoryPath(decision, ":keep"), token: e.cliSession(jy)}), "decision_needs_web")
	jyWeb, jySign := web(jy)
	e.do(call{method: "POST", path: memoryPath(decision, ":keep"), token: jyWeb, sign: jySign}).ok(http.StatusOK, &res)
	if rc := res.Receipts[0]; rc.Assurance != "human_web" || *rc.ActorID != jy {
		t.Errorf("web keep of a team decision: %+v", rc)
	}

	// Paused: refused everywhere, still reads.
	e.do(call{method: "POST", path: agentPath(codex, ":pause"), token: zzCLI}).ok(http.StatusOK, nil)
	expectPolicy(write(codexTok, project, remember("Paused.", "conventions")), "agent_paused")
	e.do(call{method: "GET", path: memoriesPath(project), token: codexTok}).ok(http.StatusOK, nil)

	// Another tenant can't see or touch any of it.
	var list page[agentConn]
	e.do(call{method: "GET", path: "/v2/agents", token: e.session(mallory)}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].PersonID != mallory {
		t.Errorf("mallory's agents = %+v", list.Items)
	}
	malloryWeb, mallorySign := web(mallory)
	for _, c := range []call{
		{method: "GET", path: agentPath(codex, "")},
		{method: "GET", path: "/v2/spaces/" + project.id.String() + "/agents"},
		{method: "PATCH", path: agentPath(codex, "/spaces/"+project.id.String()), body: map[string]any{"autonomy": "read"}},
		{method: "PATCH", path: agentPath(codex, "/spaces/"+theirs.id.String()), body: map[string]any{"autonomy": "write"}},
		{method: "POST", path: agentPath(codex, ":resume")},
		{method: "POST", path: agentPath(claude, ":disconnect")},
		{method: "POST", path: memoriesPath(project), body: remember("Planted.", "conventions")},
	} {
		c.token, c.sign = malloryWeb, mallorySign
		e.do(c).fails(http.StatusNotFound, "not_found")
	}
	// Mallory's own agent can't reach zz's spaces either.
	write(malloryTok, project, remember("Cross-tenant.", "conventions")).fails(http.StatusNotFound, "not_found")

	// Disconnect revokes the credential at once.
	e.do(call{method: "POST", path: agentPath(claude, ":disconnect"), token: zzWeb, sign: zzSign}).ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: "/v2/spaces", token: keyTok}).fails(http.StatusUnauthorized, "unauthorized")
	if n := e.count(`SELECT count(*) FROM api_keys WHERE id = $1 AND revoked_at IS NOT NULL`, keyID); n != 1 {
		t.Errorf("the API key wasn't revoked")
	}

	// Every write has a receipt naming the agent: each agent receipt is an
	// agent connection, with its slug; each connection row and space row
	// points at a receipt about that connection.
	if n := e.count(`SELECT count(*) FROM v2.receipts r
		 WHERE r.actor_kind = 'agent'
		   AND (r.agent IS NULL OR NOT EXISTS (SELECT 1 FROM v2.agent_connections c WHERE c.id = r.actor_id AND c.agent = r.agent))`); n != 0 {
		t.Errorf("%d agent receipts don't name a connection and its agent", n)
	}
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE actor_kind = 'agent' AND actor_id = $1 AND object_kind = 'memory'`, codex); n != 4 {
		t.Errorf("codex's memory receipts = %d, want 4 (propose, write, external, decision)", n)
	}
	if n := e.count(`SELECT count(*) FROM v2.memories m JOIN v2.receipts r ON r.id = m.created_receipt_id
		 WHERE r.actor_kind = 'agent' AND r.actor_id = $1`, codex); n != 4 {
		t.Errorf("memories created by codex = %d", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM v2.agent_connections c LEFT JOIN v2.receipts r ON r.id = c.last_receipt_id
		  WHERE r.id IS NULL OR r.object_kind <> 'agent' OR r.object_id <> c.id`,
		`SELECT count(*) FROM v2.agent_connection_spaces s LEFT JOIN v2.receipts r ON r.id = s.last_receipt_id
		  WHERE r.id IS NULL OR r.object_kind <> 'agent' OR r.object_id <> s.connection_id OR r.space_id <> s.space_id`,
	} {
		if n := e.count(q); n != 0 {
			t.Errorf("%d agent rows without a matching receipt: %s", n, q)
		}
	}
}
