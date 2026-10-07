package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

// /v1/auth/me says which web UI the person sees (internal/v2ui), and so
// does a sign-in code traded for a session issued to the web app, so the
// web app's server can set its routing cookie as the session starts. A
// CLI sign-in isn't told.
func TestMeAndWebSignInSayTheUI(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	secret := []byte("auth-v2-ui-test-secret-0123456789abcdef")
	h := &AuthHandler{pool: pool, jwtSecret: secret}
	h.SetV2UI(v2ui.New(pool, time.Time{}))

	person := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, name+"-"+id.String()[:8]+"@ui.test", name); err != nil {
			t.Fatal(err)
		}
		return id
	}
	v1 := person("v1")
	v2 := person("v2")
	hub := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'acme', $2, 'team', $3, 'project', now())`,
		hub, "acme-"+hub.String()[:8], v2); err != nil {
		t.Fatal(err)
	}

	me := func(id uuid.UUID) map[string]any {
		t.Helper()
		token, err := auth.SignSessionToken(id.String(), auth.SurfaceWeb, secret, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.Me(rec, req)
		var env struct {
			Data map[string]any `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &env) != nil {
			t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
		}
		return env.Data
	}
	if got := me(v1)["ui"]; got != "v1" {
		t.Errorf("a V1 person's me: ui = %v", got)
	}
	if got := me(v2)["ui"]; got != "v2" {
		t.Errorf("a member of a space on V2: ui = %v", got)
	}
	// An operator's choice shows on the next /me.
	if _, err := h.v2ui.Set(ctx, v1, v2ui.On, uuid.Nil, v2ui.ViaTest); err != nil {
		t.Fatal(err)
	}
	if got := me(v1)["ui"]; got != "v2" {
		t.Errorf("turned on: ui = %v", got)
	}
	if _, err := h.v2ui.Set(ctx, v2, v2ui.Off, uuid.Nil, v2ui.ViaTest); err != nil {
		t.Fatal(err)
	}
	if got := me(v2)["ui"]; got != "v1" {
		t.Errorf("turned off: ui = %v", got)
	}
	if _, err := h.v2ui.Set(ctx, v2, v2ui.Default, uuid.Nil, v2ui.ViaTest); err != nil {
		t.Fatal(err)
	}

	exchange := func(id uuid.UUID, surface string) map[string]any {
		t.Helper()
		code := uuid.NewString()
		if _, err := pool.Exec(ctx, `INSERT INTO auth_codes (code, user_id, expires_at, surface) VALUES ($1, $2, now() + interval '1 minute', NULLIF($3, ''))`,
			code, id, surface); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		h.ExchangeCode(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/exchange", strings.NewReader(`{"code":"`+code+`"}`)))
		var env struct {
			Data map[string]any `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &env) != nil {
			t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
		}
		if env.Data["access_token"] == "" || env.Data["refresh_token"] == "" {
			t.Fatalf("exchange without tokens: %v", env.Data)
		}
		return env.Data
	}
	if got := exchange(v2, auth.SurfaceWeb)["ui"]; got != "v2" {
		t.Errorf("web sign-in of a V2 person: ui = %v", got)
	}
	if got := exchange(person("new"), auth.SurfaceWeb)["ui"]; got != "v1" {
		t.Errorf("web sign-in of a V1 person: ui = %v", got)
	}
	if got, ok := exchange(v2, "")["ui"]; ok {
		t.Errorf("a CLI sign-in was told the web UI: %v", got)
	}

	// Without a resolver (no database for it): V1, as everywhere else.
	h.SetV2UI(nil)
	if got := me(v2)["ui"]; got != "v1" {
		t.Errorf("no resolver: ui = %v", got)
	}
}
