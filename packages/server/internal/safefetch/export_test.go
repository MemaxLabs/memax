package safefetch

import "net/http"

// NewForTest returns a Client with the test-mode loopback
// bypass enabled. URL-level validation, redirect re-validation,
// body cap, timeout, user-agent, and the IP block for
// non-loopback ranges all stay active. Used by tests that need
// to exercise HTTP-level paths against an httptest.Server
// (which binds 127.0.0.1).
//
// NEVER call this from production code paths. The
// allowLoopback escape hatch is unexported precisely so this
// file (with the _test.go suffix) is the only place that can
// flip it on.
func NewForTest(opts Options) *Client {
	opts.allowLoopback = true
	return New(opts)
}

// ExportHTTPClient returns the underlying *http.Client for
// tests that need to inspect or override the Transport. Use
// only from this package's _test.go files.
func ExportHTTPClient(c *Client) *http.Client {
	return c.httpClient
}
