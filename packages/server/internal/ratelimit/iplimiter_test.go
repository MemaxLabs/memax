package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// withTrustedProxy swaps the package-level trust mode for the
// duration of a test, restoring it on cleanup. Needed because trust
// mode is selected once at package init from an env var; tests must
// cover all three modes without restarting the process.
func withTrustedProxy(t *testing.T, mode trustedProxyMode) {
	t.Helper()
	prev := trustedProxy
	trustedProxy = mode
	t.Cleanup(func() { trustedProxy = prev })
}

// --- extractClientIP: "none" mode (default, safe) ----------------------

func TestExtractClientIP_NoneMode_IgnoresHeaders(t *testing.T) {
	// Default mode: only RemoteAddr is trusted. A malicious client
	// sending ANY proxy header can't forge identity because we don't
	// even read them.
	withTrustedProxy(t, trustedProxyNone)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "203.0.113.7")    // forgery attempt
	r.Header.Set("CF-Connecting-IP", "203.0.113.7") // forgery attempt
	r.Header.Set("X-Forwarded-For", "198.51.100.1") // forgery attempt
	r.RemoteAddr = "192.0.2.9:1234"
	if got := extractClientIP(r); got != "192.0.2.9" {
		t.Errorf("expected RemoteAddr in 'none' mode (headers must be ignored), got %q", got)
	}
}

// --- extractClientIP: "cloudflare" mode --------------------------------

func TestExtractClientIP_CloudflareMode_CFHeaderWins(t *testing.T) {
	withTrustedProxy(t, trustedProxyCloudflare)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	// In the CF → Fly → app topology these are all Cloudflare/Fly
	// edge IPs, NOT the real client; CF-Connecting-IP is the only
	// header that carries the real client.
	r.Header.Set("Fly-Client-IP", "198.51.100.1")
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	r.RemoteAddr = "127.0.0.1:1234"
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected CF-Connecting-IP to win in 'cloudflare' mode, got %q", got)
	}
}

func TestExtractClientIP_CloudflareMode_FallsBackToXFF(t *testing.T) {
	// If CF-Connecting-IP is somehow absent (CF misconfigured or
	// request originated from inside Fly's private network), fall
	// back to XFF leftmost — which Cloudflare populates with the
	// real client before Fly's edge appends.
	withTrustedProxy(t, trustedProxyCloudflare)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.1")
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected XFF fallback in 'cloudflare' mode, got %q", got)
	}
}

func TestExtractClientIP_CloudflareMode_IgnoresFlyHeader(t *testing.T) {
	// In CF topology Fly-Client-IP is Cloudflare's edge — using it
	// would collapse every real client into one bucket. Explicit
	// test that cloudflare mode does NOT consult it.
	withTrustedProxy(t, trustedProxyCloudflare)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "203.0.113.7") // forgery/Cloudflare-edge attempt
	r.RemoteAddr = "192.0.2.9:1234"
	if got := extractClientIP(r); got != "192.0.2.9" {
		t.Errorf("expected Fly-Client-IP to be ignored in 'cloudflare' mode, got %q", got)
	}
}

// --- extractClientIP: "fly" mode ---------------------------------------

func TestExtractClientIP_FlyMode_FlyHeaderWins(t *testing.T) {
	withTrustedProxy(t, trustedProxyFly)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "203.0.113.7")
	r.Header.Set("X-Forwarded-For", "198.51.100.1") // ignored in fly mode
	r.RemoteAddr = "127.0.0.1:1234"
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected Fly-Client-IP to win in 'fly' mode, got %q", got)
	}
}

func TestExtractClientIP_FlyMode_FallsBackToXFF(t *testing.T) {
	// If Fly-Client-IP is absent, fall back to leftmost XFF — both
	// are set by Fly's edge in the same trust tier.
	withTrustedProxy(t, trustedProxyFly)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.1")
	r.RemoteAddr = "127.0.0.1:1234"
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected XFF fallback in 'fly' mode, got %q", got)
	}
}

// --- extractClientIP: "xff" mode ---------------------------------------

func TestExtractClientIP_XFFMode_LeftmostWins(t *testing.T) {
	withTrustedProxy(t, trustedProxyXFF)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "10.0.0.1") // ignored in xff mode
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.1, 10.0.0.1")
	r.RemoteAddr = "127.0.0.1:1234"
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected leftmost XFF in 'xff' mode, got %q", got)
	}
}

func TestExtractClientIP_XFFMode_WhitespaceTrimmed(t *testing.T) {
	withTrustedProxy(t, trustedProxyXFF)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Forwarded-For", "  203.0.113.7  , 198.51.100.1")
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected trimmed XFF, got %q", got)
	}
}

