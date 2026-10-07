package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// OAuth consent on the web (OAuthConsent): /oauth/authorize sends the
// browser to the web app, the person signs in there with any method, and
// their web session opens and answers the request. Covered here: people
// who signed in with an email code and with Google, ending in a token and
// a connection on their own account; what the page is told about each
// space (and that it holds once the agent is connected); the grant's
// scope; binding, "Not you?" and a second person; credentials that aren't
// a person on the web; requests from before the move; and endings.

// project adds a project space on the V2 record to a person, with `kept`
// kept memories and the canonical file as its target.
func (f *oauthFlow) project(owner uuid.UUID, name string, kept int) string {
	f.t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := f.authH.pool.Exec(ctx, `
		INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at)
		VALUES ($1, $2, $3, 'team', $4, 'project', now())`, id, name, name+"-"+id.String()[:6], owner); err != nil {
		f.t.Fatalf("project: %v", err)
	}
	if _, err := f.authH.pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner); err != nil {
		f.t.Fatalf("project: %v", err)
	}
	scope, err := f.h.ledger.UserScope(ctx, owner)
	if err != nil {
		f.t.Fatal(err)
	}
	meta := func() ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: owner}, Scope: scope.Narrow(id),
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
	if owner == f.user {
		f.hubs = append(f.hubs, id.String())
	}
	return id.String()
}

// open is the page reading a request as the person with that session.
func (f *oauthFlow) open(request, token string) (int, oauthRequestView) {
	f.t.Helper()
	rec := f.call(http.MethodGet, "/oauth/authorize/requests/"+request, token, nil)
	var body struct {
		Data oauthRequestView `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Data
}

// exchange trades a sign-in's one-time code for the web session's tokens,
// as the web app's /api/auth/exchange does.
func (f *oauthFlow) exchange(code string) string {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.authH.ExchangeCode(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/exchange",
		strings.NewReader(`{"code":"`+code+`"}`)))
	var body struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Data.AccessToken == "" {
		f.t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	return body.Data.AccessToken
}

// signInByEmail signs a new person in on the web with an email code, the
// way SignIn does, and returns them and their web session's token.
func (f *oauthFlow) signInByEmail(email string) (uuid.UUID, string) {
	f.t.Helper()
	post := func(fn http.HandlerFunc, path string, body any) map[string]any {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.RemoteAddr = "10.0.0.7:5000"
		rec := httptest.NewRecorder()
		fn(rec, req)
		var out struct {
			Data map[string]any `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			f.t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		return out.Data
	}
	post(f.authH.RequestEmailOTP, "/v1/auth/email/request", map[string]string{"email": email, "redirect_uri": oauthTestApp + "/signin/callback"})
	code := f.codes[email]
	if code == "" {
		f.t.Fatalf("no code was sent to %s", email)
	}
	out := post(f.authH.VerifyEmailOTP, "/v1/auth/email/verify", map[string]string{"email": email, "code": code})
	token := f.exchange(out["code"].(string))
	user, err := uuid.Parse(out["user_id"].(string))
	if err != nil {
		f.t.Fatal(err)
	}
	return user, token
}

