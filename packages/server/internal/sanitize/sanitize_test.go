package sanitize

import (
	"strings"
	"testing"
)

// --- Body: canonical XSS payloads must be stripped --------------------

func TestBody_ScriptTagStripped(t *testing.T) {
	got := Body(`<p>hello</p><script>alert(1)</script>`)
	if strings.Contains(got, "<script") || strings.Contains(got, "alert") {
		t.Errorf("script tag should be stripped, got %q", got)
	}
	if !strings.Contains(got, "<p>hello</p>") {
		t.Errorf("safe content should be preserved, got %q", got)
	}
}

func TestBody_EventHandlersStripped(t *testing.T) {
	// onerror is the most common payload vector; onclick/onmouseover
	// are also handled by the same UGC policy attribute allowlist.
	cases := []string{
		`<img src=x onerror="alert(1)">`,
		`<a href="#" onclick="alert(1)">x</a>`,
		`<div onmouseover="alert(1)">x</div>`,
		`<body onload="alert(1)">x</body>`,
	}
	for _, payload := range cases {
		got := Body(payload)
		lc := strings.ToLower(got)
		if strings.Contains(lc, "onerror") || strings.Contains(lc, "onclick") ||
			strings.Contains(lc, "onmouseover") || strings.Contains(lc, "onload") ||
			strings.Contains(lc, "alert(") {
			t.Errorf("event handler not stripped for %q: got %q", payload, got)
		}
	}
}

func TestBody_JavascriptURLsStripped(t *testing.T) {
	cases := []string{
		`<a href="javascript:alert(1)">click</a>`,
		`<a href="JAVASCRIPT:alert(1)">click</a>`,  // case bypass
		`<a href=" javascript:alert(1)">click</a>`, // whitespace bypass
	}
	for _, payload := range cases {
		got := Body(payload)
		if strings.Contains(strings.ToLower(got), "javascript:") ||
			strings.Contains(got, "alert(") {
			t.Errorf("javascript: URL not stripped for %q: got %q", payload, got)
		}
	}
}

func TestBody_IframeAndObjectStripped(t *testing.T) {
	for _, payload := range []string{
		`<iframe src="//evil.example"></iframe>`,
		`<object data="//evil.example"></object>`,
		`<embed src="//evil.example">`,
	} {
		got := Body(payload)
		lc := strings.ToLower(got)
		if strings.Contains(lc, "<iframe") || strings.Contains(lc, "<object") || strings.Contains(lc, "<embed") {
			t.Errorf("embedding element not stripped for %q: got %q", payload, got)
		}
	}
}

func TestBody_DataURIScriptStripped(t *testing.T) {
	// data: URIs can ship scripts. bluemonday's UGC policy rejects
	// data URIs on navigational attrs.
	got := Body(`<a href="data:text/html,<script>alert(1)</script>">x</a>`)
	lc := strings.ToLower(got)
	if strings.Contains(lc, "data:text/html") || strings.Contains(lc, "alert") {
		t.Errorf("data: URI with script not stripped: got %q", got)
	}
}

func TestBody_SVGOnloadStripped(t *testing.T) {
	// SVG onload is a common CSP-bypass vector.
	got := Body(`<svg><g onload="alert(1)"/></svg>`)
	if strings.Contains(strings.ToLower(got), "onload") || strings.Contains(got, "alert") {
		t.Errorf("SVG onload not stripped: got %q", got)
	}
}

func TestBody_PreservesSafeFormatting(t *testing.T) {
	// Positive case — UGC formatting must survive. If any of these
	// get stripped, users lose intended formatting and we likely
	// over-sharpened the policy.
	in := `<p>Paragraph with <b>bold</b>, <i>italic</i>, and <a href="https://example.com">a link</a>.</p>
<ul><li>one</li><li>two</li></ul>
<pre><code>go build</code></pre>`
	got := Body(in)
	for _, want := range []string{"<p>", "<b>bold</b>", "<i>italic</i>", "<a href=",
		"example.com", "<ul>", "<li>one</li>", "<pre>", "<code>"} {
		if !strings.Contains(got, want) {
			t.Errorf("UGC policy over-sharpened: %q missing from sanitized output %q", want, got)
		}
	}
}

func TestBody_EmptyInputReturnsEmpty(t *testing.T) {
	if got := Body(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- PlainText: all tags removed -------------------------------------

func TestPlainText_StripsAllTags(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"<b>hello</b>", "hello"},
		{"<script>alert(1)</script>hello", "hello"},
		{`<img src=x onerror="alert(1)">hello`, "hello"},
		{"<a href=\"https://example.com\">click me</a>", "click me"},
		{"", ""},
	}
	for _, tt := range cases {
		if got := PlainText(tt.in); got != tt.want {
			t.Errorf("PlainText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPlainText_TrimsWhitespace(t *testing.T) {
	// After tag removal, leftover whitespace at the ends should be
	// trimmed so `title` fields don't end up with stray space after
	// sanitization.
	got := PlainText(" <b>hello</b> ")
	if got != "hello" {
		t.Errorf("expected trimmed, got %q", got)
	}
}
