package ratelimit

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
)

// EndpointLimit defines the per-IP budget for a single unauthenticated
// endpoint. The Name is used as a Redis key segment so different
// endpoints don't share a bucket — a client doing OAuth DCR spam
// shouldn't also burn the login/token budget.
type EndpointLimit struct {
	// Name is a short slug used in the Redis key (e.g. "login", "token").
	// Kept distinct from the URL path so renaming the URL doesn't
	// invalidate in-flight buckets across a deploy.
	Name string
	// RPM is the sustained requests-per-minute budget for a single IP.
	// Conservative defaults live in the package as named vars so the
	// numbers are reviewable alongside the handlers they protect.
	RPM int
}

// Preset budgets for the unauthenticated endpoints we care about. These
// are conservative; legitimate users won't hit them, but brute-force and
// spam campaigns will.
//
// Token endpoints (refresh/exchange) are higher than login because
// active sessions legitimately refresh frequently from background
// reconnects; login is the brute-force target.
var (
	IPLoginLimit      = EndpointLimit{Name: "login", RPM: 10}       // OAuth start (GitHub/Google)
	IPCallbackLimit   = EndpointLimit{Name: "callback", RPM: 30}    // OAuth callback — lenient; IdP redirects arrive in bursts when tabs reopen
	IPTokenLimit      = EndpointLimit{Name: "token", RPM: 30}       // refresh / exchange
	IPImpersonate     = EndpointLimit{Name: "impersonate", RPM: 20} // admin-only, but still IP-bounded for safety
	IPOAuthDCRLimit   = EndpointLimit{Name: "oauth_dcr", RPM: 5}    // MCP dynamic client registration — very restrictive
	IPOAuthAuthorize  = EndpointLimit{Name: "oauth_auth", RPM: 30}  // /oauth/authorize + /oauth/authorize/consent
	IPOAuthTokenLimit = EndpointLimit{Name: "oauth_token", RPM: 30} // /oauth/token
	IPInviteLimit     = EndpointLimit{Name: "invite", RPM: 10}      // invite token validation — brute-force enumeration
	IPWaitlistLimit   = EndpointLimit{Name: "waitlist", RPM: 5}     // /v1/waitlist* — may enqueue email; tight by design
	// IPUnsubscribe — public /v1/unsubscribe endpoint. Token-in-query is
	// the auth; a brute-force floor prevents token enumeration while
	// still allowing a mail client's one-click POST + redirect GET +
	// possible pre-fetch, which can briefly triple-hit from one IP.
	IPUnsubscribe = EndpointLimit{Name: "unsubscribe", RPM: 30}
	// IPWebhookResend — provider delivery webhooks. Resend fan-out on a
	// broadcast campaign can burst well over 100 req/s from a small
	// pool of egress IPs, so the cap is sized to accommodate legitimate
	// traffic rather than throttle it. The real abuse surface here is
	// invalid-signature floods, which the handler verifies in-process
	// (constant-time compare) before doing any store work. 6000 RPM =
	// 100 req/s per source IP.
	IPWebhookResend = EndpointLimit{Name: "webhook_resend", RPM: 6000}
	// IPEmailOTPRequest — /v1/auth/email/request triggers an outbound
	// email and writes a DB row; tighter than login to make
	// inbox-bomb / enumeration campaigns cost prohibitive. Per-email
	// throttling lives in the handler on top of this per-IP cap.
	IPEmailOTPRequest = EndpointLimit{Name: "email_otp_request", RPM: 5}
	// IPEmailOTPVerify — /v1/auth/email/verify is the brute-force
	// surface for a known code. Per-code attempt count is the primary
	// defense; this IP cap is the floor.
	IPEmailOTPVerify = EndpointLimit{Name: "email_otp_verify", RPM: 20}
)

// trustedProxyMode controls how extractClientIP derives the client IP.
// Selected once at startup from the TRUSTED_PROXY env var; never flips
// at runtime because a misconfigured flip would swap the trust boundary
// under live traffic.
//
//   - "none" (default): use RemoteAddr only. Ignores all proxy headers.
//     Correct for local dev, direct connections, and any deploy where
//     the operator hasn't opted in. A malicious client cannot spoof the
//     IP because we don't read headers at all.
//   - "cloudflare" (or "cf"): trust CF-Connecting-IP first — Cloudflare
//     always writes this header with the real client IP and overwrites
//     anything the client supplies. Falls back to leftmost XFF.
//     Use ONLY when Cloudflare is the outermost proxy AND origin lockdown
//     (Authenticated Origin Pulls or a shared-secret header) ensures
//     non-CF clients cannot reach the server. Without origin lockdown,
//     a direct attacker forges CF-Connecting-IP.
//   - "fly": trust Fly-Client-IP, then fall back to X-Forwarded-For
//     leftmost. Use ONLY when Fly's edge is the outermost proxy. If
//     another proxy (Cloudflare, a WAF) sits in front of Fly,
//     Fly-Client-IP is that proxy's IP, not the end user's — so every
//     user behind the outer proxy collapses into one bucket. In that
//     topology use "cloudflare" or "xff" instead.
//   - "xff": trust the LEFTMOST X-Forwarded-For value. Works for any
//     reverse-proxy chain that appends to XFF. Still spoofable by
//     clients that speak directly to the server, so only enable when a
//     trusted proxy sits in front (and when that proxy overwrites, not
//     passes through, client-supplied XFF).
//
// The safe default is "none". An explicit opt-in is required to trust
// headers. Mismatches (e.g. "fly" when Cloudflare is in front) are
// worse than the safe default because they let clients forge identity
// or collapse all users into one bucket.
type trustedProxyMode string