// signInWithGoogle signs a new person in on the web with Google, from the
// point Google has vouched for them (the callback's login and one-time
// code), and returns them and their web session's token.
func (f *oauthFlow) signInWithGoogle(email, name string) (uuid.UUID, string) {
	f.t.Helper()
	user, err := f.authH.loginOrCreateUser(context.Background(), providerUser{
		Provider: "google", ProviderID: "g-" + uuid.NewString()[:8], Email: email, EmailVerified: true, Name: name,
	}, loginOpts{})
	if err != nil {
		f.t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	f.authH.completeLogin(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback", nil), user, oauthTestApp+"/signin/callback")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc == nil || loc.Query().Get("code") == "" {
		f.t.Fatalf("google sign-in: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	return uuid.MustParse(user.ID), f.exchange(loc.Query().Get("code"))
}

func authorizeQuery(clientID, redirect, scope string) url.Values {
	return url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "state": {"s1"},
		"code_challenge": {sharedChallenge}, "code_challenge_method": {"S256"}, "scope": {scope},
	}
}

func spaceByID(v oauthRequestView, id string) oauthConsentSpace {
	for _, s := range v.Spaces {
		if s.ID == id {
			return s
		}
	}
	return oauthConsentSpace{}
}

// TestConsentForPeopleWhoSignedInWithoutGitHub: a person who signed up with
// an email code, and one with Google, each connect an agent: no GitHub
// step, the request opens under their own web session, and the token and
// the agent connection are theirs.
func TestConsentForPeopleWhoSignedInWithoutGitHub(t *testing.T) {
	f := newOAuthFlow(t)
	for _, who := range []string{"email", "google"} {
		var person uuid.UUID
		var token string
		if who == "email" {
			person, token = f.signInByEmail("ada@example.com")
		} else {
			person, token = f.signInWithGoogle("jiahao@example.com", "Jiahao")
		}
		space := f.project(person, who+"-project", 1)

		verifier, challenge := pkce()
		q := authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write")
		q.Set("code_challenge", challenge)
		request, page := f.start(q)
		if request == "" || page.Host != "app.memax.test" {
			t.Fatalf("%s: authorize sent the browser to %s, want the web app's page", who, page)
		}
		status, view := f.open(request, token)
		if status != http.StatusOK || len(view.Spaces) == 0 {
			t.Fatalf("%s: open: %d %+v", who, status, view)
		}
		if who == "google" && view.Person.Name != "Jiahao" {
			t.Errorf("google: signed in as %q", view.Person.Name)
		}
		if p := spaceByID(view, space); !p.OnV2 || p.Autonomy != "propose" {
			t.Errorf("%s: their project = %+v", who, p)
		}
		loc := f.decide(request, token, "approve", space)
		if loc.Host != "localhost:53682" || loc.Query().Get("code") == "" || loc.Query().Get("state") != "s1" {
			t.Fatalf("%s: allow sent the browser to %s", who, loc)
		}
		code, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
			"code_verifier": {verifier}, "redirect_uri": {cimdRedirect}})
		if code != http.StatusOK {
			t.Fatalf("%s: token: %d %v", who, code, body)
		}
		claims, err := auth.VerifyAccessToken(body["access_token"].(string), []byte(oauthTestSecret))
		if err != nil || claims.Sub != person.String() {
			t.Fatalf("%s: token is %+v (%v), want %s's", who, claims, err, person)
		}
		var owner, autonomy, hub string
		if err := f.authH.pool.QueryRow(context.Background(), `
			SELECT c.person_id::text, s.autonomy, s.space_id::text
			  FROM v2.agent_connections c JOIN v2.agent_connection_spaces s ON s.connection_id = c.id
			 WHERE c.credential_kind = 'oauth_grant' AND c.credential_id = $1`, claims.GrantID).Scan(&owner, &autonomy, &hub); err != nil {
			t.Fatalf("%s: connection: %v", who, err)
		}
		if owner != person.String() || autonomy != "propose" || hub != space {
			t.Errorf("%s: connection is %s's at %s in %s", who, owner, autonomy, hub)
		}
	}
}

