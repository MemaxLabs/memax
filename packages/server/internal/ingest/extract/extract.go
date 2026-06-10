package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// Extractor pulls discrete facts from unstructured content (chat logs, transcripts).
type Extractor struct {
	client *anthropic.Client
	model  string
}

func New(client *anthropic.Client) *Extractor {
	if client == nil {
		return nil
	}
	return &Extractor{
		client: client,
		model:  anthropic.ModelFromEnv("EXTRACT_MODEL"),
	}
}

type Fact struct {
	Content    string  `json:"fact"`
	KindHint   string  `json:"kind_hint"`
	Confidence float64 `json:"confidence"`
}

func (e *Extractor) Extract(content string, hint string) []Fact {
	return e.ExtractContext(context.Background(), content, hint)
}

func (e *Extractor) ExtractContext(ctx context.Context, content string, hint string) []Fact {
	if len(content) < 100 {
		return nil
	}

	truncated := content
	if len(truncated) > 16000 {
		truncated = truncated[:16000] + "\n\n[truncated]"
	}

	facts, err := e.callLLM(ctx, truncated, hint)
	if err != nil {
		slog.Warn("fact extraction failed", "error", err)
		return nil
	}
	return facts
}

func (e *Extractor) callLLM(ctx context.Context, content string, hint string) ([]Fact, error) {
	hintLine := ""
	if hint != "" {
		hintLine = fmt.Sprintf("Context: %s\n\n", hint)
	}

	prompt := fmt.Sprintf(`%sGiven this conversation/log, extract discrete, self-contained facts that would be useful in future AI agent sessions. For each fact, provide:
- fact: A concise, standalone statement (1-3 sentences)
- kind_hint: One of [episodic, semantic, procedural, rationale]
- confidence: 0.0-1.0

Rules:
- Extract decisions ("We decided to use X because Y")
- Extract facts ("The auth service runs on port 8080")
- Extract preferences ("User prefers tabs over spaces")
- Extract action items ("Need to migrate the database by Friday")
- DO NOT extract greetings, small talk, or filler
- DO NOT extract information that's only meaningful in conversation context
- Each fact must be useful standalone, without the original conversation

Content:
---
%s
---

Respond with a JSON array only:
[{"fact": "...", "kind_hint": "semantic", "confidence": 0.9}]`, hintLine, content)

	resp, err := e.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     e.model,
		MaxTokens: 2000,
		Prompt:    prompt,
		Purpose:   "ingest.fact_extract",
	})
	if err != nil {
		return nil, err
	}

	text := anthropic.ExtractJSONArray(resp.Text)

	var facts []Fact
	if err := json.Unmarshal([]byte(text), &facts); err != nil {
		return nil, fmt.Errorf("parse extracted facts: %w", err)
	}

	var filtered []Fact
	for _, f := range facts {
		if f.Content != "" && f.Confidence >= 0.3 {
			filtered = append(filtered, f)
		}
	}
	return filtered, nil
}
