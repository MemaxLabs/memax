package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func timeNowForTest() time.Time {
	return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
}

// mockPlanLimits feeds known limits into the middleware's resolver
// path without standing up the full plan registry.
type mockPlanLimits struct {
	limits model.UserLimits
}

func (m *mockPlanLimits) GetUserLimits(_ context.Context, _ string, _ string) model.UserLimits {
	return m.limits
}

type mockUsers struct {
	user *model.User
}

func (m *mockUsers) GetUser(_ string) (*model.User, error) {
	if m.user == nil {
		return nil, nil
	}
	return m.user, nil
}

func setupMiddleware(t *testing.T) (*miniredis.Miniredis, *Limiter) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, New(client)
}

// ─── Middleware — auth skip + rate pass-through ─────────────────

func TestMiddlewareSkipsNonMeteredOps(t *testing.T) {
	t.Parallel()

	_, l := setupMiddleware(t)
	called := false
	h := l.Middleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	// GET /v1/hubs isn't classified as push/recall/ask — skip.
	req := httptest.NewRequest(http.MethodGet, "/v1/hubs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("handler should have been invoked for non-metered op")
	}
}

func TestMiddlewareSkipsWhenNoUserID(t *testing.T) {
	t.Parallel()

	_, l := setupMiddleware(t)
	called := false
	h := l.Middleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Error("no user ID → should pass through")
	}
}

func TestMiddlewareSetsRateLimitHeadersOnSuccess(t *testing.T) {
	t.Parallel()

	_, l := setupMiddleware(t)
	limits := &mockPlanLimits{limits: model.UserLimits{RateLimitRPM: 100, RateLimitHeavyRPM: 10, RateLimitLightRPM: 60}}
	users := &mockUsers{user: &model.User{ID: "u1", PersonalPlanID: "personal_free"}}
	h := l.Middleware(limits, users)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", nil)
	req = handler.InjectUserIDForTest(req, "u1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("X-RateLimit-Limit header missing")
	}
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("X-RateLimit-Remaining header missing")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("X-RateLimit-Reset header missing")
	}
}

func TestMiddleware429OnOverLimit(t *testing.T) {
	t.Parallel()

	// Limit 1 per minute, fire 2 requests. Second should 429.
	_, l := setupMiddleware(t)
	limits := &mockPlanLimits{limits: model.UserLimits{RateLimitRPM: 1, RateLimitHeavyRPM: 1, RateLimitLightRPM: 1}}
	users := &mockUsers{user: &model.User{ID: "u1"}}
	h := l.Middleware(limits, users)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/ask", nil)
		req = handler.InjectUserIDForTest(req, "u1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Errorf("first request: got %d, want 200", rec.Code)
		}
		if i == 1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("second request: got %d, want 429", rec.Code)
			}
			if ra := rec.Header().Get("Retry-After"); ra == "" {
				t.Error("429 response missing Retry-After header")
			} else if n, _ := strconv.Atoi(ra); n < 1 {
				t.Errorf("Retry-After should be >= 1 second, got %q", ra)
			}
		}
	}
}

func TestMiddlewareEndpointOverrideUsesSeparateBucket(t *testing.T) {
	t.Parallel()

	_, l := setupMiddleware(t)
	limits := &mockPlanLimits{limits: model.UserLimits{RateLimitRPM: 240, RateLimitHeavyRPM: 30, RateLimitLightRPM: 240}}
	users := &mockUsers{user: &model.User{ID: "u1", PersonalPlanID: "hub_team"}}
	h := l.Middleware(limits, users)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
		req = handler.InjectUserIDForTest(req, "u1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("regular light request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", nil)
	req = handler.InjectUserIDForTest(req, "u1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first batch-delete after regular light traffic: got %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "15" {
		t.Fatalf("batch-delete limit header = %q, want 15", got)
	}
}

func TestMiddlewareEndpointOverrideStillCapsEndpoint(t *testing.T) {
	t.Parallel()

	_, l := setupMiddleware(t)
	limits := &mockPlanLimits{limits: model.UserLimits{RateLimitRPM: 240, RateLimitHeavyRPM: 30, RateLimitLightRPM: 240}}
	users := &mockUsers{user: &model.User{ID: "u1", PersonalPlanID: "hub_team"}}
	h := l.Middleware(limits, users)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 16; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", nil)
		req = handler.InjectUserIDForTest(req, "u1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if i < 15 && rec.Code != http.StatusOK {
			t.Fatalf("batch-delete request %d: got %d, want 200", i+1, rec.Code)
		}
		if i == 15 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("batch-delete request %d: got %d, want 429", i+1, rec.Code)
		}
	}
}

