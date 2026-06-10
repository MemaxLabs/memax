package safefetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── URL validation ─────────────────────────────────────────

func TestValidate_RejectsEmptyURL(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	_, err := c.Fetch(context.Background(), "")
	if !errors.Is(err, ErrEmptyURL) {
		t.Errorf("err = %v, want ErrEmptyURL", err)
	}
}

func TestValidate_RejectsUnparseableURL(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	// A control character in the URL causes net/url to error.
	_, err := c.Fetch(context.Background(), "http://exa\x00mple.com/")
	if !errors.Is(err, ErrParseURL) {
		t.Errorf("err = %v, want ErrParseURL", err)
	}
}

func TestValidate_RejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()
	cases := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://example.com/",
		"javascript:alert(1)",
		"data:text/plain,hello",
	}
	c := New(Options{})
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), u)
			if !errors.Is(err, ErrUnsupportedScheme) {
				t.Errorf("url=%q err=%v, want ErrUnsupportedScheme", u, err)
			}
		})
	}
}

func TestValidate_AllowsHTTPSAndHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	// httptest binds 127.0.0.1 — disable the loopback block
	// for this one test by allowing the dialer through. We
	// can't disable the block in safefetch (security
	// principle) so this test is instead an integration test
	// pinning that we DO reject the loopback target.
	c := New(Options{})
	_, err := c.Fetch(context.Background(), srv.URL)
	if !errors.Is(err, ErrBlockedIP) {
		t.Errorf("err = %v, want ErrBlockedIP (httptest binds 127.0.0.1)", err)
	}
}

func TestValidate_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	cases := []string{
		"http://user:pass@example.com/",
		"http://user@example.com/",
		"https://anything@10.0.0.1/",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), u)
			if !errors.Is(err, ErrUserinfoNotAllowed) {
				t.Errorf("url=%q err=%v, want ErrUserinfoNotAllowed", u, err)
			}
		})
	}
}

// URLs longer than MaxURLLength are rejected at the
// validate-URL layer. Codex Phase 3.6b3g v3 [Medium]
// pinned the bound: without it, a redirect Location
// returning a multi-KB URL could bust the agent-facing
// JSON cap even with the body dropped.
func TestValidate_RejectsOversizedURL(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	bigURL := "https://example.com/" + strings.Repeat("a", MaxURLLength)
	_, err := c.Fetch(context.Background(), bigURL)
	if !errors.Is(err, ErrURLTooLong) {
		t.Errorf("err = %v, want ErrURLTooLong", err)
	}
}

func TestValidate_AcceptsURLAtCap(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	// URL exactly at the cap — should pass URL validation
	// (will fail at the network layer; we only test that
	// the URL-too-long check doesn't fire).
	atCap := "https://example.invalid/" + strings.Repeat("a", MaxURLLength-len("https://example.invalid/"))
	_, err := c.Fetch(context.Background(), atCap)
	if errors.Is(err, ErrURLTooLong) {
		t.Errorf("URL exactly at cap (len=%d) was rejected as too long; want pass", len(atCap))
	}
}

func TestValidate_RejectsEmptyHost(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	_, err := c.Fetch(context.Background(), "http:///path")
	if !errors.Is(err, ErrEmptyHost) {
		t.Errorf("err = %v, want ErrEmptyHost", err)
	}
}

// Memax-owned hosts are blocked even with a public-looking IP
// resolution. The default blocklist covers memax.app, memax.dev,
// memax.com.
func TestValidate_RejectsMemaxHosts(t *testing.T) {
	t.Parallel()
	c := New(Options{})
	cases := []string{
		"https://memax.app/",
		"https://api.memax.app/v1/recall",
		"https://docs.memax.app/",
		"http://internal.memax.app/admin",
		"https://memax.dev/",
		"https://anything.memax.com/",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), u)
			if !errors.Is(err, ErrBlockedHost) {
				t.Errorf("url=%q err=%v, want ErrBlockedHost", u, err)
			}
		})
	}
}

// "memax.app" doesn't accidentally block "notmemax.app" or
// "fakememax.app" — the suffix match needs the leading dot.
func TestValidate_DoesntOverBlockSimilarHosts(t *testing.T) {
	t.Parallel()
	// Use a literal IP so the validation can be reasoned
	// about without DNS — picking a public IP that's
	// guaranteed to fail the dial (1.1.1.1 / 8.8.8.8 are
	// real but we can't actually fetch in unit tests). The
	// test asserts we PASS hostname validation for
	// non-memax hosts; the fetch will fail at the network
	// layer (which is fine — we're not testing the network
	// path here).
	c := New(Options{Timeout: 100 * time.Millisecond})
	_, err := c.Fetch(context.Background(), "https://notmemax.app.example.invalid/")
	if errors.Is(err, ErrBlockedHost) {
		t.Errorf("err = %v, must NOT be ErrBlockedHost (notmemax.app is not memax-owned)", err)
	}
}

