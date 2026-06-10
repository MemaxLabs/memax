package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func makeBareRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}

// parseOAuthAllowlist already has coverage via TestParseOAuthAllowlist
// in auth_test.go.

// ─── parseState / generateState ─────────────────────────────────

func TestGenerateStateAndParseStateRoundTrip(t *testing.T) {
	t.Parallel()

	// "none" sentinel: generateState("") → "none", parseState("none") → "".
	if got := generateState(""); got != "none" {
		t.Errorf("generateState(\"\") = %q, want \"none\"", got)
	}
	if got := parseState("none"); got != "" {
		t.Errorf("parseState(\"none\") = %q, want \"\"", got)
	}
	// Non-empty redirect roundtrips through hex encoding.
	for _, redirect := range []string{
		"https://app.memax.app/auth/callback",
		"https://staging-app.memaxlabs.com/auth/callback?invite=TOKEN",
		"x", // minimum viable
	} {
		state := generateState(redirect)
		if state == "none" || state == "" {
			t.Errorf("generateState(%q) produced sentinel %q", redirect, state)
		}
		if got := parseState(state); got != redirect {
			t.Errorf("roundtrip lost data: %q → %q → %q", redirect, state, got)
		}
	}
}

func TestParseStateRejectsGarbage(t *testing.T) {
	t.Parallel()

	// Any non-hex input should produce "" (a refused state),
	// NEVER a partial decode that might spoof a valid redirect.
	for _, bad := range []string{
		"this is not hex",
		"!@#$",
		"deadbeef_not_really_hex",
	} {
		if got := parseState(bad); got != "" {
			t.Errorf("garbage input %q produced %q — must return empty", bad, got)
		}
	}
}

// ─── extractInviteToken ─────────────────────────────────────────

func TestExtractInviteTokenPullsFromQuery(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://x.test/auth/callback?invite=abc123":      "abc123",
		"https://x.test/auth/callback?foo=bar&invite=abc": "abc",
		"https://x.test/auth/callback":                    "", // missing
		"https://x.test/auth/callback?invite=":            "", // empty
		"":                                                "",
		"://broken":                                       "", // URL parse fail
	}
	for in, want := range cases {
		if got := extractInviteToken(in); got != want {
			t.Errorf("extractInviteToken(%q) = %q, want %q", in, got, want)
		}
	}
}

// ─── parseTypeFilter ────────────────────────────────────────────

func TestParseTypeFilter(t *testing.T) {
	t.Parallel()

	if got := parseTypeFilter(""); got != nil {
		t.Errorf("empty should produce nil (SSE broker treats nil as 'all types'), got %v", got)
	}
	if got := parseTypeFilter("   "); got != nil {
		t.Errorf("whitespace-only should also be nil, got %v", got)
	}

	got := parseTypeFilter("hub.memories.changed,hub.topics.changed, , notification.created ")
	want := map[string]bool{
		"hub.memories.changed": true,
		"hub.topics.changed":   true,
		"notification.created": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ─── normalizeHubIDs ────────────────────────────────────────────

func TestNormalizeHubIDs(t *testing.T) {
	t.Parallel()

	if got := normalizeHubIDs(nil); got != nil {
		t.Errorf("nil input should produce nil, got %v", got)
	}
	if got := normalizeHubIDs([]string{}); got != nil {
		t.Errorf("empty input should produce nil, got %v", got)
	}
	// Dedup + trim + sort are the three invariants. Input order
	// and whitespace variations shouldn't affect the output.
	got := normalizeHubIDs([]string{"  bbb ", "aaa", "bbb", " aaa", "", "ccc"})
	want := []string{"aaa", "bbb", "ccc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ─── normalizeInviteRole ────────────────────────────────────────

func TestNormalizeInviteRole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "contributor", true}, // default
		{"contributor", "contributor", true},
		{"admin", "admin", true},
		{"viewer", "viewer", true},
		{"member", "contributor", true}, // legacy alias
		{"reader", "viewer", true},      // legacy alias
		{"  admin ", "admin", true},     // trim
		{"owner", "", false},            // owner isn't invitable — it's assigned at hub creation
		{"editor", "", false},           // unknown role
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := normalizeInviteRole(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("normalizeInviteRole(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// ─── InjectUserIDForTest roundtrip guard ────────────────────────

func TestInjectUserIDForTestRoundtrip(t *testing.T) {
	t.Parallel()

	// The exported helper must actually populate the unexported
	// userIDKey that GetUserID reads from — otherwise cross-
	// package tests silently get empty user IDs.
	req := makeBareRequest()
	req = InjectUserIDForTest(req, "u-test")
	if got := GetUserID(req); got != "u-test" {
		t.Errorf("InjectUserIDForTest + GetUserID roundtrip broken: got %q", got)
	}
}
