package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
)

// Native MCP clients' redirects (RFC 8252), with the payloads the clients
// send: Cursor's private cursor:// scheme, VS Code's loopback port that
// moves when 33418 is busy, Claude Code's random loopback port with a
// metadata document. Each registers, is allowed by a person on the web,
// and redeems its code at the redirect it used. Dangerous schemes and
// userinfo are refused, and PKCE is required throughout.

// register is dynamic client registration (RFC 7591) with a raw payload.
func (f *oauthFlow) register(payload string) (int, map[string]any) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.h.DynamicClientRegistration(rec, httptest.NewRequest(http.MethodPost, oauthTestBase+"/oauth/register", bytes.NewBufferString(payload)))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// connectNative runs a native client's whole flow: authorize at redirect,
// the person allows it on the web for their first space, and the code is
// redeemed at tokenRedirect. It returns where the browser was sent and
// the token's claims.
func (f *oauthFlow) connectNative(clientID, redirect, tokenRedirect string) (*url.URL, *auth.Claims) {
	f.t.Helper()
	request, loc := f.start(authorizeQuery(clientID, redirect, "memax:read memax:write"))
	if request == "" {
		f.t.Fatalf("authorize at %s: sent to %s", redirect, loc)
	}
	token := f.webSession(f.user)
	if status, _ := f.open(request, token); status != http.StatusOK {
		f.t.Fatalf("open: %d", status)
	}
	back := f.decide(request, token, "approve", f.hubs[0])
	status, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"code_verifier": {sharedVerifier}, "redirect_uri": {tokenRedirect}})
	if status != http.StatusOK {
		f.t.Fatalf("token at %s: %d %v", tokenRedirect, status, body)
	}
	claims, err := auth.VerifyAccessToken(body["access_token"].(string), []byte(oauthTestSecret))
	if err != nil {
		f.t.Fatal(err)
	}
	return back, claims
}

