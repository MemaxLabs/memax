package serverapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/contract"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/openapi"
)

// TestV2RoutesMatchSpec mounts /v2 the way registerRoutes does, behind
// the real auth chain, and sends every operation in v2.yaml through it.
// Without a token each one is 401 from RequireAuth; with one, each
// reaches its v2 handler (503 unavailable: these deps have no ledger)
// rather than a 404 or 405 from the router. Together with
// v2api.TestRoutesMatchSpec (the route table equals the spec) and
// TestV2RoutesOnlyComeFromV2API below, the spec and the server can't
// disagree about which /v2 routes exist.
func TestV2RoutesMatchSpec(t *testing.T) {
	t.Parallel()
	spec, err := contract.Load(openapi.V2)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("serverapp-v2-test-secret")
	s := store.NewInMemoryStore()
	deps := routeDeps{
		authMiddleware: handler.RequireAuth(secret, nil, nil),
		hubMiddleware:  handler.HubContext(s),
		store:          s,
		v2:             v2api.New(nil, nil),
	}
	root := http.NewServeMux()
	registerV2Routes(root, authChain(deps), deps)
	tok, err := auth.SignAccessToken(uuid.NewString(), secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	template := regexp.MustCompile(`\{[a-z_]+\}`)
	for _, op := range spec.Operations() {
		path := template.ReplaceAllString(op.Path, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")
		for _, c := range []struct {
			token  string
			status int
			code   string
		}{{"", 401, "unauthorized"}, {tok, 503, "unavailable"}} {
			r := httptest.NewRequest(op.Method, path, nil)
			if c.token != "" {
				r.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			root.ServeHTTP(rec, r)
			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
			if rec.Code != c.status || env.Error.Code != c.code {
				t.Errorf("%s %s (%s): %d %s, want %d %s", op.Method, path, op.ID, rec.Code, env.Error.Code, c.status, c.code)
			}
		}
	}
}

// TestV2RoutesOnlyComeFromV2API: every /v2 route is registered through
// registerV2Routes (from v2api.Routes()), behind withAuth, and app.go
// wires the handler. A /v2 pattern registered anywhere else in routes.go
// would escape the spec-parity tests.
func TestV2RoutesOnlyComeFromV2API(t *testing.T) {
	t.Parallel()
	routes, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`"(?:[A-Z]+ )?/v2[/"]`).FindAll(routes, -1); len(m) > 0 {
		t.Errorf("routes.go registers /v2 paths directly (%q); add them to v2.yaml and v2api.routes instead", m)
	}
	if !strings.Contains(string(routes), "registerV2Routes(mux, withAuth, deps)") {
		t.Error("registerRoutes must mount /v2 with registerV2Routes(mux, withAuth, deps)")
	}
	app, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	// v2Handler builds the handler on the ledger (with River and the
	// compile coordinator around it), the searcher MCP v2 reads with, and
	// the read recorder.
	if !regexp.MustCompile(`v2h, v2Search, readRecorder := v2Handler\(pool, `).Match(app) ||
		!regexp.MustCompile(`v2:\s+v2h,`).Match(app) ||
		!regexp.MustCompile(`v2Search:\s+v2Search,`).Match(app) ||
		!regexp.MustCompile(`l := ledger\.New\(pool, opts\.\.\.\)`).Match(app) ||
		!regexp.MustCompile(`return v2api\.New\(l, `).Match(app) {
		t.Error("app.go must pass v2 and v2Search from v2Handler(pool, …) to registerRoutes, and v2Handler must return v2api.New(ledger.New(pool, …), …)")
	}
}
