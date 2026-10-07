package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

const sessionsTestSecret = "auth-sessions-test-secret-0123456789"

// testClock is a settable clock for the sessions store.
type testClock struct{ ns atomic.Int64 }

func (c *testClock) now() time.Time      { return time.Unix(0, c.ns.Load()) }
func (c *testClock) add(d time.Duration) { c.ns.Add(int64(d)) }
func newTestClock() *testClock           { c := &testClock{}; c.ns.Store(time.Now().UnixNano()); return c }
func (c *testClock) store(p *pgxpool.Pool) *sessions.Store {
	return sessions.New(p, sessions.WithClock(c.now))
}

// sessionRig is the auth handler on a real database, its sessions on a
// test clock.
type sessionRig struct {
	t     *testing.T
	h     *AuthHandler
	pool  *pgxpool.Pool
	clock *testClock
	user  uuid.UUID
}

func newSessionRig(t *testing.T) *sessionRig {
	t.Helper()
	_, pool := testdb.Acquire(t)
	r := &sessionRig{t: t, pool: pool, clock: newTestClock(), user: uuid.New()}
	r.h = &AuthHandler{pool: pool, jwtSecret: []byte(sessionsTestSecret), redirectAllowlist: []string{"https://memax.app"}}
	r.h.SetSessions(r.clock.store(pool))
	t.Cleanup(r.h.Sessions().Wait)
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`,
		r.user, r.user.String()[:8]+"@sessions.test"); err != nil {
		t.Fatal(err)
	}
	return r
}

// cliLogin is `memax login` getting tokens in the response.
func (r *sessionRig) cliLogin() model.TokenPair {
	r.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil)
	req.Header.Set("User-Agent", "memax-cli/0.2.1 (darwin)")
	r.h.completeLogin(rec, req, &model.User{ID: r.user.String()}, "")
	var env struct {
		Data model.TokenPair `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &env) != nil {
		r.t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	return env.Data
}

// oldCLIRefresh is /v1/auth/refresh exactly as every published memax CLI
// sends it and reads it (lib/client.ts and, before the SDK, lib/api.js):
// a JSON body with refresh_token, and json.data's three fields back.
func (r *sessionRig) oldCLIRefresh(refresh string) (int, map[string]any, map[string]any) {
	r.t.Helper()
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.h.Refresh(rec, req)
	var env struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		r.t.Fatalf("refresh answer isn't JSON: %s", rec.Body.String())
	}
	return rec.Code, env.Data, env.Error
}

// TestV1RefreshRotatesAndOldCLIsKeepUp: /v1/auth/refresh answers a new
// refresh token every time, in the shape old CLIs read, and they store it;
// processes sharing a credentials file that refresh together all get the
// same next token; a token replaced long ago signs the session out.
func TestV1RefreshRotatesAndOldCLIsKeepUp(t *testing.T) {
	t.Parallel()
	r := newSessionRig(t)
	login := r.cliLogin()
	claims, err := auth.VerifyAccessToken(login.AccessToken, []byte(sessionsTestSecret))
	if err != nil || claims.Sid == "" || claims.Surface != auth.SurfaceCLI {
		t.Fatalf("login token %+v (%v): want a cli token naming its session", claims, err)
	}

	r.clock.add(time.Hour)
	status, data, _ := r.oldCLIRefresh(login.RefreshToken)
	if status != http.StatusOK {
		t.Fatalf("refresh: %d", status)
	}
	next, _ := data["refresh_token"].(string)
	if next == "" || next == login.RefreshToken || data["access_token"] == "" || data["expires_in"] != float64(3600) {
		t.Fatalf("an old CLI reads %v: want a new refresh token", data)
	}
	if rem, ok := data["refresh_expires_in"].(float64); !ok || rem <= 0 || rem > sessionTTL.Seconds() {
		t.Fatalf("refresh_expires_in = %v", data["refresh_expires_in"])
	}
	refreshed, _ := auth.VerifyAccessToken(data["access_token"].(string), []byte(sessionsTestSecret))
	if refreshed == nil || refreshed.Sid != claims.Sid || refreshed.Surface != auth.SurfaceCLI {
		t.Fatalf("refreshed claims %+v", refreshed)
	}

	// The daemon and two MCP servers read the file before any of them
	// wrote the new token: all three refresh with it, and all three get the
	// same next token, whichever writes the file last.
	r.clock.add(time.Hour)
	var wg sync.WaitGroup
	got := make([]string, 3)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s, d, e := r.oldCLIRefresh(next); s == http.StatusOK {
				got[i], _ = d["refresh_token"].(string)
			} else {
				t.Errorf("process %d: %d %v", i, s, e)
			}
		}()
	}
	wg.Wait()
	if got[0] == "" || got[0] != got[1] || got[1] != got[2] || got[0] == next {
		t.Fatalf("processes refreshing together got %v", got)
	}

	// Long after, the token replaced first comes back: someone else holds
	// this session. It is signed out, newest token and all.
	r.clock.add(time.Hour)
	status, _, apiErr := r.oldCLIRefresh(login.RefreshToken)
	if status != http.StatusUnauthorized || apiErr["code"] != "session_revoked" {
		t.Fatalf("a long-replaced token: %d %v", status, apiErr)
	}
	if status, _, _ := r.oldCLIRefresh(got[0]); status != http.StatusUnauthorized {
		t.Fatalf("the newest token after reuse: %d", status)
	}
	if status, _, apiErr := r.oldCLIRefresh("never-issued"); status != http.StatusUnauthorized || apiErr["code"] != "invalid_token" {
		t.Fatalf("an unknown token: %d %v", status, apiErr)
	}
}

