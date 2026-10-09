package mcpv2_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// TestConsentedAgentKeepsOnceAPersonAllowsWrite: an agent connected through
// OAuth consent (OAuthConsent) asked for memax:write and got it in its
// token, connected at Propose as the page said. Its writes wait in Review
// until a person on the web allows Write in Agents; then the same token
// keeps. The scope is a ceiling; the connection decides.
func TestConsentedAgentKeepsOnceAPersonAllowsWrite(t *testing.T) {
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.toV2(sp)
	const redirect = "http://127.0.0.1:1455/callback"
	e.exec(`INSERT INTO oauth_clients (client_id, client_name, redirect_uris) VALUES ('codex-cli', 'Codex', $1)`, []string{redirect})
	o := handler.NewMCPOAuthHandler(e.authH)
	o.SetLedger(e.ledger)

	// The client asks; the browser goes to the web app's page.
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {"codex-cli"}, "redirect_uri": {redirect}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope": {"memax:read memax:write"}}
	rec := httptest.NewRecorder()
	o.Authorize(rec, httptest.NewRequest(http.MethodGet, e.srv.URL+"/oauth/authorize?"+q.Encode(), nil))
	page, _ := url.Parse(rec.Header().Get("Location"))
	request := page.Query().Get("request")
	if rec.Code != http.StatusSeeOther || page.Path != "/oauth/authorize" || request == "" {
		t.Fatalf("authorize: %d %s", rec.Code, page)
	}

	// The person, signed in on the web, opens it and allows it in memax-v2.
	web, err := auth.SignSessionToken(zz.String(), auth.SurfaceWeb, []byte(testSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize/requests/{id}", o.OpenRequest)
	mux.HandleFunc("POST /oauth/authorize/requests/{id}/decision", o.DecideRequest)
	consent := handler.RequireAuth([]byte(testSecret), e.authH.ResolveAPIKey, e.authH.ResolveOAuthGrant)(mux)
	viaWeb := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+web)
		rec := httptest.NewRecorder()
		consent.ServeHTTP(rec, req)
		return rec
	}
	var view struct {
		Data struct {
			Scope  string `json:"scope"`
			Spaces []struct {
				ID       string `json:"id"`
				Autonomy string `json:"autonomy"`
				Ceiling  string `json:"ceiling"`
			} `json:"spaces"`
		} `json:"data"`
	}
	opened := viaWeb(http.MethodGet, "/oauth/authorize/requests/"+request, "")
	if opened.Code != http.StatusOK || json.Unmarshal(opened.Body.Bytes(), &view) != nil {
		t.Fatalf("open: %d %s", opened.Code, opened.Body.String())
	}
	shown := ""
	for _, s := range view.Data.Spaces {
		if s.ID == sp.id.String() {
			shown = s.Autonomy + " up to " + s.Ceiling
		}
	}
	if view.Data.Scope != "memax:read memax:write" || shown != "propose up to write" {
		t.Fatalf("the page was told %q, %q", view.Data.Scope, shown)
	}
	decided := viaWeb(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision",
		`{"decision":"approve","space_id":"`+sp.id.String()+`"}`)
	var answer struct {
		Data struct {
			RedirectTo string `json:"redirect_to"`
		} `json:"data"`
	}
	if decided.Code != http.StatusOK || json.Unmarshal(decided.Body.Bytes(), &answer) != nil {
		t.Fatalf("allow: %d %s", decided.Code, decided.Body.String())
	}
	back, _ := url.Parse(answer.Data.RedirectTo)

	// The client redeems the code for a token bound to this MCP endpoint.
	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {redirect}}
	tokReq := httptest.NewRequest(http.MethodPost, e.srv.URL+"/oauth/token", strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	o.Token(rec, tokReq)
	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &tok) != nil || tok.Scope != "memax:read memax:write" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	claims, err := auth.VerifyAccessToken(tok.AccessToken, []byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}

	cs := e.connectClient(tok.AccessToken, "/mcp", older, nil)
	push := func(statement string) string {
		t.Helper()
		res := call(t, cs, "memax_push", map[string]any{"content": statement, "hub_id": sp.id.String()})
		if res.IsError {
			t.Fatalf("push: %s", text(res))
		}
		return structured[handler.MCPPushOutput](t, res).Status
	}
	if got := push("Deploys go out on Fridays only."); got != handler.MCPPushProposed {
		t.Fatalf("at Propose the agent's write is %s, want proposed", got)
	}

	// A person on the web allows Write in Agents.
	scope, err := e.ledger.UserScope(context.Background(), zz)
	if err != nil {
		t.Fatal(err)
	}
	var conn uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM v2.agent_connections WHERE credential_id = $1`, claims.GrantID).Scan(&conn); err != nil {
		t.Fatal(err)
	}
	res, err := e.ledger.Apply(context.Background(), &ledger.SetAutonomy{
		Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: zz}, Scope: scope.Narrow(sp.id),
			Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		Connection: conn, SpaceID: sp.id, Autonomy: policy.AutonomyWrite,
	})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("allow Write: %v %+v", err, res.Policy)
	}
	if got := push("Releases are tagged from main."); got != handler.MCPPushKept {
		t.Errorf("at Write the same token's write is %s, want kept", got)
	}
}
