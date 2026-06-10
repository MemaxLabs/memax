package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests cover auth.go's validation lanes. They construct the
// AuthHandler directly with zero-value fields — that's enough to
// exercise every "input rejected before OAuth work starts" branch.
// The full OAuth callback path needs a live DB + GitHub mock, which
// is a larger infrastructure investment.

func TestAuthGitHubLogin_ReturnsServiceUnavailableWithoutClientID(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/github/login", nil)
	rec := httptest.NewRecorder()
	h.GitHubLogin(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no clientID: expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth_disabled") {
		t.Errorf("error code missing: %s", rec.Body.String())
	}
}

func TestAuthGitHubCallback_MissingCodeIs400(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback", nil)
	rec := httptest.NewRecorder()
	h.GitHubCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no code: expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_code") {
		t.Errorf("error code missing: %s", rec.Body.String())
	}
}

func TestAuthRefresh_RejectsMissingCookie(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)
	// Missing refresh cookie → 401 or 400.
	if rec.Code == http.StatusOK {
		t.Errorf("refresh without cookie should not 200")
	}
}

func TestAuthExchangeCode_RejectsMissingCode(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/exchange",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ExchangeCode(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("empty code should not 200")
	}
}

func TestAuthExchangeCode_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/exchange",
		strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.ExchangeCode(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("bad JSON should not 200")
	}
}

func TestAuthSetters_NoPanic(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	h.SetPlanChanger(nil)
	h.SetMCPOAuth(nil)
	h.SetStore(nil)
}

func TestAuthMe_UnauthenticatedDoesNotPanic(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	h.Me(rec, req)
	// Without auth, handler writes some response (401/400); what matters
	// is it doesn't panic.
	if rec.Code == 0 {
		t.Error("Me should write a response even without user")
	}
}

func TestAuthUpdateMe_UnauthenticatedRejects(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodPatch, "/v1/auth/me",
		strings.NewReader(`{"display_name":"x"}`))
	rec := httptest.NewRecorder()
	h.UpdateMe(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("unauthenticated UpdateMe should not 200")
	}
}

func TestAuthImpersonate_RejectsEmptyBody(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/impersonate",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Impersonate(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("empty impersonate body should not 200")
	}
}

func TestAuthRevokeAPIKey_UnauthenticatedRejects(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/api-keys/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()
	h.RevokeAPIKey(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("unauthenticated revoke should not 200")
	}
}

func TestAuthListAPIKeys_UnauthenticatedDoesNotPanic(t *testing.T) {
	t.Parallel()
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/api-keys", nil)
	rec := httptest.NewRecorder()
	h.ListAPIKeys(rec, req)
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}