// --- extractClientIP: RemoteAddr fallback across modes -----------------

func TestExtractClientIP_RemoteAddrFallback(t *testing.T) {
	// Any mode, no headers set → use RemoteAddr host with port stripped.
	for _, mode := range []trustedProxyMode{trustedProxyNone, trustedProxyCloudflare, trustedProxyFly, trustedProxyXFF} {
		t.Run(string(mode), func(t *testing.T) {
			withTrustedProxy(t, mode)
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			r.RemoteAddr = "203.0.113.7:54321"
			if got := extractClientIP(r); got != "203.0.113.7" {
				t.Errorf("mode %s: expected RemoteAddr host, got %q", mode, got)
			}
		})
	}
}

func TestExtractClientIP_IPv6WithBrackets(t *testing.T) {
	// Fly mode + IPv6 literal with brackets in the header.
	withTrustedProxy(t, trustedProxyFly)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "[2001:db8::1]")
	if got := extractClientIP(r); got != "2001:db8::1" {
		t.Errorf("expected IPv6 unwrapped, got %q", got)
	}
}

func TestExtractClientIP_IPv6RemoteAddrFallback(t *testing.T) {
	withTrustedProxy(t, trustedProxyNone)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "[2001:db8::1]:54321"
	if got := extractClientIP(r); got != "2001:db8::1" {
		t.Errorf("expected IPv6 via RemoteAddr, got %q", got)
	}
}

func TestExtractClientIP_InvalidValuesReturnEmpty(t *testing.T) {
	// Garbage in every slot → "" so middleware can choose fail-open.
	withTrustedProxy(t, trustedProxyFly)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "not-an-ip")
	r.Header.Set("X-Forwarded-For", "also-garbage")
	r.RemoteAddr = "not-an-addr"
	if got := extractClientIP(r); got != "" {
		t.Errorf("expected empty for unparseable, got %q", got)
	}
}

func TestExtractClientIP_EmptyHeaders_FallsThrough(t *testing.T) {
	// Headers present but blank — treat as absent and fall through
	// to RemoteAddr. Matches what an intermediate proxy might set when
	// it can't determine the real client.
	withTrustedProxy(t, trustedProxyFly)
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Fly-Client-IP", "")
	r.Header.Set("X-Forwarded-For", "")
	r.RemoteAddr = "203.0.113.7:1234"
	if got := extractClientIP(r); got != "203.0.113.7" {
		t.Errorf("expected RemoteAddr fallback when headers are blank, got %q", got)
	}
}

// --- normalizeIP --------------------------------------------------------

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"203.0.113.7", "203.0.113.7"},
		{"  203.0.113.7  ", "203.0.113.7"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"", ""},
		{"not-an-ip", ""},
		{"203.0.113.7.99", ""},            // five octets
		{"[::invalid]", ""},               // malformed IPv6
		{"::ffff:192.0.2.1", "192.0.2.1"}, // IPv4-mapped IPv6 collapses to canonical v4
	}
	for _, tt := range tests {
		if got := normalizeIP(tt.in); got != tt.want {
			t.Errorf("normalizeIP(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- WrapIP pass-through behaviour --------------------------------------

func TestWrapIP_NilRedis_PassesThrough(t *testing.T) {
	// With nil Redis, Check fail-opens; WrapIP should delegate to the
	// inner handler every time. Key property: no 429 ever returned
	// when the limiter is in degraded mode, matching the global
	// safety-valve contract.
	l := New(nil)
	called := 0
	h := l.WrapIP(EndpointLimit{Name: "test", RPM: 1}, func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("Fly-Client-IP", "203.0.113.7")
		rec := httptest.NewRecorder()
		h(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200 (nil-Redis fail-open), got %d", i, rec.Code)
		}
	}
	if called != 5 {
		t.Errorf("inner handler should run every time in degraded mode, got %d calls", called)
	}
}

func TestWrapIP_NoExtractableIP_PassesThrough(t *testing.T) {
	// When extractClientIP returns "" the wrapper fails open rather
	// than denying — WrapIP's docstring rationale is that denying
	// here would reject legit clients behind an unexpected proxy
	// topology. The right place to be strict is the load balancer.
	l := New(nil)
	called := 0
	h := l.WrapIP(EndpointLimit{Name: "test", RPM: 1}, func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "not-an-addr"
	rec := httptest.NewRecorder()
	h(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 when IP can't be extracted, got %d", rec.Code)
	}
	if called != 1 {
		t.Errorf("inner handler should have run, got %d calls", called)
	}
}
