package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The Ledger consent page (OAuthConsent) against the real flow: who gets
// it, what it is told about each space and the agent's abilities there
// (and that they are true once consent connects the agent), the grant it
// makes (never above Propose), Cancel, "Not you?", requests that ended,
// and posts from other sites.

const oauthTestApp = "https://app.memax.test"

// ledgerPage is a browser on the web app posting the Ledger consent form.
var ledgerPage = map[string]string{"Sec-Fetch-Site": "same-site", "Origin": oauthTestApp}

// project adds a project space on the V2 record to the flow's person, with
// `kept` kept memories and the canonical file as its target.
func (f *oauthFlow) project(name string, kept int) string {
	f.t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := f.authH.pool.Exec(ctx, `
		INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at)
		VALUES ($1, $2, $3, 'team', $4, 'project', now())`, id, name, name+"-"+id.String()[:6], f.user); err != nil {
		f.t.Fatalf("project: %v", err)
	}
	if _, err := f.authH.pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, f.user); err != nil {
		f.t.Fatalf("project: %v", err)
	}
	scope, err := f.h.ledger.UserScope(ctx, f.user)
	if err != nil {
		f.t.Fatal(err)
	}
	meta := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: f.user}, Scope: scope.Narrow(id),
			Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}
	}
	for i := range kept {
		res, err := f.h.ledger.Apply(ctx, &ledger.Remember{Meta: meta(), NewMemory: ledger.NewMemory{
			SpaceID: id, Statement: "Convention number " + string(rune('A'+i)) + " holds.", Section: ledger.SectionConventions}})
		if err != nil || res.Outcome != ledger.OutcomeApplied {
			f.t.Fatalf("keep: %v %+v", err, res.Policy)
		}
	}
	if res, err := f.h.ledger.Apply(ctx, &ledger.ReviseBrief{Meta: meta(), SpaceID: id, Title: name}); err != nil || res.Outcome != ledger.OutcomeApplied {
		f.t.Fatalf("brief: %v %+v", err, res.Policy)
	}
	if res, err := f.h.ledger.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(), SpaceID: id, Kind: ledger.TargetAgentsMD}); err != nil || res.Outcome != ledger.OutcomeApplied {
		f.t.Fatalf("target: %v %+v", err, res.Policy)
	}
	f.hubs = append(f.hubs, id.String())
	return id.String()
}

// signIn runs /oauth/authorize and the provider's callback as `user`, and
// returns the request, its consent token and where the callback sent the
// browser.
func (f *oauthFlow) signIn(q url.Values, user uuid.UUID) (request, token string, page *url.URL) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.h.Authorize(rec, httptest.NewRequest(http.MethodGet, oauthTestBase+"/oauth/authorize?"+q.Encode(), nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusTemporaryRedirect || loc.Host != "github.com" {
		f.t.Fatalf("authorize: %d %s", rec.Code, rec.Body.String())
	}
	request = strings.TrimPrefix(loc.Query().Get("state"), "mcp:")
	return f.callback(request, user)
}

// callback is the provider's callback for a pending request.
func (f *oauthFlow) callback(request string, user uuid.UUID) (string, string, *url.URL) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.h.HandleMCPCallback(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil), user.String(), request)
	page, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || page.Query().Get("request_id") != request {
		f.t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}
	return request, page.Query().Get("consent_token"), page
}