// TestRedirectedLoginsStartOneSession: a login whose code is redirected
// starts its session at the exchange, once (a second exchange of the code
// gets nothing), and a web session records where the browser is when the
// web app's server says so, signed.
func TestRedirectedLoginsStartOneSession(t *testing.T) {
	t.Parallel()
	r := newSessionRig(t)
	const surfaceSecret = "web-surface-secret-0123456789abcdef"
	web, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	r.h.SetWebSurface(web)
	count := func() int {
		var n int
		if err := r.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id = $1`, r.user).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	code := func() string {
		rec := httptest.NewRecorder()
		r.h.completeLogin(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil),
			&model.User{ID: r.user.String()}, "https://memax.app/auth/callback")
		loc, _ := url.Parse(rec.Header().Get("Location"))
		return loc.Query().Get("code")
	}
	exchange := func(code string, sign bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"code": code})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/exchange", strings.NewReader(string(body)))
		req.RemoteAddr = "198.51.100.9:443" // the web app's server
		req.Header.Set("User-Agent", "node")
		ts := time.Now().Unix()
		ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 Chrome/131.0 Safari/537.36"
		req.Header.Set(websurface.HeaderClientIP, "203.0.113.7")
		req.Header.Set(websurface.HeaderClientCity, url.PathEscape("São Paulo"))
		req.Header.Set(websurface.HeaderClientUserAgent, ua)
		req.Header.Set(websurface.HeaderClientTimestamp, strconv.FormatInt(ts, 10))
		sig := websurface.SignClientInfo([]byte(surfaceSecret), ts, "203.0.113.7", url.PathEscape("São Paulo"), ua)
		if !sign {
			sig = "v1=forged"
		}
		req.Header.Set(websurface.HeaderClientSignature, sig)
		rec := httptest.NewRecorder()
		r.h.ExchangeCode(rec, req)
		return rec
	}

	c := code()
	if n := count(); n != 0 {
		t.Fatalf("a redirected login left %d sessions before its code was exchanged", n)
	}
	if rec := exchange(c, true); rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	if rec := exchange(c, true); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a second exchange of the same code: %d", rec.Code)
	}
	list, err := r.h.Sessions().List(context.Background(), r.user)
	if err != nil || len(list) != 1 {
		t.Fatalf("sessions %+v, %v", list, err)
	}
	s := list[0]
	if s.Kind != sessions.KindWeb || s.Surface != auth.SurfaceWeb || s.CreatedIP != "203.0.113.7" ||
		s.CreatedCity != "São Paulo" || s.Client != "Chrome on macOS" {
		t.Fatalf("a web session signed in from the browser: %+v", s)
	}

	// Unsigned or forged, what the web app's server says is ignored: the
	// API records what it sees.
	if rec := exchange(code(), false); rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d", rec.Code)
	}
	byIP := map[string]int{}
	list, _ = r.h.Sessions().List(context.Background(), r.user)
	for _, s := range list {
		byIP[s.CreatedIP]++
	}
	if byIP["203.0.113.7"] != 1 || byIP["198.51.100.9"] != 1 {
		t.Fatalf("a forged client address was believed: %+v", list)
	}
}

// TestRevokeEndpointSignsOut: POST /oauth/revoke (RFC 7009) ends the
// session a refresh token belongs to, or the one an access token names,
// and answers 200 for a token that names nothing.
func TestRevokeEndpointSignsOut(t *testing.T) {
	t.Parallel()
	r := newSessionRig(t)
	mcp := &MCPOAuthHandler{authH: r.h, baseURL: "https://api.memax.test"}
	revoke := func(token string) int {
		form := url.Values{}
		if token != "" {
			form.Set("token", token)
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		mcp.Revoke(rec, req)
		return rec.Code
	}

	byRefresh := r.cliLogin()
	if code := revoke(byRefresh.RefreshToken); code != http.StatusOK {
		t.Fatalf("revoke by refresh token: %d", code)
	}
	if status, _, _ := r.oldCLIRefresh(byRefresh.RefreshToken); status != http.StatusUnauthorized {
		t.Fatalf("a signed-out session refreshed: %d", status)
	}

	byAccess := r.cliLogin()
	if code := revoke(byAccess.AccessToken); code != http.StatusOK {
		t.Fatalf("revoke by access token: %d", code)
	}
	if status, _, _ := r.oldCLIRefresh(byAccess.RefreshToken); status != http.StatusUnauthorized {
		t.Fatalf("a session signed out by its access token refreshed: %d", status)
	}

	if code := revoke("never-issued"); code != http.StatusOK {
		t.Fatalf("an unknown token: %d, want 200 (RFC 7009 §2.2)", code)
	}
	if code := revoke(""); code != http.StatusBadRequest {
		t.Fatalf("no token: %d", code)
	}
	if list, _ := r.h.Sessions().List(context.Background(), r.user); len(list) != 0 {
		t.Fatalf("sessions left: %+v", list)
	}
}

// TestMCPRefreshRotatesWithReuseDetection: an MCP client's refresh token
// rotates at /oauth/token, a long-replaced one revokes the session, and a
// person's refresh token there is refused without harming the session.
func TestMCPRefreshRotatesWithReuseDetection(t *testing.T) {
	f := newOAuthFlow(t)
	clock := newTestClock()
	f.authH.SetSessions(clock.store(f.authH.pool))
	t.Cleanup(f.authH.Sessions().Wait)
	verifier, challenge := pkce()
	loc := f.authorize(url.Values{
		"client_id": {cimdClientID}, "redirect_uri": {cimdRedirect}, "state": {"s"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"memax:read"},
	}, []string{"memax:read"})
	status, body := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"code_verifier": {verifier}, "redirect_uri": {cimdRedirect}})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, body)
	}
	first := body["refresh_token"].(string)
	var kind, client string
	if err := f.authH.pool.QueryRow(context.Background(), `SELECT kind, client FROM sessions WHERE refresh_token_hash = $1`,
		sessions.HashToken(first)).Scan(&kind, &client); err != nil || kind != "mcp" || client != "Claude Code" {
		t.Fatalf("the MCP session: kind %q client %q (%v)", kind, client, err)
	}

	clock.add(time.Hour)
	status, body = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}})
	second, _ := body["refresh_token"].(string)
	if status != http.StatusOK || second == "" || second == first {
		t.Fatalf("refresh: %d %v", status, body)
	}
	if c, err := auth.VerifyAccessToken(body["access_token"].(string), []byte(oauthTestSecret)); err != nil || c.Sid == "" || c.GrantID == "" {
		t.Fatalf("refreshed claims %+v (%v)", c, err)
	}

	// A person's refresh token isn't an MCP client's: refused, untouched.
	person := &sessionRig{t: t, h: f.authH, pool: f.authH.pool, user: f.user}
	login := person.cliLogin()
	status, body = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {login.RefreshToken}})
	if status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("a person's token at /oauth/token: %d %v", status, body)
	}
	if status, _, _ := person.oldCLIRefresh(login.RefreshToken); status != http.StatusOK {
		t.Fatalf("the person's session after: %d", status)
	}

	clock.add(time.Hour)
	status, body = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first}})
	if status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("reuse: %d %v", status, body)
	}
	status, body = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {second}})
	if status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("the newest token after reuse: %d %v", status, body)
	}
}

func TestDescribeClient(t *testing.T) {
	t.Parallel()
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36":        "Chrome on macOS",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0": "Edge on Windows",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0":                                                    "Firefox on Linux",
		"": "A browser",
	} {
		if got := describeClient(sessions.KindWeb, ua); got != want {
			t.Errorf("%q: %q, want %q", ua, got, want)
		}
	}
	if got := describeClient(sessions.KindCLI, "memax-cli/0.2.1 (darwin)"); got != "memax CLI 0.2.1" {
		t.Errorf("CLI: %q", got)
	}
	if got := describeClient(sessions.KindCLI, "node"); got != "memax CLI" {
		t.Errorf("an old CLI: %q", got)
	}
}
