package handler

import (
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// AdminConfigHandler exposes read-only infra/security status for the
// admin dashboard. Single endpoint today (/v1/admin/config); grows
// only when new status signals prove load-bearing during incidents.
//
// Process-level flags are captured at construction because the
// features they describe (meter, ratelimit, trusted-proxy mode) are
// wired once at startup and don't flip at runtime. Reading from the
// live packages would also reintroduce a handler → ratelimit import
// cycle (ratelimit already imports handler for error helpers).
type AdminConfigHandler struct {
	trustedProxy     string
	meterEnabled     bool
	rateLimitEnabled bool
}

// NewAdminConfigHandler snapshots the flags to report. trustedProxy
// comes from ratelimit.TrustedProxyMode() at the composition root in
// app.go, where ratelimit is already imported.
func NewAdminConfigHandler(trustedProxy string, meterEnabled, rateLimitEnabled bool) *AdminConfigHandler {
	return &AdminConfigHandler{
		trustedProxy:     trustedProxy,
		meterEnabled:     meterEnabled,
		rateLimitEnabled: rateLimitEnabled,
	}
}

// Get returns the current infra/security status:
//
//   - trusted_proxy: the ratelimit trust mode (none|cloudflare|fly|xff),
//     so ops can verify "we flipped to cloudflare" after a deploy.
//   - origin_secret_set: whether ORIGIN_SHARED_SECRET is populated,
//     without leaking its value.
//   - meter_enabled / rate_limit_enabled: snapshots captured at
//     startup; if false, the matching quota path is in degraded mode.
//   - version: main module VCS revision from runtime/debug when the
//     binary was built with module info (the normal Go toolchain does
//     this by default); "unknown" otherwise.
//
// Shape is a flat map so the admin UI can render it as a config panel
// without client-side reshaping.
//
// GET /v1/admin/config
func (h *AdminConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"trusted_proxy":      h.trustedProxy,
		"origin_secret_set":  strings.TrimSpace(os.Getenv("ORIGIN_SHARED_SECRET")) != "",
		"meter_enabled":      h.meterEnabled,
		"rate_limit_enabled": h.rateLimitEnabled,
		"server_time":        nowUTC().Format("2006-01-02T15:04:05Z"),
		// `version` kept as the field name for backward compatibility
		// with existing admin UI code that reads it. Now carries the
		// short commit SHA (prior behavior). `semver` is the new
		// field; UI renders semver as the headline and version as a
		// supplementary commit line.
		"version": buildCommitShort(),
		"semver":  buildSemver(),
	}})
}

// buildCommit is overridden at build time via `-ldflags -X ...` in
// the Dockerfile. CI passes GITHUB_SHA through a --build-arg →
// Dockerfile ARG → ldflags chain so the container binary carries the
// commit even though Fly's build context doesn't include .git (the
// server package is the context; the repo's .git sits at the
// monorepo root, outside the context). An empty value means "not
// injected" and we fall back to Go's auto-VCS stamp.
var buildCommit string

// buildVersion is the semver the binary was built as, injected at
// build time alongside buildCommit. Prod deploys pass the exact
// `vMAJOR.MINOR.PATCH` computed by resolve-version (guaranteed to
// match the tag the deploy will push). Staging deploys pass
// `git describe --tags --always` output (e.g. `v0.1.10-5-gabc…`,
// still semver-compatible via the pre-release suffix). Local builds
// with no --build-arg get the default `dev`. The admin UI renders
// the semver as the primary Version value and the commit SHA as a
// secondary Commit line — operators scan the semver at a glance
// and can cross-reference the SHA when they need to.

var buildVersion string

// buildSemver returns the semantic version string the binary was
// built as. Empty / "dev" fallback means "not injected" — the admin
// UI renders an em-dash in that case.
func buildSemver() string {
	s := strings.TrimSpace(buildVersion)
	if s == "" || s == "dev" {
		return ""
	}
	return s
}

// buildCommitShort returns the short commit SHA the binary was built
// from. Three-way resolution: the explicit ldflags-injected
// `buildCommit` (production path), then Go's auto-detected
// `vcs.revision` (works for local `go build` inside a clean git
// checkout), and finally "unknown" as a visible fallback so the
// admin panel shows the binary exists but can't be correlated.
func buildCommitShort() string {
	if s := strings.TrimSpace(buildCommit); s != "" && s != "unknown" {
		if len(s) >= 12 {
			return s[:12]
		}
		return s
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) >= 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "unknown"
}