const (
	trustedProxyNone       trustedProxyMode = "none"
	trustedProxyCloudflare trustedProxyMode = "cloudflare"
	trustedProxyFly        trustedProxyMode = "fly"
	trustedProxyXFF        trustedProxyMode = "xff"
)

// TrustedProxyMode returns the currently configured trust mode as a
// human-readable string ("none", "cloudflare", "fly", or "xff"). Used
// by the admin config panel so operators can verify which header
// source the rate limiter is trusting — catches "we flipped
// TRUSTED_PROXY but forgot to deploy" type drift.
func TrustedProxyMode() string { return string(trustedProxy) }

var trustedProxy = func() trustedProxyMode {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("TRUSTED_PROXY")))
	switch raw {
	case "cloudflare", "cf":
		return trustedProxyCloudflare
	case "fly":
		return trustedProxyFly
	case "xff", "x-forwarded-for":
		return trustedProxyXFF
	case "", "none":
		return trustedProxyNone
	default:
		slog.Warn("ratelimit: unknown TRUSTED_PROXY, defaulting to 'none'",
			"raw", raw, "valid", "none|cloudflare|fly|xff")
		return trustedProxyNone
	}
}()

// WrapIP returns an http.HandlerFunc that enforces limit per client IP
// before delegating to h. If Redis is unreachable, the underlying
// Limiter fail-opens (see Check) — matching the global safety-valve
// policy of never wedging the service on metering outages.
//
// When the request has no extractable IP (malformed RemoteAddr, no
// Fly/XFF headers, and a dev setup behind some weird proxy), we also
// fail open. Dropping here would deny legitimate requests on any
// corner case the extractor hasn't been taught about; the right place
// to add defensive rejection is the outer load balancer.
func (l *Limiter) WrapIP(limit EndpointLimit, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)
		if ip == "" {
			h(w, r)
			return
		}
		key := fmt.Sprintf("ip:%s:%s", limit.Name, ip)
		result := l.Check(r.Context(), key, limit.RPM)
		setRateLimitHeaders(w, result)
		if !result.Allowed {
			writeRateLimited(w, result)
			return
		}
		h(w, r)
	}
}

// extractClientIP returns the canonical client IP for rate-limit
// keying. The trust source is chosen by the TRUSTED_PROXY env var at
// startup (see trustedProxyMode).
//
// In "none" mode (default) we use RemoteAddr only — no header reads at
// all, so clients can't spoof the IP even if they fabricate Fly-Client-IP
// or X-Forwarded-For. This is deliberately conservative: operators
// must opt in to header-based trust.
//
// In "fly" or "xff" modes we derive from the selected header first and
// fall back to RemoteAddr only if the header is absent or unparseable.
// Returns "" when nothing is extractable; the middleware treats "" as
// fail-open (see WrapIP for rationale).
func extractClientIP(r *http.Request) string {
	switch trustedProxy {
	case trustedProxyCloudflare:
		if ip := normalizeIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if ip := firstXFF(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
	case trustedProxyFly:
		if ip := normalizeIP(r.Header.Get("Fly-Client-IP")); ip != "" {
			return ip
		}
		if ip := firstXFF(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
	case trustedProxyXFF:
		if ip := firstXFF(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
	case trustedProxyNone:
		// No header reads — RemoteAddr only. A malicious client
		// cannot forge RemoteAddr because it's the TCP peer.
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(r.RemoteAddr)
}

// firstXFF returns the normalized leftmost value of an X-Forwarded-For
// header. Leftmost is the original client IP per RFC 7239 when proxies
// only append (which is the universal convention for well-behaved
// reverse proxies). Returns "" if empty or unparseable.
func firstXFF(xff string) string {
	if xff == "" {
		return ""
	}
	if idx := strings.IndexByte(xff, ','); idx >= 0 {
		xff = xff[:idx]
	}
	return normalizeIP(strings.TrimSpace(xff))
}

// normalizeIP validates and canonicalizes an IP string. Accepts v4 and
// v6 literals with or without IPv6 brackets. Returns "" if unparseable
// so callers can unambiguously distinguish "missing" from "present".
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	// Strip IPv6 brackets: "[::1]" → "::1"
	if len(ip) >= 2 && ip[0] == '[' && ip[len(ip)-1] == ']' {
		ip = ip[1 : len(ip)-1]
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}
	return ""
}
