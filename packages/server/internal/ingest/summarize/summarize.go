package summarize

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// jsonFieldMarker matches JSON field syntax for metadata keys, with optional
// whitespace around the colon: "title" : or "summary":
var jsonFieldMarker = regexp.MustCompile(`"(?:title|summary)"\s*:`)

var (
	markdownHeadingMarker = regexp.MustCompile(`(?m)^\s*#{1,6}\s+`)
	timestampLikeTitle    = regexp.MustCompile(`^\d{4}[-/]\d{1,2}[-/]\d{1,2}(?:[\sT]\d{1,2}:\d{2})?`)
	sourceLabelTitle      = regexp.MustCompile(`(?i)^(?:body|content|filename|file|source|subject|summary|title)\s*:`)
	promptLeakageMarker   = regexp.MustCompile(`(?i)\b(?:return json|here is|the title is|the summary is)\b`)
	emailGreetingTitle    = regexp.MustCompile(`(?i)^(?:hi|hello|hey|dear)\b[\s,].+`)
)

const summarizeMaxTokens = 512

var genericTitles = map[string]struct{}{
	"body":            {},
	"document":        {},
	"memory":          {},
	"note":            {},
	"screenshot":      {},
	"session capture": {},
	"summary":         {},
	"untitled":        {},
}

// Summarizer generates concise summaries of memories using an LLM.
type Summarizer struct {
	client *anthropic.Client
	model  string
}

type Result struct {
	Summary string
	Title   string
}

// New returns a Summarizer. Returns nil if client is nil (LLM not configured).
func New(client *anthropic.Client) *Summarizer {
	if client == nil {
		return nil
	}
	return &Summarizer{
		client: client,
		model:  anthropic.ModelFromEnv("SUMMARIZE_MODEL"),
	}
}

// Summarize generates a 2-4 sentence summary of the given content.
func (s *Summarizer) Summarize(title string, content string, classification string, hint string) string {
	return s.SummarizeContext(context.Background(), title, content, classification, hint)
}

func (s *Summarizer) SummarizeContext(ctx context.Context, title string, content string, classification string, hint string) string {
	return s.SummarizeWithTitleContext(ctx, title, content, classification, hint).Summary
}

func (s *Summarizer) SummarizeWithTitleContext(ctx context.Context, title string, content string, classification string, hint string) Result {
	return s.SummarizeWithRelatedContext(ctx, title, content, classification, hint, "")
}

// SummarizeWithRelatedContext generates a summary with optional related-context
// from the same hub. The relatedContext block (if non-empty) is injected into
// the LLM prompt so it can produce better titles and summaries for short or
// context-dependent memories.
//
// When relatedContext is non-empty, the summarize threshold is lowered to 50
// chars (from 200) because short notes benefit most from context-assisted
// summarization — that's the primary motivation for this feature.
func (s *Summarizer) SummarizeWithRelatedContext(ctx context.Context, title, content, classification, hint, relatedContext string) Result {
	if relatedContext != "" {
		if !ShouldEnrich(title, content) {
			return Result{}
		}
	} else if !ShouldSummarize(content) {
		return Result{}
	}

	truncated := content
	if len(truncated) > 12000 {
		truncated = truncated[:12000] + "\n\n[truncated]"
	}

	result, err := s.callLLM(ctx, title, truncated, classification, hint, relatedContext)
	if err != nil {
		slog.Warn("summary generation failed", "error", err, "title", title)
		return Result{}
	}
	return result
}

func ShouldSummarize(content string) bool {
	return len(content) >= 200
}

// ShouldEnrich returns true when the memory has enough substance for
// context-assisted summarization. Unlike ShouldSummarize (which gates
// on content length alone), this considers title + content together
// because short notes with a descriptive title are exactly the class
// of memories that benefit most from related-context enrichment.
// Example: title="Pricing Decision", content="Decided to go with $9/$19 tiers"
func ShouldEnrich(title, content string) bool {
	combined := strings.TrimSpace(title) + " " + strings.TrimSpace(content)
	return len(strings.TrimSpace(combined)) >= 20
}

