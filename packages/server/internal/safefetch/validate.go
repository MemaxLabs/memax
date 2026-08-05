package safefetch

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validation error sentinels. Each maps 1:1 to a specific
// reject reason so callers (and tests) can distinguish without
// parsing error strings. The tool wrapper translates these to
// a generic "blocked" error for the agent — no need to leak
// the exact reason to a potentially-hostile prompt-author.
var (
	ErrEmptyURL           = errors.New("safefetch: url is empty")
	ErrParseURL           = errors.New("safefetch: url could not be parsed")
	ErrURLTooLong         = errors.New("safefetch: url exceeds the maximum allowed length")
	ErrUnsupportedScheme  = errors.New("safefetch: only http and https schemes are allowed")
	ErrUserinfoNotAllowed = errors.New("safefetch: url userinfo (user:password@host) is not allowed")
	ErrEmptyHost          = errors.New("safefetch: url has no host")
	ErrBlockedHost        = errors.New("safefetch: host is blocked (memax-owned or operator-blocked)")
	ErrBlockedIP          = errors.New("safefetch: literal IP host is in a blocked range (private / loopback / link-local / multicast / metadata / etc.)")
)

// MaxURLLength caps the byte-length of a URL we'll dispatch.
// Most server stacks reject URLs longer than ~2KB; 8KB is a
// generous cap that accommodates query-heavy public APIs
// while bounding the JSON-encoded tool result the agent
// receives (the FinalURL field plus metadata must fit within
// the SDK's MaxResultBytes). Codex Phase 3.6b3g v3 [Medium]
// flagged that without this bound, a redirect chain ending
// at a multi-KB Location URL could still bust the result cap
// even after the body is dropped.
const MaxURLLength = 8 * 1024

// validateURL performs the FIRST stage of SSRF protection —
// everything we can check before DNS. The dial-time
// ControlContext does the IP-after-resolution check.
//
// Returns the parsed *url.URL on success so the caller can
// reuse it without re-parsing.
//
// allowLoopback is the test-mode escape hatch — when true,
// literal loopback IPs (127.0.0.0/8, ::1) bypass the IP block
// so httptest can reach the test server. Production callers
// always pass false.
func validateURL(rawURL string, blockedHosts []string, allowLoopback bool) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, ErrEmptyURL
	}
	if len(rawURL) > MaxURLLength {
		return nil, fmt.Errorf("%w (got %d bytes, max %d)", ErrURLTooLong, len(rawURL), MaxURLLength)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseURL, err)
	}

	// Scheme allowlist. Lowercase it because the parser
	// preserves case in some weird inputs.
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w: got %q", ErrUnsupportedScheme, u.Scheme)
	}

	// No userinfo. Credentials in URLs are almost never the
	// fetch target's intended auth path — they're either
	// SSRF bait (foo@private-host/) or accidentally-leaked
	// secrets. Reject either way.
	if u.User != nil {
		return nil, ErrUserinfoNotAllowed
	}

	host := u.Hostname()
	if host == "" {
		return nil, ErrEmptyHost
	}

	// Hostname blocklist. Suffix match catches subdomains:
	// "memax.app" blocks "api.memax.app".
	if IsBlockedHost(host, blockedHosts) {
		return nil, fmt.Errorf("%w: host=%q", ErrBlockedHost, host)
	}

	// If the host is a literal IP (no DNS needed), validate
	// it directly. Otherwise the dialer's ControlContext
	// catches it after DNS resolves.
	if ip := net.ParseIP(host); ip != nil {
		if allowLoopback && ip.IsLoopback() {
			// Test-mode escape hatch: httptest binds 127.0.0.1
			// and the production block would refuse to dial.
			// Production callers always pass allowLoopback=false.
		} else if IsBlockedIP(ip) {
			return nil, fmt.Errorf("%w: ip=%s", ErrBlockedIP, ip)
		}
	}

	return u, nil
}
