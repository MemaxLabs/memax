package workerapp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/safefetch"
)

// fakeChatFetchURLClient is a minimal stub for the
// chatFetchURLClient interface. Tests assemble whatever
// (FetchResult, error) tuple they want to drive each branch
// of chatFetchURLAdapter.FetchURL.
type fakeChatFetchURLClient struct {
	calls  []string
	result *safefetch.FetchResult
	err    error
}

func (f *fakeChatFetchURLClient) Fetch(_ context.Context, rawURL string) (*safefetch.FetchResult, error) {
	f.calls = append(f.calls, rawURL)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

const (
	fuOwner = "u-fetch"
	fuURL   = "https://example.com/page"
)

// Happy path: client returns a FetchResult, adapter passes it
// through. HTML title is extracted; logging happens.
func TestChatFetchURLAdapter_HappyPath(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			FinalURL:    fuURL,
			StatusCode:  200,
			ContentType: "text/html",
			Body:        []byte("<html><head><title>Example Domain</title></head><body>...</body></html>"),
		},
	}
	a := &chatFetchURLAdapter{client: client}

	out, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     fuURL,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", out.StatusCode)
	}
	if out.Title != "Example Domain" {
		t.Errorf("Title = %q, want %q", out.Title, "Example Domain")
	}
	if !strings.Contains(out.Body, "Example Domain") {
		t.Errorf("Body missing 'Example Domain': %q", out.Body)
	}
	if len(client.calls) != 1 || client.calls[0] != fuURL {
		t.Errorf("client.calls = %v, want [%q]", client.calls, fuURL)
	}
}

// safefetch URL-rejection sentinels all map to
// ErrFetchURLBlocked (terminal — agent shouldn't retry the
// same URL). Codex Phase 3.6b3f v3 emphasized that the agent
// shouldn't be able to enumerate the blocklist via error
// surfaces, so the wrapper above this surfaces a generic
// rejection.
func TestChatFetchURLAdapter_AllSafefetchSentinelsMapToBlocked(t *testing.T) {
	t.Parallel()
	cases := map[string]error{
		"empty url":            safefetch.ErrEmptyURL,
		"parse error":          safefetch.ErrParseURL,
		"unsupported scheme":   safefetch.ErrUnsupportedScheme,
		"userinfo not allowed": safefetch.ErrUserinfoNotAllowed,
		"empty host":           safefetch.ErrEmptyHost,
		"blocked host (memax)": safefetch.ErrBlockedHost,
		"blocked literal IP":   safefetch.ErrBlockedIP,
	}
	for name, sentinel := range cases {
		sentinel := sentinel
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &fakeChatFetchURLClient{err: sentinel}
			a := &chatFetchURLAdapter{client: client}

			_, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
				OwnerID: fuOwner,
				URL:     "https://attacker.controlled/",
			})
			if !errors.Is(err, tools.ErrFetchURLBlocked) {
				t.Errorf("err = %v, want ErrFetchURLBlocked", err)
			}
		})
	}
}

// Network-layer failure (non-sentinel error) maps to
// ErrFetchURLNetworkFailed. Distinct from blocked → agent can
// retry.
func TestChatFetchURLAdapter_NetworkErrorMaps(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{err: errors.New("dial: connection refused")}
	a := &chatFetchURLAdapter{client: client}

	_, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     "https://example.com/",
	})
	if !errors.Is(err, tools.ErrFetchURLNetworkFailed) {
		t.Errorf("err = %v, want ErrFetchURLNetworkFailed", err)
	}
}

// Validation: empty URL rejected before client call.
func TestChatFetchURLAdapter_EmptyURLRejected(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{}
	a := &chatFetchURLAdapter{client: client}
	_, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     "",
	})
	if err == nil {
		t.Errorf("err = nil, want validation error")
	}
	if len(client.calls) != 0 {
		t.Errorf("client.calls = %v, want 0 (empty URL must short-circuit)", client.calls)
	}
}

// Nil client = misconfiguration; adapter surfaces an error.
func TestChatFetchURLAdapter_NilClientErrors(t *testing.T) {
	t.Parallel()
	a := &chatFetchURLAdapter{client: nil}
	_, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     "https://example.com/",
	})
	if err == nil {
		t.Errorf("err = nil, want misconfig error")
	}
}

