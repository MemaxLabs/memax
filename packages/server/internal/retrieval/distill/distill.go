package distill

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// QueryFilters holds structured constraints extracted from the query.
type QueryFilters struct {
	Temporal *TemporalFilter `json:"temporal,omitempty"`
	People   []string        `json:"people,omitempty"`
	Authors  []string        `json:"authors,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	Source   string          `json:"source,omitempty"`
	Hub      string          `json:"hub,omitempty"`
}

// TemporalFilter represents a date range extracted from the query.
type TemporalFilter struct {
	Start string `json:"start,omitempty"` // ISO-8601 date (YYYY-MM-DD)
	End   string `json:"end,omitempty"`   // ISO-8601 date (YYYY-MM-DD)
	Raw   string `json:"raw,omitempty"`   // original expression (e.g., "last Tuesday", "四月")
}

// Result is the output of query understanding.
type Result struct {
	Query           string        `json:"query"`
	Keywords        []string      `json:"keywords"`
	InformationNeed string        `json:"information_need"`
	CodeReferences  []string      `json:"code_references"`
	Confidence      float64       `json:"confidence"`
	Filters         *QueryFilters `json:"filters,omitempty"`
}

// Distiller performs query understanding — extracting a semantic search query
// and structured filters (temporal, people, authors, kind, source, hub) from raw input.
// Always calls the LLM for all queries (no token threshold).
type Distiller struct {
	client *anthropic.Client
	model  string
}

// New returns a Distiller. Returns nil if client is nil (LLM not configured).
func New(client *anthropic.Client) *Distiller {
	if client == nil {
		return nil
	}
	return &Distiller{
		client: client,
		model:  anthropic.ModelFromEnv("DISTILL_MODEL"),
	}
}

const longQueryThreshold = 80 // tokens — above this, use the full distillation prompt

// Distill processes a raw query into a semantic search query + structured filters.
// Always calls the LLM. Falls back to regex-based truncation if the LLM call fails.
func (d *Distiller) Distill(rawQuery string, source string, cwd string, now time.Time, userName string, timezone string) *Result {
	return d.DistillContext(context.Background(), rawQuery, source, cwd, now, userName, timezone)
}

func (d *Distiller) DistillContext(ctx context.Context, rawQuery string, source string, cwd string, now time.Time, userName string, timezone string) *Result {
	tokens := estimateTokens(rawQuery)

	var result *Result
	var err error
	if tokens > longQueryThreshold {
		result, err = d.callFullLLM(ctx, rawQuery, source, cwd, now, userName, timezone)
	} else {
		result, err = d.callCompactLLM(ctx, rawQuery, now, userName, timezone)
	}

	if err != nil {
		slog.Warn("query understanding LLM call failed, using fallback", "error", err)
		return truncationFallback(rawQuery)
	}
	return result
}

// callCompactLLM is the prompt for short queries — focused on filter extraction.
func (d *Distiller) callCompactLLM(ctx context.Context, rawQuery string, now time.Time, userName string, timezone string) (*Result, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	// Convert to user's local time so date resolution is correct
	if loc, err := time.LoadLocation(timezone); err == nil {
		now = now.In(loc)
	}
	weekday := now.Weekday().String()

	prompt := fmt.Sprintf(`You are a query understanding engine for a knowledge retrieval system.
Today: %s (%s)
User: %s
Timezone: %s

Given the user's search query, extract:
1. "query": Core semantic search terms. Strip names, dates, and structural words. Keep only the topic/concept the user is looking for. 3-30 words. If the query has no topical constraint (only asks who/when/where with no subject), use an empty string.
2. "keywords": Exact identifiers that should match verbatim (function names, error codes, etc.). May be empty.
3. "information_need": One of: explanation, procedure, decision_rationale, reference, debugging, general
4. "filters": Structured constraints. All fields optional.
   - "temporal": If the query references a date/time (absolute or relative), resolve to ISO-8601 start/end. For a single day, end = start + 1 day. For a month, end = start of next month.
   - "people": People mentioned as subjects inside the memory content. Include the original name PLUS romanized/transliterated variants and common short forms (e.g., "小明" → ["小明", "xiaoming"]; "John Smith" → ["john", "smith"]).
   - "authors": People who created, pushed, saved, wrote, or added the memory. Use this for queries like "what did Jiahao push/save/add yesterday". Include original name, romanized variants, and common short forms.
   - "kind": If targeting a memory kind: episodic, semantic, procedural, rationale
   - "source": If targeting a tool/agent (e.g., "claude-code", "cursor")
   - "hub": ONLY when the user explicitly references a hub, team, or workspace — NOT when they mention a product or project name. Look for explicit signals: "in the X hub", "from the X team", "X workspace", "X hub 里的". Product/project names that happen to match a hub should stay in the query, not become hub filters.

Examples:
"小明四月说过什么限时来着？" → {"query": "限时 限购 预购", "keywords": [], "information_need": "reference", "confidence": 0.85, "filters": {"temporal": {"start": "2026-04-01", "end": "2026-05-01", "raw": "四月"}, "people": ["小明", "xiaoming"]}}
"What did John push in May?" → {"query": "", "keywords": [], "information_need": "general", "confidence": 0.8, "filters": {"temporal": {"start": "2026-05-01", "end": "2026-06-01", "raw": "May"}, "authors": ["john"]}}
"what did Ming say about the bug in the memax-test hub?" → {"query": "bug issue", "keywords": [], "information_need": "debugging", "confidence": 0.85, "filters": {"people": ["ming", "小明"], "hub": "memax-test"}}
"memax 用什么 email 选型？" → {"query": "email 选型 email provider selection", "keywords": ["email"], "information_need": "decision_rationale", "confidence": 0.85, "filters": {}}
"how to deploy to production" → {"query": "deploy to production", "keywords": [], "information_need": "procedure", "confidence": 0.9, "filters": {}}
"昨天的会议讨论了什么" → {"query": "会议讨论", "keywords": [], "information_need": "reference", "confidence": 0.85, "filters": {"temporal": {"start": "%s", "end": "%s", "raw": "昨天"}, "kind": "episodic"}}

Query: "%s"
Respond with JSON only.`,
		now.Format("2006-01-02"), weekday, userName, timezone,
		now.AddDate(0, 0, -1).Format("2006-01-02"), now.Format("2006-01-02"),
		strings.ReplaceAll(rawQuery, `"`, `\"`))

	resp, err := d.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     d.model,
		MaxTokens: 300,
		Prompt:    prompt,
		Purpose:   "retrieval.query_understanding",
	})
	if err != nil {
		return nil, err
	}

	text := anthropic.ExtractJSONObject(resp.Text)

	var result Result
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse query understanding result: %w", err)
	}
	if result.Confidence == 0 {
		result.Confidence = 0.7
	}
	return &result, nil
}

