package v2api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// sessionsEnv is newWebEnv with the sessions store, and the auth handler
// that signs people in and refreshes their tokens on the same store.
type sessionsEnv struct {
	*env
	store *sessions.Store
	authH *handler.AuthHandler
}

func newSessionsEnv(t *testing.T) *sessionsEnv {
	t.Helper()
	v, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	var store *sessions.Store
	e := newEnvWith(t, func(e *env) []v2api.Option {
		store = sessions.New(e.pool, sessions.WithLogger(quiet))
		return []v2api.Option{v2api.WithWebSurface(v), v2api.WithSessions(store)}
	})
	t.Cleanup(store.Wait)
	authH, err := handler.NewAuthHandler(e.pool)
	if err != nil {
		t.Fatal(err)
	}
	authH.SetSessions(store)
	authH.SetWebSurface(v)
	return &sessionsEnv{env: e, store: store, authH: authH}
}

// signIn starts a session of the kind and returns its access token (as the
// server signs them: naming the session) and the issued session.
func (e *sessionsEnv) signIn(user uuid.UUID, kind sessions.Kind, client string) (string, *sessions.Issued) {
	e.t.Helper()
	surface := auth.SurfaceCLI
	if kind == sessions.KindWeb {
		surface = auth.SurfaceWeb
	}
	is, err := e.store.Issue(context.Background(), sessions.Start{UserID: user, Kind: kind, Surface: surface, Client: client,
		Where: sessions.Where{IP: "203.0.113.7", City: "Lisbon"}, TTL: 30 * 24 * time.Hour})
	if err != nil {
		e.t.Fatal(err)
	}
	tok, err := auth.Sign(auth.Claims{Sub: user.String(), Surface: surface, Sid: is.Session.ID.String()}, []byte(testSecret), time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok, is
}

type sessionWire struct {
	ID         uuid.UUID  `json:"id"`
	Surface    string     `json:"surface"`
	Client     string     `json:"client"`
	Agent      string     `json:"agent"`
	Address    string     `json:"address"`
	City       string     `json:"city"`
	SignedInAt time.Time  `json:"signed_in_at"`
	LastUsedAt time.Time  `json:"last_used_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	Current    bool       `json:"current"`
}

func (e *sessionsEnv) list(token string) []sessionWire {
	e.t.Helper()
	var out struct {
		Items []sessionWire `json:"items"`
	}
	e.do(call{method: "GET", path: "/v2/sessions", token: token}).ok(http.StatusOK, &out)
	return out.Items
}

// TestSessionsListAndSignOut: a person lists everywhere they are signed
// in, this session marked; signs one out on the web, which ends its
// refresh at once; signs the rest out; and nobody but the person, on the
// web for anything but their own session, can.
func TestSessionsListAndSignOut(t *testing.T) {
	t.Parallel()
	e := newSessionsEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	web, webIs := e.signIn(zz, sessions.KindWeb, "Chrome on macOS")
	cli, cliIs := e.signIn(zz, sessions.KindCLI, "memax CLI 0.2.1")
	device, deviceIs := e.signIn(zz, sessions.KindDevice, "memax CLI 2.0.0 on zz-mbp (macOS)")
	grant := uuid.New()
	e.exec(`INSERT INTO oauth_clients (client_id, client_name) VALUES ('claude', 'Claude') ON CONFLICT DO NOTHING`)
	e.exec(`INSERT INTO oauth_grants (id, user_id, client_id, agent_name, default_permissions) VALUES ($1, $2, 'claude', 'claude-ai', ARRAY['memory:read'])`, grant, zz)
	mcpIs, err := e.store.Issue(context.Background(), sessions.Start{UserID: zz, Kind: sessions.KindMCP, AgentName: "claude-ai",
		GrantID: grant.String(), Client: "Claude", TTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	_, jyIs := e.signIn(jy, sessions.KindWeb, "Firefox on Linux")

	items := e.list(web)
	if len(items) != 4 {
		t.Fatalf("zz's sessions: %+v", items)
	}
	bySurface := map[string]sessionWire{}
	for _, s := range items {
		bySurface[s.Surface] = s
		if s.ID == jyIs.Session.ID {
			t.Fatal("someone else's session is listed")
		}
	}
	if w := bySurface["web"]; !w.Current || w.ID != webIs.Session.ID || w.Client != "Chrome on macOS" || w.Address != "203.0.113.7" || w.City != "Lisbon" {
		t.Errorf("the web session: %+v", w)
	}
	if m := bySurface["mcp"]; m.Current || m.Agent != "claude-ai" || m.Client != "Claude" {
		t.Errorf("the MCP session: %+v", m)
	}
	if d := bySurface["device"]; d.Current || d.ID != deviceIs.Session.ID || !d.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) {
		t.Errorf("the device session: %+v", d)
	}
	if cur := e.list(cli); !func() bool {
		for _, s := range cur {
			if s.Current {
				return s.ID == cliIs.Session.ID
			}
		}
		return false
	}() {
		t.Errorf("listed from the CLI, the CLI's session isn't current: %+v", cur)
	}

	revoke := func(token string, id uuid.UUID, sign func(*http.Request, []byte)) *resp {
		return e.do(call{method: "POST", path: "/v2/sessions/" + id.String() + ":revoke", token: token, sign: sign})
	}
	// Signing another session out needs the web app: the CLI can't, and
	// neither can the web session without the proxy's signature.
	if code := policyCode(t, revoke(cli, webIs.Session.ID, nil)); code != policy.CodeSessionNeedsWeb {
		t.Errorf("the CLI signing the web out: %s", code)
	}
	if code := policyCode(t, revoke(web, cliIs.Session.ID, nil)); code != policy.CodeSessionNeedsWeb {
		t.Errorf("an unsigned web request: %s", code)
	}
	// On the web: the CLI's session ends, and its refresh token with it.
	var ended sessionWire
	revoke(web, cliIs.Session.ID, webSigned(zz, tamper{})).ok(http.StatusOK, &ended)
	if ended.ID != cliIs.Session.ID || ended.RevokedAt == nil || ended.Current {
		t.Errorf("signed out: %+v", ended)
	}
	if _, err := e.store.Refresh(context.Background(), cliIs.RefreshToken, sessions.RefreshOptions{}); !errors.Is(err, sessions.ErrRevoked) {
		t.Errorf("the signed-out session refreshed: %v", err)
	}
	if status, code := e.v1Refresh(cliIs.RefreshToken); status != http.StatusUnauthorized || code != "session_revoked" {
		t.Errorf("/v1/auth/refresh after sign-out: %d %s", status, code)
	}
	// Again, or someone else's: 404.
	revoke(web, cliIs.Session.ID, webSigned(zz, tamper{})).fails(http.StatusNotFound, "not_found")
	revoke(web, jyIs.Session.ID, webSigned(zz, tamper{})).fails(http.StatusNotFound, "not_found")
	revoke(web, uuid.New(), webSigned(zz, tamper{})).fails(http.StatusNotFound, "not_found")

	// A session signs itself out from anywhere: the device, unsigned.
	revoke(device, deviceIs.Session.ID, nil).ok(http.StatusOK, &ended)
	if ended.ID != deviceIs.Session.ID {
		t.Errorf("own sign-out: %+v", ended)
	}

	// Agents and keys don't manage sessions; impersonation reads only.
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	if code := policyCode(t, e.do(call{method: "GET", path: "/v2/sessions", token: key})); code != policy.CodeSessionByPerson {
		t.Errorf("an API key listing: %s", code)
	}
	imp, _ := auth.SignImpersonationToken(zz.String(), jy.String(), []byte(testSecret), time.Hour)
	revoke(imp, mcpIs.Session.ID, nil).fails(http.StatusForbidden, "impersonation_read_only")

	// Everywhere else: needs the web, and spares this session.
	if code := policyCode(t, e.do(call{method: "POST", path: "/v2/sessions:revoke-others", token: web})); code != policy.CodeSessionNeedsWeb {
		t.Errorf("revoke-others unsigned: %s", code)
	}
	var n struct {
		Revoked int `json:"revoked"`
	}
	e.do(call{method: "POST", path: "/v2/sessions:revoke-others", token: web, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &n)
	if n.Revoked != 1 { // the MCP session; the CLI and the device already ended
		t.Errorf("revoked %d", n.Revoked)
	}
	if left := e.list(web); len(left) != 1 || left[0].ID != webIs.Session.ID {
		t.Errorf("after revoke-others: %+v", left)
	}
	if _, err := e.store.Refresh(context.Background(), mcpIs.RefreshToken, sessions.RefreshOptions{}); !errors.Is(err, sessions.ErrRevoked) {
		t.Errorf("the MCP session refreshed after revoke-others: %v", err)
	}
	// jy's session is untouched.
	if _, err := e.store.Refresh(context.Background(), jyIs.RefreshToken, sessions.RefreshOptions{}); err != nil {
		t.Errorf("someone else's session: %v", err)
	}

	// A token from before sessions were named can't say which one to keep.
	old := e.webSession(zz)
	e.do(call{method: "POST", path: "/v2/sessions:revoke-others", token: old, sign: webSigned(zz, tamper{})}).
		fails(http.StatusConflict, "invalid_transition")
}

// v1Refresh is /v1/auth/refresh on the env's store.
func (e *sessionsEnv) v1Refresh(token string) (int, string) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{"refresh_token": token})
	rec := httptest.NewRecorder()
	e.authH.Refresh(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(string(body))))
	var env struct {
		Data  model.TokenPair `json:"data"`
		Error *model.Error    `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error != nil {
		return rec.Code, env.Error.Code
	}
	return rec.Code, ""
}

// TestHumanWebStillNeedsTheWebSessionAndTheSignature: with the tokens in
// the web app's cookies, nothing changes in what the API believes. A web
// session signed in through the real exchange and refreshed by rotation
// keeps human_web only when the web app's proxy signs the request; the
// same token unsigned, and a CLI or device session signed, are
// client-attested.
func TestHumanWebStillNeedsTheWebSessionAndTheSignature(t *testing.T) {
	t.Parallel()
	e := newSessionsEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	// Sign in on the web: the code the API redirected to the web app,
	// exchanged by the web app's server.
	code := uuid.NewString()
	e.exec(`INSERT INTO auth_codes (code, user_id, expires_at, surface) VALUES ($1, $2, now() + interval '1 minute', 'web')`, code, zz)
	body, _ := json.Marshal(map[string]string{"code": code})
	rec := httptest.NewRecorder()
	e.authH.ExchangeCode(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/exchange", strings.NewReader(string(body))))
	var signedIn struct {
		Data model.TokenPair `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &signedIn); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	// The BFF refreshes it: rotated, still a web session.
	body, _ = json.Marshal(map[string]string{"refresh_token": signedIn.Data.RefreshToken})
	rec = httptest.NewRecorder()
	e.authH.Refresh(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(string(body))))
	var refreshed struct {
		Data model.TokenPair `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshed); err != nil || rec.Code != http.StatusOK ||
		refreshed.Data.RefreshToken == signedIn.Data.RefreshToken {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	web := refreshed.Data.AccessToken
	if c, err := auth.VerifyAccessToken(web, []byte(testSecret)); err != nil || c.Surface != auth.SurfaceWeb || c.Sid == "" {
		t.Fatalf("the refreshed web token: %+v (%v)", c, err)
	}
	device, _ := e.signIn(zz, sessions.KindDevice, "memax CLI")

	quarantined := func() memory {
		var res result
		e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: map[string]any{
			"statement": "The blog says to pin the Go toolchain " + uuid.NewString()[:8] + ".", "section": "conventions",
			"sources": []map[string]any{{"kind": "url", "ref": "a blog", "uri": "https://example.com/go"}},
		}}).ok(http.StatusCreated, &res)
		return res.Memory
	}
	m := quarantined()
	keep := func(token string, sign func(*http.Request, []byte)) *resp {
		return e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: sign})
	}
	if code := policyCode(t, keep(web, nil)); code != "external_needs_review" {
		t.Errorf("the web token without the proxy's signature: %s", code)
	}
	if code := policyCode(t, keep(device, webSigned(zz, tamper{}))); code != "external_needs_review" {
		t.Errorf("a device session, signed: %s", code)
	}
	var res result
	keep(web, webSigned(zz, tamper{})).ok(http.StatusOK, &res)
	if rc := res.Receipts[len(res.Receipts)-1]; rc.Assurance != "human_web" || rc.Via != "web" {
		t.Errorf("the signed web keep: %+v", rc)
	}
}