// Adapter-level body cap: when safefetch returns a body
// LARGER than the adapter's chatFetchURLBodyCap (but still
// under the safefetch 5MB cap), the adapter truncates and
// sets Truncated=true so the agent's wire result reflects
// the actual byte count it received. Codex Phase 3.6b3g v1
// [High] caught that without this cap, the SDK's 64KB
// MaxResultBytes truncated the JSON STRING after marshaling
// — invalid JSON + Truncated=false lying about it.
func TestChatFetchURLAdapter_AdapterBodyCapEnforced(t *testing.T) {
	t.Parallel()
	// Body that's well under the safefetch 5MB cap but well
	// over the adapter's 48KB cap.
	bigBody := bytes.Repeat([]byte("a"), chatFetchURLBodyCap*2)
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			FinalURL:    fuURL,
			StatusCode:  200,
			ContentType: "text/plain",
			Body:        bigBody,
			Truncated:   false, // safefetch did NOT truncate
		},
	}
	a := &chatFetchURLAdapter{client: client}
	out, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     fuURL,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if int64(len(out.Body)) != int64(chatFetchURLBodyCap) {
		t.Errorf("len(Body) = %d, want %d (adapter cap)", len(out.Body), chatFetchURLBodyCap)
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true (adapter must mark truncated even when safefetch said full body)")
	}
}

// Adapter cap doesn't activate when the body is under it —
// the wire result reports the full body.
func TestChatFetchURLAdapter_SmallBodyNotTruncated(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			FinalURL:    fuURL,
			StatusCode:  200,
			ContentType: "text/plain",
			Body:        []byte("small"),
			Truncated:   false,
		},
	}
	a := &chatFetchURLAdapter{client: client}
	out, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     fuURL,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Body != "small" {
		t.Errorf("Body = %q, want %q", out.Body, "small")
	}
	if out.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

// Truncated response: the safefetch.Truncated flag flows
// through to the outcome.
func TestChatFetchURLAdapter_TruncatedFlagFlowsThrough(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			FinalURL:    fuURL,
			StatusCode:  200,
			ContentType: "text/plain",
			Body:        []byte("partial body"),
			Truncated:   true,
		},
	}
	a := &chatFetchURLAdapter{client: client}
	out, err := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     fuURL,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !out.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

// Title extraction is HTML-only — content-types like
// application/json don't get title extraction even if their
// body happens to contain a "<title>" substring.
func TestChatFetchURLAdapter_NonHTMLDoesntExtractTitle(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			StatusCode:  200,
			ContentType: "application/json",
			// JSON happens to contain the substring; we MUST NOT
			// run the title extractor on non-HTML bodies.
			Body: []byte(`{"title":"<title>Hidden</title>","value":42}`),
		},
	}
	a := &chatFetchURLAdapter{client: client}
	out, _ := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     "https://api.example.com/data",
	})
	if out.Title != "" {
		t.Errorf("Title = %q, want empty (JSON content-type)", out.Title)
	}
}

// XHTML content type triggers title extraction (same lane as
// text/html).
func TestChatFetchURLAdapter_XHTMLExtractsTitle(t *testing.T) {
	t.Parallel()
	client := &fakeChatFetchURLClient{
		result: &safefetch.FetchResult{
			StatusCode:  200,
			ContentType: "application/xhtml+xml",
			Body:        []byte("<?xml version=\"1.0\"?><html xmlns=\"...\"><head><title>X</title></head></html>"),
		},
	}
	a := &chatFetchURLAdapter{client: client}
	out, _ := a.FetchURL(context.Background(), tools.FetchURLRequest{
		OwnerID: fuOwner,
		URL:     "https://example.com/x.xhtml",
	})
	if out.Title != "X" {
		t.Errorf("Title = %q, want %q", out.Title, "X")
	}
}

// ─── extractHTMLTitle table ─────────────────────────────────

