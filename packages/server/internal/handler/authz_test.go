package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExpandPermissionBundles(t *testing.T) {
	perms, invalid := ExpandPermissionBundles([]string{"read", "write", "organize"})
	if len(invalid) != 0 {
		t.Fatalf("invalid = %v, want none", invalid)
	}
	for _, perm := range []Permission{PermMemoryRead, PermMemoryWrite, PermTopicRead, PermTopicWrite, PermDreamRead, PermDreamRun, PermHubRead, PermHubMembersRead} {
		if !perms.Has(perm) {
			t.Fatalf("expected expanded permissions to include %s", perm)
		}
	}
	if perms.Has(PermMemoryDelete) {
		t.Fatalf("write/organize should not imply %s", PermMemoryDelete)
	}
}

func TestExpandPermissionBundlesRejectsUnknown(t *testing.T) {
	_, invalid := ExpandPermissionBundles([]string{"read", "unknown:scope"})
	if len(invalid) != 1 || invalid[0] != "unknown:scope" {
		t.Fatalf("invalid = %v, want unknown:scope", invalid)
	}
}

func TestAuthorizeHTTPDeniesWriteWithoutGrantPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", strings.NewReader(`{}`))
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, writeHubIDKey, "hub1")
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID: "u1",
		PermissionsByHub: map[string]PermissionSet{
			"hub1": NewPermissionSet(PermMemoryRead),
		},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	AuthorizeHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCanDeniesWhenAuthContextMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)

	if Can(req, PermMemoryRead, "hub1") {
		t.Fatalf("Can should deny when AuthContext is missing")
	}
	if CanAny(req, PermMemoryRead) {
		t.Fatalf("CanAny should deny when AuthContext is missing")
	}
}

// TestRequiredHTTPPermissionNotificationRoutes drift-proofs the
// authz matrix entry for /v1/notifications. Every Phase 3b endpoint
// the router mounts must resolve to a permission + hub-scope tuple
// and must NOT fall through to "no permission required", which
// would silently skip authorization.
//
// If a new notification endpoint is added to routes.go and this
// test is not updated, the router will start serving it without
// the AuthorizeHTTP gate — exactly the class of bug this test
// catches.
func TestRequiredHTTPPermissionNotificationRoutes(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		wantPerm   Permission
		wantScoped bool
	}{
		{"list", http.MethodGet, "/v1/notifications", PermDreamRead, false},
		{"summary", http.MethodGet, "/v1/notifications/summary", PermDreamRead, false},
		{"mark seen", http.MethodPost, "/v1/notifications/abc/seen", PermDreamRead, false},
		{"dismiss", http.MethodPost, "/v1/notifications/abc/dismiss", PermDreamRead, false},
		{"resolve", http.MethodPost, "/v1/notifications/abc/resolve", PermDreamRead, false},
		{"bulk seen", http.MethodPost, "/v1/notifications/seen", PermDreamRead, false},
		{"bulk dismiss", http.MethodPost, "/v1/notifications/dismiss", PermDreamRead, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			perm, hubScoped, ok := requiredHTTPPermission(req)
			if !ok {
				t.Fatalf("%s %s: no permission matched; routes added without authz are a privilege-escalation bug", tc.method, tc.path)
			}
			if perm != tc.wantPerm {
				t.Errorf("%s %s: permission = %q, want %q", tc.method, tc.path, perm, tc.wantPerm)
			}
			if hubScoped != tc.wantScoped {
				t.Errorf("%s %s: hubScoped = %v, want %v", tc.method, tc.path, hubScoped, tc.wantScoped)
			}
		})
	}
}

// TestRequiredHTTPPermissionReviewsRoutes is a companion drift-proof
// for the legacy /v1/reviews surface. /v1/notifications must stay
// in lockstep with this until Phase 6 removes /v1/reviews — if the
// two divergence in scope / permission, clients see different
// authz behavior between the two inbox surfaces.
func TestRequiredHTTPPermissionReviewsRoutes(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		wantPerm   Permission
		wantScoped bool
	}{
		{"list", http.MethodGet, "/v1/reviews", PermDreamRead, false},
		{"resolve", http.MethodPost, "/v1/reviews/abc/resolve", PermDreamRead, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			perm, hubScoped, ok := requiredHTTPPermission(req)
			if !ok {
				t.Fatalf("%s %s: no permission matched", tc.method, tc.path)
			}
			if perm != tc.wantPerm {
				t.Errorf("%s %s: permission = %q, want %q", tc.method, tc.path, perm, tc.wantPerm)
			}
			if hubScoped != tc.wantScoped {
				t.Errorf("%s %s: hubScoped = %v, want %v", tc.method, tc.path, hubScoped, tc.wantScoped)
			}
		})
	}
}
