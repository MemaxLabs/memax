package marker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScan_Empty(t *testing.T) {
	t.Parallel()
	if got := Scan(""); got != "" {
		t.Errorf("Scan(empty) = %q, want empty", got)
	}
}

func TestScan_NoMarker(t *testing.T) {
	t.Parallel()
	body := "This is a memory about my morning coffee. Nothing to do here."
	if got := Scan(body); got != "" {
		t.Errorf("Scan(no marker) = %q, want empty", got)
	}
}

func TestScan_StartOfBody(t *testing.T) {
	t.Parallel()
	got := Scan("@TODO verify the rotation cadence")
	if want := "@TODO verify the rotation cadence"; got != want {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}

func TestScan_AfterText(t *testing.T) {
	t.Parallel()
	body := "Notes on the API. @FIXME the token refresh is racy under load.\nMore notes."
	got := Scan(body)
	if want := "@FIXME the token refresh is racy under load."; got != want {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}

func TestScan_PreservesCase(t *testing.T) {
	t.Parallel()
	body := "@todo lowercase keyword should match too"
	got := Scan(body)
	if want := "@todo lowercase keyword should match too"; got != want {
		t.Errorf("Scan = %q, want %q (case must be preserved verbatim)", got, want)
	}
}

func TestScan_QuestionMarker(t *testing.T) {
	t.Parallel()
	body := "Project notes. @QUESTION should we use HSM-backed keys?"
	got := Scan(body)
	if want := "@QUESTION should we use HSM-backed keys?"; got != want {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}

// First marker wins — the dream agent treats the marker as triage
// context, not an exhaustive list. A future change to "all markers"
// would need a different return type and a deliberate decision.
func TestScan_FirstMarkerWins(t *testing.T) {
	t.Parallel()
	body := "@TODO first thing\n@FIXME second thing"
	got := Scan(body)
	if want := "@TODO first thing"; got != want {
		t.Errorf("Scan = %q, want %q (first marker should win)", got, want)
	}
}

// Email addresses must NOT match — the (?:^|\s) lookbehind alternative
// prevents in-word matches.
func TestScan_DoesNotMatchEmailLike(t *testing.T) {
	t.Parallel()
	body := "Contact user@todo.example.com for details."
	if got := Scan(body); got != "" {
		t.Errorf("Scan(email-like) = %q, want empty", got)
	}
}

// Compound words like "@TODOMORROW" must not match — the trailing
// \b enforces word-boundary at the end of the keyword.
func TestScan_RejectsCompoundKeyword(t *testing.T) {
	t.Parallel()
	body := "@TODOMORROW remember to file taxes"
	if got := Scan(body); got != "" {
		t.Errorf("Scan(compound) = %q, want empty", got)
	}
}

// Long markers truncate at MaxMarkerLen runes. Using runes (not
// bytes) preserves multi-byte characters at the boundary.
func TestScan_TruncatesAtMaxLen(t *testing.T) {
	t.Parallel()
	long := "@TODO " + strings.Repeat("x", MaxMarkerLen+50)
	got := Scan(long)
	if utf8.RuneCountInString(got) != MaxMarkerLen {
		t.Errorf("Scan truncated to %d runes, want %d", utf8.RuneCountInString(got), MaxMarkerLen)
	}
	if !strings.HasPrefix(got, "@TODO ") {
		t.Errorf("Scan truncated marker = %q, prefix lost", got[:20])
	}
}

// Multi-byte runes near the truncation boundary must remain intact —
// no half-emoji or split UTF-8 sequences.
func TestScan_TruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()
	prefix := "@TODO "
	// Use a 4-byte rune so byte truncation would split it.
	emoji := "📌"
	body := prefix + strings.Repeat(emoji, MaxMarkerLen)
	got := Scan(body)
	if !utf8.ValidString(got) {
		t.Errorf("Scan returned invalid UTF-8")
	}
	if utf8.RuneCountInString(got) > MaxMarkerLen {
		t.Errorf("Scan exceeded MaxMarkerLen rune cap: %d", utf8.RuneCountInString(got))
	}
}

// Multi-line bodies: marker wins anywhere, but only that line is
// captured (the [^\n]* clause).
func TestScan_StopsAtNewline(t *testing.T) {
	t.Parallel()
	body := "Line one.\n@TODO inspect this\nLine three."
	got := Scan(body)
	if want := "@TODO inspect this"; got != want {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}

// Unicode whitespace — Notion exports use NBSP, CJK editors use
// U+3000 (ideographic space). The regex's `\p{Z}` class catches
// both. ASCII-only `\s` would miss them and false-negative on
// pasted content.
func TestScan_AcceptsUnicodeWhitespace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"NBSP (U+00A0)", "Notes:\u00a0@TODO confirm timing"},
		{"narrow no-break (U+202F)", "Notes:\u202f@FIXME race"},
		{"ideographic space (U+3000)", "Notes:\u3000@QUESTION should we?"},
		{"em-space (U+2003)", "Notes:\u2003@TODO file ticket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Scan(tc.body)
			if got == "" {
				t.Errorf("Scan(%s) = empty, want a non-empty marker (Unicode whitespace must act as boundary)", tc.name)
			}
		})
	}
}

// Markdown lists, common Notion / Linear shape — the leading
// whitespace counts as the (?:^|\s) boundary.
func TestScan_InMarkdownListItem(t *testing.T) {
	t.Parallel()
	body := "- Pick up groceries\n- @TODO call dentist Monday\n- File expense report"
	got := Scan(body)
	if want := "@TODO call dentist Monday"; got != want {
		t.Errorf("Scan = %q, want %q", got, want)
	}
}
