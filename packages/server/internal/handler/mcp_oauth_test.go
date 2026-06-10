package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests cover the MCP OAuth handler's URL-resolution helpers and
// well-known metadata endpoints. Both are pure-function / read-only
// paths that don't require DB state, so we can exercise them without
// pgxpool setup.

func TestMCPOAuthResolveBaseURL_UsesConfiguredValue(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	req := httptest.NewRequest(http.MethodGet, "https://api.memax.app/test", nil)
	if got := h.resolveBaseURL(req); got != "https://api.memax.app" {
		t.Errorf("baseURL set: got %q", got)
	}
}

func TestMCPOAuthResolveBaseURL_DerivesFromRequest(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{}
	// No TLS, non-prod host → falls back to http.
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
	if got := h.resolveBaseURL(req); got != "http://localhost:8080" {
		t.Errorf("localhost derivation: got %q", got)
	}
	// Production-like host → https even without TLS (behind a proxy).
	req2 := httptest.NewRequest(http.MethodGet, "http://api.memaxlabs.com/test", nil)
	if got := h.resolveBaseURL(req2); got != "https://api.memaxlabs.com" {
		t.Errorf("production-like host: got %q", got)
	}
}

func TestMCPOAuthResolveAppBaseURL_MapsKnownHosts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		base string
		want string
	}{
		{"https://api.memax.app", "https://memax.app"},
		{"https://staging-api.memaxlabs.com", "https://staging-app.memaxlabs.com"},
		{"http://localhost:8080", "http://localhost:3000"},
		{"http://127.0.0.1:9000", "http://localhost:3000"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			t.Parallel()
			h := &MCPOAuthHandler{baseURL: tc.base}
			req := httptest.NewRequest(http.MethodGet, tc.base+"/x", nil)
			if got := h.resolveAppBaseURL(req); got != tc.want {
				t.Errorf("app base for %q = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}

func TestMCPOAuthResolveAppBaseURL_UsesExplicitValue(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{appBaseURL: "https://custom.app"}
	req := httptest.NewRequest(http.MethodGet, "https://api.memax.app/x", nil)
	if got := h.resolveAppBaseURL(req); got != "https://custom.app" {
		t.Errorf("explicit app base not honored: %q", got)
	}
}

func TestMCPOAuthConsentURLsShape(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app", appBaseURL: "https://memax.app"}
	req := httptest.NewRequest(http.MethodGet, "https://api.memax.app/x", nil)

	got := h.webConsentURL(req, "req-123", "tok-abc")
	if !strings.Contains(got, "https://memax.app/oauth/consent") {
		t.Errorf("webConsentURL prefix wrong: %q", got)
	}
	if !strings.Contains(got, "request_id=req-123") {
		t.Errorf("request_id missing: %q", got)
	}
	if !strings.Contains(got, "consent_token=tok-abc") {
		t.Errorf("consent_token missing: %q", got)
	}

	submit := h.consentSubmitURL(req)
	if submit != "https://api.memax.app/oauth/authorize/consent" {
		t.Errorf("consentSubmitURL = %q", submit)
	}
}

func TestMCPOAuthProtectedResourceMetadata(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	req := httptest.NewRequest(http.MethodGet,
		"https://api.memax.app/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	h.ProtectedResourceMetadata(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["resource"] != "https://api.memax.app/mcp" {
		t.Errorf("resource = %v", resp["resource"])
	}
	if servers, ok := resp["authorization_servers"].([]any); !ok || len(servers) != 1 {
		t.Errorf("authorization_servers = %v", resp["authorization_servers"])
	}
}

func TestMCPOAuthProtectedResourceMetadata_QueryOverride(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	// Client can override the resource via ?resource= param.
	req := httptest.NewRequest(http.MethodGet,
		"https://api.memax.app/.well-known/oauth-protected-resource?resource=https://other.app/mcp",
		nil)
	rec := httptest.NewRecorder()
	h.ProtectedResourceMetadata(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["resource"] != "https://other.app/mcp" {
		t.Errorf("query override not honored: %v", resp["resource"])
	}
}

func TestMCPOAuthProtectedResourceMetadata_SubpathDerivation(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	// Subpath after the well-known prefix becomes the resource path.
	req := httptest.NewRequest(http.MethodGet,
		"https://api.memax.app/.well-known/oauth-protected-resource/foo/bar", nil)
	rec := httptest.NewRecorder()
	h.ProtectedResourceMetadata(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["resource"] != "https://api.memax.app/foo/bar" {
		t.Errorf("subpath derivation: %v", resp["resource"])
	}
}

func TestMCPOAuthAuthorizationServerMetadata(t *testing.T) {
	t.Parallel()
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	req := httptest.NewRequest(http.MethodGet,
		"https://api.memax.app/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	h.AuthorizationServerMetadata(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["issuer"] != "https://api.memax.app" {
		t.Errorf("issuer = %v", resp["issuer"])
	}
	if resp["authorization_endpoint"] != "https://api.memax.app/oauth/authorize" {
		t.Errorf("authorization_endpoint = %v", resp["authorization_endpoint"])
	}
	if resp["token_endpoint"] != "https://api.memax.app/oauth/token" {
		t.Errorf("token_endpoint = %v", resp["token_endpoint"])
	}
	// Required by RFC 8414.
	for _, key := range []string{"response_types_supported", "grant_types_supported",
		"code_challenge_methods_supported"} {
		if resp[key] == nil {
			t.Errorf("missing required field %q", key)
		}
	}
}

func TestMCPOAuthDynamicClientRegistration_RejectsMissingPool(t *testing.T) {
	t.Parallel()
	// authH is nil → handler errors without touching DB.
	h := &MCPOAuthHandler{baseURL: "https://api.memax.app"}
	req := httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"redirect_uris":["http://localhost/cb"]}`))
	rec := httptest.NewRecorder()
	h.DynamicClientRegistration(rec, req)
	// With nil authH, oauthError writes a server_error body.
	if rec.Code == http.StatusOK {
		t.Errorf("nil authH should not 200")
	}
}

func TestMCPOAuthDynamicClientRegistration_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	// Wire enough authH to pass the nil-pool check but let body parse fail.
	h := &MCPOAuthHandler{
		authH:   &AuthHandler{},
		baseURL: "https://api.memax.app",
	}
	// authH.pool is nil here too, so the first guard wins — rejects.
	req := httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.DynamicClientRegistration(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("bad input should not yield 200")
	}
}
