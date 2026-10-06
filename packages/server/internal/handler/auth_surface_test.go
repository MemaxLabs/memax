package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestRedirectSurface(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{redirectAllowlist: []string{"https://memax.app"}}
	cases := map[string]string{
		"https://memax.app/auth/callback":            auth.SurfaceWeb,
		"https://memax.app/auth/callback?invite=abc": auth.SurfaceWeb,
		"http://localhost:51234/callback":            auth.SurfaceCLI, // memax login
		"http://127.0.0.1:3000/callback":             auth.SurfaceCLI,
		"https://memax.app.evil.example/cb":          auth.SurfaceCLI,
		"https://evil.example/https://memax.app":     auth.SurfaceCLI,
		"":                                           auth.SurfaceCLI,
		"not a url":                                  auth.SurfaceCLI,
	}
	for redirect, want := range cases {
		if got := h.redirectSurface(redirect); got != want {
			t.Errorf("redirectSurface(%q) = %s, want %s", redirect, got, want)
		}
	}
	// Local dev: the web app on localhost:3000 is the web.
	dev := &AuthHandler{redirectAllowlist: redirectAllowlistFromAppBaseURL("http://localhost:3000")}
	if got := dev.redirectSurface("http://localhost:3000/auth/callback"); got != auth.SurfaceWeb {
		t.Errorf("dev web callback = %s", got)
	}
	if got := dev.redirectSurface("http://localhost:3001/callback"); got != auth.SurfaceCLI {
		t.Errorf("dev CLI callback = %s", got)
	}
}

// TestSessionSurfaceSurvivesExchangeAndRefresh: a login whose code goes to
// the web app yields "web" tokens, before and after refresh; a CLI login
// yields "cli" tokens; neither can become the other.
func TestSessionSurfaceSurvivesExchangeAndRefresh(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	secret := []byte("auth-surface-test-secret-0123456789")
	h := &AuthHandler{pool: pool, jwtSecret: secret, redirectAllowlist: []string{"https://memax.app"}}
	user := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, user, user.String()[:8]+"@surface.test"); err != nil {
		t.Fatal(err)
	}

	surfaceOf := func(token string) string {
		t.Helper()
		c, err := auth.VerifyAccessToken(token, secret)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return c.Surface
	}
	post := func(fn http.HandlerFunc, body any) model.TokenPair {
		t.Helper()
		raw, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw))))
		var env struct {
			Data model.TokenPair `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &env) != nil {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		return env.Data
	}
	login := func(redirect string) model.TokenPair {
		t.Helper()
		rec := httptest.NewRecorder()
		h.completeLogin(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil), &model.User{ID: user.String()}, redirect)
		if redirect == "" {
			var env struct {
				Data model.TokenPair `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			return env.Data
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil || loc.Query().Get("code") == "" {
			t.Fatalf("redirect = %q", rec.Header().Get("Location"))
		}
		return post(h.ExchangeCode, map[string]string{"code": loc.Query().Get("code")})
	}

	for _, c := range []struct {
		name, redirect, want string
	}{
		{"web app", "https://memax.app/auth/callback", auth.SurfaceWeb},
		{"memax login", "http://localhost:51234/callback", auth.SurfaceCLI},
		{"tokens in the response", "", auth.SurfaceCLI},
	} {
		tokens := login(c.redirect)
		if got := surfaceOf(tokens.AccessToken); got != c.want {
			t.Errorf("%s: token surface %q, want %q", c.name, got, c.want)
		}
		refreshed := post(h.Refresh, map[string]string{"refresh_token": tokens.RefreshToken})
		if got := surfaceOf(refreshed.AccessToken); got != c.want {
			t.Errorf("%s: refreshed token surface %q, want %q", c.name, got, c.want)
		}
	}

	// A session from before migration 030 has no surface, and stays so.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (user_id, refresh_token, expires_at) VALUES ($1, 'old-refresh-token', now() + interval '1 day')`, user); err != nil {
		t.Fatal(err)
	}
	if got := surfaceOf(post(h.Refresh, map[string]string{"refresh_token": "old-refresh-token"}).AccessToken); got != "" {
		t.Errorf("an old session refreshed into surface %q", got)
	}
}

func TestRequireAuthCarriesTheSessionSurface(t *testing.T) {
	t.Parallel()
	secret := []byte("auth-surface-test-secret-0123456789")
	var got GrantContext
	mw := RequireAuth(secret, nil, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = GetGrant(r) }))
	user := uuid.NewString()
	web, _ := auth.SignSessionToken(user, auth.SurfaceWeb, secret, time.Hour)
	impersonated, _ := auth.SignImpersonationToken(user, uuid.NewString(), secret, time.Hour)
	agent, _ := auth.SignAgentAccessToken(user, "claude-code", secret, time.Hour)
	for token, want := range map[string]string{web: auth.SurfaceWeb, impersonated: "", agent: ""} {
		r := httptest.NewRequest(http.MethodGet, "/v2/spaces", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		got = GrantContext{}
		mw.ServeHTTP(httptest.NewRecorder(), r)
		if got.Surface != want || got.UserID != user {
			t.Errorf("surface %q for %q, want %q", got.Surface, got.UserID, want)
		}
	}
}
