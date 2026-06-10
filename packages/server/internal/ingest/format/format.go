package format

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	golanghtml "golang.org/x/net/html"
)

const maxCleanupChars = 20000
const defaultCleanupTimeout = 90 * time.Second

type Formatter struct {
	client         *anthropic.Client
	model          string
	cleanupTimeout time.Duration
}

type Result struct {
	Content string
	Reason  string
	Changed bool
}

func New(client *anthropic.Client) *Formatter {
	if client == nil {
		return &Formatter{}
	}
	return &Formatter{
		client:         client,
		model:          anthropic.ModelFromEnv("FORMAT_MODEL"),
		cleanupTimeout: readDurationFromEnv("FORMAT_TIMEOUT_MS", defaultCleanupTimeout),
	}
}

func Prepare(content string, contentType string, sourcePath string) Result {
	return (&Formatter{}).Prepare(content, contentType, sourcePath)
}

func (f *Formatter) Prepare(content string, contentType string, sourcePath string) Result {
	cleaned := normalizeWhitespace(content)
	if looksLikeHTML(cleaned, contentType, sourcePath) {
		markdown := htmlToMarkdown(cleaned)
		markdown = normalizeMarkdownSpacing(normalizeWhitespace(markdown))
		return Result{Content: markdown, Reason: "html_to_markdown", Changed: markdown != cleaned}
	}
	cleanedOCR := cleanupExtractedText(cleaned)
	return Result{Content: cleanedOCR, Reason: "normalize_whitespace", Changed: cleanedOCR != content}
}

func (f *Formatter) NormalizeContext(ctx context.Context, content string, contentType string, sourcePath string) Result {
	prepared := f.Prepare(content, contentType, sourcePath)
	if f.client == nil {
		return prepared
	}
	if !shouldLLMCleanup(prepared.Content, contentType, sourcePath) {
		return prepared
	}
	if len(prepared.Content) > maxCleanupChars {
		return Result{Content: prepared.Content, Reason: prepared.Reason + "+skip_large", Changed: prepared.Changed}
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, f.cleanupTimeout)
	defer cancel()
	formatted, err := f.llmCleanup(cleanupCtx, prepared.Content, contentType, sourcePath)
	if err != nil {
		slog.Warn("content formatting cleanup failed", "error", err, "content_type", contentType, "source_path", sourcePath)
		return Result{Content: prepared.Content, Reason: prepared.Reason + "+llm_failed", Changed: prepared.Changed}
	}
	formatted = normalizeWhitespace(formatted)
	if formatted == "" {
		return prepared
	}
	return Result{Content: formatted, Reason: prepared.Reason + "+llm_cleanup", Changed: formatted != content}
}

func shouldLLMCleanup(content string, contentType string, sourcePath string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if looksLikeHTML(trimmed, contentType, sourcePath) {
		return true
	}
	if contentType == "pdf" || contentType == "image" {
		return true
	}
	return looksStructurallyMessy(trimmed)
}

func looksStructurallyMessy(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) < 8 {
		return false
	}
	shortLines := 0
	headerLike := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(trimmed) <= 40 {
			shortLines++
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			headerLike++
		}
	}
	if shortLines >= len(lines)/2 && headerLike <= 1 {
		return true
	}
	return false
}

func (f *Formatter) llmCleanup(ctx context.Context, content string, contentType string, sourcePath string) (string, error) {
	kind := contentType
	if kind == "" {
		kind = "document"
	}
	prompt := fmt.Sprintf(`Rewrite the following extracted %s content into clean, readable Markdown.

Rules:
- Preserve all substantive information.
- Do not invent, summarize away, or omit details.
- Repair broken line wrapping and obvious OCR/layout issues.
- Preserve headings, lists, code blocks, and tables when they are present.
- If the source is HTML, convert it into human-readable Markdown instead of showing raw tags.
- Output only Markdown. No commentary.

Source path: %s

Content:
%s`, kind, sourcePath, content)
	resp, err := f.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     f.model,
		MaxTokens: 4096,
		Prompt:    prompt,
		Purpose:   "ingest.format_cleanup",
	})
	if err != nil {
		return "", err
	}
	return anthropic.StripMarkdownFences(strings.TrimSpace(resp.Text)), nil
}