// callFullLLM is the prompt for long queries — full distillation + filter extraction.
func (d *Distiller) callFullLLM(ctx context.Context, rawQuery string, source string, cwd string, now time.Time, userName string, timezone string) (*Result, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	if loc, err := time.LoadLocation(timezone); err == nil {
		now = now.In(loc)
	}
	rawPrompt := anthropic.TruncateMiddleToTokens(rawQuery, 8000)
	weekday := now.Weekday().String()

	prompt := fmt.Sprintf(`You are a query understanding engine for a knowledge retrieval system. Given a raw user prompt (which may be very long, contain code, or include conversational context), extract the core information need and any structured constraints.

Today: %s (%s)
User: %s
Timezone: %s

Rules:
1. The distilled query must be 10-40 words. It is used for semantic similarity search. If the query has no topical constraint (only asks who/when/where with no subject), use an empty string.
2. Preserve specific identifiers verbatim: function names, file paths, error codes, package names.
3. Strip: greetings, politeness, code that is context (not the question), repeated information.
4. If the user is asking about THEIR code (debugging), the query should describe the problem/concept, not quote the code.
5. If the user's prompt contains multiple questions, focus on the primary one.
6. Return keywords that should match exactly in stored documents.
7. Extract structured filters where present:
   - "temporal": Dates/times mentioned (absolute or relative) → ISO-8601 start/end bounds
   - "people": People mentioned as subjects inside the memory content, with romanized variants
   - "authors": People who created, pushed, saved, wrote, or added the memory
   - "kind": If targeting a memory kind (episodic, semantic, procedural, rationale)
   - "source": If targeting a specific tool/agent
   - "hub": ONLY when the user explicitly references a hub, team, or workspace — NOT when they mention a product or project name. Explicit signals: "in the X hub", "from the X team", "X workspace", "X hub 里的". Product/project names that happen to match a hub should stay in the query, not become hub filters.

Examples:

Input: "I'm working on the auth middleware in packages/server and I keep getting a nil pointer when the OAuth token expires. The error is at handler/authz.go:290. Can you find any notes about how we handle token refresh?"
→ {"query": "OAuth token refresh nil pointer auth middleware", "keywords": ["authz.go", "handler/authz.go"], "information_need": "debugging", "code_references": ["handler/authz.go"], "confidence": 0.9, "filters": {}}

Input: "Hey claude, what did Jiahao push about the API key rotation last week? I think it was in the memax-test hub"
→ {"query": "API key rotation", "keywords": [], "information_need": "reference", "confidence": 0.85, "filters": {"temporal": {"start": "%s", "end": "%s", "raw": "last week"}, "authors": ["jiahao"], "hub": "memax-test"}}

Input: "我在找之前关于 pgvector 向量检索的技术选型文档，好像是三月份写的"
→ {"query": "pgvector 向量检索 技术选型", "keywords": ["pgvector"], "information_need": "decision_rationale", "confidence": 0.85, "filters": {"temporal": {"start": "2026-03-01", "end": "2026-04-01", "raw": "三月份"}}}

Input: "what do we use for email for memax?"
→ {"query": "email provider selection", "keywords": ["email"], "information_need": "decision_rationale", "confidence": 0.85, "filters": {}}

Input: "context: I'm in github.com/MemaxLabs/memax-internal working on the retrieval pipeline. The RRF fusion scores look wrong after the latest change — the top result is scoring 0.016 when it used to be 0.05. Where are our notes about how RRF k-parameter tuning works?"
→ {"query": "RRF k-parameter tuning fusion scoring", "keywords": ["RRF"], "information_need": "reference", "confidence": 0.9, "filters": {}}

Raw prompt:
---
%s
---

Working directory: %s
Source: %s

Respond with JSON only:
{"query": "concise search query", "keywords": ["specific", "identifiers"], "information_need": "explanation|procedure|decision_rationale|reference|debugging|general", "code_references": ["file/paths"], "confidence": 0.85, "filters": {"temporal": {"start": "2026-04-04", "end": "2026-04-05", "raw": "April 4th"}, "people": ["sarah"], "authors": ["john"], "kind": "", "source": "", "hub": ""}}`,
		now.Format("2006-01-02"), weekday, userName, timezone,
		now.AddDate(0, 0, -7).Format("2006-01-02"), now.Format("2006-01-02"),
		rawPrompt, cwd, source)

	resp, err := d.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     d.model,
		MaxTokens: 400,
		Prompt:    prompt,
		Purpose:   "retrieval.query_distill",
	})
	if err != nil {
		return nil, err
	}

	text := anthropic.ExtractJSONObject(resp.Text)

	var result Result
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse distilled result: %w", err)
	}
	return &result, nil
}

