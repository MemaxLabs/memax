package categorize

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

type Categorizer struct {
	client *anthropic.Client
	model  string
}

func New(client *anthropic.Client) *Categorizer {
	if client == nil {
		return nil
	}
	return &Categorizer{
		client: client,
		model:  anthropic.ModelFromEnv("CATEGORIZE_MODEL"),
	}
}

func IsValidKind(kind string) bool {
	switch kind {
	case model.MemoryKindEpisodic, model.MemoryKindSemantic, model.MemoryKindProcedural, model.MemoryKindRationale:
		return true
	default:
		return false
	}
}

func IsValidStability(stability string) bool {
	switch stability {
	case model.MemoryStabilityVolatile, model.MemoryStabilityEvolving, model.MemoryStabilityStable:
		return true
	default:
		return false
	}
}

type ClassifyResult struct {
	Kind       string   `json:"kind"`
	Stability  string   `json:"stability"`
	Tags       []string `json:"tags"`
	TopicID    string   `json:"topic_id,omitempty"`
	EventDates []string `json:"event_dates,omitempty"` // ISO-8601 dates mentioned in content
}

type TopicHint struct {
	ID   string
	Name string
	Path string
}

func (c *Categorizer) Categorize(title string, content string) string {
	result := c.Classify(title, content, "")
	if result == nil {
		return ""
	}
	return result.Kind
}

func (c *Categorizer) Classify(title string, content string, hint string) *ClassifyResult {
	return c.ClassifyContext(context.Background(), title, content, hint, time.Time{}, "")
}

func (c *Categorizer) ClassifyContext(ctx context.Context, title string, content string, hint string, now time.Time, authorName string) *ClassifyResult {
	return c.ClassifyWithTopicsContext(ctx, title, content, hint, nil, now, authorName)
}

func (c *Categorizer) ClassifyWithTopics(title, content, hint string, topics []TopicHint) *ClassifyResult {
	return c.ClassifyWithTopicsContext(context.Background(), title, content, hint, topics, time.Time{}, "")
}

func (c *Categorizer) ClassifyWithTopicsContext(ctx context.Context, title, content, hint string, topics []TopicHint, now time.Time, authorName string) *ClassifyResult {
	if len(content) < 20 {
		return nil
	}

	truncated := content
	if len(truncated) > 4000 {
		truncated = truncated[:4000]
	}

	result, err := c.callLLM(ctx, title, truncated, hint, topics, now, authorName)
	if err != nil {
		slog.Warn("auto-classification failed", "error", err, "title", title)
		return nil
	}
	return result
}