func (s *Summarizer) callLLM(ctx context.Context, title string, content string, classification string, hint string, relatedContext string) (Result, error) {
	prompt := buildPrompt(title, content, classification, hint, relatedContext)
	resp, err := s.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     s.model,
		MaxTokens: summarizeMaxTokens,
		Prompt:    prompt,
		Purpose:   "ingest.summarize",
	})
	if err != nil {
		return Result{}, err
	}
	result, retry := parseResultForRetry(resp.Text)
	if !retry && resp.StopReason != "max_tokens" {
		return result, nil
	}

	slog.Warn("summarize.retry_attempted",
		"model", s.model,
		"stop_reason", resp.StopReason,
		"output_tokens", resp.OutputTokens,
		"output_len", len(resp.Text),
		"has_partial_result", result.Title != "" || result.Summary != "",
	)
	retryResp, err := s.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     s.model,
		MaxTokens: summarizeMaxTokens,
		Prompt:    buildRetryPrompt(prompt),
		Purpose:   "ingest.summarize.retry",
	})
	if err != nil {
		return Result{}, err
	}
	retryResult, retryAgain := parseResultForRetry(retryResp.Text)
	if retryAgain || retryResp.StopReason == "max_tokens" {
		slog.Warn("summarize.retry_failed",
			"model", s.model,
			"stop_reason", retryResp.StopReason,
			"output_tokens", retryResp.OutputTokens,
			"output_len", len(retryResp.Text),
		)
		return Result{}, nil
	}
	slog.Info("summarize.retry_succeeded",
		"model", s.model,
		"output_tokens", retryResp.OutputTokens,
		"has_title", retryResult.Title != "",
		"has_summary", retryResult.Summary != "",
	)
	return retryResult, nil
}

func buildPrompt(title string, content string, classification string, hint string, relatedContext string) string {
	hintLine := ""
	if hint != "" {
		hintLine = fmt.Sprintf("Context: %s\n", hint)
	}
	if relatedContext != "" {
		hintLine += "\n" + relatedContext + "\n"
	}

	return fmt.Sprintf(`You are memax, summarizing and naming a memory for future recall.
Return JSON only: {"title":"...","summary":"..."}

Summary rules:
1. 2-4 sentences, under 100 words. Use markdown.
2. Lead with the most important fact, decision, or conclusion.
3. **Bold** key terms, names, and concepts.
4. Preserve specifics: names, numbers, dates, file paths, tech terms.
5. Don't start with "This memory..." - just state what the user stored.
6. Decisions -> state the decision + reason. Processes -> state the outcome + key steps.
7. Write as if reminding the user of something they already know.
8. If context indicates ownership (e.g. "my resume"), reflect that in the summary.
9. Include the concrete names, products, people, dates, repositories, and decisions that would help this memory be found later — but only when they are supported by the content.

Title rules:
1. 2 to 12 words, under 80 characters. Prefer specificity over brevity.
2. Make it specific and easy to browse later.
3. Name the main product, company, person, system, or topic when known.
4. Do not include file extensions.
5. Do not invent facts not supported by the content.
6. Never use source labels, timestamps, email greetings, or first-line fragments as titles.
7. Avoid generic labels like Screenshot, Document, Note, Untitled, Session capture, or Body.
8. For emails or letters, include the essential ask, action, decision, or request. Mention sender/recipient only when it helps identify who is doing or asking what.

Title examples:
- Bad: Session capture — 2026-04-11
  Good: Memax AI Ask context tuning
- Bad: Body: Hi Mobility Contact / Fragomen Attorney
  Good: Fragomen attorney requests mobility contact intro
- Bad: CLI hub references now support personal alias
  Good: Memax CLI: personal hub alias support

%sTitle: %s
Classification: %s

Content:
---
%s
---

JSON:`, hintLine, title, classification, content)
}

func buildRetryPrompt(originalPrompt string) string {
	return fmt.Sprintf(`Your previous response was invalid. Return valid JSON only with exactly:
{"title":"...","summary":"..."}
No markdown fences. No extra text. No preamble.

Original task:
%s`, originalPrompt)
}

