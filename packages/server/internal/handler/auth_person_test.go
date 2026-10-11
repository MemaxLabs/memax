package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// TestOnlyThePersonManagesAPIKeys: an API key, an agent's token or an
// operator impersonating can't mint a key (which could be wider than the
// credential asking: all hubs, standalone), list them, change them or
// revoke another; a key may revoke itself. The requests go through
// RequireAuth with real keys and tokens, as the routes do.
func TestOnlyThePersonManagesAPIKeys(t *testing.T) {
	t.Parallel()
	st, pool := testdb.Acquire(t)
	secret := []byte("auth-person-test-secret-0123456789")
	h := &AuthHandler{pool: pool, store: st, jwtSecret: secret}
	user := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`,
		user, user.String()[:8]+"@person.test"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/api-keys", h.CreateAPIKey)
	mux.HandleFunc("GET /v1/auth/api-keys", h.ListAPIKeys)
	mux.HandleFunc("PATCH /v1/auth/api-keys/{id}", h.UpdateAPIKey)
	mux.HandleFunc("DELETE /v1/auth/api-keys/{id}", h.RevokeAPIKey)
	mux.HandleFunc("GET /v1/auth/identities", h.ListIdentities)
	mux.HandleFunc("GET /v1/auth/link/{provider}", h.LinkProvider)
	mux.HandleFunc("DELETE /v1/auth/link/{provider}", h.UnlinkProvider)
	srv := RequireAuth(secret, h.ResolveAPIKey, nil)(mux)

	do := func(token, method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		raw, _ := io.ReadAll(rec.Body)
		return rec.Code, string(raw)
	}
	type created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	create := func(token, body string) created {
		t.Helper()
		code, raw := do(token, http.MethodPost, "/v1/auth/api-keys", body)
		if code != http.StatusCreated {
			t.Fatalf("create: %d %s", code, raw)
		}
		var env struct {
			Data created `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &env); err != nil || env.Data.Key == "" {
			t.Fatalf("create answer: %v %s", err, raw)
		}
		return env.Data
	}

	person, _ := auth.SignSessionToken(user.String(), auth.SurfaceWeb, secret, time.Hour)
	cli, _ := auth.SignSessionToken(user.String(), auth.SurfaceCLI, secret, time.Hour)
	agentJWT, _ := auth.SignAgentAccessToken(user.String(), "claude-ai", secret, time.Hour)
	impersonated, _ := auth.SignImpersonationToken(user.String(), uuid.NewString(), secret, time.Hour)

	agentKey := create(person, `{"name":"agent","agent_name":"claude-code","scopes":["read"]}`)
	other := create(cli, `{"name":"other","agent_name":"codex"}`)

	refused := map[string]string{"api key": agentKey.Key, "agent token": agentJWT}
	for who, token := range refused {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodPost, "/v1/auth/api-keys", `{"name":"wider","standalone":true,"scopes":["read","write"]}`},
			{http.MethodGet, "/v1/auth/api-keys", ""},
			{http.MethodPatch, "/v1/auth/api-keys/" + other.ID, `{"standalone":true}`},
			{http.MethodDelete, "/v1/auth/api-keys/" + other.ID, ""},
			{http.MethodGet, "/v1/auth/identities", ""},
			{http.MethodGet, "/v1/auth/link/github", ""},
			{http.MethodDelete, "/v1/auth/link/github", ""},
		} {
			if code, raw := do(token, c.method, c.path, c.body); code != http.StatusForbidden {
				t.Errorf("%s: %s %s = %d %s, want 403", who, c.method, c.path, code, raw)
			}
		}
	}

	// An operator impersonating reads the keys but mints none and links nothing.
	if code, raw := do(impersonated, http.MethodPost, "/v1/auth/api-keys", `{"name":"x","standalone":true}`); code != http.StatusForbidden {
		t.Errorf("impersonating: create = %d %s, want 403", code, raw)
	}
	if code, raw := do(impersonated, http.MethodGet, "/v1/auth/link/github", ""); code != http.StatusForbidden {
		t.Errorf("impersonating: link = %d %s, want 403", code, raw)
	}
	if code, raw := do(impersonated, http.MethodGet, "/v1/auth/api-keys", ""); code != http.StatusOK {
		t.Errorf("impersonating: list = %d %s, want 200", code, raw)
	}

	// Nothing the refused requests asked for happened.
	var keys int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM api_keys WHERE user_id = $1`, user).Scan(&keys); err != nil || keys != 2 {
		t.Fatalf("keys = %d (%v), want the person's 2", keys, err)
	}
	if code, _ := do(other.Key, http.MethodGet, "/v1/auth/api-keys", ""); code != http.StatusForbidden {
		t.Errorf("the other key should still resolve (and be refused the list), got %d", code)
	}

	// A key revokes itself, and then is no credential at all.
	if code, raw := do(agentKey.Key, http.MethodDelete, "/v1/auth/api-keys/"+agentKey.ID, ""); code != http.StatusOK || !strings.Contains(raw, `"revoked":true`) {
		t.Fatalf("self-revoke = %d %s", code, raw)
	}
	if code, _ := do(agentKey.Key, http.MethodGet, "/v1/auth/api-keys", ""); code != http.StatusUnauthorized {
		t.Errorf("a revoked key should be refused as a credential, got %d", code)
	}

	// The person, on the web or the CLI, does all of it.
	if code, raw := do(person, http.MethodGet, "/v1/auth/api-keys", ""); code != http.StatusOK || !strings.Contains(raw, other.ID) {
		t.Errorf("person list = %d %s", code, raw)
	}
	if code, raw := do(cli, http.MethodDelete, "/v1/auth/api-keys/"+other.ID, ""); code != http.StatusOK || !strings.Contains(raw, `"revoked":true`) {
		t.Errorf("person revoke = %d %s", code, raw)
	}
}

// TestImpersonateAndProfileRefuseAgents: the two routes that read the JWT
// themselves, outside RequireAuth, take only a person's own token, and
// impersonation only from the web app.
func TestImpersonateAndProfileRefuseAgents(t *testing.T) {
	t.Parallel()
	secret := []byte("auth-person-test-secret-0123456789")
	h := &AuthHandler{jwtSecret: secret} // no store: past the checks, Impersonate answers 503
	user := uuid.NewString()
	web, _ := auth.SignSessionToken(user, auth.SurfaceWeb, secret, time.Hour)
	cli, _ := auth.SignSessionToken(user, auth.SurfaceCLI, secret, time.Hour)
	agent, _ := auth.SignAgentAccessToken(user, "claude-ai", secret, time.Hour)
	grant, _ := auth.SignGrantAccessToken(user, "claude-ai", uuid.NewString(), secret, time.Hour)
	bound, _ := auth.SignBoundGrantAccessToken(user, "claude-ai", uuid.NewString(), "https://api.memax.app",
		[]string{"https://api.memax.app/mcp"}, secret, time.Hour)

	call := func(fn http.HandlerFunc, token, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec.Code
	}
	for name, token := range map[string]string{"cli": cli, "agent": agent, "grant": grant, "bound grant": bound} {
		if code := call(h.Impersonate, token, `{"email":"a@b.c"}`); code != http.StatusForbidden {
			t.Errorf("impersonate with %s token = %d, want 403", name, code)
		}
	}
	if code := call(h.Impersonate, web, `{"email":"a@b.c"}`); code != http.StatusServiceUnavailable {
		t.Errorf("impersonate with web token = %d, want past the checks (503 without a store)", code)
	}
	for name, token := range map[string]string{"agent": agent, "grant": grant, "bound grant": bound} {
		if code := call(h.UpdateMe, token, `{"display_name":"x"}`); code != http.StatusForbidden {
			t.Errorf("update profile with %s token = %d, want 403", name, code)
		}
	}
	for name, token := range map[string]string{"web": web, "cli": cli} {
		if code := call(h.UpdateMe, token, `{"display_name":"x"}`); code != http.StatusOK {
			t.Errorf("update profile with %s token = %d, want 200", name, code)
		}
	}
}
