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

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

// Operators read and change a person's V2 UI flag behind the admin
// sub-mux: an admin turns it on, off and back to the rules, each change
// audited with them; anyone else is refused and changes nothing.
func TestAdminV2UI(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h := NewAdminV2UIHandler(v2ui.New(pool, since))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/admin/users/{id}/v2-ui", h.Get)
	mux.HandleFunc("PUT /v1/admin/users/{id}/v2-ui", h.Set)
	guarded := AdminMiddleware(s)(mux)

	person := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, created_at) VALUES ($1, $2, $3, $4)`,
			id, name+"-"+id.String()[:8]+"@admin-ui.test", name, since.AddDate(0, -1, 0)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	admin, someone, target := person("admin"), person("someone"), person("target")
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles (user_id, role) VALUES ($1, 'super_admin')`, admin); err != nil {
		t.Fatal(err)
	}

	do := func(as uuid.UUID, method, path, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if as != uuid.Nil {
			req = req.WithContext(context.WithValue(req.Context(), userIDKey, as.String()))
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		var env map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return rec.Code, env
	}
	path := "/v1/admin/users/" + target.String() + "/v2-ui"
	flag := func(env map[string]any) string {
		d, _ := env["data"].(map[string]any)
		return d["ui"].(string) + " " + d["reason"].(string) + " " + d["setting"].(string)
	}

	// Not an admin: refused, and the flag stays as it was.
	for name, as := range map[string]uuid.UUID{"no session": uuid.Nil, "not an admin": someone} {
		for _, m := range []string{http.MethodGet, http.MethodPut} {
			if code, env := do(as, m, path, `{"setting":"on"}`); code != http.StatusUnauthorized && code != http.StatusForbidden {
				t.Errorf("%s %s: %d %v", name, m, code, env)
			}
		}
	}
	code, env := do(admin, http.MethodGet, path, "")
	if code != http.StatusOK || flag(env) != "v1 none default" {
		t.Fatalf("get: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["user_id"] != target.String() || data["since"] != "2026-11-01T00:00:00Z" {
		t.Errorf("get = %v", data)
	}

	// On, then off over a space on V2, then back to the rules.
	if code, env := do(admin, http.MethodPut, path, `{"setting":"on"}`); code != http.StatusOK || flag(env) != "v2 operator_on on" {
		t.Errorf("on: %d %v", code, env)
	}
	if _, env := do(admin, http.MethodGet, path, ""); flag(env) != "v2 operator_on on" {
		t.Errorf("after on: %v", env)
	}
	hub := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'acme', $2, 'team', $3, 'project', now())`,
		hub, "acme-"+hub.String()[:8], target); err != nil {
		t.Fatal(err)
	}
	if code, env := do(admin, http.MethodPut, path, `{"setting":"off"}`); code != http.StatusOK || flag(env) != "v1 operator_off off" {
		t.Errorf("off: %d %v", code, env)
	}
	if code, env := do(admin, http.MethodPut, path, `{"setting":"default"}`); code != http.StatusOK || flag(env) != "v2 v2_space default" {
		t.Errorf("default: %d %v", code, env)
	}
	var audited string
	if err := pool.QueryRow(ctx, `
		SELECT string_agg((metadata->>'setting') || ':' || (metadata->>'via') || ':' || (actor_id = $2)::text, ',' ORDER BY created_at)
		  FROM admin_audit WHERE resource_type = 'user' AND resource_id = $1::text AND action = 'v2_ui'`, target, admin).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != "on:admin:true,off:admin:true,default:admin:true" {
		t.Errorf("audit = %s", audited)
	}

	// What isn't a setting, or a person.
	for body, want := range map[string]string{`{"setting":"maybe"}`: "invalid_setting", `{"setting":`: "invalid_json", `{}`: "invalid_setting"} {
		if code, env := do(admin, http.MethodPut, path, body); code != http.StatusBadRequest || env["error"].(map[string]any)["code"] != want {
			t.Errorf("%s: %d %v", body, code, env)
		}
	}
	for _, p := range []string{"/v1/admin/users/" + uuid.NewString() + "/v2-ui", "/v1/admin/users/not-a-uuid/v2-ui"} {
		for _, m := range []string{http.MethodGet, http.MethodPut} {
			if code, _ := do(admin, m, p, `{"setting":"on"}`); code != http.StatusNotFound {
				t.Errorf("%s %s: %d", m, p, code)
			}
		}
	}

	// Without V2_UI_SINCE, since is null; without a database, no handler.
	unset := NewAdminV2UIHandler(v2ui.New(pool, time.Time{}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", target.String())
	unset.Get(rec, req)
	if !strings.Contains(rec.Body.String(), `"since":null`) {
		t.Errorf("since unset: %s", rec.Body.String())
	}
	if NewAdminV2UIHandler(nil) != nil {
		t.Error("a handler without a database")
	}
}
