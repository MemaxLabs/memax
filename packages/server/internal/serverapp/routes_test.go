package serverapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
)

func TestRegisterAgentSyncRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	deps := routeDeps{
		configs: handler.NewConfigsHandler(nil, nil),
		uploads: handler.NewUploadsHandler(nil),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerAgentSyncRoutes panicked: %v", r)
		}
	}()

	registerAgentSyncRoutes(mux, deps)
}

// Regression: /v1/usage was registered on the protected inner mux
// (see registerAccountRoutes → `GET /v1/usage`) but never mounted on
// the root mux. Result in staging: every client hit 404, useUsage()
// returned undefined, and the bar's canUseAI gate fell through to
// false so early_access users saw the "AI is a Pro feature" free
// stub even though their plan carried ask_limit=150.
//
// Sibling `/v1/settings` was mounted correctly; /v1/usage just
// slipped the list when usage was split out as its own handler.
func TestRegisterProtectedMountsIncludesUsage(t *testing.T) {
	t.Parallel()

	root := http.NewServeMux()
	protected := http.NewServeMux()
	protected.HandleFunc("GET /v1/usage", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	registerProtectedMounts(root, protected, func(h http.Handler) http.Handler { return h })

	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected mounted /v1/usage route to be handled, got %d", rec.Code)
	}
}

// TestAPIKeyPatchRouteIsReachable is a regression guard against the
// prod 405 we saw after the first attribution ship. It stands up the
// api-key mount the same way registerAuthRoutes does and confirms a
// PATCH request gets to its handler rather than falling through to a
// sibling method's 405.
//
// If this ever starts failing, either the sub-mux lost the PATCH
// pattern registration, a conflicting sibling pattern was added
// higher up the tree, or a middleware wrapper (authMiddleware here
// replaced with pass-through for isolation) started rejecting the
// method before the handler runs.
func TestAPIKeyPatchRouteIsReachable(t *testing.T) {
	t.Parallel()

	root := http.NewServeMux()
	protected := http.NewServeMux()

	reached := false
	protected.HandleFunc("POST /v1/auth/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protected.HandleFunc("GET /v1/auth/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protected.HandleFunc("PATCH /v1/auth/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if id := r.PathValue("id"); id == "" {
			t.Errorf("expected {id} path value to populate, got empty")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	protected.HandleFunc("DELETE /v1/auth/api-keys/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Mirror registerAuthRoutes's mount pair. The trailing-slash
	// variant is what catches the /{id} pattern; the bare pattern
	// matches POST / GET at the collection root.
	root.Handle("/v1/auth/api-keys", protected)
	root.Handle("/v1/auth/api-keys/", protected)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/auth/api-keys/fe9103ad-f8f6-48f9-9dfc-8ecf700ccdb4",
		nil,
	)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if !reached {
		t.Errorf("PATCH handler was never invoked — mux returned %d", rec.Code)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf(
			"expected 204 from mounted PATCH route, got %d (regression: prod observed 405)",
			rec.Code,
		)
	}
}
