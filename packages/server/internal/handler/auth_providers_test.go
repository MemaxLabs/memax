package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pure helper tests for auth_providers.go — URL validation and
// OAuth state helpers that don't require a running DB.

func TestRedirectOrigin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{"https://memax.app", "https://memax.app", true},
		{"https://memax.app/some/path?q=1", "https://memax.app", true},
		{"http://localhost:3000", "http://localhost:3000", true},
		{"", "", false},
		{"   ", "", false},
		{"://broken", "", false},
		{"ftp://memax.app", "", false},
		{"https://", "", false},
		{"https://user:pass@memax.app", "", false}, // userinfo rejected
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, ok := redirectOrigin(tc.raw)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("origin = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRedirectAllowlistFromAppBaseURL already covered in auth_test.go.

func TestIsAllowedRedirect(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{redirectAllowlist: []string{"https://app.memax.app"}}

	cases := []struct {
		raw  string
		want bool
	}{
		{"", true}, // empty is always allowed
		{"https://app.memax.app", true},
		{"https://app.memax.app/path", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1:9000", true},
		{"http://[::1]:8080", true},
		{"http://other.com", false},                // non-loopback http rejected
		{"https://evil.com", false},                // not in allowlist
		{"https://user:pass@app.memax.app", false}, // userinfo rejected
		{"://broken", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := h.isAllowedRedirect(tc.raw); got != tc.want {
				t.Errorf("%q: got %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSHA256Hex(t *testing.T) {
	t.Parallel()
	// Stable hash — same input always yields the same hex.
	s := "test string"
	h1 := sha256Hex(s)
	h2 := sha256Hex(s)
	if h1 != h2 {
		t.Error("sha256Hex not deterministic")
	}
	if len(h1) != 64 { // 32 bytes → 64 hex chars
		t.Errorf("expected 64 hex chars, got %d", len(h1))
	}
	if sha256Hex("a") == sha256Hex("b") {
		t.Error("different inputs must hash differently")
	}
}

func TestIsAdminEmail(t *testing.T) {
	t.Parallel()
	// The admin list is always stored lowercase (parseOAuthAllowlist
	// normalizes on load).
	h := &AuthHandler{adminEmails: []string{"admin@memax.app", "ops@memax.app"}}
	if !h.isAdminEmail("admin@memax.app") {
		t.Error("exact match should be admin")
	}
	if !h.isAdminEmail("ADMIN@MEMAX.APP") {
		t.Error("case-insensitive match expected")
	}
	if h.isAdminEmail("user@memax.app") {
		t.Error("non-admin user should not be admin")
	}
	if h.isAdminEmail("") {
		t.Error("empty email should not be admin")
	}
}

func TestIsGoogleEmailAllowed_NoAllowlistAllowsAll(t *testing.T) {
	t.Parallel()
	// Both allowlists empty → all emails allowed (the `return true`
	// fast-path), matching the "open sign-up" default.
	h := &AuthHandler{}
	if !h.isGoogleEmailAllowed("anyone@anywhere.com") {
		t.Error("empty allowlist should allow all")
	}
}

func TestIsGoogleEmailAllowed_OrgGatedFailsClosedOnEmptyAllowlist(t *testing.T) {
	t.Parallel()
	// Regression: an earlier version of isGoogleEmailAllowed
	// returned true whenever both allowlists were empty, regardless
	// of registrationMode. That let any Google account sign in on
	// staging (org_gated) whenever the OAUTH_GOOGLE_ALLOWED_* env
	// vars were unset. In org_gated mode an empty allowlist must
	// fail closed — the allowlist IS the gate.
	h := &AuthHandler{registrationMode: "org_gated"}
	if h.isGoogleEmailAllowed("random@gmail.com") {
		t.Error("org_gated with empty allowlist must reject all Google accounts")
	}
	if h.isGoogleEmailAllowed("") {
		t.Error("empty email must also be rejected under org_gated")
	}
}

func TestIsGoogleEmailAllowed_OrgGatedRespectsPopulatedAllowlist(t *testing.T) {
	t.Parallel()
	// With an explicit allowlist, org_gated accepts matches and
	// rejects non-matches, same as open mode does.
	h := &AuthHandler{
		registrationMode:     "org_gated",
		googleAllowedDomains: map[string]struct{}{"memaxlabs.com": {}},
	}
	if !h.isGoogleEmailAllowed("ziyang@memaxlabs.com") {
		t.Error("org_gated should accept an allowlisted domain")
	}
	if h.isGoogleEmailAllowed("outsider@gmail.com") {
		t.Error("org_gated should reject a non-allowlisted domain")
	}
}

func TestIsGoogleEmailAllowed_WithDomainAllowlist(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{
		googleAllowedDomains: map[string]struct{}{
			"memax.app":     {},
			"memaxlabs.com": {},
		},
	}
	if !h.isGoogleEmailAllowed("user@memax.app") {
		t.Error("allowed domain rejected")
	}
	if !h.isGoogleEmailAllowed("USER@MEMAX.APP") {
		t.Error("case-insensitive not working")
	}
	if h.isGoogleEmailAllowed("user@other.com") {
		t.Error("disallowed domain accepted")
	}
	if h.isGoogleEmailAllowed("") {
		t.Error("empty email should not be allowed")
	}
	if h.isGoogleEmailAllowed("bademail") {
		t.Error("email without @ should not be allowed")
	}
	if h.isGoogleEmailAllowed("dangling@") {
		t.Error("trailing @ should not be allowed")
	}
}

func TestIsGoogleEmailAllowed_EmailOverridesDomain(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{
		googleAllowedEmails: map[string]struct{}{
			"vip@other.com": {},
		},
	}
	// An explicit email allowlist lets one user in even without a
	// domain rule.
	if !h.isGoogleEmailAllowed("vip@other.com") {
		t.Error("explicit email allowlist should let the user in")
	}
	if h.isGoogleEmailAllowed("someone-else@other.com") {
		t.Error("non-listed email from same domain should be rejected")
	}
}

// ─── Google OAuth login endpoint ───────────────────────────────

func TestAuthGoogleLogin_ServiceUnavailableWithoutClientID(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{} // no googleClientID
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/login", nil)
	rec := httptest.NewRecorder()
	h.GoogleLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no googleClientID: expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_disabled") {
		t.Errorf("error code missing: %s", rec.Body.String())
	}
}

func TestAuthGoogleCallback_MissingCodeIs400(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{googleClientID: "x"} // need clientID to get past the early guard
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback", nil)
	rec := httptest.NewRecorder()
	h.GoogleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no code: expected 400, got %d", rec.Code)
	}
}
