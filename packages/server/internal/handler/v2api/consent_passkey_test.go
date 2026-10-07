package v2api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys/passkeytest"
)

// TestConsentAfterAPasskeySignIn: a person who signs in to the web with a
// passkey answers an agent's OAuth request (OAuthConsent) as themselves,
// and the token and the connection are theirs. Allowing it asks for no
// passkey re-check: it connects the agent at the space's default, at most
// Propose (policy.DecideConnection's quiet level), which is no raise;
// raising it later in Agents is where the re-check applies.
func TestConsentAfterAPasskeySignIn(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz := e.user("zz")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	e.exec(`UPDATE hubs SET v2_enabled_at = now() WHERE id = $1`, project.id)
	first, _ := e.web(zz)
	laptop := passkeytest.New(passkeyOrigin)
	e.addPasskey(zz, first, laptop, "")

	// Sign in with the passkey: the public begin and finish, then the code
	// is exchanged for the web session, as the web app's /api/auth/exchange does.
	var begin struct {
		Options json.RawMessage `json:"options"`
	}
	e.do(call{method: "POST", path: "/v2/passkey-sign-ins"}).ok(http.StatusOK, &begin)
	assertion, err := laptop.Get(begin.Options)
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		Code string `json:"code"`
	}
	e.do(call{method: "POST", path: "/v2/passkey-sign-ins:finish", body: map[string]any{"credential": json.RawMessage(assertion)}}).
		ok(http.StatusOK, &signed)
	ex := httptest.NewRecorder()
	e.authH.ExchangeCode(ex, httptest.NewRequest(http.MethodPost, "/v1/auth/exchange", strings.NewReader(`{"code":"`+signed.Code+`"}`)))
	var session struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if ex.Code != http.StatusOK || json.Unmarshal(ex.Body.Bytes(), &session) != nil {
		t.Fatalf("exchange: %d %s", ex.Code, ex.Body.String())
	}
	web := session.Data.AccessToken

	// The agent asks; the browser goes to the web app's page for the request.
	const redirect = "http://127.0.0.1:1455/callback"
	e.exec(`INSERT INTO oauth_clients (client_id, client_name, redirect_uris) VALUES ('codex-passkey', 'Codex', $1)`, []string{redirect})
	o := handler.NewMCPOAuthHandler(e.authH)
	o.SetLedger(e.ledger)
	verifier := strings.Repeat("k", 50)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {"codex-passkey"}, "redirect_uri": {redirect}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope": {"memax:read memax:write"}}
	rec := httptest.NewRecorder()
	o.Authorize(rec, httptest.NewRequest(http.MethodGet, "http://localhost:8080/oauth/authorize?"+q.Encode(), nil))
	page, _ := url.Parse(rec.Header().Get("Location"))
	request := page.Query().Get("request")
	if rec.Code != http.StatusSeeOther || page.Host != "localhost:3000" || request == "" {
		t.Fatalf("authorize: %d %s", rec.Code, page)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize/requests/{id}", o.OpenRequest)
	mux.HandleFunc("POST /oauth/authorize/requests/{id}/decision", o.DecideRequest)
	consent := handler.RequireAuth([]byte(testSecret), e.authH.ResolveAPIKey, e.authH.ResolveOAuthGrant)(mux)
	viaWeb := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+web)
		rec := httptest.NewRecorder()
		consent.ServeHTTP(rec, req)
		return rec
	}
	var view struct {
		Data struct {
			Person struct {
				Name string `json:"name"`
			} `json:"person"`
			Spaces []struct {
				ID       string `json:"id"`
				Autonomy string `json:"autonomy"`
			} `json:"spaces"`
		} `json:"data"`
	}
	opened := viaWeb(http.MethodGet, "/oauth/authorize/requests/"+request, "")
	if opened.Code != http.StatusOK || json.Unmarshal(opened.Body.Bytes(), &view) != nil || view.Data.Person.Name != "zz" {
		t.Fatalf("open: %d %s", opened.Code, opened.Body.String())
	}
	decided := viaWeb(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision",
		`{"decision":"approve","space_id":"`+project.id.String()+`"}`)
	var answer struct {
		Data struct {
			RedirectTo string `json:"redirect_to"`
		} `json:"data"`
	}
	if decided.Code != http.StatusOK || json.Unmarshal(decided.Body.Bytes(), &answer) != nil {
		t.Fatalf("allow, no re-check: %d %s", decided.Code, decided.Body.String())
	}
	back, _ := url.Parse(answer.Data.RedirectTo)

	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {redirect}}
	tokReq := httptest.NewRequest(http.MethodPost, "http://localhost:8080/oauth/token", strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	o.Token(rec, tokReq)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &tok) != nil {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	claims, err := auth.VerifyAccessToken(tok.AccessToken, []byte(testSecret))
	if err != nil || claims.Sub != zz.String() {
		t.Fatalf("token is %+v (%v), want zz's", claims, err)
	}
	if n := e.count(`
		SELECT count(*) FROM v2.agent_connections c JOIN v2.agent_connection_spaces s ON s.connection_id = c.id
		 WHERE c.credential_id = $1 AND c.person_id = $2 AND s.space_id = $3 AND s.autonomy = 'propose'`,
		claims.GrantID, zz, project.id); n != 1 {
		t.Errorf("zz's agent isn't connected at Propose in memax-v2 (%d)", n)
	}
}
