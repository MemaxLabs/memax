package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// Default limits. Plan 24 §"Tool table" pins these — if the
// chat surface ever wants to tune per-environment, it's an
// Options field, not a global.
const (
	DefaultMaxBodyBytes = 5 * 1024 * 1024 // 5 MB
	DefaultMaxRedirects = 3
	DefaultTimeout      = 10 * time.Second
	DefaultDialTimeout  = 5 * time.Second
)

// Options configures a Client. Zero values fall back to the
// constants above; pass non-zero values to override.
//
// **UserAgent** — sent on every fetch. Defaults to
// "memax-safefetch/1". Plan 24 doesn't pin a value; some hosts
// reject blank UA, others rate-limit unknown UAs harder.
// Setting an explicit identifier helps operator-side debugging
// when a target site sees suspicious traffic from us.
//
// **BlockedHosts** is appended to the package's default
// memax-domain blocklist. A nil slice falls through to the
// defaults; passing additional hosts ADDS to the list rather
// than replacing it.
type Options struct {
	MaxBodyBytes int64
	MaxRedirects int
	Timeout      time.Duration
	DialTimeout  time.Duration
	UserAgent    string
	BlockedHosts []string

	// allowLoopback is the unexported test-mode escape hatch.
	// Only NewForTest sets it; New() always leaves it false.
	// When true, validateURL doesn't reject literal loopback
	// IP hosts (so httptest's 127.0.0.1 URL is reachable) and
	// the dial-time IP check skips loopback. Production code
	// has no path to reach this — calling New() with a non-zero
	// value here is harmless because the field is unexported
	// and external packages can't even SEE it.
	allowLoopback bool
}

func (o Options) withDefaults() Options {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = DefaultMaxBodyBytes
	}
	// MaxRedirects = the number of redirect FOLLOWS permitted.
	// CheckRedirect uses `>` (not `>=`) so MaxRedirects = N
	// follows exactly N redirects and rejects the (N+1)th —
	// matches the plan-24 "≤N redirects" wording.
	//
	// Non-positive values fall back to the default. Callers
	// wanting zero follows don't have a clean shape today; in
	// practice no caller has needed it.
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = DefaultMaxRedirects
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = DefaultDialTimeout
	}
	if o.UserAgent == "" {
		o.UserAgent = "memax-safefetch/1"
	}
	return o
}

// Client is the SSRF-safe HTTP client. Reuse a single Client
// across requests — it holds a configured *http.Client with
// pooled connections.
type Client struct {
	httpClient *http.Client
	opts       Options
}

// New builds a Client with the supplied Options. Safe defaults
// fill in zero-value fields. Returns a non-nil *Client; nil
// callers don't make sense for this package.
func New(opts Options) *Client {
	opts = opts.withDefaults()

	dialer := &net.Dialer{
		Timeout: opts.DialTimeout,
		// ControlContext fires AFTER DNS resolution and BEFORE
		// the syscall connect(). The addr we get is "ip:port"
		// — we parse the IP and reject if it's in a blocked
		// range. This is the line of defense that catches:
		//   - DNS rebinding (hostname's first response was
		//     public, the connection-time response points at
		//     127.0.0.1).
		//   - A-records that always resolve to private IPs
		//     (e.g. example.com → 10.0.0.5 attacks).
		//   - IPv4-mapped IPv6 sneakery (validate also re-maps
		//     to v4 before the CIDR check).
		ControlContext: func(_ context.Context, network, addr string, _ syscall.RawConn) error {
			return validateDialAddr(network, addr, opts.allowLoopback)
		},
	}

	transport := &http.Transport{
		DialContext: dialer.DialContext,
		// Disable HTTP/2 default fallback because the redirect
		// re-validation logic relies on a fresh DialContext per
		// hop — keeping HTTP/1.1 connection pooling makes the
		// validation surface predictable.
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   opts.DialTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		// Disable proxy support — a configured HTTP_PROXY env
		// var would let an outbound request go to an attacker-
		// controlled middlebox that ignores our SSRF rules.
		Proxy: nil,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		// CheckRedirect runs URL-level validation on every hop.
		// The IP-level re-check happens automatically because
		// each redirect opens a new dial (different host),
		// triggering ControlContext. Redirects to the same host
		// reuse a connection and skip the dial — but a
		// same-host redirect doesn't change the IP we already
		// validated, so this is correct.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// MaxRedirects = N means "permit N follow-hops."
			// CheckRedirect is called BEFORE following each
			// redirect; len(via) is the number of follows
			// already done at the point of the check.
			//
			// - 1st redirect: len(via)=1, check 1 > N. With
			//   N=3, allow.
			// - 2nd redirect: len(via)=2, allow.
			// - 3rd redirect: len(via)=3, allow.
			// - 4th redirect: len(via)=4 > 3, reject.
			//
			// So MaxRedirects=3 follows up to 3 redirects, as
			// promised. Codex Phase 3.6b3g v1 [Low] flagged
			// the prior >= which silently allowed N-1.
			if len(via) > opts.MaxRedirects {
				return fmt.Errorf("safefetch: too many redirects (max %d)", opts.MaxRedirects)
			}
			if _, err := validateURL(req.URL.String(), opts.BlockedHosts, opts.allowLoopback); err != nil {
				return fmt.Errorf("safefetch: redirect target rejected: %w", err)
			}
			return nil
		},
	}

	return &Client{httpClient: httpClient, opts: opts}
}