func looksLikeHTML(content string, contentType string, sourcePath string) bool {
	if strings.EqualFold(contentType, "html") {
		return true
	}
	lowerPath := strings.ToLower(strings.TrimSpace(sourcePath))
	if strings.HasSuffix(lowerPath, ".html") || strings.HasSuffix(lowerPath, ".htm") {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	if strings.Contains(lower, "<!doctype html") || strings.Contains(lower, "<html") || strings.Contains(lower, "<body") {
		return true
	}
	return htmlTagRe.MatchString(lower)
}

var htmlTagRe = regexp.MustCompile(`</?(html|head|body|div|span|p|a|ul|ol|li|table|tr|td|th|h[1-6]|pre|code|section|article|main|header|footer)\b`)

func normalizeWhitespace(content string) string {
	content = strings.ReplaceAll(content, "\x00", "")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.ReplaceAll(content, "\t", "    ")
	content = multipleBlankLinesRe.ReplaceAllString(content, "\n\n")
	return strings.TrimSpace(content)
}

var multipleBlankLinesRe = regexp.MustCompile(`\n{3,}`)

func cleanupExtractedText(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			out = append(out, "")
			continue
		}
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if !shouldJoinLines(line, next) {
				break
			}
			line += " " + next
			i++
		}
		out = append(out, line)
	}
	return normalizeWhitespace(strings.Join(out, "\n"))
}

func shouldJoinLines(current string, next string) bool {
	if current == "" || next == "" {
		return false
	}
	if strings.HasPrefix(next, "#") || strings.HasPrefix(next, "- ") || strings.HasPrefix(next, "* ") || orderedListRe.MatchString(next) {
		return false
	}
	if strings.HasSuffix(current, ":") || strings.HasSuffix(current, "|") || strings.HasPrefix(current, "|") {
		return false
	}
	if strings.HasSuffix(current, "```") || strings.HasPrefix(current, "```") || strings.HasPrefix(next, "```") {
		return false
	}
	r := []rune(next)
	if len(r) == 0 {
		return false
	}
	if unicode.IsLower(r[0]) {
		return true
	}
	if unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) {
		last := []rune(current)
		if len(last) == 0 {
			return false
		}
		switch last[len(last)-1] {
		case '.', '!', '?':
			return false
		}
		return true
	}
	return false
}

var orderedListRe = regexp.MustCompile(`^\d+\.\s+`)

func readDurationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		slog.Warn("invalid formatting timeout env, using fallback", "key", key, "value", raw, "fallback", fallback)
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func htmlToMarkdown(input string) string {
	node, err := golanghtml.Parse(strings.NewReader(input))
	if err != nil {
		return cleanupExtractedText(stripTags(input))
	}
	body := findBody(node)
	if body == nil {
		body = node
	}
	var r markdownRenderer
	r.renderChildren(body)
	return r.String()
}

type markdownRenderer struct {
	buf       bytes.Buffer
	listDepth int
}

func (r *markdownRenderer) String() string {
	return normalizeWhitespace(r.buf.String())
}

func (r *markdownRenderer) write(s string) {
	r.buf.WriteString(s)
}

func (r *markdownRenderer) ensureBlankLine() {
	text := r.buf.String()
	if strings.HasSuffix(text, "\n\n") {
		return
	}
	if strings.HasSuffix(text, "\n") {
		r.write("\n")
		return
	}
	if text != "" {
		r.write("\n\n")
	}
}

func (r *markdownRenderer) renderChildren(n *golanghtml.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		r.render(child)
	}
}

func (r *markdownRenderer) render(n *golanghtml.Node) {
	if n == nil {
		return
	}
	switch n.Type {
	case golanghtml.TextNode:
		text := collapseInlineWhitespace(html.UnescapeString(n.Data))
		if text != "" {
			r.write(text)
		}
	case golanghtml.ElementNode:
		switch n.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			r.ensureBlankLine()
			level := 1
			fmt.Sscanf(n.Data, "h%d", &level)
			r.write(strings.Repeat("#", level) + " ")
			r.renderInlineChildren(n)
			r.write("\n\n")
		case "p", "section", "article", "main", "header", "footer", "div":
			r.ensureBlankLine()
			r.renderChildren(n)
			r.write("\n\n")
		case "br":
			r.write("\n")
		case "ul", "ol":
			r.ensureBlankLine()
			r.listDepth++
			r.renderChildren(n)
			r.listDepth--
			r.write("\n")
		case "li":
			indent := strings.Repeat("  ", maxInt(0, r.listDepth-1))
			r.write(indent + "- ")
			r.renderInlineChildren(n)
			r.write("\n")
		case "pre":
			r.ensureBlankLine()
			code := strings.TrimSpace(extractText(n))
			if code != "" {
				r.write("```\n" + code + "\n```\n\n")
			}
		case "code":
			if n.Parent != nil && n.Parent.Data == "pre" {
				return
			}
			text := strings.TrimSpace(extractText(n))
			if text != "" {
				r.write("`" + text + "`")
			}
		case "a":
			text := strings.TrimSpace(extractText(n))
			href := attrValue(n, "href")
			if href != "" && text != "" {
				r.write("[" + text + "](" + href + ")")
			} else {
				r.renderInlineChildren(n)
			}
		case "strong", "b":
			text := strings.TrimSpace(extractText(n))
			if text != "" {
				r.write("**" + text + "**")
			}
		case "em", "i":
			text := strings.TrimSpace(extractText(n))
			if text != "" {
				r.write("*" + text + "*")
			}
		case "blockquote":
			r.ensureBlankLine()
			for _, line := range strings.Split(strings.TrimSpace(extractText(n)), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					r.write("> " + line + "\n")
				}
			}
			r.write("\n")
		case "table":
			r.ensureBlankLine()
			r.renderTable(n)
			r.write("\n")
		default:
			r.renderChildren(n)
		}
	}
}

