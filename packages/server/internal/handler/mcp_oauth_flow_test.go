package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/safefetch"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

const (
	oauthTestBase   = "https://api.memax.test"
	oauthTestApp    = "https://app.memax.test"
	oauthTestSecret = "mcp-oauth-flow-test-secret-0123456789"
	cimdClientID    = "https://claude.example/oauth/claude-code-client-metadata"
	cimdRedirect    = "http://localhost:53682/callback"
)

// oauthFlow is one database with the MCP OAuth handlers in front of it.
type oauthFlow struct {
	t     *testing.T
	authH *AuthHandler
	h     *MCPOAuthHandler
	user  uuid.UUID
	hubs  []string
	// fetched counts metadata document fetches.
	fetched int
	doc     string
	// web is the consent page's API, behind the real auth middleware, as
	// the web app's proxy reaches it.
	web http.Handler
	// codes are the email sign-in codes sent, by address.
	codes map[string]string
}

func newOAuthFlow(t *testing.T) *oauthFlow {
	t.Helper()
	t.Setenv("API_BASE_URL", oauthTestBase)
	st, pool := testdb.Acquire(t)
	f := &oauthFlow{t: t, user: uuid.New()}
	f.authH = &AuthHandler{pool: pool, jwtSecret: []byte(oauthTestSecret), store: st,
		registrationMode: "open", redirectAllowlist: []string{oauthTestApp}}
	f.codes = map[string]string{}
	f.authH.SetEnqueueEmail(func(_, to string, vars map[string]string) error {
		f.codes[to] = vars["Code"]
		return nil
	})
	f.h = &MCPOAuthHandler{authH: f.authH, baseURL: oauthTestBase, appBaseURL: oauthTestApp,
		ledger: ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))}
	chain := RequireAuth([]byte(oauthTestSecret), f.authH.ResolveAPIKey, f.authH.ResolveOAuthGrant)
	mux := http.NewServeMux()
	mux.Handle("GET /oauth/authorize/requests/{id}", chain(http.HandlerFunc(f.h.OpenRequest)))
	mux.Handle("POST /oauth/authorize/requests/{id}/decision", chain(http.HandlerFunc(f.h.DecideRequest)))
	mux.Handle("POST /oauth/authorize/requests/{id}/release", chain(http.HandlerFunc(f.h.ReleaseRequest)))
	f.web = mux
	f.doc = `{"client_id":"` + cimdClientID + `","client_name":"Claude Code","redirect_uris":["` + cimdRedirect + `"],"token_endpoint_auth_method":"none"}`
	f.h.fetchMetadata = func(_ context.Context, u string) (*safefetch.FetchResult, error) {
		f.fetched++
		return &safefetch.FetchResult{FinalURL: u, StatusCode: 200, ContentType: "application/json", Body: []byte(f.doc)}, nil
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'ZZ')`, f.user, f.user.String()[:8]+"@oauth.test")
	for _, kind := range []string{"personal", "team"} {
		id := uuid.New()
		exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, $2, $3, $2, $4)`,
			id, kind, kind+"-"+id.String()[:8], f.user)
		exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, f.user)
		f.hubs = append(f.hubs, id.String())
	}
	return f
}

func pkce() (verifier, challenge string) {
	verifier = strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// webSession is an access token for a session the web app was issued (its
// surface), as signing in on the web gives one, whatever the method.
func (f *oauthFlow) webSession(user uuid.UUID) string {
	f.t.Helper()
	tok, err := auth.SignSessionToken(user.String(), auth.SurfaceWeb, []byte(oauthTestSecret), time.Hour)
	if err != nil {
		f.t.Fatal(err)
	}
	return tok
}

// start runs /oauth/authorize. A request it stores sends the browser to
// the web app's page for it (request is its ID); a refusal goes back to
// the client (loc).
func (f *oauthFlow) start(q url.Values) (request string, loc *url.URL) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.h.Authorize(rec, httptest.NewRequest(http.MethodGet, oauthTestBase+"/oauth/authorize?"+q.Encode(), nil))
	loc, _ = url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther {
		f.t.Fatalf("authorize: %d %s", rec.Code, rec.Body.String())
	}
	if loc.Scheme+"://"+loc.Host == oauthTestApp && loc.Path == "/oauth/authorize" {
		return loc.Query().Get("request"), loc
	}
	return "", loc
}

// call is the consent page calling the API through the web app's proxy:
// the session's access token, and a JSON body.
func (f *oauthFlow) call(method, path, token string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, oauthTestBase+path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.web.ServeHTTP(rec, req)
	return rec
}