// TestCursorConnectsWithItsPrivateScheme: Cursor's registration (no
// application_type, a cursor:// redirect beside https and loopback ones)
// is accepted, and the person's answer sends the browser to cursor://,
// which hands the code to the app.
func TestCursorConnectsWithItsPrivateScheme(t *testing.T) {
	f := newOAuthFlow(t)
	const cursorCallback = "cursor://anysphere.cursor-mcp/oauth/callback"
	status, client := f.register(`{
		"client_name": "Cursor",
		"redirect_uris": [
			"cursor://anysphere.cursor-mcp/oauth/callback",
			"https://www.cursor.com/agents/mcp/oauth/callback",
			"http://localhost:8787/callback"
		],
		"grant_types": ["authorization_code", "refresh_token"],
		"response_types": ["code"],
		"token_endpoint_auth_method": "none"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("Cursor's registration: %d %v", status, client)
	}
	clientID := client["client_id"].(string)
	back, claims := f.connectNative(clientID, cursorCallback, cursorCallback)
	if back.Scheme != "cursor" || back.Host != "anysphere.cursor-mcp" || back.Path != "/oauth/callback" ||
		back.Query().Get("state") != "s1" || back.Query().Get("iss") != oauthTestBase {
		t.Errorf("Cursor's callback = %s", back)
	}
	if claims.Sub != f.user.String() || claims.AgentName != "cursor" {
		t.Errorf("Cursor's token: %+v", claims)
	}
	// Its loopback redirect works too, on another port.
	if _, claims := f.connectNative(clientID, "http://localhost:51515/callback", "http://localhost:51515/callback"); claims.Sub != f.user.String() {
		t.Errorf("Cursor over loopback: %+v", claims)
	}
	// A redirect Cursor didn't register is refused at authorize.
	rec := httptest.NewRecorder()
	f.h.Authorize(rec, httptest.NewRequest(http.MethodGet, oauthTestBase+"/oauth/authorize?"+
		authorizeQuery(clientID, "cursor://anysphere.cursor-mcp/other", "memax:read").Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unregistered cursor:// path: %d", rec.Code)
	}
}

// TestVSCodeConnectsOnAnotherLoopbackPort: VS Code registers 33418 but
// listens on a random port when that one is busy (microsoft/vscode#278512);
// authorize and the token exchange accept the port it used.
func TestVSCodeConnectsOnAnotherLoopbackPort(t *testing.T) {
	f := newOAuthFlow(t)
	status, client := f.register(`{
		"client_name": "Visual Studio Code",
		"redirect_uris": [
			"https://insiders.vscode.dev/redirect",
			"https://vscode.dev/redirect",
			"http://127.0.0.1/",
			"http://127.0.0.1:33418"
		],
		"grant_types": ["authorization_code", "refresh_token"],
		"token_endpoint_auth_method": "none"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("VS Code's registration: %d %v", status, client)
	}
	const busy = "http://127.0.0.1:59656/"
	back, claims := f.connectNative(client["client_id"].(string), busy, busy)
	if back.Host != "127.0.0.1:59656" || back.Query().Get("code") == "" {
		t.Errorf("VS Code's callback = %s", back)
	}
	if claims.AgentName != "copilot" {
		t.Errorf("VS Code's agent = %q, want copilot", claims.AgentName)
	}
	// Its web redirect, exactly.
	if back, _ := f.connectNative(client["client_id"].(string), "https://vscode.dev/redirect", "https://vscode.dev/redirect"); back.Host != "vscode.dev" {
		t.Errorf("VS Code over vscode.dev: %s", back)
	}
}

// TestClaudeCodeConnectsOnARandomLoopbackPort: Claude Code's metadata
// document lists a loopback redirect, and it listens on whatever port is
// free.
func TestClaudeCodeConnectsOnARandomLoopbackPort(t *testing.T) {
	f := newOAuthFlow(t)
	const random = "http://localhost:41234/callback"
	back, claims := f.connectNative(cimdClientID, random, random)
	if back.Host != "localhost:41234" || claims.AgentName != "claude-code" || claims.Sub != f.user.String() {
		t.Errorf("Claude Code: %s %+v", back, claims)
	}
	// The cached document answers the next request on yet another port.
	if _, claims := f.connectNative(cimdClientID, "http://localhost:42424/callback", "http://localhost:42424/callback"); claims.Sub != f.user.String() {
		t.Errorf("again: %+v", claims)
	}
	if f.fetched != 1 {
		t.Errorf("metadata fetched %d times, want 1", f.fetched)
	}
}

// TestNativeRedirectRefusals: dangerous schemes, userinfo and hosts that
// aren't loopback are refused at registration and in metadata documents;
// a code is redeemed only with the PKCE verifier and at the redirect it
// was issued for.
func TestNativeRedirectRefusals(t *testing.T) {
	f := newOAuthFlow(t)
	for _, uri := range []string{
		"javascript:alert(1)", "vbscript:msgbox(1)", "data:text/html,x", "file:///etc/passwd", "about:blank",
		"blob:https://memax.app/1", "filesystem:https://memax.app/temporary/x", "ws://127.0.0.1:1/cb", "wss://agent.example/cb",
		"ftp://agent.example/cb", "mailto:a@example.com", "tel:+15555555555", "intent://cb", "myapp://callback",
		"https://user:pass@agent.example/cb", "http://user@127.0.0.1:1/cb", "http://agent.example/cb",
		"https://agent.example/cb#frag",
	} {
		payload, _ := json.Marshal(map[string]any{"client_name": "Codex", "redirect_uris": []string{"http://127.0.0.1:1/cb", uri}})
		if status, body := f.register(string(payload)); status != http.StatusBadRequest || body["error"] != "invalid_redirect_uri" {
			t.Errorf("registering %q: %d %v", uri, status, body)
		}
		f.doc = `{"client_id":"` + cimdClientID + `","client_name":"X","redirect_uris":["` + strings.ReplaceAll(uri, `"`, `\"`) + `"]}`
		if _, err := f.h.fetchClientMetadata(t.Context(), cimdClientID); err == nil {
			t.Errorf("a metadata document listing %q was accepted", uri)
		}
	}

	// PKCE: authorize needs an S256 challenge; the token, the verifier.
	f.doc = `{"client_id":"` + cimdClientID + `","client_name":"Claude Code","redirect_uris":["` + cimdRedirect + `"]}`
	q := authorizeQuery(cimdClientID, cimdRedirect, "memax:read")
	q.Del("code_challenge")
	if _, loc := f.start(q); loc.Query().Get("error") != "invalid_request" {
		t.Errorf("authorize without PKCE: %s", loc)
	}
	q = authorizeQuery(cimdClientID, cimdRedirect, "memax:read")
	q.Set("code_challenge_method", "plain")
	if _, loc := f.start(q); loc.Query().Get("error") != "invalid_request" {
		t.Errorf("authorize with plain PKCE: %s", loc)
	}
	back := f.authorize(authorizeQuery(cimdClientID, cimdRedirect, "memax:read"), f.hubs[0])
	code := back.Query().Get("code")
	for name, form := range map[string]url.Values{
		"no verifier":      {"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cimdRedirect}},
		"a wrong verifier": {"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cimdRedirect}, "code_verifier": {strings.Repeat("x", 50)}},
		"another redirect": {"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://localhost:53682/other"}, "code_verifier": {sharedVerifier}},
		"another host":     {"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://127.0.0.1:53682/callback"}, "code_verifier": {sharedVerifier}},
	} {
		if status, body := f.token(form); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}
}
