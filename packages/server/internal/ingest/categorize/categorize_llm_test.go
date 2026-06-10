package categorize_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/categorize"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func longBody() string {
	return strings.Repeat("Sample memory content body. ", 5)
}

func TestClassifyRoundTripsValidLLMResponse(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"kind":        "semantic",
		"stability":   "evolving",
		"tags":        []string{"golang", "postgres"},
		"event_dates": []string{"2026-04-07"},
	})
	c := categorize.New(srv.Client())
	if c == nil {
		t.Fatal("expected non-nil Categorizer")
	}
	got := c.Classify("Architecture", longBody(), "")
	if got == nil {
		t.Fatal("Classify returned nil on valid response")
	}
	if got.Kind != model.MemoryKindSemantic {
		t.Errorf("Kind = %q", got.Kind)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "golang" {
		t.Errorf("Tags = %v", got.Tags)
	}
	if len(got.EventDates) != 1 || got.EventDates[0] != "2026-04-07" {
		t.Errorf("EventDates = %v", got.EventDates)
	}
}

func TestClassifyDefaultsInvalidKindToSemantic(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// sanitizeClassifyResult must coerce unknown kinds — this is the
	// defense against LLM hallucinating a new label that breaks the
	// memory kind CHECK constraint downstream.
	srv.EnqueueJSON(map[string]any{
		"kind":      "nonsense",
		"stability": "also_nonsense",
		"tags":      []string{},
	})
	c := categorize.New(srv.Client())
	got := c.Classify("x", longBody(), "")
	if got.Kind != model.MemoryKindSemantic {
		t.Errorf("invalid kind should default to semantic, got %q", got.Kind)
	}
	if got.Stability != model.MemoryStabilityEvolving {
		t.Errorf("invalid stability should default to evolving, got %q", got.Stability)
	}
}

func TestClassifyClampsTagsToEight(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"kind":      "semantic",
		"stability": "evolving",
		"tags": []string{
			"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10",
		},
	})
	c := categorize.New(srv.Client())
	got := c.Classify("x", longBody(), "")
	if len(got.Tags) != 8 {
		t.Errorf("tags should be clamped to 8, got %d", len(got.Tags))
	}
}

func TestClassifyReturnsNilOnShortContent(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText(`{"kind":"semantic","stability":"evolving","tags":[]}`)
	c := categorize.New(srv.Client())

	// len < 20 → short-circuit before LLM.
	got := c.Classify("x", "tiny", "")
	if got != nil {
		t.Errorf("short content should return nil, got %+v", got)
	}
	if srv.CallCount() != 0 {
		t.Errorf("LLM should not have been called for short content (got %d)",
			srv.CallCount())
	}
}

func TestClassifyReturnsNilOnLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusInternalServerError, "boom")
	c := categorize.New(srv.Client())
	if got := c.Classify("x", longBody(), ""); got != nil {
		t.Errorf("LLM error should yield nil, got %+v", got)
	}
}

func TestClassifyReturnsNilOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("not json")
	c := categorize.New(srv.Client())
	if got := c.Classify("x", longBody(), ""); got != nil {
		t.Errorf("malformed JSON should yield nil, got %+v", got)
	}
}

func TestClassifyStripsMarkdownFences(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("```json\n" +
		`{"kind":"procedural","stability":"stable","tags":["deploy"]}` +
		"\n```")
	c := categorize.New(srv.Client())
	got := c.Classify("Deploy", longBody(), "")
	if got == nil || got.Kind != model.MemoryKindProcedural {
		t.Errorf("fence-wrapped JSON should parse, got %+v", got)
	}
}

func TestClassifyWithTopicsPassesTopicListToPromptAndKeepsValidID(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"kind":      "semantic",
		"stability": "evolving",
		"tags":      []string{"api"},
		"topic_id":  "t-1",
	})
	c := categorize.New(srv.Client())
	topics := []categorize.TopicHint{
		{ID: "t-1", Name: "API Design"},
		{ID: "t-2", Name: "Data Model", Path: "Backend"},
	}
	got := c.ClassifyWithTopics("API pattern", longBody(), "", topics)
	if got.TopicID != "t-1" {
		t.Errorf("TopicID not preserved: %q", got.TopicID)
	}
	// Both topic IDs should have been listed in the prompt.
	prompt := srv.Requests()[0].UserPrompt()
	if !strings.Contains(prompt, "t-1") || !strings.Contains(prompt, "t-2") {
		t.Error("topics missing from prompt")
	}
	// Path-qualified name should appear.
	if !strings.Contains(prompt, "Backend > Data Model") {
		t.Error("path-qualified topic name not formatted correctly")
	}
}

func TestClassifyWithTopicsDropsPlaceholderTopicID(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// LLM sometimes echoes the JSON shape placeholder. That must NOT
	// flow through as a real topic ID.
	srv.EnqueueJSON(map[string]any{
		"kind":      "semantic",
		"stability": "evolving",
		"tags":      []string{},
		"topic_id":  "existing-topic-id-or-empty",
	})
	c := categorize.New(srv.Client())
	got := c.ClassifyWithTopics("x", longBody(), "",
		[]categorize.TopicHint{{ID: "real", Name: "Real"}})
	if got.TopicID != "" {
		t.Errorf("placeholder topic_id should be cleared, got %q", got.TopicID)
	}
}

func TestCategorizeReturnsKindOnly(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"kind":      "rationale",
		"stability": "stable",
		"tags":      []string{},
	})
	c := categorize.New(srv.Client())
	if kind := c.Categorize("Decision", longBody()); kind != model.MemoryKindRationale {
		t.Errorf("Categorize returned %q, want rationale", kind)
	}
}

func TestClassifyContextForwardsAuthorAndToday(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"kind":      "episodic",
		"stability": "volatile",
		"tags":      []string{},
	})
	c := categorize.New(srv.Client())
	now := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	_ = c.ClassifyContext(context.Background(), "meeting", longBody(), "", now, "Alice")
	prompt := srv.Requests()[0].UserPrompt()
	if !strings.Contains(prompt, "Today: 2026-04-07") {
		t.Error("today date missing from prompt")
	}
	if !strings.Contains(prompt, "Author: Alice") {
		t.Error("author missing from prompt")
	}
}