func parseResult(raw string) Result {
	result, _ := parseResultForRetry(raw)
	return result
}

func parseResultForRetry(raw string) (Result, bool) {
	raw = strings.TrimSpace(anthropic.StripMarkdownFences(raw))
	extracted := anthropic.ExtractJSONObject(raw)
	var parsed struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(extracted), &parsed); err == nil {
		result := Result{
			Title:   validateGeneratedTitle(parsed.Title),
			Summary: validateGeneratedSummary(parsed.Summary),
		}
		if parsed.Title != "" && result.Title == "" {
			slog.Info("summarize.validation_failed.title", "raw_len", len(parsed.Title))
		}
		if parsed.Summary != "" && result.Summary == "" {
			slog.Info("summarize.validation_failed.summary", "raw_len", len(parsed.Summary))
		}
		if result.Title == "" && result.Summary == "" {
			slog.Warn("summarize.metadata_dropped", "output_len", len(raw))
			return Result{}, true
		}
		return result, false
	}
	if looksLikeJSON(raw) || looksLikeJSON(extracted) {
		slog.Warn("summarize.parse_failed", "output_len", len(raw))
		return Result{}, true
	}
	return Result{Summary: validateGeneratedSummary(raw)}, false
}

// looksLikeJSON returns true if the string looks like it contains JSON protocol
// output — even if preceded by preamble text. This catches both direct JSON
// and the "Here is the JSON:\n{...}" failure mode where the model violates
// "JSON only" and then gets truncated.
func looksLikeJSON(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	// Preamble + JSON markers: the output contains JSON field syntax even
	// though it doesn't start with { — likely a model preamble before JSON.
	// Handles whitespace around the colon (e.g. "title" : "value").
	if jsonFieldMarker.MatchString(trimmed) {
		return true
	}
	return false
}

func looksLikeProtocolArtifact(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if looksLikeJSON(trimmed) || strings.HasPrefix(trimmed, "<") {
		return true
	}
	if strings.Contains(trimmed, "```") || jsonFieldMarker.MatchString(trimmed) {
		return true
	}
	return promptLeakageMarker.MatchString(lower)
}

func validateGeneratedTitle(raw string) string {
	if looksLikeProtocolArtifact(raw) {
		return ""
	}
	title := strings.TrimSpace(strings.Trim(raw, "\""))
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return ""
	}
	if len([]rune(title)) > 80 {
		return ""
	}
	if looksLikeProtocolArtifact(title) {
		return ""
	}
	if strings.Contains(title, "|") || markdownHeadingMarker.MatchString(title) {
		return ""
	}
	if _, ok := genericTitles[strings.ToLower(title)]; ok {
		return ""
	}
	if timestampLikeTitle.MatchString(title) || sourceLabelTitle.MatchString(title) || emailGreetingTitle.MatchString(title) {
		return ""
	}
	return title
}

func validateGeneratedSummary(raw string) string {
	if looksLikeProtocolArtifact(raw) {
		return ""
	}
	summary := strings.TrimSpace(strings.Trim(raw, "\""))
	if summary == "" {
		return ""
	}
	if len([]rune(summary)) > 1000 {
		return ""
	}
	if looksLikeProtocolArtifact(summary) {
		return ""
	}
	lower := strings.ToLower(summary)
	if strings.HasPrefix(lower, "this memory") {
		return ""
	}
	// Reject summaries that start with "Summary:" or "Title:" specifically —
	// these are the LLM failure shapes where the model outputs labeled fields
	// instead of prose. We do NOT use the broader sourceLabelTitle regex here
	// because it also matches "Subject:", "Source:", "File:" etc., which are
	// valid summary content for memories about emails or configs.
	if strings.HasPrefix(lower, "summary:") || strings.HasPrefix(lower, "title:") {
		return ""
	}
	if strings.HasSuffix(summary, "\\") || strings.HasSuffix(summary, `\"`) || strings.HasSuffix(summary, "{") || strings.HasSuffix(summary, "[") {
		return ""
	}
	return summary
}
