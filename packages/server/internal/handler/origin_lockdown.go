package handler

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// OriginSharedSecretHeader is the header name Cloudflare Transform Rules
// inject with the shared secret. Kept here so both the middleware and
// the runbook reference the same constant.
const OriginSharedSecretHeader = "X-Origin-Secret"

// originSharedSecretAllowlist is the set of paths that bypass the
// origin-secret check. Fly's HTTP health check can't inject custom
// headers, so /health MUST be reachable from the Fly internal network
// without the secret. Keep this list TINY — every entry is an open
// door; prefer adding routes to the secret-checked path.
var originSharedSecretAllowlist = map[string]struct{}{
	"/health":  {},
	"/healthz": {},
}

// OriginSharedSecretMiddleware enforces that every incoming request
// (except the narrow allowlist) carries the configured shared secret
// in the X-Origin-Secret header. When ORIGIN_SHARED_SECRET is unset,
// the middleware is a pure pass-through — this preserves local dev
// and non-locked-down deployments.
//
// The secret lives ONLY in Fly env (set via `fly secrets`) and in a
// Cloudflare Transform Rule that injects the header on proxied
// requests. A direct attacker hitting the Fly app bypasses Cloudflare
// and therefore cannot produce the header — request is rejected with
// 403. See docs/infra/cloudflare-runbook.md for the setup.
//
// Constant-time compare prevents timing oracles on the secret value.
func OriginSharedSecretMiddleware() func(http.Handler) http.Handler {
	expected := strings.TrimSpace(os.Getenv("ORIGIN_SHARED_SECRET"))
	if expected == "" {
		// Not configured — no enforcement. Same contract as every
		// other security layer (rate limit, meter, auth): opt-in.
		return func(next http.Handler) http.Handler { return next }
	}
	expectedBytes := []byte(expected)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, exempt := originSharedSecretAllowlist[r.URL.Path]; exempt {
				next.ServeHTTP(w, r)
				return
			}
			// Comparing bytes directly via subtle.ConstantTimeCompare
			// avoids the early-exit timing leak of `==` on strings of
			// different lengths. The extra allocation is negligible at
			// request scale.
			got := []byte(r.Header.Get(OriginSharedSecretHeader))
			if subtle.ConstantTimeCompare(got, expectedBytes) != 1 {
				WriteError(w, http.StatusForbidden, "origin_not_allowed",
					"This origin is not reachable without the shared-secret header. Go through the public edge.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
