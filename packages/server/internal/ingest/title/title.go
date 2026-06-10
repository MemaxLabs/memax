package title

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

const maxTitleLength = 80

type Resolver struct {
	client *anthropic.Client
	model  string
}

type ResolveInput struct {
	Title       string
	SourcePath  string
	Content     string
	ContentType string
	Hint        string
}

type ResolveResult struct {
	Title  string
	Reason string
}

func New(client *anthropic.Client) *Resolver {
	if client == nil {
		return nil
	}
	return &Resolver{client: client, model: anthropic.ModelFromEnv("TITLE_MODEL")}
}

func PrepareIncoming(title, sourcePath, content, contentType string) string {
	title = sanitizeTitle(title)
	if title == "" {
		if isMeaningfulFilename(sourcePath) {
			return cleanFilenameTitle(sourcePath)
		}
		if contentType != "pdf" && contentType != "image" {
			if generated := GenerateFromContent(content); isMeaningfulGeneratedTitle(generated) {
				return generated
			}
		}
		return fallbackTitle(sourcePath, contentType)
	}

	if isFilenameDerivedTitle(title, sourcePath) {
		if isMeaningfulFilename(sourcePath) {
			return cleanFilenameTitle(sourcePath)
		}
		if contentType != "pdf" && contentType != "image" {
			if generated := GenerateFromContent(content); isMeaningfulGeneratedTitle(generated) {
				return generated
			}
		}
		return fallbackTitle(sourcePath, contentType)
	}

	return title
}

func (r *Resolver) ResolveContext(ctx context.Context, in ResolveInput) ResolveResult {
	return resolveContext(ctx, r, in, true)
}

func ResolveDeterministic(in ResolveInput) ResolveResult {
	return resolveContext(context.Background(), nil, in, false)
}

func ResolveGeneratedCandidate(in ResolveInput, candidate string) ResolveResult {
	prepared := PrepareIncoming(in.Title, in.SourcePath, in.Content, in.ContentType)
	proposed := sanitizeCandidate(candidate)
	if proposed == "" {
		return ResolveResult{Title: prepared, Reason: "candidate_rejected"}
	}
	if !shouldAutoRefine(in.Title, in.SourcePath) && !isContextualTitleUpgrade(prepared, proposed) {
		return ResolveResult{Title: prepared, Reason: "user_preserved"}
	}
	if isContextualTitleUpgrade(prepared, proposed) && !shouldAutoRefine(in.Title, in.SourcePath) {
		return ResolveResult{Title: proposed, Reason: "summary_llm_context_upgrade"}
	}
	if proposed != "" {
		return ResolveResult{Title: proposed, Reason: "summary_llm_generated"}
	}
	return ResolveResult{Title: prepared, Reason: "candidate_rejected"}
}

func resolveContext(ctx context.Context, r *Resolver, in ResolveInput, allowLLM bool) ResolveResult {
	prepared := PrepareIncoming(in.Title, in.SourcePath, in.Content, in.ContentType)
	if !shouldAutoRefine(in.Title, in.SourcePath) {
		return ResolveResult{Title: prepared, Reason: "user_preserved"}
	}
	if isMeaningfulFilename(in.SourcePath) {
		return ResolveResult{Title: cleanFilenameTitle(in.SourcePath), Reason: "filename_cleaned"}
	}

	if generated := GenerateFromContent(in.Content); isMeaningfulGeneratedTitle(generated) {
		return ResolveResult{Title: generated, Reason: "content_generated"}
	}

	if allowLLM && r != nil && shouldUseLLM(in.Content, in.ContentType) {
		if proposed := r.proposeWithLLM(ctx, in); proposed != "" {
			return ResolveResult{Title: proposed, Reason: "llm_generated"}
		}
	}

	return ResolveResult{Title: fallbackTitle(in.SourcePath, in.ContentType), Reason: "fallback_generic"}
}