// read is the page's GET of the request.
func (f *oauthFlow) read(request, token string) (int, oauthConsentData) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.h.ConsentRequest(rec, httptest.NewRequest(http.MethodGet, oauthTestBase+"/oauth/authorize/consent-request?"+
		url.Values{"request_id": {request}, "consent_token": {token}}.Encode(), nil))
	var body struct {
		Data oauthConsentData `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Data
}

// post sends the consent form with the browser's headers.
func (f *oauthFlow) post(form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, oauthTestBase+"/oauth/authorize/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.Consent(rec, req)
	return rec
}

// ledgerForm is what the Ledger page posts: one space, read and propose.
func ledgerForm(request, token, decision, hub string) url.Values {
	form := url.Values{"session_id": {request}, "csrf_token": {token}, "decision": {decision}, "ui": {"v2"}}
	if hub != "" {
		form["hub_id"] = []string{hub}
		form["permission"] = []string{ScopeRead, ScopePropose}
	}
	return form
}

// testVerifier is the PKCE verifier of every authorizeQuery.
var testVerifier, testChallenge = pkce()

func authorizeQuery(clientID, redirect, scope string) url.Values {
	return url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "state": {"s1"},
		"code_challenge": {testChallenge}, "code_challenge_method": {"S256"}, "scope": {scope},
	}
}

func hubByID(d oauthConsentData, id string) oauthConsentHub {
	for _, h := range d.Hubs {
		if h.ID == id {
			return h
		}
	}
	return oauthConsentHub{}
}

// TestLedgerConsentSaysWhatIsTrue: a person with a space on V2 gets the
// Ledger page, which hears each space's kind, counts and file, and what
// the agent will and won't be able to do there, from policy. Allowing it
// grants memax:propose though the client asked to write, and the agent is
// connected to the one space, at the level the page was told.
func TestLedgerConsentSaysWhatIsTrue(t *testing.T) {
	f := newOAuthFlow(t)
	f.h.appBaseURL = oauthTestApp
	project := f.project("memax-v2", 3)
	personal, team := f.hubs[0], f.hubs[1]

	request, token, page := f.signIn(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"), f.user)
	if page.Scheme+"://"+page.Host != oauthTestApp || page.Path != "/oauth/authorize" {
		t.Fatalf("a person on V2 was sent to %s, want the Ledger page", page)
	}
	status, data := f.read(request, token)
	if status != http.StatusOK {
		t.Fatalf("consent request: %d", status)
	}
	if data.Person == nil || data.Person.Name != "ZZ" {
		t.Errorf("person = %+v, want ZZ", data.Person)
	}
	if data.ClientHost != "claude.example" || data.ClientName != "Claude Code" || data.AgentName != "claude-code" {
		t.Errorf("client = %q %q %q", data.ClientName, data.ClientHost, data.AgentName)
	}
	if data.ConsentScope != "memax:read memax:propose" || data.ExpiresIn <= 0 || data.ExpiresIn > 600 {
		t.Errorf("consent scope %q, expires in %d", data.ConsentScope, data.ExpiresIn)
	}

	p := hubByID(data, project)
	if p.SpaceKind != "project" || !p.OnV2 || p.KeptCount == nil || *p.KeptCount != 3 || p.PeopleCount != 1 {
		t.Errorf("project = %+v", p)
	}
	if len(p.Targets) != 1 || p.Targets[0].Kind != "agents_md" || p.Targets[0].Path != "AGENTS.md" {
		t.Errorf("project targets = %+v", p.Targets)
	}
	if p.Autonomy != "propose" ||
		!slices.Equal(p.Can, []string{abilityReadBrief, abilityPropose, abilityGate}) ||
		!slices.Equal(p.Cannot, []string{abilityKeep, abilityForget, abilityOtherSpaces}) {
		t.Errorf("project abilities: %s can %v cannot %v", p.Autonomy, p.Can, p.Cannot)
	}
	// Spaces still on V1 say what V1 does: what the agent writes is kept.
	for _, id := range []string{personal, team} {
		h := hubByID(data, id)
		if h.OnV2 || h.Autonomy != "" || h.KeptCount != nil ||
			!slices.Equal(h.Can, []string{abilityReadMemories, abilityAdd, abilityGate}) ||
			!slices.Equal(h.Cannot, []string{abilityForget, abilityOtherSpaces}) {
			t.Errorf("V1 space %s = %+v", id, h)
		}
	}
	if hubByID(data, personal).SpaceKind != "personal" || hubByID(data, team).SpaceKind != "team" {
		t.Errorf("V1 kinds = %q %q", hubByID(data, personal).SpaceKind, hubByID(data, team).SpaceKind)
	}

	rec := f.post(ledgerForm(request, token, "approve", project), ledgerPage)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Host != "localhost:53682" || loc.Query().Get("code") == "" ||
		loc.Query().Get("state") != "s1" || loc.Query().Get("iss") != oauthTestBase {
		t.Fatalf("allow: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// The client hears the narrower scope it was granted.
	status, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"code_verifier": {testVerifier}, "redirect_uri": {cimdRedirect}})
	if status != http.StatusOK || body["scope"] != "memax:read memax:propose" {
		t.Errorf("token: %d scope %v", status, body["scope"])
	}
	var scope string
	var hubs []string
	if err := f.authH.pool.QueryRow(context.Background(), `
		SELECT scope, hub_ids::text[] FROM oauth_grants WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, f.user).
		Scan(&scope, &hubs); err != nil {
		t.Fatal(err)
	}
	if scope != "memax:read memax:propose" || !slices.Equal(hubs, []string{project}) {
		t.Errorf("grant scope %q, spaces %v", scope, hubs)
	}
	var autonomy []string
	if err := f.authH.pool.QueryRow(context.Background(), `
		SELECT array_agg(s.autonomy || ':' || s.space_id::text)
		  FROM v2.agent_connections c JOIN v2.agent_connection_spaces s ON s.connection_id = c.id
		 WHERE c.credential_kind = 'oauth_grant' AND c.person_id = $1`, f.user).Scan(&autonomy); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(autonomy, []string{p.Autonomy + ":" + project}) {
		t.Errorf("connected %v, the page said %s in %s", autonomy, p.Autonomy, project)
	}
	if status, _ := f.read(request, token); status != http.StatusNotFound {
		t.Errorf("an answered request reads %d, want 404", status)
	}
}

