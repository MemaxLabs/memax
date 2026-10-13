package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// Explicit permissions need no legacy scope bundle. Keep the stored scope
// array non-null and do not add the default read/write permissions.
func TestCreateAPIKeyWithExplicitPermissions(t *testing.T) {
	t.Parallel()
	st, pool := testdb.Acquire(t)
	ctx := context.Background()
	secret := []byte("auth-key-permissions-test-secret-0123456789")
	h := &AuthHandler{pool: pool, store: st, jwtSecret: secret}
	user := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`,
		user, user.String()[:8]+"@key-permissions.test"); err != nil {
		t.Fatal(err)
	}
	person, err := auth.SignSessionToken(user.String(), auth.SurfaceCLI, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := RequireAuth(secret, h.ResolveAPIKey, nil)(http.HandlerFunc(h.CreateAPIKey))
	for _, c := range []struct {
		name, body string
		wantScopes []string
	}{
		{"omitted scopes", `{"name":"explicit","standalone":true,"permissions":["memory:read"]}`, []string{}},
		{"empty scopes", `{"name":"empty","standalone":true,"scopes":[],"permissions":["memory:read"]}`, []string{}},
		{"permission as scope", `{"name":"scope","standalone":true,"scopes":["memory:read"]}`, []string{"memory:read"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/api-keys", strings.NewReader(c.body))
			req.Header.Set("Authorization", "Bearer "+person)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
			}
			var env struct {
				Data struct {
					ID                 string   `json:"id"`
					Key                string   `json:"key"`
					DefaultPermissions []string `json:"default_permissions"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(env.Data.DefaultPermissions, []string{"memory:read"}) {
				t.Fatalf("response permissions = %v, want only memory:read", env.Data.DefaultPermissions)
			}
			var scopes, permissions []string
			if err := pool.QueryRow(ctx, `SELECT scopes, default_permissions FROM api_keys WHERE id = $1 AND user_id = $2`,
				env.Data.ID, user).Scan(&scopes, &permissions); err != nil {
				t.Fatal(err)
			}
			if scopes == nil || !slices.Equal(scopes, c.wantScopes) {
				t.Fatalf("stored scopes = %#v, want non-null %v", scopes, c.wantScopes)
			}
			if !slices.Equal(permissions, []string{"memory:read"}) {
				t.Fatalf("stored permissions = %v, want only memory:read", permissions)
			}
			grant := h.ResolveAPIKey(env.Data.Key)
			if grant.DefaultPermissions.Has(PermMemoryWrite) || !grant.DefaultPermissions.Has(PermMemoryRead) {
				t.Fatalf("resolved permissions = %v", grant.DefaultPermissions)
			}
		})
	}
}