func GenerateFromContent(content string) string {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line == "```" {
			continue
		}
		if shouldSkipContentTitleLine(line) {
			continue
		}
		line = stripContentTitlePrefix(line)
		line = strings.TrimLeft(line, "#>*-0123456789.) ")
		line = sanitizeTitle(line)
		if line == "" || !isMeaningfulGeneratedTitle(line) {
			continue
		}
		return line
	}
	return "Untitled"
}

func shouldAutoRefine(title, sourcePath string) bool {
	trimmed := sanitizeTitle(title)
	return trimmed == "" || isFilenameDerivedTitle(trimmed, sourcePath) || IsWeakTitle(trimmed)
}

func IsWeakTitle(title string) bool {
	title = sanitizeTitle(title)
	if title == "" {
		return true
	}
	return isGenericFallbackTitle(title) || isProvenanceOnlyTitle(title) || isBoilerplateDerivedTitle(title)
}

func isGenericFallbackTitle(title string) bool {
	switch strings.ToLower(sanitizeTitle(title)) {
	case "untitled", "screenshot", "document", "image", "note", "file", "uploaded file":
		return true
	default:
		return false
	}
}

func isProvenanceOnlyTitle(title string) bool {
	return provenanceOnlyTitleRe.MatchString(sanitizeTitle(title))
}

func isBoilerplateDerivedTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	normalized = strings.TrimPrefix(normalized, "body:")
	normalized = strings.TrimSpace(normalized)
	if emailGreetingTitleRe.MatchString(normalized) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), "body:")
}

func isContextualTitleUpgrade(current string, candidate string) bool {
	current = sanitizeTitle(current)
	candidate = sanitizeTitle(candidate)
	if current == "" || candidate == "" || IsWeakTitle(current) || IsWeakTitle(candidate) {
		return false
	}
	if hasExplicitScope(current) || !hasExplicitScope(candidate) {
		return false
	}
	return tokenOverlapCount(current, candidate) >= 3
}

func hasExplicitScope(title string) bool {
	if strings.Contains(title, ":") {
		return true
	}
	words := strings.Fields(title)
	return len(words) > 0 && isLikelyEntityToken(words[0])
}

func isLikelyEntityToken(word string) bool {
	word = strings.Trim(word, " -_:;,.[](){}")
	if word == "" {
		return false
	}
	if strings.EqualFold(word, "CLI") || strings.EqualFold(word, "API") || strings.EqualFold(word, "SDK") {
		return false
	}
	return isAllUpper(word) || (unicode.IsUpper([]rune(word)[0]) && strings.ToLower(word) != strings.ToLower(humanizeWord(word, 0)))
}

func tokenOverlapCount(a string, b string) int {
	aTokens := titleTokenSet(a)
	bTokens := titleTokenSet(b)
	overlap := 0
	for token := range aTokens {
		if bTokens[token] {
			overlap++
		}
	}
	return overlap
}

func titleTokenSet(title string) map[string]bool {
	tokens := make(map[string]bool)
	for _, raw := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		token := strings.TrimSpace(raw)
		if len(token) < 3 || stopwordSet[token] || genericTokenSet[token] {
			continue
		}
		tokens[token] = true
	}
	return tokens
}

func shouldSkipContentTitleLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if lower == "body:" || strings.HasPrefix(lower, "body: ") {
		return true
	}
	if emailGreetingTitleRe.MatchString(lower) {
		return true
	}
	if strings.HasPrefix(lower, "from:") || strings.HasPrefix(lower, "to:") || strings.HasPrefix(lower, "cc:") || strings.HasPrefix(lower, "bcc:") || strings.HasPrefix(lower, "date:") {
		return true
	}
	return false
}

func stripContentTitlePrefix(line string) string {
	for _, prefix := range []string{"Subject:", "Title:"} {
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(prefix)) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return line
}

func shouldUseLLM(content, contentType string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if strings.Contains(content, "text extraction failed") {
		return false
	}
	if contentType == "image" || contentType == "pdf" {
		return len(content) >= 40
	}
	return len(content) >= 80
}