// TestLedgerConsentAtRead: an agent that starts at Read (Cursor), or a
// client that asked only to read, is told it can only read: no proposals
// and no questions, and the connection agrees.
func TestLedgerConsentAtRead(t *testing.T) {
	f := newOAuthFlow(t)
	f.h.appBaseURL = oauthTestApp
	project := f.project("memax-v2", 0)
	const cursorID = "https://cursor.example/oauth/client-metadata"
	f.doc = `{"client_id":"` + cursorID + `","client_name":"Cursor","redirect_uris":["` + cimdRedirect + `"]}`

	for name, q := range map[string]url.Values{
		"cursor":    authorizeQuery(cursorID, cimdRedirect, "memax:read memax:write"),
		"read only": authorizeQuery(cimdClientID, cimdRedirect, "memax:read"),
	} {
		if name == "read only" {
			f.doc = `{"client_id":"` + cimdClientID + `","client_name":"Claude Code","redirect_uris":["` + cimdRedirect + `"]}`
		}
		request, token, _ := f.signIn(q, f.user)
		_, data := f.read(request, token)
		p := hubByID(data, project)
		if p.Autonomy != "read" || !slices.Equal(p.Can, []string{abilityReadBrief}) ||
			!slices.Equal(p.Cannot, []string{abilityPropose, abilityGate, abilityForget, abilityOtherSpaces}) {
			t.Errorf("%s: %s can %v cannot %v", name, p.Autonomy, p.Can, p.Cannot)
		}
		if rec := f.post(ledgerForm(request, token, "approve", project), ledgerPage); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: allow %d", name, rec.Code)
		}
	}
	var levels []string
	if err := f.authH.pool.QueryRow(context.Background(), `
		SELECT array_agg(DISTINCT s.autonomy) FROM v2.agent_connections c
		  JOIN v2.agent_connection_spaces s ON s.connection_id = c.id WHERE c.person_id = $1`, f.user).Scan(&levels); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(levels, []string{"read"}) {
		t.Errorf("connected at %v, want read", levels)
	}
}