// truncationFallback is a deterministic, free, instant fallback when LLM is unavailable.
func truncationFallback(rawQuery string) *Result {
	// Strip code blocks
	codeBlockRe := regexp.MustCompile("(?s)```.*?```")
	cleaned := codeBlockRe.ReplaceAllString(rawQuery, "")

	// Strip quoted lines
	lines := strings.Split(cleaned, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ">") && trimmed != "" {
			kept = append(kept, trimmed)
		}
	}

	// Take last 5 sentences (the question is usually at the end)
	text := strings.Join(kept, " ")
	sentences := splitSentences(text)
	if len(sentences) > 5 {
		sentences = sentences[len(sentences)-5:]
	}
	query := strings.Join(sentences, " ")

	// Truncate to ~200 tokens worth of text
	if len(query) > 800 {
		query = query[len(query)-800:]
	}

	// Extract file paths and function-like references
	keywords := extractIdentifiers(rawQuery)

	return &Result{
		Query:           strings.TrimSpace(query),
		Keywords:        keywords,
		InformationNeed: "general",
		CodeReferences:  extractFilePaths(rawQuery),
		Confidence:      0.4, // low confidence — truncation is a rough heuristic
	}
}

func estimateTokens(s string) int {
	// Fast approximation: ~4 chars per token for English, ~3.5 for code
	if looksLikeCode(s) {
		return int(float64(len(s)) / 3.5)
	}
	return len(s) / 4
}

func looksLikeCode(s string) bool {
	codeSignals := []string{"{", "}", "()", "//", "#include", "func ", "def ", "class ", "import "}
	count := 0
	for _, sig := range codeSignals {
		if strings.Contains(s, sig) {
			count++
		}
	}
	return count >= 2
}

func splitSentences(text string) []string {
	re := regexp.MustCompile(`[.!?]+\s+`)
	parts := re.Split(text, -1)
	var sentences []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 5 {
			sentences = append(sentences, p)
		}
	}
	return sentences
}

var filePathRe = regexp.MustCompile(`[\w./\-]+\.\w{1,10}`)

func extractFilePaths(text string) []string {
	matches := filePathRe.FindAllString(text, 10)
	var paths []string
	seen := map[string]bool{}
	for _, m := range matches {
		if strings.Contains(m, "/") && !seen[m] {
			seen[m] = true
			paths = append(paths, m)
		}
	}
	return paths
}

func extractIdentifiers(text string) []string {
	// Look for camelCase, snake_case, and function() patterns
	re := regexp.MustCompile(`\b[a-z][a-zA-Z0-9]*[A-Z]\w*\b|\b\w+_\w+\b|\b\w+\(\)`)
	matches := re.FindAllString(text, 20)
	seen := map[string]bool{}
	var unique []string
	for _, m := range matches {
		if !seen[m] && len(m) > 3 {
			seen[m] = true
			unique = append(unique, m)
		}
	}
	if len(unique) > 8 {
		unique = unique[:8]
	}
	return unique
}
