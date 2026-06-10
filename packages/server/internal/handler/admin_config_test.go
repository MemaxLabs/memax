package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestAdminConfig_Get_ShapeAndFlags(t *testing.T) {
	// Origin secret is read from env at request time (not snapshot) so
	// we can assert the bool tracks the env without restarting.
	t.Setenv("ORIGIN_SHARED_SECRET", "")

	h := NewAdminConfigHandler("fly", true, true)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/config", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}

	if data["trusted_proxy"] != "fly" {
		t.Errorf("trusted_proxy = %v, want fly", data["trusted_proxy"])
	}
	if data["origin_secret_set"] != false {
		t.Errorf("origin_secret_set = %v, want false (env unset)", data["origin_secret_set"])
	}
	if data["meter_enabled"] != true {
		t.Errorf("meter_enabled = %v, want true", data["meter_enabled"])
	}
	if data["rate_limit_enabled"] != true {
		t.Errorf("rate_limit_enabled = %v, want true", data["rate_limit_enabled"])
	}
	if _, ok := data["server_time"].(string); !ok {
		t.Errorf("server_time missing or not a string: %v", data["server_time"])
	}
	if _, ok := data["version"].(string); !ok {
		t.Errorf("version missing or not a string: %v", data["version"])
	}
	// The `semver` field was added after `version`. Locking in the
	// API contract here so a handler refactor can't silently drop
	// it — the admin UI reads `data.semver` as the headline value.
	// Type check only; content comes from the ldflags-injected
	// buildVersion var which is already covered by
	// TestBuildSemver_InjectionAndDevFallback.
	if _, ok := data["semver"].(string); !ok {
		t.Errorf("semver missing or not a string: %v", data["semver"])
	}
}

func TestBuildVersion_PrefersLdflagsInjected(t *testing.T) {
	// Save and restore the package-level var so this test doesn't
	// bleed into others. The production path sets buildCommit via
	// `-ldflags -X`; tests set it directly.
	orig := buildCommit
	t.Cleanup(func() { buildCommit = orig })

	buildCommit = "abcdef1234567890"
	if got := buildCommitShort(); got != "abcdef123456" {
		t.Errorf("expected 12-char prefix of injected SHA, got %q", got)
	}

	// Empty → falls back to ReadBuildInfo. We can't assert the
	// fallback's exact output in CI (depends on git state), but we
	// can assert it doesn't return the stale value from the
	// previous subtest.
	buildCommit = ""
	got := buildCommitShort()
	if got == "abcdef123456" {
		t.Errorf("expected fallback, got stale injected value %q", got)
	}

	// "unknown" literal should ALSO fall back — Dockerfile default
	// when no --build-arg was passed.
	buildCommit = "unknown"
	fallback := buildCommitShort()
	if fallback == "unknown" && got != "unknown" {
		t.Errorf("unknown literal should hit the same fallback as empty: got=%q fallback=%q", got, fallback)
	}
}

func TestBuildSemver_InjectionAndDevFallback(t *testing.T) {
	orig := buildVersion
	t.Cleanup(func() { buildVersion = orig })

	// Production: the ldflags-injected semver is returned verbatim.
	buildVersion = "v0.1.11"
	if got := buildSemver(); got != "v0.1.11" {
		t.Errorf("expected v0.1.11 passthrough, got %q", got)
	}

	// Staging: git-describe output flows through unchanged. Kept
	// opaque (not parsed) because the UI renders the string as-is
	// and a future latest-version check can do its own parsing.
	buildVersion = "v0.1.10-5-gabcdef"
	if got := buildSemver(); got != "v0.1.10-5-gabcdef" {
		t.Errorf("expected describe-format passthrough, got %q", got)
	}

	// Empty (uninstrumented binary) collapses to "" so the UI can
	// render an em-dash instead of an empty pill.
	buildVersion = ""
	if got := buildSemver(); got != "" {
		t.Errorf("expected empty for uninjected, got %q", got)
	}

	// "dev" literal is the Dockerfile default for local builds.
	// Treated as "not really a version" so the UI doesn't show
	// the literal word dev to operators.
	buildVersion = "dev"
	if got := buildSemver(); got != "" {
		t.Errorf("expected empty for 'dev' literal, got %q", got)
	}
}

func TestAdminConfig_Get_OriginSecretSetTracksEnv(t *testing.T) {
	// Flipping the env between requests should flip the bool, because
	// admin visibility of this flag has to stay truthful — we don't
	// want a stale "not set" after ops rolls the secret.
	h := NewAdminConfigHandler("none", false, false)

	t.Setenv("ORIGIN_SHARED_SECRET", "a-real-secret")
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/config", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	var resp model.ApiResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	data, _ := resp.Data.(map[string]any)
	if data["origin_secret_set"] != true {
		t.Errorf("origin_secret_set = %v, want true when env is set", data["origin_secret_set"])
	}
	if data["trusted_proxy"] != "none" {
		t.Errorf("trusted_proxy snapshot = %v, want none", data["trusted_proxy"])
	}
}