// TestLedgerConsentCancelSwitchAndEndings: Cancel answers the client
// access_denied; "Not you?" signs the request out and back in at GitHub's
// account picker with a new token (the old one stops working); a request
// that expired or was answered sends the Ledger page to its ending; a
// space the person can't use comes back to the page with the same request.
func TestLedgerConsentCancelSwitchAndEndings(t *testing.T) {
	f := newOAuthFlow(t)
	f.h.appBaseURL = oauthTestApp
	project := f.project("memax-v2", 1)
	q := authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:propose")

	// Cancel.
	request, token, _ := f.signIn(q, f.user)
	rec := f.post(ledgerForm(request, token, "deny", ""), ledgerPage)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Host != "localhost:53682" || loc.Query().Get("error") != "access_denied" ||
		loc.Query().Get("state") != "s1" || loc.Query().Get("iss") != oauthTestBase || loc.Query().Get("code") != "" {
		t.Fatalf("cancel: %d %s", rec.Code, loc)
	}
	// It was used: posting again ends on the page.
	rec = f.post(ledgerForm(request, token, "approve", project), ledgerPage)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != oauthTestApp+"/oauth/authorize?ended=gone" {
		t.Errorf("a used request: %d %s", rec.Code, loc)
	}

	// Not you?
	request, token, _ = f.signIn(q, f.user)
	rec = f.post(ledgerForm(request, token, "switch", ""), ledgerPage)
	loc, _ = url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Host != "github.com" || loc.Query().Get("prompt") != "select_account" ||
		loc.Query().Get("state") != "mcp:"+request {
		t.Fatalf("not you: %d %s", rec.Code, loc)
	}
	if status, _ := f.read(request, token); status != http.StatusNotFound {
		t.Errorf("the old token still reads the request: %d", status)
	}
	other := uuid.New()
	if _, err := f.authH.pool.Exec(context.Background(), `INSERT INTO users (id, email, name, display_name) VALUES ($1, $2, 'jh', 'Jiahao')`,
		other, other.String()[:8]+"@oauth.test"); err != nil {
		t.Fatal(err)
	}
	_, token2, page := f.callback(request, other)
	if token2 == token || page.Path != "/oauth/consent" {
		t.Errorf("signed in again: token reused %t, page %s (Jiahao has no space on V2)", token2 == token, page.Path)
	}
	if status, data := f.read(request, token2); status != http.StatusOK || data.Person == nil || data.Person.Name != "Jiahao" || len(data.Hubs) != 0 {
		t.Errorf("after switching: %d %+v", status, data.Person)
	}
	// Jiahao can't connect ZZ's space: back to the page, same request.
	rec = f.post(ledgerForm(request, token2, "approve", project), ledgerPage)
	loc, _ = url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Path != "/oauth/authorize" || loc.Query().Get("error") != "space" ||
		loc.Query().Get("request_id") != request || loc.Query().Get("consent_token") != token2 {
		t.Errorf("someone else's space: %d %s", rec.Code, loc)
	}

	// Expired.
	request, token, _ = f.signIn(q, f.user)
	if _, err := f.authH.pool.Exec(context.Background(), `UPDATE oauth_authorization_requests SET expires_at = now() - interval '1 second' WHERE id = $1`, request); err != nil {
		t.Fatal(err)
	}
	rec = f.post(ledgerForm(request, token, "approve", project), ledgerPage)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != oauthTestApp+"/oauth/authorize?ended=expired" {
		t.Errorf("expired: %d %s", rec.Code, loc)
	}
	// V1's page still hears the API's answer.
	request, token, _ = f.signIn(q, f.user)
	form := ledgerForm(request, "not-the-token", "approve", project)
	form.Del("ui")
	if rec := f.post(form, ledgerPage); rec.Code != http.StatusBadRequest {
		t.Errorf("V1 page, wrong token: %d", rec.Code)
	}
}

