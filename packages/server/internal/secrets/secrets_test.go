package secrets

import (
	"strings"
	"testing"
)

// The reject set catches credentials; the deliberate EXCLUSIONS are as
// load-bearing as the catches — a developer memory product records git
// SHAs, localhost URLs and intranet topology every day, and rejecting
// those would make push unusable.
func TestDetectCredentials(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"env paste", "STRIPE_KEY=sk_live_" + strings.Repeat("a1B2", 8), true},
		{"github token", "use ghp_" + strings.Repeat("Zx9", 8) + " for auth", true},
		{"aws key", "AKIAIOSFODNN7EXAMPLE is the key", true},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIE...", true},
		{"jwt", "token was eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c", true},
		{"assignment", `password: "hunter2hunter2hunter2"`, true},
		{"bearer", "Authorization: Bearer abc123def456ghi789jkl012", true},
		// Exclusions — legitimate developer memory content:
		{"git sha", "fixed in commit 72fd973a8bc4e1d2f3a4b5c6d7e8f9a0b1c2d3e4", false},
		{"localhost url", "dev server runs at http://localhost:3000", false},
		{"rfc1918", "the NAS lives at 192.168.1.42", false},
		{"digit run", "order number 4111 1111 1111 1111 from the test doc", false},
		{"plain prose", "we decided to use Fly.io for deployment", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectCredentials(tc.text)
			if (len(got) > 0) != tc.want {
				t.Fatalf("DetectCredentials(%q) = %v, want detected=%v", tc.text, got, tc.want)
			}
		})
	}
}

// Redact keeps the wider posture (credentials + PII + network shapes)
// and must swallow whole PEM envelopes, not just the BEGIN header.
func TestRedact(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEsecretbody\n-----END RSA PRIVATE KEY-----"
	out := Redact("before " + pem + " after")
	if strings.Contains(out, "secretbody") || strings.Contains(out, "BEGIN") {
		t.Fatalf("PEM envelope must be fully redacted, got %q", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatalf("surrounding text must survive, got %q", out)
	}
	// Redact-only patterns still fire (SSN not in the reject set).
	if out := Redact("ssn 123-45-6789"); strings.Contains(out, "123-45-6789") {
		t.Fatalf("SSN must be redacted, got %q", out)
	}
	if got := DetectCredentials("ssn 123-45-6789"); len(got) > 0 {
		t.Fatalf("SSN must NOT trip the push reject gate, got %v", got)
	}
}
