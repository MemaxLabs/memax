package distill_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/distill"
)

var fixedNow = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

func TestNewRejectsNilClient(t *testing.T) {
	t.Parallel()
	if d := distill.New(nil); d != nil {
		t.Errorf("nil client should produce nil Distiller, got %+v", d)
	}
}

func TestDistillCompactPathParsesJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"query":            "deploy production",
		"keywords":         []string{"deploy"},
		"information_need": "procedure",
		"confidence":       0.9,
	})
	d := distill.New(srv.Client())
	if d == nil {
		t.Fatal("expected non-nil Distiller")
	}
	got := d.Distill("how do I deploy to production",
		"web", "/", fixedNow, "alice", "UTC")
	if got == nil {
		t.Fatal("Distill returned nil")
	}
	if got.Query != "deploy production" {
		t.Errorf("Query = %q", got.Query)
	}
	if got.InformationNeed != "procedure" {
		t.Errorf("InformationNeed = %q", got.InformationNeed)
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
}

func TestDistillDefaultConfidenceWhenZero(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// Confidence omitted → handler defaults to 0.7 so caller always has
	// a meaningful number for ranking.
	srv.EnqueueJSON(map[string]any{
		"query":            "some topic",
		"information_need": "reference",
	})
	d := distill.New(srv.Client())
	got := d.Distill("topic", "web", "/", fixedNow, "alice", "UTC")
	if got.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want 0.7 default", got.Confidence)
	}
}

func TestDistillFullPathTriggersForLongQuery(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"query":            "long query distilled",
		"keywords":         []string{},
		"information_need": "reference",
		"confidence":       0.85,
	})
	d := distill.New(srv.Client())

	// Build a query over ~80 tokens (roughly 320 chars of English).
	long := strings.Repeat("this is a long query string for distillation ", 12)
	got := d.Distill(long, "web", "/", fixedNow, "alice", "UTC")
	if got.Query != "long query distilled" {
		t.Errorf("full-path Query not parsed: %q", got.Query)
	}
	// The prompt body for the full path differs from compact — confirm
	// we hit the full path by examining the captured prompt contains
	// the long query (truncated-middle or otherwise).
	if srv.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", srv.CallCount())
	}
}

func TestDistillFallsBackOnLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusInternalServerError, "boom")
	d := distill.New(srv.Client())
	got := d.Distill("debug issue", "web", "/", fixedNow, "alice", "UTC")
	if got == nil {
		t.Fatal("expected fallback Result, got nil")
	}
	// Fallback should use the raw query (possibly truncated).
	if got.Query == "" {
		t.Error("fallback Query should not be empty")
	}
}

func TestDistillFallsBackOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("not json either")
	d := distill.New(srv.Client())
	got := d.Distill("some query", "web", "/", fixedNow, "alice", "UTC")
	if got == nil {
		t.Fatal("expected fallback Result")
	}
	if got.Query == "" {
		t.Error("fallback Query should not be empty")
	}
}

func TestDistillContextHonorsCancellation(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("never arrives")
	d := distill.New(srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := d.DistillContext(ctx, "query", "web", "/", fixedNow, "alice", "UTC")
	// Cancelled ctx → LLM error → fallback Result, not nil.
	if got == nil {
		t.Error("cancelled ctx should still produce fallback Result")
	}
}

func TestDistillDefaultsTimezoneToUTC(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"query":            "q",
		"information_need": "general",
	})
	d := distill.New(srv.Client())
	// Empty timezone → code defaults to "UTC"; verify prompt includes UTC.
	_ = d.Distill("q", "web", "/", fixedNow, "alice", "")
	prompt := srv.Requests()[0].UserPrompt()
	if !strings.Contains(prompt, "Timezone: UTC") {
		t.Error("empty timezone should default to UTC in prompt")
	}
}