// TestConsentRefusesOtherSites: a consent post another site started is
// refused before anything is read, even with the right token, and the
// request stays usable from Memax's own page.
func TestConsentRefusesOtherSites(t *testing.T) {
	f := newOAuthFlow(t)
	f.h.appBaseURL = oauthTestApp
	project := f.project("memax-v2", 0)
	request, token, _ := f.signIn(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:propose"), f.user)

	for name, headers := range map[string]map[string]string{
		"cross-site":            {"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		"a sibling subdomain":   {"Sec-Fetch-Site": "same-site", "Origin": "https://user-content.memax.test"},
		"cross-site, no origin": {"Sec-Fetch-Site": "cross-site"},
		"an old browser":        {"Origin": "https://evil.example"},
		"an opaque origin":      {"Origin": "null"},
		"not from a page":       {"Sec-Fetch-Site": "none"},
	} {
		rec := f.post(ledgerForm(request, token, "approve", project), headers)
		if rec.Code != http.StatusForbidden || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	// Cancel from another site is refused too: it would end the request.
	if rec := f.post(ledgerForm(request, token, "deny", ""), map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site cancel: %d", rec.Code)
	}
	if status, _ := f.read(request, token); status != http.StatusOK {
		t.Fatalf("the request after refused posts: %d", status)
	}
	// The API's own page (same origin) and a self-hosted web app on
	// another site (its Origin is APP_BASE_URL) are Memax's.
	if got := consentFetchProblem(httptest.NewRequest(http.MethodPost, "/", nil), nil); got != "" {
		t.Errorf("no browser headers: %q", got)
	}
	same := httptest.NewRequest(http.MethodPost, "/", nil)
	same.Header.Set("Sec-Fetch-Site", "same-origin")
	if got := consentFetchProblem(same, nil); got != "" {
		t.Errorf("same origin: %q", got)
	}
	selfHosted := httptest.NewRequest(http.MethodPost, "/", nil)
	selfHosted.Header.Set("Sec-Fetch-Site", "cross-site")
	selfHosted.Header.Set("Origin", "https://memax.example.org")
	if got := consentFetchProblem(selfHosted, []string{"https://memax.example.org"}); got != "" {
		t.Errorf("self-hosted web app: %q", got)
	}
	rec := f.post(ledgerForm(request, token, "approve", project), ledgerPage)
	if loc, _ := url.Parse(rec.Header().Get("Location")); rec.Code != http.StatusSeeOther || loc.Query().Get("code") == "" {
		t.Fatalf("from the Ledger page: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestConsentScopes: the Ledger page grants no more than Propose, whatever
// the client asked for, and never more than it asked for.
func TestConsentScopes(t *testing.T) {
	t.Parallel()
	for requested, want := range map[string]string{
		"memax:read memax:write":               "memax:read memax:propose",
		"memax:read memax:propose":             "memax:read memax:propose",
		"memax:read memax:propose memax:write": "memax:read memax:propose",
		"memax:read":                           "memax:read",
		"":                                     "memax:read memax:propose",
		"memax:write":                          "memax:propose",
	} {
		if got := consentScope(requested); got != want {
			t.Errorf("consentScope(%q) = %q, want %q", requested, got, want)
		}
	}
	// V1's page keeps what it had.
	if got := intersectScopes("memax:read memax:write", "memax:read memax:write"); got != "memax:read memax:write" {
		t.Errorf("V1 write = %q", got)
	}
}

// TestConsentLevelMatchesTheLedger: the level the page is told is the one
// ConnectAgent's policy picks, for each role, space rule and agent.
func TestConsentLevelMatchesTheLedger(t *testing.T) {
	t.Parallel()
	team := policy.Space{Name: "Memax team", Kind: policy.SpaceTeam}
	readFirst := policy.Space{Name: "acme", Kind: policy.SpaceProject, Rules: policy.Rules{NewAgentAutonomy: policy.AutonomyRead}}
	writeFirst := policy.Space{Name: "acme", Kind: policy.SpaceProject, Rules: policy.Rules{NewAgentAutonomy: policy.AutonomyWrite}}
	for _, c := range []struct {
		agent ledger.AgentKind
		role  policy.Role
		sp    policy.Space
		scope string
		want  policy.Autonomy
	}{
		{ledger.AgentCodex, policy.RoleOwner, team, "memax:read memax:propose", policy.AutonomyPropose},
		{ledger.AgentCodex, policy.RoleOwner, writeFirst, "memax:read memax:propose", policy.AutonomyPropose},
		{ledger.AgentCodex, policy.RoleOwner, readFirst, "memax:read memax:propose", policy.AutonomyRead},
		{ledger.AgentCodex, policy.RoleViewer, team, "memax:read memax:propose", policy.AutonomyPropose},
		{ledger.AgentCodex, policy.RoleOwner, team, "memax:read", policy.AutonomyRead},
		{ledger.AgentCursor, policy.RoleOwner, team, "memax:read memax:propose", policy.AutonomyRead},
		{ledger.AgentGeminiCLI, policy.RoleMember, team, "memax:read memax:propose", policy.AutonomyRead},
	} {
		if got := consentLevel(c.agent, c.role, c.sp, c.scope); got != c.want {
			t.Errorf("%s as %s in %+v with %q: %s, want %s", c.agent, c.role, c.sp.Rules, c.scope, got, c.want)
		}
	}
	// A viewer's agent proposes but never asks a person to keep for it
	// without them; nobody's agent forgets.
	can, cannot := v2Abilities("Codex", policy.RoleViewer, team, policy.AutonomyPropose)
	if !slices.Equal(can, []string{abilityReadBrief, abilityPropose, abilityGate}) || !slices.Contains(cannot, abilityForget) {
		t.Errorf("viewer's agent: can %v cannot %v", can, cannot)
	}
}