// TestConsentSaysWhatIsTrue: the page hears each space's kind, counts and
// file, the level the agent gets and how far a person may raise it, and
// what the agent will and won't be able to do, from policy. The grant
// carries the scope the client asked for (memax:write here); the
// connection is made at the level the page showed, in the one space.
func TestConsentSaysWhatIsTrue(t *testing.T) {
	f := newOAuthFlow(t)
	project := f.project(f.user, "memax-v2", 3)
	personal, team := f.hubs[0], f.hubs[1]
	token := f.webSession(f.user)

	request, _ := f.start(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"))
	status, view := f.open(request, token)
	if status != http.StatusOK {
		t.Fatalf("open: %d", status)
	}
	if view.Person.Name != "ZZ" || view.ClientHost != "claude.example" || view.ClientName != "Claude Code" ||
		view.AgentName != "claude-code" || view.Scope != "memax:read memax:write" || view.ExpiresIn <= 0 || view.ExpiresIn > 600 {
		t.Errorf("request = %+v", view)
	}
	p := spaceByID(view, project)
	if p.Kind != "project" || !p.OnV2 || p.Memories == nil || *p.Memories != 3 || p.People != 1 {
		t.Errorf("project = %+v", p)
	}
	if len(p.Targets) != 1 || p.Targets[0].Kind != "agents_md" || p.Targets[0].Path != "AGENTS.md" {
		t.Errorf("project targets = %+v", p.Targets)
	}
	if p.Autonomy != "propose" || p.Ceiling != "write" ||
		!slices.Equal(p.Can, []string{abilityReadBrief, abilityPropose, abilityGate}) ||
		!slices.Equal(p.Cannot, []string{abilityKeep, abilityForget, abilityOtherSpaces}) {
		t.Errorf("project: %s up to %s, can %v cannot %v", p.Autonomy, p.Ceiling, p.Can, p.Cannot)
	}
	// Spaces still on V1 say what V1 does: what the agent writes is kept.
	for _, id := range []string{personal, team} {
		h := spaceByID(view, id)
		if h.OnV2 || h.Autonomy != "" || h.Ceiling != "" ||
			!slices.Equal(h.Can, []string{abilityReadMemories, abilityAdd, abilityGate}) ||
			!slices.Equal(h.Cannot, []string{abilityForget, abilityOtherSpaces}) {
			t.Errorf("V1 space %s = %+v", id, h)
		}
	}

	loc := f.decide(request, token, "approve", project)
	if loc.Host != "localhost:53682" || loc.Query().Get("code") == "" || loc.Query().Get("iss") != oauthTestBase {
		t.Fatalf("allow: %s", loc)
	}
	var scope string
	var hubs []string
	if err := f.authH.pool.QueryRow(context.Background(), `
		SELECT scope, hub_ids::text[] FROM oauth_grants WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, f.user).
		Scan(&scope, &hubs); err != nil {
		t.Fatal(err)
	}
	if scope != "memax:read memax:write" || !slices.Equal(hubs, []string{project}) {
		t.Errorf("grant scope %q, spaces %v", scope, hubs)
	}
	var connected []string
	if err := f.authH.pool.QueryRow(context.Background(), `
		SELECT array_agg(s.autonomy || ':' || s.space_id::text)
		  FROM v2.agent_connections c JOIN v2.agent_connection_spaces s ON s.connection_id = c.id
		 WHERE c.credential_kind = 'oauth_grant' AND c.person_id = $1`, f.user).Scan(&connected); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(connected, []string{p.Autonomy + ":" + project}) {
		t.Errorf("connected %v, the page said %s in %s", connected, p.Autonomy, project)
	}
	if status, _ := f.open(request, token); status != http.StatusNotFound {
		t.Errorf("an answered request opens with %d, want 404", status)
	}
}

// TestConsentAtRead: an agent that starts at Read (Cursor), or a client
// that asked only to read, is told it can only read, and how far a person
// may raise it; the connection agrees.
func TestConsentAtRead(t *testing.T) {
	f := newOAuthFlow(t)
	project := f.project(f.user, "memax-v2", 0)
	token := f.webSession(f.user)
	const cursorID = "https://cursor.example/oauth/client-metadata"
	for _, c := range []struct {
		name, clientID, clientName, scope, ceiling string
	}{
		{"cursor", cursorID, "Cursor", "memax:read memax:write", "write"},
		{"read only", cimdClientID, "Claude Code", "memax:read", "read"},
	} {
		f.doc = `{"client_id":"` + c.clientID + `","client_name":"` + c.clientName + `","redirect_uris":["` + cimdRedirect + `"]}`
		request, _ := f.start(authorizeQuery(c.clientID, cimdRedirect, c.scope))
		_, view := f.open(request, token)
		p := spaceByID(view, project)
		if p.Autonomy != "read" || p.Ceiling != c.ceiling || !slices.Equal(p.Can, []string{abilityReadBrief}) ||
			!slices.Equal(p.Cannot, []string{abilityPropose, abilityGate, abilityForget, abilityOtherSpaces}) {
			t.Errorf("%s: %s up to %s, can %v cannot %v", c.name, p.Autonomy, p.Ceiling, p.Can, p.Cannot)
		}
		f.decide(request, token, "approve", project)
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

// TestConsentBindsTheFirstPerson: the first person to open a request has
// it; a second person is told it isn't there, and can't answer or release
// it. "Not you?" releases it, and then the next person to open it has it.
func TestConsentBindsTheFirstPerson(t *testing.T) {
	f := newOAuthFlow(t)
	project := f.project(f.user, "memax-v2", 0)
	zz := f.webSession(f.user)
	other := uuid.New()
	if _, err := f.authH.pool.Exec(context.Background(), `INSERT INTO users (id, email, name, display_name) VALUES ($1, $2, 'jh', 'Jiahao')`,
		other, other.String()[:8]+"@oauth.test"); err != nil {
		t.Fatal(err)
	}
	jh := f.webSession(other)
	theirs := f.project(other, "theirs", 0)
	request, _ := f.start(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"))

	if status, view := f.open(request, zz); status != http.StatusOK || view.Person.Name != "ZZ" {
		t.Fatalf("ZZ opens: %d %+v", status, view.Person)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"opens":    f.call(http.MethodGet, "/oauth/authorize/requests/"+request, jh, nil),
		"allows":   f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", jh, map[string]string{"decision": "approve", "space_id": theirs}),
		"cancels":  f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", jh, map[string]string{"decision": "deny"}),
		"releases": f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/release", jh, nil),
	} {
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "consent_request_not_found") {
			t.Errorf("Jiahao %s ZZ's request: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// ZZ can't connect someone else's space.
	if rec := f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", zz,
		map[string]string{"decision": "approve", "space_id": theirs}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("ZZ allows Jiahao's space: %d %s", rec.Code, rec.Body.String())
	}

	// Not you?
	if rec := f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/release", zz, nil); rec.Code != http.StatusOK {
		t.Fatalf("release: %d %s", rec.Code, rec.Body.String())
	}
	if status, view := f.open(request, jh); status != http.StatusOK || view.Person.Name != "Jiahao" ||
		spaceByID(view, theirs).ID == "" || spaceByID(view, project).ID != "" {
		t.Fatalf("Jiahao opens after: %d %+v", status, view)
	}
	if status, _ := f.open(request, zz); status != http.StatusNotFound {
		t.Errorf("ZZ opens Jiahao's request: %d", status)
	}
	loc := f.decide(request, jh, "approve", theirs)
	if loc.Query().Get("code") == "" {
		t.Fatalf("Jiahao allows: %s", loc)
	}
	var owner string
	if err := f.authH.pool.QueryRow(context.Background(), `SELECT user_id::text FROM oauth_grants ORDER BY created_at DESC LIMIT 1`).Scan(&owner); err != nil || owner != other.String() {
		t.Errorf("the grant is %s's (%v), want Jiahao's", owner, err)
	}
}

// TestConsentNeedsAPersonOnTheWeb: only a session the web app was issued
// opens or answers a request. Agents' credentials, a CLI session and a
// request with no token (a page on another site can't send the session's,
// which lives in the web app's server) are refused, and the request is
// untouched.
func TestConsentNeedsAPersonOnTheWeb(t *testing.T) {
	f := newOAuthFlow(t)
	project := f.project(f.user, "memax-v2", 0)
	request, _ := f.start(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"))
	cli, err := auth.SignSessionToken(f.user.String(), auth.SurfaceCLI, []byte(oauthTestSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// An agent's OAuth token, from an earlier consent: bound to the MCP
	// endpoints, it is no credential here at all.
	earlier := f.authorize(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"), project)
	_, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {earlier.Query().Get("code")},
		"code_verifier": {sharedVerifier}, "redirect_uri": {cimdRedirect}})
	grantToken, _ := body["access_token"].(string)
	// An agent's own token (the legacy agent JWT `memax setup` issued).
	agent, err := auth.Sign(auth.Claims{Sub: f.user.String(), AgentName: "codex"}, []byte(oauthTestSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		token string
		want  int
	}{
		"a CLI session":         {cli, http.StatusForbidden},
		"an agent's token":      {agent, http.StatusForbidden},
		"an agent's MCP token":  {grantToken, http.StatusUnauthorized},
		"no token (cross-site)": {"", http.StatusUnauthorized},
	} {
		if c.token == "" && name != "no token (cross-site)" {
			t.Fatalf("%s: no token", name)
		}
		for _, rec := range []*httptest.ResponseRecorder{
			f.call(http.MethodGet, "/oauth/authorize/requests/"+request, c.token, nil),
			f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", c.token, map[string]string{"decision": "approve", "space_id": project}),
			f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/release", c.token, nil),
		} {
			if rec.Code != c.want {
				t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body.String(), c.want)
			}
		}
	}
	var bound *string
	if err := f.authH.pool.QueryRow(context.Background(), `SELECT user_id::text FROM oauth_authorization_requests WHERE id = $1`, request).Scan(&bound); err != nil || bound != nil {
		t.Errorf("the request after refusals: bound to %v (%v)", bound, err)
	}
}

// sharedVerifier and sharedChallenge are every authorizeQuery's PKCE
// pair, so a test can redeem the code it leads to.
var sharedVerifier, sharedChallenge = pkce()

// TestConsentCancelExpiryAndOldRequests: Cancel answers the client
// access_denied (with state and iss) and ends the request; an expired
// request says so; requests from before sign-in moved to the web (at
// GitHub, or on V1's page) go on on the web, where the first person to
// open them has them.
func TestConsentCancelExpiryAndOldRequests(t *testing.T) {
	f := newOAuthFlow(t)
	f.project(f.user, "memax-v2", 0)
	token := f.webSession(f.user)
	q := authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:propose")

	request, _ := f.start(q)
	f.open(request, token)
	loc := f.decide(request, token, "deny", "")
	if loc.Host != "localhost:53682" || loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "s1" ||
		loc.Query().Get("iss") != oauthTestBase || loc.Query().Get("code") != "" {
		t.Fatalf("cancel: %s", loc)
	}
	if status, _ := f.open(request, token); status != http.StatusNotFound {
		t.Errorf("a cancelled request opens with %d", status)
	}

	request, _ = f.start(q)
	if _, err := f.authH.pool.Exec(context.Background(), `UPDATE oauth_authorization_requests SET expires_at = now() - interval '1 second' WHERE id = $1`, request); err != nil {
		t.Fatal(err)
	}
	if rec := f.call(http.MethodGet, "/oauth/authorize/requests/"+request, token, nil); rec.Code != http.StatusGone ||
		!strings.Contains(rec.Body.String(), "consent_request_expired") {
		t.Errorf("expired: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", token, map[string]string{"decision": "maybe"}); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown decision: %d", rec.Code)
	}

	// A request GitHub returns with (state "mcp:<id>") goes on on the web;
	// nobody is signed in by it.
	request, _ = f.start(q)
	f.authH.SetMCPOAuth(f.h)
	rec := httptest.NewRecorder()
	f.authH.GitHubCallback(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback?code=gh&state=mcp:"+request, nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != oauthTestApp+"/oauth/authorize?request="+request {
		t.Fatalf("GitHub's callback: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// A request the old callback bound (it carries a consent token), posted
	// from V1's page loaded before the deploy, goes on on the web too, and
	// the person who opens it there has it.
	githubMade := uuid.New()
	if _, err := f.authH.pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'gh')`,
		githubMade, githubMade.String()[:8]+"@github.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.authH.pool.Exec(context.Background(), `UPDATE oauth_authorization_requests SET user_id = $2, csrf_token = 'old' WHERE id = $1`, request, githubMade); err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest(http.MethodPost, "/oauth/authorize/consent", strings.NewReader(url.Values{"session_id": {request}, "csrf_token": {"old"}, "decision": {"approve"}}.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	f.h.LegacyConsent(rec, post)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != oauthTestApp+"/oauth/authorize?request="+request {
		t.Fatalf("V1's form post: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if status, view := f.open(request, token); status != http.StatusOK || view.Person.Name != "ZZ" {
		t.Errorf("an old request on the web: %d %+v", status, view.Person)
	}
}

// TestConsentScopes: a consent grants the scope the client asked for, up
// to memax:write; the level is the connection's.
func TestConsentScopes(t *testing.T) {
	t.Parallel()
	for requested, want := range map[string]string{
		"memax:read memax:write":               "memax:read memax:write",
		"memax:read memax:propose":             "memax:read memax:propose",
		"memax:read memax:propose memax:write": "memax:read memax:propose memax:write",
		"memax:read":                           "memax:read",
		"":                                     "memax:read memax:write",
		"memax:write":                          "memax:write",
	} {
		if got := consentScope(requested); got != want {
			t.Errorf("consentScope(%q) = %q, want %q", requested, got, want)
		}
	}
	// A client that asked to write and was allowed to propose gets propose.
	if got := intersectScopes("memax:read memax:write", "memax:read memax:propose"); got != "memax:read memax:propose" {
		t.Errorf("intersectScopes write → propose = %q", got)
	}
}

// TestConsentLevelMatchesTheLedger: the level the page is told is the one
// ConnectAgent's policy picks, and the ceiling the most a person may later
// allow, for each role, space rule, agent and scope.
func TestConsentLevelMatchesTheLedger(t *testing.T) {
	t.Parallel()
	team := policy.Space{Name: "Memax team", Kind: policy.SpaceTeam}
	readFirst := policy.Space{Name: "acme", Kind: policy.SpaceProject, Rules: policy.Rules{NewAgentAutonomy: policy.AutonomyRead}}
	writeFirst := policy.Space{Name: "acme", Kind: policy.SpaceProject, Rules: policy.Rules{NewAgentAutonomy: policy.AutonomyWrite}}
	ownersKeep := policy.Space{Name: "acme", Kind: policy.SpaceTeam, Rules: policy.Rules{Keep: policy.WhoOwners}}
	const write, propose, read = "memax:read memax:write", "memax:read memax:propose", "memax:read"
	for _, c := range []struct {
		agent          ledger.AgentKind
		role           policy.Role
		sp             policy.Space
		scope          string
		level, ceiling policy.Autonomy
	}{
		{ledger.AgentCodex, policy.RoleOwner, team, write, policy.AutonomyPropose, policy.AutonomyWrite},
		{ledger.AgentCodex, policy.RoleOwner, writeFirst, write, policy.AutonomyPropose, policy.AutonomyWrite},
		{ledger.AgentCodex, policy.RoleOwner, readFirst, write, policy.AutonomyRead, policy.AutonomyWrite},
		{ledger.AgentCodex, policy.RoleOwner, team, propose, policy.AutonomyPropose, policy.AutonomyPropose},
		{ledger.AgentCodex, policy.RoleOwner, team, read, policy.AutonomyRead, policy.AutonomyRead},
		{ledger.AgentCodex, policy.RoleViewer, team, write, policy.AutonomyPropose, policy.AutonomyPropose},
		{ledger.AgentCodex, policy.RoleMember, ownersKeep, write, policy.AutonomyPropose, policy.AutonomyPropose},
		{ledger.AgentCursor, policy.RoleOwner, team, write, policy.AutonomyRead, policy.AutonomyWrite},
		{ledger.AgentGeminiCLI, policy.RoleMember, team, write, policy.AutonomyRead, policy.AutonomyWrite},
	} {
		if got := consentLevel(c.agent, c.role, c.sp, c.scope); got != c.level {
			t.Errorf("%s as %s in %+v with %q: level %s, want %s", c.agent, c.role, c.sp.Rules, c.scope, got, c.level)
		}
		if got := consentCeiling(c.role, c.sp, c.scope); got != c.ceiling {
			t.Errorf("%s as %s in %+v with %q: ceiling %s, want %s", c.agent, c.role, c.sp.Rules, c.scope, got, c.ceiling)
		}
	}
	can, cannot := v2Abilities("Codex", policy.RoleViewer, team, policy.AutonomyPropose)
	if !slices.Equal(can, []string{abilityReadBrief, abilityPropose, abilityGate}) || !slices.Contains(cannot, abilityForget) {
		t.Errorf("viewer's agent: can %v cannot %v", can, cannot)
	}
}