// ─── resolveLimitWithHub fallback paths ─────────────────────────

func TestResolveLimitWithHubFallbackWhenAllResolversMissing(t *testing.T) {
	t.Parallel()

	// No resolver, no registry, no users — function should emit
	// the per-opClass hardcoded defaults (10 heavy, 60 light, 120
	// metadata) so a misconfiguration still produces SOME limit
	// rather than 0 (which would deny every request).
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", nil)
	for _, tc := range []struct {
		op   OpClass
		want int
	}{
		{OpClassHeavy, 10},
		{OpClassLight, 60},
		{OpClassMetadata, 120},
	} {
		got := resolveLimitWithHub(req, "u1", "/v1/recall", tc.op, nil, nil, nil)
		if got != tc.want {
			t.Errorf("opClass %v: got %d, want %d", tc.op, got, tc.want)
		}
	}
}

func TestResolveLimitWithHubRegistryPath(t *testing.T) {
	t.Parallel()

	registry := &mockPlanLimits{limits: model.UserLimits{
		RateLimitRPM: 99, RateLimitHeavyRPM: 7, RateLimitLightRPM: 33,
	}}
	users := &mockUsers{user: &model.User{ID: "u1", PersonalPlanID: "personal_free"}}
	req := httptest.NewRequest(http.MethodPost, "/v1/ask", nil)
	if got := resolveLimitWithHub(req, "u1", "/v1/ask", OpClassHeavy, registry, users, nil); got != 7 {
		t.Errorf("heavy class via registry: got %d, want 7", got)
	}
	if got := resolveLimitWithHub(req, "u1", "/v1/memories", OpClassLight, registry, users, nil); got != 33 {
		t.Errorf("light class via registry: got %d, want 33", got)
	}
}

func TestResolveLimitWithHubReturnsDefaultWhenUserMissing(t *testing.T) {
	t.Parallel()

	// User lookup fails → fall back to the defensive 60 rpm.
	// Prevents a missing user from producing a 0-limit "deny all".
	registry := &mockPlanLimits{limits: model.UserLimits{RateLimitRPM: 99}}
	users := &mockUsers{user: nil}
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
	if got := resolveLimitWithHub(req, "u1", "/v1/memories", OpClassLight, registry, users, nil); got != 60 {
		t.Errorf("missing user: got %d, want 60 default", got)
	}
}

// ─── writeRateLimited shape ─────────────────────────────────────

func TestWriteRateLimitedHasHeadersAndRetryAfter(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeRateLimited(rec, Result{
		Allowed:    false,
		Limit:      10,
		Remaining:  0,
		ResetAt:    timeNowForTest(),
		RetryAfter: 5_000_000_000, // 5s in nanoseconds, via time.Duration
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "10" {
		t.Error("X-RateLimit-Limit missing or wrong")
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Error("X-RateLimit-Remaining missing or wrong")
	}
}

func TestWriteRateLimitedRetryAfterMinFloor(t *testing.T) {
	t.Parallel()

	// Sub-second RetryAfter should floor to 1s — clients that
	// retry immediately on "0" would re-hit the same limit.
	rec := httptest.NewRecorder()
	writeRateLimited(rec, Result{
		Allowed:    false,
		Limit:      1,
		RetryAfter: 100_000_000, // 100ms
	})
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("sub-second RetryAfter should floor to 1, got %q", got)
	}
}

// ─── TrustedProxyMode reads the env-cached var ──────────────────

func TestTrustedProxyModeReturnsConfiguredString(t *testing.T) {
	t.Parallel()

	// The var is set once at init from TRUSTED_PROXY; we can't
	// easily rewire it in a test, but we CAN assert the accessor
	// returns a known valid enum value (one of the documented
	// shapes). If a refactor ever returns a different shape,
	// callers that switch on the string break silently — this
	// guards against that.
	got := TrustedProxyMode()
	switch got {
	case "none", "cloudflare", "fly", "xff":
		// ok
	default:
		t.Errorf("TrustedProxyMode returned unexpected shape %q", got)
	}
}
