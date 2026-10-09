package oauthredirect

import (
	"encoding/json"
	"os"
	"testing"
)

// Which redirect URIs a client may register: web, loopback and private-use
// scheme URIs (RFC 8252), never a dangerous scheme, userinfo or a fragment.
// The cases are shared with the web app's consent page
// (src/lib/oauth-redirects.test.ts), so the two can't drift.
func TestValid(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Valid [][2]any `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases.Valid) < 20 {
		t.Fatalf("cases: %v (%d)", err, len(cases.Valid))
	}
	for _, c := range cases.Valid {
		uri, want := c[0].(string), c[1].(bool)
		if got := Valid(uri); got != want {
			t.Errorf("Valid(%q) = %t, want %t", uri, got, want)
		}
	}
}

// A registered loopback redirect matches with any port, on the same
// scheme, host, path and query; everything else matches exactly.
func TestMatches(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		presented, registered string
		want                  bool
	}{
		{"http://127.0.0.1:59656/", "http://127.0.0.1:33418", true},
		{"http://127.0.0.1:59656/", "http://127.0.0.1/", true},
		{"http://127.0.0.1:59656", "http://127.0.0.1:33418/", true},
		{"http://localhost:49152/callback", "http://localhost:8787/callback", true},
		{"http://[::1]:5000/cb?x=1", "http://[::1]/cb?x=1", true},
		{"http://localhost:49152/other", "http://localhost:8787/callback", false},
		{"http://localhost:49152/callback?x=2", "http://localhost:8787/callback?x=1", false},
		{"http://127.0.0.1:49152/callback", "http://localhost:8787/callback", false},
		{"https://127.0.0.1:443/cb", "http://127.0.0.1/cb", false},
		{"https://vscode.dev/redirect", "https://vscode.dev/redirect", true},
		{"https://vscode.dev:8443/redirect", "https://vscode.dev/redirect", false},
		{"cursor://anysphere.cursor-mcp/oauth/callback", "cursor://anysphere.cursor-mcp/oauth/callback", true},
		{"cursor://anysphere.cursor-mcp/oauth/other", "cursor://anysphere.cursor-mcp/oauth/callback", false},
		{"javascript:alert(1)", "javascript:alert(1)", false},
	} {
		if got := Matches(c.presented, c.registered); got != c.want {
			t.Errorf("Matches(%q, %q) = %t, want %t", c.presented, c.registered, got, c.want)
		}
	}
	if !Allowed("http://127.0.0.1:61234/", []string{"https://vscode.dev/redirect", "http://127.0.0.1:33418"}) {
		t.Error("VS Code's random port isn't allowed")
	}
}
