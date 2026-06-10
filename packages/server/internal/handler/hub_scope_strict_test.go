package handler

import (
	"context"
	"net/http/httptest"
	"testing"
)

// hub_scope_strict_test.go pins the load-bearing decision: when a
// request's authorization is scope-bounded (HubScopeAllowlist on
// the AuthContext), requestRecallScope MUST set Strict=true so
// RunPipeline routes to SearchChunksInHubs and the principal can
// never see content outside their granted hubs. When the auth is
// HubScopeAllAccessible (default web/CLI session), Strict=false
// keeps cross-hub recall semantics with active-hub-boost as a
// ranking signal, NOT an access boundary.
//
// These are unit tests on the helper, not integration tests on
// the full pipeline. The store-level leak prevention is pinned
// separately in postgres_chunks_strict_hub_test.go; what we test
// here is that the right strict signal flows from auth → scope.

func TestRequestRecallScopeMarksStrictForHubAllowlist(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/recall", nil)

	auth := &AuthContext{
		UserID:        "u1",
		PrincipalType: "oauth_grant",
		HubScopeMode:  HubScopeAllowlist, // <-- the bounded shape
		ScopedHubIDs:  []string{"h-team"},
	}
	req = req.WithContext(context.WithValue(req.Context(), authContextKey, auth))
	req = InjectAccessibleHubIDsForTest(req, []string{"h-team"})

	scope := requestRecallScope(req, nil)
	if !scope.Strict {
		t.Errorf("expected Strict=true for HubScopeAllowlist principal, got false")
	}
}

func TestRequestRecallScopeUnmarkedForAllAccessible(t *testing.T) {
	// Cookie-based web session; HubScopeAllAccessible. Cross-hub
	// recall is by design — the user's own content surfaces no
	// matter which hub they explicitly active-hub'd to. Active
	// hub is a ranking-relevance signal, not an access boundary.
	req := httptest.NewRequest("GET", "/v1/recall", nil)
	auth := &AuthContext{
		UserID:        "u1",
		PrincipalType: "session",
		HubScopeMode:  HubScopeAllAccessible,
	}
	req = req.WithContext(context.WithValue(req.Context(), authContextKey, auth))
	req = InjectAccessibleHubIDsForTest(req, []string{"h-personal", "h-work", "h-team"})

	scope := requestRecallScope(req, nil)
	if scope.Strict {
		t.Errorf("expected Strict=false for HubScopeAllAccessible session, got true")
	}
}

func TestRequestRecallScopeFailsClosedWhenAuthMissing(t *testing.T) {
	// FAIL-CLOSED — codex flagged this on the strict-recall review
	// (commit 5655ac2e). When AuthContext is missing or has empty
	// HubScopeMode, isHubScopeBounded returns true (strict), so
	// downstream recall returns empty rather than leak owner
	// content. A wiring regression is detectable as "recall
	// suddenly empty"; a leak isn't detectable until it's already
	// happened.
	req := httptest.NewRequest("GET", "/v1/recall", nil)
	req = InjectAccessibleHubIDsForTest(req, []string{"h-personal"})

	scope := requestRecallScope(req, nil)
	if !scope.Strict {
		t.Errorf("expected Strict=true (fail-closed) when auth context is missing, got false")
	}
}

func TestIsHubScopeBoundedHelper(t *testing.T) {
	cases := []struct {
		name string
		auth *AuthContext
		want bool
	}{
		{
			name: "nil_auth_fails_closed",
			auth: nil,
			want: true, // fail-closed: missing auth → strict, returns empty
		},
		{
			name: "empty_mode_fails_closed",
			auth: &AuthContext{},
			want: true, // fail-closed: half-constructed AuthContext → strict
		},
		{
			name: "all_accessible_explicit_false",
			auth: &AuthContext{HubScopeMode: HubScopeAllAccessible},
			want: false, // only HubScopeAllAccessible explicitly opens cross-hub
		},
		{
			name: "allowlist_marks_strict",
			auth: &AuthContext{HubScopeMode: HubScopeAllowlist},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.auth != nil {
				req = req.WithContext(context.WithValue(req.Context(), authContextKey, tc.auth))
			}
			if got := isHubScopeBounded(req); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
