package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestOriginSharedSecret_Unset_PassesThrough(t *testing.T) {
	// No env var — middleware must be a pure no-op. Every request
	// reaches the inner handler, no header check performed.
	t.Setenv("ORIGIN_SHARED_SECRET", "")
	inner := 0
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner++
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (disabled mode), got %d", rec.Code)
	}
	if inner != 1 {
		t.Errorf("inner should have run once, got %d", inner)
	}
}

func TestOriginSharedSecret_Set_WrongSecret_Forbids(t *testing.T) {
	t.Setenv("ORIGIN_SHARED_SECRET", "correct-horse-battery-staple")
	inner := 0
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner++
	}))
	r := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	r.Header.Set(OriginSharedSecretHeader, "wrong-value")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if inner != 0 {
		t.Error("inner handler must not run when secret mismatches")
	}
}

func TestOriginSharedSecret_Set_MissingHeader_Forbids(t *testing.T) {
	// Header omitted entirely — should still be 403. This is the
	// realistic attack: direct-to-origin hit bypasses Cloudflare so
	// the header was never injected.
	t.Setenv("ORIGIN_SHARED_SECRET", "hunter2")
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner must not run")
	}))
	r := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing header, got %d", rec.Code)
	}
}

func TestOriginSharedSecret_Set_CorrectSecret_Allows(t *testing.T) {
	t.Setenv("ORIGIN_SHARED_SECRET", "hunter2")
	inner := 0
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner++
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	r.Header.Set(OriginSharedSecretHeader, "hunter2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if inner != 1 {
		t.Errorf("inner should have run once, got %d", inner)
	}
}

func TestOriginSharedSecret_HealthPathExempt(t *testing.T) {
	// Fly's HTTP health checks can't inject custom headers, so /health
	// MUST be reachable without the secret — otherwise the entire
	// machine fails health and the platform kills it.
	t.Setenv("ORIGIN_SHARED_SECRET", "hunter2")
	called := 0
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/health", "/healthz"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		// NO X-Origin-Secret header.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200 (exempt), got %d", path, rec.Code)
		}
	}
	if called != 2 {
		t.Errorf("expected 2 calls to inner for exempt paths, got %d", called)
	}
}

func TestWriteJSON_DefaultsToNoStore(t *testing.T) {
	// Defense-in-depth: every ApiResponse gets Cache-Control: no-store
	// unless the handler explicitly set one beforehand.
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, model.ApiResponse{Data: "ok"})
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("expected default Cache-Control: no-store, got %q", got)
	}
}

func TestWriteJSON_RespectsExistingCacheControl(t *testing.T) {
	// /v1/plans deliberately sets `public, max-age=60`. writeJSON
	// must not clobber an explicit caller choice.
	rec := httptest.NewRecorder()
	rec.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(rec, http.StatusOK, model.ApiResponse{Data: "ok"})
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("handler-set Cache-Control must be preserved, got %q", got)
	}
}

func TestOriginSharedSecret_OptionsPreflight_Rejected(t *testing.T) {
	// Regression for Codex finding: OPTIONS preflights without the
	// secret must be rejected. Previously the CORS middleware was
	// outermost and short-circuited OPTIONS to 200 before the secret
	// check ran, creating a direct-to-origin bypass for anything a
	// browser could drive.
	t.Setenv("ORIGIN_SHARED_SECRET", "test-secret")
	h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner must not run for OPTIONS without secret")
	}))
	r := httptest.NewRequest(http.MethodOptions, "/v1/memories", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for OPTIONS without secret, got %d", rec.Code)
	}
}

func TestOriginSharedSecret_LengthMismatch_Rejected(t *testing.T) {
	// Constant-time compare must reject mismatched-length input
	// without leaking timing info. Behavioral check: shorter or
	// longer input than the secret both return 403.
	t.Setenv("ORIGIN_SHARED_SECRET", "correct-horse-battery-staple")
	for _, bad := range []string{"", "short", "correct-horse-battery-staple-plus-extra"} {
		h := OriginSharedSecretMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("inner must not run for %q", bad)
		}))
		r := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
		if bad != "" {
			r.Header.Set(OriginSharedSecretHeader, bad)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("len=%d: expected 403, got %d", len(bad), rec.Code)
		}
	}
}