func (c *Categorizer) callLLM(ctx context.Context, title string, content string, hint string, topics []TopicHint, now time.Time, authorName string) (*ClassifyResult, error) {
	hintSection := ""
	if hint != "" {
		hintSection = fmt.Sprintf("\nContext about this content: %s\n", hint)
	}

	topicSection := ""
	topicJSONField := ""
	if len(topics) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## Existing Topics\nPick the BEST existing topic for this memory. Only pick one if it clearly fits - if none fit well, omit topic_id.\n")
		for _, t := range topics {
			if t.Path != "" {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.ID, t.Path+" > "+t.Name))
			} else {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.ID, t.Name))
			}
		}
		topicSection = sb.String()
		topicJSONField = `,"topic_id":"existing-topic-id-or-empty"`
	}

	// Build context header for temporal/identity resolution
	contextHeader := ""
	if !now.IsZero() {
		contextHeader += fmt.Sprintf("Today: %s\n", now.Format("2006-01-02"))
	}
	if authorName != "" {
		contextHeader += fmt.Sprintf("Author: %s\n", authorName)
	}

	prompt := fmt.Sprintf(`Classify this knowledge memory using Memax's invisible retrieval axes.
Reply with JSON only, no other text.

{"kind": "semantic", "stability": "evolving", "tags": ["tag1", "tag2", "tag3"], "event_dates": ["2026-04-05"]%s}
%s%s%s
## Kind Rules
- episodic: events, conversations, meetings, daily updates, what someone did or said, time-bound observations.
- semantic: facts, references, definitions, stable context, people/team/project knowledge.
- procedural: how-to guides, runbooks, workflows, checklists, setup/deploy/debug procedures.
- rationale: decisions, tradeoffs, ADRs, postmortems, reasons why something changed.

Kind is a strong ranking hint, not a user-facing label. Pick exactly one.

## Stability Rules
- volatile: short-lived status, recent activity, meeting notes, transient debug context.
- evolving: information that may change as products, processes, or projects evolve.
- stable: durable facts, principles, preferences, identities, major decisions, historical rationale.

## Tag Rules
- 3-8 tags, lowercase, hyphenated (no spaces)
- Extract key topics, technologies, and concepts as plain tags
- Extract people/person names with a "person:" prefix. Include romanized/transliterated variants (e.g., "小明" → "person:小明" and "person:xiaoming"; "John Smith" → "person:john" and "person:smith")
- If a person is mentioned by name, always include them as person: tags
- Do NOT tag the author as a person — they are already known
- Tags should help with search and cross-referencing

## Event Date Rules
- Extract specific dates mentioned in the content as ISO-8601 (YYYY-MM-DD)
- Resolve relative expressions ("两天之前", "yesterday", "last week") using the content's context and today's date
- Include ALL dates referenced — both direct ("4月5号") and indirect ("两天之前" relative to a date in context)
- If no dates are mentioned, return empty array []

## Examples
- Architecture reference -> kind: semantic, stability: evolving, tags: ["golang", "postgresql", "monorepo"], event_dates: []
- "小明4月5号说两天之前有限购" -> kind: episodic, stability: volatile, tags: ["person:小明", "person:xiaoming", "限购"], event_dates: ["2026-04-05", "2026-04-03"]
- Meeting notes about sprint -> kind: episodic, stability: volatile, tags: ["sprint-review", "q2-planning", "person:sarah"], event_dates: ["2026-04-07"]
- Deploy guide -> kind: procedural, stability: evolving, tags: ["fly-io", "vercel", "ci-cd"], event_dates: []
- ADR about PostgreSQL -> kind: rationale, stability: stable, tags: ["postgresql", "tradeoff", "architecture"], event_dates: []

Title: %s

Content:
%s`, topicJSONField, contextHeader, hintSection, topicSection, title, content)

	maxTokens := 250
	if len(topics) > 0 {
		maxTokens = 300
	}

	resp, err := c.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Prompt:    prompt,
		Purpose:   "ingest.categorize",
	})
	if err != nil {
		return nil, err
	}

	text := anthropic.StripMarkdownFences(resp.Text)

	var result ClassifyResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse LLM JSON response: %w (raw: %s)", err, text)
	}

	return sanitizeClassifyResult(&result, title), nil
}

func sanitizeClassifyResult(result *ClassifyResult, title string) *ClassifyResult {
	result.Kind = strings.ToLower(strings.TrimSpace(result.Kind))
	if !IsValidKind(result.Kind) {
		slog.Warn("LLM returned invalid memory kind", "kind", result.Kind, "title", title)
		result.Kind = model.MemoryKindSemantic
	}

	result.Stability = strings.ToLower(strings.TrimSpace(result.Stability))
	if !IsValidStability(result.Stability) {
		slog.Warn("LLM returned invalid memory stability", "stability", result.Stability, "title", title)
		result.Stability = model.MemoryStabilityEvolving
	}

	cleanTags := make([]string, 0, min(len(result.Tags), 8))
	for _, tag := range result.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
		if len(cleanTags) >= 8 {
			break
		}
	}
	result.Tags = cleanTags

	result.TopicID = strings.TrimSpace(result.TopicID)
	if result.TopicID == "null" || result.TopicID == "" || result.TopicID == "existing-topic-id-or-empty" {
		result.TopicID = ""
	}

	return result
}
