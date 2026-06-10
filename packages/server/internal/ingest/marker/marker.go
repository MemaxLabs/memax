// Package marker scans memory body text for user-flagged action
// markers (@TODO, @FIXME, @QUESTION) and returns the matched
// marker line.
//
// Why this exists: plan 24's user-followup trigger needs to know
// when the user has explicitly flagged a memory for follow-up.
// Most knowledge tools (Notion, Linear, IDE comments) already use
// the @TODO / @FIXME / @QUESTION convention, so memax inherits the
// signal at zero training cost. The dream agent then surfaces the
// matched text ("the user wrote @TODO verify the API key
// rotation cadence — should I research this?") rather than just
// firing on a boolean.
//
// This package is deliberately tiny — a regex + a length clamp.
// Larger NLP-style flagging (sentiment, urgency classification,
// intent extraction) belongs above this layer; this scan is the
// floor that captures the explicit, user-typed signal.
package marker

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxMarkerLen caps how much of the marker line we capture. The
// dream agent only needs enough context to act on the marker —
// "@TODO verify the API key rotation cadence" is plenty; we don't
// need a full paragraph if the marker happens to be followed by a
// long rant. 240 runes covers ~3-4 short sentences without dragging
// the signal into noise territory and keeps the column from
// inflating the row size for memories with verbose markers.
const MaxMarkerLen = 240

// markerPattern matches @TODO / @FIXME / @QUESTION (case-insensitive)
// at the start of a line OR immediately following whitespace, then
// captures the rest of that line up to (but not including) the
// next newline.
//
// The `(?:^|[\s\p{Z}])` lookbehind alternative prevents matches on
// identifiers like "no@todo" or email addresses like
// "user@todo.com" — the marker MUST be at a line/whitespace
// boundary. We accept both Go's ASCII `\s` (space/tab/CR/LF/FF)
// AND Unicode separator class `\p{Z}` (NBSP U+00A0, en/em spaces
// U+2000–200A, narrow no-break U+202F, ideographic space U+3000,
// etc.) so markers in imported docs from Notion / Word / Chinese
// editors fire correctly. RE2 supports `\p{Z}` natively.
//
// `\b` after the keyword ensures we don't false-match on words
// like "@TODOMORROW" — the keyword must end at a word boundary.
var markerPattern = regexp.MustCompile(`(?i)(?:^|[\s\p{Z}])(@(?:TODO|FIXME|QUESTION)\b[^\n]*)`)

// Scan returns the first action marker found in body, or "" if
// none.
//
// The returned text starts with "@TODO" / "@FIXME" / "@QUESTION"
// (in the original case the user wrote) and continues to the end
// of the line, leading and trailing whitespace trimmed. Long
// markers are truncated at MaxMarkerLen runes — when truncation
// happens, the returned string is exactly MaxMarkerLen runes long
// (not bytes) so multi-byte chars are preserved.
//
// The "first marker wins" rule is deliberate: a single memory rarely
// carries multiple unrelated TODOs, and when it does the first one
// is almost always the most actionable. The dream agent treats the
// marker as triage context, not as an exhaustive list — surfacing
// many markers per memory would dilute the signal.
//
// Concurrency: Scan is safe for concurrent use. The package's regex
// is compiled once at init.
func Scan(body string) string {
	if body == "" {
		return ""
	}
	m := markerPattern.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	marker := strings.TrimSpace(m[1])
	if utf8.RuneCountInString(marker) > MaxMarkerLen {
		runes := []rune(marker)
		marker = string(runes[:MaxMarkerLen])
	}
	return marker
}