// decide answers a request as the person with that session, and returns
// where the API sends the browser.
func (f *oauthFlow) decide(request, token, decision, space string) *url.URL {
	f.t.Helper()
	body := map[string]string{"decision": decision}
	if space != "" {
		body["space_id"] = space
	}
	rec := f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", token, body)
	var out struct {
		Data oauthDecided `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		f.t.Fatalf("decide %s: %d %s", decision, rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(out.Data.RedirectTo)
	return loc
}

// authorize runs /oauth/authorize, opens the request on the web as the
// flow's person and allows it for one space, and returns the redirect to
// the client (or Authorize's refusal).
func (f *oauthFlow) authorize(q url.Values, space string) *url.URL {
	f.t.Helper()
	request, loc := f.start(q)
	if request == "" {
		return loc
	}
	token := f.webSession(f.user)
	if rec := f.call(http.MethodGet, "/oauth/authorize/requests/"+request, token, nil); rec.Code != http.StatusOK {
		f.t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	return f.decide(request, token, "approve", space)
}

func (f *oauthFlow) token(form url.Values) (int, map[string]any) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, oauthTestBase+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.h.Token(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// TestMCPOAuthCIMDClientConnectsAnAudienceBoundAgent walks the whole flow
// with a Client ID Metadata Document client: it registers by its URL, the
// authorization response names the issuer (RFC 9207), consent connects an
// agent connection that records the CIMD URL, and the tokens are bound to
// the MCP resource the client asked for (RFC 8707).
func TestMCPOAuthCIMDClientConnectsAnAudienceBoundAgent(t *testing.T) {
	f := newOAuthFlow(t)
	verifier, challenge := pkce()
	resource := oauthTestBase + "/mcp"
	loc := f.authorize(url.Values{
		"client_id": {cimdClientID}, "redirect_uri": {cimdRedirect}, "state": {"s1"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"scope": {"memax:read memax:propose"}, "resource": {resource},
	}, f.hubs[0])
	if got := loc.Query().Get("iss"); got != oauthTestBase {
		t.Errorf("authorization response iss = %q, want %q", got, oauthTestBase)
	}
	if loc.Query().Get("state") != "s1" || loc.Query().Get("code") == "" {
		t.Fatalf("authorization response = %s", loc)
	}
	if f.fetched != 1 {
		t.Errorf("metadata document fetched %d times, want 1", f.fetched)
	}

	// A token request for another resource is refused.
	status, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {cimdRedirect}, "resource": {oauthTestBase + "/mcp/chatgpt"}})
	if status != http.StatusBadRequest || body["error"] != "invalid_target" {
		t.Fatalf("token for another resource: %d %v", status, body)
	}

	loc = f.authorize(url.Values{
		"client_id": {cimdClientID}, "redirect_uri": {cimdRedirect}, "state": {"s2"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"scope": {"memax:read memax:propose"}, "resource": {resource},
	}, f.hubs[0])
	if f.fetched != 1 {
		t.Errorf("a fresh metadata document was fetched again (%d fetches)", f.fetched)
	}
	status, body = f.token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {cimdRedirect}, "resource": {resource}})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, body)
	}
	if body["scope"] != "memax:read memax:propose" {
		t.Errorf("token scope = %v", body["scope"])
	}
	access, _ := body["access_token"].(string)
	claims, err := auth.VerifyAccessToken(access, []byte(oauthTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Iss != oauthTestBase || len(claims.Aud) != 1 || claims.Aud[0] != resource {
		t.Errorf("claims iss=%q aud=%v, want %q [%q]", claims.Iss, claims.Aud, oauthTestBase, resource)
	}

	// The grant records the resource and scope; the agent connection the
	// CIMD URL, at the space's default autonomy (Propose), in the one space
	// the person chose.
	ctx := context.Background()
	var grantResource, grantScope string
	if err := f.authH.pool.QueryRow(ctx, `SELECT resource, scope FROM oauth_grants WHERE id = $1`, claims.GrantID).Scan(&grantResource, &grantScope); err != nil {
		t.Fatal(err)
	}
	if grantResource != resource || grantScope != "memax:read memax:propose" {
		t.Errorf("grant resource=%q scope=%q", grantResource, grantScope)
	}
	var agent, clientID string
	var spaces int
	if err := f.authH.pool.QueryRow(ctx, `
		SELECT c.agent, c.client_id, (SELECT count(*) FROM v2.agent_connection_spaces s WHERE s.connection_id = c.id AND s.autonomy = 'propose')
		  FROM v2.agent_connections c WHERE c.credential_kind = 'oauth_grant' AND c.credential_id = $1`, claims.GrantID).
		Scan(&agent, &clientID, &spaces); err != nil {
		t.Fatalf("agent connection: %v", err)
	}
	if agent != "claude-code" || clientID != cimdClientID || spaces != 1 {
		t.Errorf("connection agent=%s client_id=%s spaces at propose=%d", agent, clientID, spaces)
	}

	// The token works at /mcp only.
	chain := RequireAuth([]byte(oauthTestSecret), f.authH.ResolveAPIKey, f.authH.ResolveOAuthGrant)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if GetGrant(r).AutonomyCeiling() != "propose" {
				t.Errorf("ceiling = %q, want propose", GetGrant(r).AutonomyCeiling())
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	for path, want := range map[string]int{"/mcp": http.StatusNoContent, "/mcp/chatgpt": http.StatusUnauthorized, "/v1/memories": http.StatusUnauthorized} {
		req := httptest.NewRequest(http.MethodPost, oauthTestBase+path, nil)
		req.Header.Set("Authorization", "Bearer "+access)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d (%s)", path, rec.Code, want, rec.Body.String())
		}
		if path == "/mcp/chatgpt" && !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Errorf("audience mismatch challenge = %q", rec.Header().Get("WWW-Authenticate"))
		}
	}

	// A refreshed token keeps the audience and the issuer.
	status, body = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {body["refresh_token"].(string)}})
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %v", status, body)
	}
	refreshed, err := auth.VerifyAccessToken(body["access_token"].(string), []byte(oauthTestSecret))
	if err != nil || refreshed.Iss != oauthTestBase || !refreshed.Aud.Contains(resource) || len(refreshed.Aud) != 1 {
		t.Errorf("refreshed claims = %+v (%v)", refreshed, err)
	}
}

// TestMCPOAuthResourceAndMetadataChecks covers the refusals: a resource
// that isn't ours (invalid_target, with iss), and metadata documents that
// don't hold up.
func TestMCPOAuthResourceAndMetadataChecks(t *testing.T) {
	f := newOAuthFlow(t)
	_, challenge := pkce()
	q := url.Values{
		"client_id": {cimdClientID}, "redirect_uri": {cimdRedirect}, "state": {"s"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"memax:read"},
		"resource": {"https://elsewhere.example/mcp"},
	}
	loc := f.authorize(q, "")
	if loc.Query().Get("error") != "invalid_target" || loc.Query().Get("iss") != oauthTestBase {
		t.Errorf("foreign resource: %s", loc)
	}

	for name, doc := range map[string]string{
		"client_id mismatch": `{"client_id":"https://other.example/meta","client_name":"X","redirect_uris":["` + cimdRedirect + `"]}`,
		"secret":             `{"client_id":"` + cimdClientID + `","client_name":"X","client_secret":"s","redirect_uris":["` + cimdRedirect + `"]}`,
		"private key auth":   `{"client_id":"` + cimdClientID + `","client_name":"X","token_endpoint_auth_method":"private_key_jwt","redirect_uris":["` + cimdRedirect + `"]}`,
		"bad redirect":       `{"client_id":"` + cimdClientID + `","client_name":"X","redirect_uris":["http://evil.example/cb"]}`,
	} {
		f.doc = doc
		_, err := f.h.fetchClientMetadata(context.Background(), cimdClientID)
		if err == nil {
			t.Errorf("%s: document accepted", name)
		}
	}
	for id, want := range map[string]bool{
		cimdClientID:                    true,
		"https://claude.example/":       false,
		"http://claude.example/meta":    false,
		"https://claude.example/m?q=1":  false,
		"https://u:p@claude.example/m":  false,
		"https://claude.example/a/../b": false,
		"mcp_0123abcd":                  false,
	} {
		if got := isMetadataClientID(id); got != want {
			t.Errorf("isMetadataClientID(%q) = %t, want %t", id, got, want)
		}
	}
}

// TestMCPOAuthScopeCeiling: the scope caps the agent's autonomy.
func TestMCPOAuthScopeCeiling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		principal, scope, want string
	}{
		{"oauth_grant", "memax:read", "read"},
		{"oauth_grant", "memax:read memax:propose", "propose"},
		{"oauth_grant", "memax:propose memax:write", "write"},
		{"oauth_grant", "", ""},
		{"api_key", "memax:read", ""},
	}
	for _, c := range cases {
		g := GrantContext{PrincipalType: c.principal, OAuthScope: c.scope}
		if got := g.AutonomyCeiling(); got != c.want {
			t.Errorf("%s %q: ceiling %q, want %q", c.principal, c.scope, got, c.want)
		}
	}
	perms, normalized, invalid := oauthPermissionsFromScope("memax:propose memax:read memax:propose")
	if len(invalid) > 0 || normalized != "memax:propose memax:read" || !perms.Has(PermMemoryWrite) {
		t.Errorf("propose scope: %v %q %v", perms, normalized, invalid)
	}
	if got := intersectScopes("memax:read memax:write", "memax:read"); got != "memax:read" {
		t.Errorf("intersectScopes = %q", got)
	}
}