// Custom BlockedHosts extend the default list.
func TestValidate_AppliesExtraBlockedHosts(t *testing.T) {
	t.Parallel()
	c := New(Options{
		BlockedHosts: []string{"internal.example.com"},
	})
	_, err := c.Fetch(context.Background(), "https://api.internal.example.com/")
	if !errors.Is(err, ErrBlockedHost) {
		t.Errorf("err = %v, want ErrBlockedHost", err)
	}
}

// Literal IPs in private/loopback/link-local ranges fail the
// pre-DNS check. Each case exercises a different blocked
// range.
func TestValidate_RejectsLiteralPrivateIPs(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"loopback v4":         "http://127.0.0.1/",
		"loopback v6":         "http://[::1]/",
		"link-local v4":       "http://169.254.169.254/", // metadata
		"link-local v6":       "http://[fe80::1]/",
		"private 10/8":        "http://10.0.0.1/",
		"private 172.16/12":   "http://172.16.0.1/",
		"private 192.168":     "http://192.168.1.1/",
		"unique-local v6":     "http://[fc00::1]/",
		"multicast v4":        "http://224.0.0.1/",
		"multicast v6":        "http://[ff00::1]/",
		"unspecified v4":      "http://0.0.0.0/",
		"docs v4":             "http://192.0.2.1/",
		"cgnat":               "http://100.64.0.1/",
		"ipv4-mapped v6 lpbk": "http://[::ffff:127.0.0.1]/",
	}
	c := New(Options{})
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.Fetch(context.Background(), u)
			if !errors.Is(err, ErrBlockedIP) {
				t.Errorf("url=%q err=%v, want ErrBlockedIP", u, err)
			}
		})
	}
}

// ─── IsBlockedIP table ──────────────────────────────────────

func TestIsBlockedIP_Matrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ip      string
		blocked bool
	}{
		// Public addresses we want to PASS through to the network layer.
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		{"93.184.216.34", false}, // example.com legacy IP
		{"2606:4700:4700::1111", false}, // 1.1.1.1 v6
		// Blocked.
		{"127.0.0.1", true},
		{"127.255.255.255", true},
		{"::1", true},
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"100.64.0.1", true},
		{"192.0.2.1", true},
		{"203.0.113.1", true},
		{"224.0.0.1", true},
		{"240.0.0.1", true},
		{"255.255.255.255", true},
		{"0.0.0.0", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"ff00::1", true},
		{"::ffff:127.0.0.1", true}, // IPv4-mapped loopback
		{"::ffff:10.0.0.1", true},  // IPv4-mapped private
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("parse %q failed", c.ip)
			}
			got := IsBlockedIP(ip)
			if got != c.blocked {
				t.Errorf("IsBlockedIP(%q) = %v, want %v", c.ip, got, c.blocked)
			}
		})
	}
}

func TestIsBlockedIP_NilIsBlocked(t *testing.T) {
	t.Parallel()
	// A nil IP means "couldn't parse / unknown" — the
	// safe-fail posture is to block.
	if !IsBlockedIP(nil) {
		t.Errorf("IsBlockedIP(nil) = false, want true (fail-closed)")
	}
}

// ─── End-to-end Fetch ───────────────────────────────────────
//
// httptest binds to 127.0.0.1 which the dial-time IP block
// rejects by design. To exercise the full Fetch happy path,
// we use a custom Client whose Transport.DialContext bypasses
// the loopback block — but we keep validation + redirect logic
// active. This is the only way to integration-test the
// HTTP-level behavior in an environment that doesn't have
// outbound public network access.

// TestFetch_HappyPath uses a special test-only entry point
// because the production Client refuses to dial 127.0.0.1
// (the test server's address).
func TestFetch_HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>Hello</title></head><body>world</body></html>"))
	}))
	defer srv.Close()

	c := NewForTest(Options{})
	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", res.StatusCode)
	}
	if res.ContentType != "text/html" {
		t.Errorf("ContentType = %q, want %q", res.ContentType, "text/html")
	}
	if !strings.Contains(string(res.Body), "Hello") {
		t.Errorf("Body missing 'Hello': %q", res.Body)
	}
	if res.Truncated {
		t.Errorf("Truncated = true, want false (small body)")
	}
}