// FetchResult is the read-only view of what a successful Fetch
// returned. Callers parse Body themselves (HTML extraction,
// JSON decoding, etc.); safefetch doesn't impose semantics on
// the bytes.
type FetchResult struct {
	// FinalURL is the URL we ended up at after redirects. Same
	// as the request URL when there were no redirects.
	FinalURL string
	// StatusCode is the HTTP status of the final response.
	// 2xx is the happy path; 3xx never lands here (redirects
	// were followed); 4xx/5xx are returned as a successful
	// FetchResult — the caller decides whether non-2xx is an
	// error in their context.
	StatusCode int
	// ContentType is the Content-Type header value, lowercased
	// + with parameters stripped (e.g. "text/html;charset=utf-8"
	// → "text/html").
	ContentType string
	// Body is the response body, capped at Options.MaxBodyBytes.
	// May be empty if the response had no body. UTF-8 encoding
	// is NOT enforced — callers handling text content are
	// responsible for charset detection.
	Body []byte
	// Truncated is true when the response body exceeded
	// MaxBodyBytes and we stopped reading. Indicates the
	// caller is seeing a partial body.
	Truncated bool
}

// Fetch performs a GET against rawURL with the SSRF protections
// baked in. Returns the response body (capped) and metadata.
//
// Errors fall into three buckets:
//
//  1. URL-level rejection — bad scheme, userinfo, blocked
//     host, blocked literal IP. Returns one of the Err*
//     sentinels from validate.go.
//  2. Network rejection — dial-time IP block, DNS failure,
//     TCP/TLS error, redirect-loop, timeout. Wrapped with
//     "safefetch:" prefix.
//  3. Response-read error — body read fails, server
//     disconnects mid-stream. Wrapped with "safefetch:" prefix.
//     The partial body collected before the failure is NOT
//     returned (the caller can't trust an incomplete read).
func (c *Client) Fetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	if c == nil {
		return nil, errors.New("safefetch: nil Client")
	}
	if _, err := validateURL(rawURL, c.opts.BlockedHosts, c.opts.allowLoopback); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("safefetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "text/html, application/xhtml+xml, application/json, text/plain, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("safefetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	// Strip the optional ";charset=..." or ";boundary=..." parameter
	// so the caller can do exact-string comparisons.
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		contentType = contentType[:idx]
	}
	contentType = strings.TrimSpace(strings.ToLower(contentType))

	// Read up to MaxBodyBytes+1 to detect truncation. The +1
	// trick: if we read exactly MaxBodyBytes+1 bytes, we know
	// the server had more to send. If we read <= MaxBodyBytes,
	// we have the full body.
	limit := c.opts.MaxBodyBytes
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if readErr != nil {
		return nil, fmt.Errorf("safefetch: read body: %w", readErr)
	}
	truncated := false
	if int64(len(body)) > limit {
		body = body[:limit]
		truncated = true
	}

	return &FetchResult{
		FinalURL:    resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Body:        body,
		Truncated:   truncated,
	}, nil
}

// validateDialAddr is the dialer ControlContext callback. Called
// with the resolved "ip:port" string Go is about to dial.
// Rejects literal IPs in any blocked range.
//
// allowLoopback is the test-mode bypass — when true, loopback
// IPs (127.0.0.0/8, ::1) are permitted so httptest's loopback
// server can be reached. Production code always passes false.
func validateDialAddr(_ /* network */, addr string, allowLoopback bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Failure to parse the host:port is a programmer
		// error, not a hostile input — but fail closed.
		return fmt.Errorf("safefetch: invalid dial address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// ControlContext is called after DNS resolution, so
		// host MUST be a literal IP at this point. If not,
		// something is very wrong — fail closed.
		return fmt.Errorf("safefetch: dial address has non-literal IP host %q", host)
	}
	if allowLoopback && ip.IsLoopback() {
		return nil
	}
	if IsBlockedIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedIP, ip)
	}
	return nil
}
