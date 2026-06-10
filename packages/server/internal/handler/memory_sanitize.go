package handler

import (
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/sanitize"
)

// isHTMLContentType returns true when req.ContentType identifies HTML,
// whether as the short "html" alias the web uses or as a real MIME
// type that happens to be text/html. Matches case-insensitive and
// tolerates parameters like "; charset=utf-8".
//
// Kept narrow on purpose — only HTML ever triggers body sanitization.
// Markdown, code, text, pdf, image, link, and structured formats either
// can't carry an XSS vector through the web renderer (ReactMarkdown
// escapes HTML in markdown) or don't get rendered as HTML anywhere.
func isHTMLContentType(ct string) bool {
	if ct == "" {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(ct))
	if idx := strings.IndexByte(normalized, ';'); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	switch normalized {
	case "html", "text/html", "application/xhtml+xml":
		return true
	}
	return false
}

// sanitizeMemoryFields applies the server-side sanitization policy to
// the three user-authored string fields that appear on every memory
// regardless of intake shape — Content, Title, Hint. This is the
// single policy chokepoint; both REST (PushRequest) and MCP (Memory
// struct constructed inline) intake routes MUST pass through here.
//
// Rules:
//   - Title and Hint run through the strict (tag-stripping) policy
//     because they're single-line text fields and every renderer
//     treats them as text today. Belt-and-suspenders for the day
//     someone drops them into an innerHTML context.
//   - Content runs through the UGC policy ONLY when ContentType is
//     HTML. Markdown content is left alone because the markdown
//     renderer (ReactMarkdown) already escapes embedded HTML; running
//     the UGC policy on markdown would mangle intentional backticks
//     and angle-bracket literals.
//   - Sanitization is idempotent: running twice on the same string
//     yields the same result, so Update callers don't double-sanitize.
func sanitizeMemoryFields(contentType string, content, title, hint *string) {
	if title != nil && *title != "" {
		*title = sanitize.PlainText(*title)
	}
	if hint != nil && *hint != "" {
		*hint = sanitize.PlainText(*hint)
	}
	if content != nil && *content != "" && isHTMLContentType(contentType) {
		*content = sanitize.Body(*content)
	}
}

// sanitizePushRequest is the REST-ingest adapter around
// sanitizeMemoryFields. Keeps Create/Update calling sites unchanged
// while the underlying policy lives in one function shared with the
// MCP path.
func sanitizePushRequest(req *model.PushRequest) {
	if req == nil {
		return
	}
	sanitizeMemoryFields(req.ContentType, &req.Content, &req.Title, &req.Hint)
}

// sanitizeMemory is the MCP-ingest adapter around sanitizeMemoryFields,
// applied to a constructed model.Memory immediately before CreateMemory.
// Mirrors sanitizePushRequest for the path that skips PushRequest.
func sanitizeMemory(m *model.Memory) {
	if m == nil {
		return
	}
	sanitizeMemoryFields(m.ContentType, &m.Content, &m.Title, &m.Hint)
}