func (r *Resolver) proposeWithLLM(ctx context.Context, in ResolveInput) string {
	if r == nil || r.client == nil {
		return ""
	}
	content := strings.TrimSpace(in.Content)
	if len(content) > 4000 {
		content = content[:4000] + "\n\n[truncated]"
	}
	hintLine := ""
	if in.Hint != "" {
		hintLine = fmt.Sprintf("Hint: %s\n", in.Hint)
	}
	prompt := fmt.Sprintf(`You are naming a saved memory for browsing in Memax.
Return JSON only: {"title":"..."}

Rules:
- 2 to 12 words. Prefer specificity over brevity.
- Under 80 characters.
- Make it specific and easy to browse later.
- Name the main product, company, person, system, or topic when known.
- Do not include file extensions.
- Prefer the actual subject of the content over generic labels like Screenshot or Document.
- If the filename is useful, you may incorporate it in cleaner form.
- Do not invent facts not supported by the content.
- Never use source labels, timestamps, email greetings, or first-line fragments as titles.
- For emails or letters, include the essential ask, action, decision, or request. Mention sender/recipient only when it helps identify who is doing or asking what.

Examples:
- Bad: Session capture — 2026-04-11
  Good: Memax AI Ask context tuning
- Bad: Body: Hi Mobility Contact / Fragomen Attorney
  Good: Fragomen attorney requests mobility contact intro
- Bad: CLI hub references now support personal alias
  Good: Memax CLI: personal hub alias support

Filename: %s
Content type: %s
%sContent:
---
%s
---`, filepath.Base(strings.TrimSpace(in.SourcePath)), in.ContentType, hintLine, content)

	resp, err := r.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     r.model,
		MaxTokens: 120,
		Prompt:    prompt,
		Purpose:   "ingest.title_generate",
	})
	if err != nil {
		return ""
	}

	var parsed struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(anthropic.ExtractJSONObject(resp.Text)), &parsed); err == nil {
		return sanitizeCandidate(parsed.Title)
	}
	return sanitizeCandidate(resp.Text)
}

func sanitizeCandidate(raw string) string {
	candidate := sanitizeTitle(anthropic.StripMarkdownFences(raw))
	if !isMeaningfulGeneratedTitle(candidate) {
		return ""
	}
	return candidate
}

func isFilenameDerivedTitle(title, sourcePath string) bool {
	if title == "" || sourcePath == "" || looksLikeURL(sourcePath) {
		return false
	}
	t := comparable(title)
	if t == "" {
		return false
	}
	base := filepath.Base(strings.TrimSpace(sourcePath))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return t == comparable(base) || t == comparable(stem) || t == comparable(cleanFilenameTitle(sourcePath))
}

func isMeaningfulFilename(sourcePath string) bool {
	if sourcePath == "" || looksLikeURL(sourcePath) {
		return false
	}
	base := filepath.Base(strings.TrimSpace(sourcePath))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		return false
	}
	if uuidRe.MatchString(stem) || longHexRe.MatchString(stem) || timestampOnlyRe.MatchString(stem) {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(stem, "_", " "))
	if screenshotRe.MatchString(lower) || cameraRe.MatchString(lower) || scanRe.MatchString(lower) {
		return false
	}
	cleaned := cleanFilenameTitle(sourcePath)
	if cleaned == "" {
		return false
	}
	meaningful := 0
	for _, token := range strings.Fields(strings.ToLower(cleaned)) {
		if len(token) < 2 {
			continue
		}
		if genericTokenSet[token] {
			continue
		}
		if hasLetter(token) {
			meaningful++
		}
	}
	return meaningful >= 2 || (meaningful == 1 && len(strings.Fields(cleaned)) == 1 && len(cleaned) >= 5)
}