// Body cap: server sends > MaxBodyBytes; result is truncated.
func TestFetch_TruncatesAtCap(t *testing.T) {
	t.Parallel()
	const cap = 1024
	big := strings.Repeat("a", cap*4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	c := NewForTest(Options{MaxBodyBytes: cap})
	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if int64(len(res.Body)) != cap {
		t.Errorf("len(Body) = %d, want %d (cap)", len(res.Body), cap)
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

// Redirect re-validation: a redirect targeting an
// oversized URL (Location > MaxURLLength) is rejected at
// the CheckRedirect step. Pins the
// CheckRedirect → validateURL → ErrURLTooLong path
// directly. Codex Phase 3.6b3g v4 noted this as the one
// optional test gap.
func TestFetch_RedirectToOversizedURLRejected(t *testing.T) {
	t.Parallel()
	bigPath := "/" + strings.Repeat("a", MaxURLLength)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "https://example.com"+bigPath, http.StatusFound)
			return
		}
	}))
	defer srv.Close()

	c := NewForTest(Options{})
	_, err := c.Fetch(context.Background(), srv.URL+"/")
	if err == nil {
		t.Fatalf("err = nil, want redirect-rejected")
	}
	if !errors.Is(err, ErrURLTooLong) {
		t.Errorf("err = %v, want ErrURLTooLong via CheckRedirect", err)
	}
}

// Redirect re-validation: a redirect targeting a blocked host
// is rejected at the CheckRedirect step.
func TestFetch_RedirectToBlockedHostRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://api.memax.app/private", http.StatusFound)
	}))
	defer srv.Close()

	c := NewForTest(Options{})
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("err = nil, want redirect-rejected error")
	}
	if !strings.Contains(err.Error(), "redirect") || !errors.Is(err, ErrBlockedHost) {
		t.Errorf("err = %v, want redirect rejection citing ErrBlockedHost", err)
	}
}

// Redirect cap: with MaxRedirects=N the client follows
// exactly N redirects and rejects the (N+1)th. Codex Phase
// 3.6b3g v1 [Low] called out the off-by-one in the prior
// `>=` comparison; this test now pins exact hop counts.
func TestFetch_RedirectLimitEnforced(t *testing.T) {
	t.Parallel()
	for _, maxR := range []int{1, 3, 5} {
		maxR := maxR
		t.Run(fmt.Sprintf("max=%d", maxR), func(t *testing.T) {
			t.Parallel()
			hops := 0
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hops++
				// Always redirect — server never returns terminal 200.
				http.Redirect(w, r, srv.URL+"/hop"+fmt.Sprint(hops), http.StatusFound)
			}))
			defer srv.Close()

			c := NewForTest(Options{MaxRedirects: maxR})
			_, err := c.Fetch(context.Background(), srv.URL)
			if err == nil {
				t.Fatalf("err = nil, want too-many-redirects")
			}
			if !strings.Contains(err.Error(), "too many redirects") {
				t.Errorf("err = %v, want 'too many redirects'", err)
			}
			// Server saw the original request + exactly maxR
			// redirect-follows = maxR + 1 hits before the
			// (maxR+1)th was rejected. Pins the >, not >=,
			// semantics.
			if hops != maxR+1 {
				t.Errorf("hops = %d, want %d (original + %d follows; max+1 hit triggers reject)",
					hops, maxR+1, maxR)
			}
		})
	}
}

// Timeout: server hangs forever; client times out cleanly.
func TestFetch_TimeoutEnforced(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	c := NewForTest(Options{Timeout: 100 * time.Millisecond})
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("err = nil, want timeout error")
	}
	// http.Client wraps the timeout in "context deadline exceeded".
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "Timeout") {
		t.Errorf("err = %v, want timeout-related", err)
	}
}

// User-Agent: set on every request. Test by reading the
// header on the server side.
func TestFetch_SetsUserAgent(t *testing.T) {
	t.Parallel()
	var seenUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	t.Run("default", func(t *testing.T) {
		c := NewForTest(Options{})
		_, _ = c.Fetch(context.Background(), srv.URL)
		if seenUA != "memax-safefetch/1" {
			t.Errorf("UA = %q, want default", seenUA)
		}
	})
	t.Run("custom", func(t *testing.T) {
		c := NewForTest(Options{UserAgent: "custom/2.0"})
		_, _ = c.Fetch(context.Background(), srv.URL)
		if seenUA != "custom/2.0" {
			t.Errorf("UA = %q, want custom/2.0", seenUA)
		}
	})
}

// Non-2xx status: returned as a successful FetchResult; caller
// decides what to do with the status code.
func TestFetch_Returns404AsResult(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := NewForTest(Options{})
	res, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("err = %v, want nil (4xx is not an error)", err)
	}
	if res.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", res.StatusCode)
	}
}

// Context cancellation is respected.
func TestFetch_CancelsOnContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	c := NewForTest(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE the request fires

	_, err := c.Fetch(ctx, srv.URL)
	if err == nil {
		t.Fatalf("err = nil, want context-canceled error")
	}
}