func TestExtractHTMLTitle_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"plain", "<html><head><title>Hello</title></head></html>", "Hello"},
		{"upper-case tag", "<HTML><HEAD><TITLE>Hi</TITLE></HEAD></HTML>", "Hi"},
		{"mixed-case tag", "<TiTle>Mixed</TiTle>", "Mixed"},
		{"with attributes", `<title data-foo="bar">Attrs</title>`, "Attrs"},
		{"whitespace collapsed", "<title>  hello\n\tworld  </title>", "hello world"},
		{"empty title", "<title></title>", ""},
		{"only whitespace", "<title>   \n   </title>", ""},
		{"truncated input — no closing tag", "<title>Never ending", ""},
		{"no title element", "<html><body>no title here</body></html>", ""},
		{"title-attr in another tag (not real title)", `<meta name="title" content="X">`, ""},
		{"multiple titles — first wins", "<title>First</title><title>Second</title>", "First"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := extractHTMLTitle([]byte(c.body))
			if got != c.want {
				t.Errorf("extractHTMLTitle(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

// Title cap: a title longer than 256 chars is truncated.
func TestExtractHTMLTitle_Truncates(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 500)
	body := "<title>" + long + "</title>"
	got := extractHTMLTitle([]byte(body))
	if len(got) != 256 {
		t.Errorf("len = %d, want 256 (cap)", len(got))
	}
}

// ─── redactURLForLog ────────────────────────────────────────
//
// Codex Phase 3.6b3g v1 [Medium] flagged that url.Error
// includes the full URL in its .Error() string and that
// presigned/tokenized URLs commonly land in fetch_url
// targets. Logs strip query + fragment via this helper.

func TestRedactURLForLog_StripsQueryAndFragment(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://example.com/path?token=secret":           "https://example.com/path",
		"https://example.com/path?a=1&b=2":                "https://example.com/path",
		"https://example.com/path#section":                "https://example.com/path",
		"https://example.com/path?q=1#frag":               "https://example.com/path",
		"https://s3.amazonaws.com/bucket/key?Signature=x": "https://s3.amazonaws.com/bucket/key",
		"http://example.com/":                             "http://example.com/",
		"http://example.com":                              "http://example.com/",
		"  https://example.com/p?secret=1  ":              "https://example.com/p",
		"":                                                "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := redactURLForLog(in); got != want {
				t.Errorf("redactURLForLog(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

// Unparseable URLs return a sanitized "[unparseable]" rather
// than the raw input — the raw might itself contain a token.
func TestRedactURLForLog_UnparseableSanitized(t *testing.T) {
	t.Parallel()
	got := redactURLForLog("http://exa\x00mple.com/?secret=1")
	if !strings.Contains(got, "[unparseable]") {
		t.Errorf("redactURLForLog(invalid) = %q, want a sanitized form", got)
	}
	if strings.Contains(got, "secret=1") {
		t.Errorf("redactURLForLog leaked the query string: %q", got)
	}
}

// ─── safefetchSentinelName ──────────────────────────────────

func TestSafefetchSentinelName_Mapping(t *testing.T) {
	t.Parallel()
	cases := map[error]string{
		safefetch.ErrEmptyURL:           "empty_url",
		safefetch.ErrParseURL:           "parse_failed",
		safefetch.ErrUnsupportedScheme:  "unsupported_scheme",
		safefetch.ErrUserinfoNotAllowed: "userinfo_in_url",
		safefetch.ErrEmptyHost:          "empty_host",
		safefetch.ErrBlockedHost:        "blocked_host",
		safefetch.ErrBlockedIP:          "blocked_ip",
		errors.New("mystery error"):     "unknown",
	}
	for err, want := range cases {
		if got := safefetchSentinelName(err); got != want {
			t.Errorf("safefetchSentinelName(%v) = %q, want %q", err, got, want)
		}
	}
}

// ─── isHTMLContentType ──────────────────────────────────────

func TestIsHTMLContentType_Matrix(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"text/html":                true,
		"application/xhtml+xml":    true,
		"text/plain":               false,
		"application/json":         false,
		"":                         false,
		"text/html; charset=utf-8": false, // pre-strip; safefetch does the strip
	}
	for ct, want := range cases {
		t.Run(ct, func(t *testing.T) {
			if got := isHTMLContentType(ct); got != want {
				t.Errorf("isHTMLContentType(%q) = %v, want %v", ct, got, want)
			}
		})
	}
}
