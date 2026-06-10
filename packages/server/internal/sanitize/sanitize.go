// Package sanitize provides server-side HTML sanitization for user-supplied
// memory content. The single intake point runs on every Create/Update, so
// stored content is safe to hand to any downstream renderer — web
// (ReactMarkdown today), a future SDK consumer, or a third-party client
// rendering via innerHTML — without re-deriving sanitization at each hop.
//
// Two policies:
//
//   - Body applies to memory.content when content_type == "html". Built on
//     bluemonday's UGCPolicy: keeps the tags a user would reasonably
//     author (<p>, <b>, <i>, <a href>, code blocks, lists, tables, simple
//     inline formatting) and strips anything that can execute —
//     <script>, <iframe>, <object>, <embed>, inline event handlers,
//     javascript:/data: URLs, form controls, etc.
//
//   - PlainText applies to memory.title and memory.hint (and any other
//     field rendered as text today but whose authorship we don't want
//     to assume). StrictPolicy strips every tag. Equivalent to "keep
//     the text nodes, drop everything else." Safe for contexts that
//     would later pass the value into innerHTML by mistake.
//
// Policies are package-level singletons — bluemonday's internal data is
// immutable after construction and the library's own docs recommend
// allocating once. Callers MUST use the constructors (Body / PlainText)
// rather than reach in with custom policies so any future policy
// sharpening lives in one place.
package sanitize

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// bodyPolicy is the UGC policy applied to HTML memory bodies. Built once
// at package init because bluemonday's Policy is safe for concurrent use
// once Sanitize is called on it the first time, and rebuilding per-call
// wastes allocations on the hot path.
var bodyPolicy = bluemonday.UGCPolicy()

// plainTextPolicy strips every tag — used for fields that are rendered
// as plain text today. Server-side defense-in-depth; if a future
// component renders title/hint via innerHTML, they're still safe.
var plainTextPolicy = bluemonday.StrictPolicy()

// Body sanitizes an HTML memory body. Input is UTF-8 HTML; output is
// UTF-8 HTML with dangerous constructs stripped. Empty input returns
// empty — caller does not need to guard.
//
// This is the ONLY place memory content should be sanitized; we want a
// single policy decision, not ad-hoc escaping at every callsite.
func Body(input string) string {
	if input == "" {
		return ""
	}
	return bodyPolicy.Sanitize(input)
}

// PlainText strips all HTML tags from a short text field (title, hint,
// summary). Preserves text content and whitespace; removes everything
// that could be an element. Trims leading/trailing whitespace because
// bluemonday preserves stray whitespace created by tag removal and the
// fields it protects are always single-line.
func PlainText(input string) string {
	if input == "" {
		return ""
	}
	return strings.TrimSpace(plainTextPolicy.Sanitize(input))
}