func (r *markdownRenderer) renderInlineChildren(n *golanghtml.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		r.render(child)
	}
}

func (r *markdownRenderer) renderTable(n *golanghtml.Node) {
	var rows [][]string
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == golanghtml.ElementNode && (child.Data == "tbody" || child.Data == "thead" || child.Data == "tfoot") {
			for tr := child.FirstChild; tr != nil; tr = tr.NextSibling {
				if tr.Type == golanghtml.ElementNode && tr.Data == "tr" {
					rows = append(rows, extractTableRow(tr))
				}
			}
		}
		if child.Type == golanghtml.ElementNode && child.Data == "tr" {
			rows = append(rows, extractTableRow(child))
		}
	}
	if len(rows) == 0 {
		return
	}
	for i, row := range rows {
		r.write("| " + strings.Join(row, " | ") + " |\n")
		if i == 0 {
			separators := make([]string, len(row))
			for j := range separators {
				separators[j] = "---"
			}
			r.write("| " + strings.Join(separators, " | ") + " |\n")
		}
	}
}

func extractTableRow(tr *golanghtml.Node) []string {
	var row []string
	for cell := tr.FirstChild; cell != nil; cell = cell.NextSibling {
		if cell.Type == golanghtml.ElementNode && (cell.Data == "td" || cell.Data == "th") {
			row = append(row, strings.TrimSpace(collapseInlineWhitespace(extractText(cell))))
		}
	}
	return row
}

func findBody(n *golanghtml.Node) *golanghtml.Node {
	if n == nil {
		return nil
	}
	if n.Type == golanghtml.ElementNode && n.Data == "body" {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if body := findBody(child); body != nil {
			return body
		}
	}
	return nil
}

func extractText(n *golanghtml.Node) string {
	var parts []string
	var walk func(*golanghtml.Node)
	walk = func(node *golanghtml.Node) {
		if node == nil {
			return
		}
		if node.Type == golanghtml.TextNode {
			text := collapseInlineWhitespace(html.UnescapeString(node.Data))
			if text != "" {
				parts = append(parts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}

func stripTags(input string) string {
	inTag := false
	var b strings.Builder
	for _, r := range input {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return html.UnescapeString(b.String())
}

func attrValue(n *golanghtml.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func collapseInlineWhitespace(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return whitespaceRe.ReplaceAllString(s, " ")
}

var whitespaceRe = regexp.MustCompile(`\s+`)
var wordBeforeMarkdownMarkerRe = regexp.MustCompile(`([[:alnum:]])(\*\*|\*|` + "`" + `|\[)`)
var markdownMarkerBeforeWordRe = regexp.MustCompile(`(\*\*|\*|` + "`" + `|\])([[:alnum:]])`)
var spacedBoldRe = regexp.MustCompile(`\*\*\s+([^*]+?)\s+\*\*`)
var spacedItalicRe = regexp.MustCompile(`\*\s+([^*]+?)\s+\*`)
var spacedCodeRe = regexp.MustCompile("`\\s+([^`]+?)\\s+`")

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalizeMarkdownSpacing(content string) string {
	content = wordBeforeMarkdownMarkerRe.ReplaceAllString(content, "$1 $2")
	content = markdownMarkerBeforeWordRe.ReplaceAllString(content, "$1 $2")
	content = spacedBoldRe.ReplaceAllString(content, "**$1**")
	content = spacedItalicRe.ReplaceAllString(content, "*$1*")
	content = spacedCodeRe.ReplaceAllString(content, "`$1`")
	return content
}