func cleanFilenameTitle(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}
	base := filepath.Base(strings.TrimSpace(sourcePath))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = separatorRe.ReplaceAllString(stem, " ")
	stem = copySuffixRe.ReplaceAllString(stem, "")
	stem = spaceRe.ReplaceAllString(strings.TrimSpace(stem), " ")
	if stem == "" {
		return ""
	}
	words := strings.Fields(stem)
	for i, word := range words {
		words[i] = humanizeWord(word, i)
	}
	return sanitizeTitle(strings.Join(words, " "))
}

func fallbackTitle(sourcePath, contentType string) string {
	switch contentType {
	case "image":
		return "Screenshot"
	case "pdf":
		return "Document"
	}
	if isMeaningfulFilename(sourcePath) {
		return cleanFilenameTitle(sourcePath)
	}
	if sourcePath != "" {
		lower := strings.ToLower(filepath.Base(sourcePath))
		if strings.Contains(lower, "screenshot") {
			return "Screenshot"
		}
	}
	return "Untitled"
}

func sanitizeTitle(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	raw = spaceRe.ReplaceAllString(raw, " ")
	raw = strings.Trim(raw, "-_:;,.[](){}")
	if raw == "" {
		return ""
	}
	if utf8.RuneCountInString(raw) > maxTitleLength {
		runes := []rune(raw)
		raw = strings.TrimSpace(string(runes[:maxTitleLength]))
	}
	return raw
}

func isMeaningfulGeneratedTitle(title string) bool {
	title = sanitizeTitle(title)
	if IsWeakTitle(title) {
		return false
	}
	lower := strings.ToLower(title)
	if strings.Contains(lower, "text extraction failed") {
		return false
	}
	if !strings.Contains(title, " ") && longBase64ishRe.MatchString(title) {
		return false
	}
	letters := 0
	for _, r := range title {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 3
}

func comparable(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func humanizeWord(word string, idx int) string {
	if word == "" {
		return word
	}
	lower := strings.ToLower(word)
	if idx > 0 && stopwordSet[lower] {
		return lower
	}
	if isAllUpper(word) || hasDigit(word) {
		return strings.ToUpper(word)
	}
	runes := []rune(lower)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func isAllUpper(s string) bool {
	hasUpper := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			if !unicode.IsUpper(r) {
				return false
			}
			hasUpper = true
		}
	}
	return hasUpper
}

func looksLikeURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

var (
	spaceRe               = regexp.MustCompile(`\s+`)
	separatorRe           = regexp.MustCompile(`[_\-.]+`)
	copySuffixRe          = regexp.MustCompile(`(?i)\s*\((copy|final|edited|\d+)\)$`)
	uuidRe                = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	longHexRe             = regexp.MustCompile(`(?i)^[0-9a-f]{16,}$`)
	timestampOnlyRe       = regexp.MustCompile(`^[0-9_\-:. ]{8,}$`)
	screenshotRe          = regexp.MustCompile(`(?i)^(screenshot|screen shot)(\s|$)`)
	cameraRe              = regexp.MustCompile(`(?i)^(img|dsc|pxl|mvimg|image|photo)[ _-]*\d`)
	scanRe                = regexp.MustCompile(`(?i)^(scan|scanned|document|attachment|file)[ _-]*\d`)
	longBase64ishRe       = regexp.MustCompile(`^[A-Za-z0-9+/=]{24,}$`)
	provenanceOnlyTitleRe = regexp.MustCompile(`(?i)^(session capture|session|capture|conversation|chat|note|memory)(\s*[—-]\s*|\s+)\d{4}-\d{2}-\d{2}($|\b)`)
	emailGreetingTitleRe  = regexp.MustCompile(`(?i)^(hi|hello|dear|greetings)\b`)
	stopwordSet           = map[string]bool{"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true, "from": true, "in": true, "of": true, "on": true, "or": true, "the": true, "to": true, "vs": true, "with": true}
	genericTokenSet       = map[string]bool{"attachment": true, "copy": true, "document": true, "edited": true, "export": true, "file": true, "final": true, "image": true, "photo": true, "scan": true, "scanned": true, "screenshot": true, "untitled": true}
)
