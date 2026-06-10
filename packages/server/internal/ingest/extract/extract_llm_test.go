package extract_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/extract"
)

// longContent returns a string >= 100 chars so it passes the early
// short-content guard and actually invokes the LLM.
func longContent() string {
	return strings.Repeat("We decided to use Postgres for the primary store. ", 4)
}

func TestExtractParsesJSONArrayResponse(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText(`[
		{"fact": "Team uses Postgres", "kind_hint": "semantic", "confidence": 0.9},
		{"fact": "Migrations run on deploy", "kind_hint": "procedural", "confidence": 0.8}
	]`)

	e := extract.New(srv.Client())
	if e == nil {
		t.Fatal("extractor should be non-nil when client is provided")
	}
	facts := e.Extract(longContent(), "project: backend")
	if len(facts) != 2 {
		t.Fatalf("got %d facts, want 2", len(facts))
	}
	if facts[0].Content != "Team uses Postgres" {
		t.Errorf("first fact wrong: %+v", facts[0])
	}
	if facts[0].KindHint != "semantic" {
		t.Errorf("kind_hint round-trip: %q", facts[0].KindHint)
	}
	// The hint string is passed through as a Context: line in the
	// prompt — verify that reached the LLM.
	reqs := srv.Requests()
	if !strings.Contains(reqs[0].UserPrompt(), "Context: project: backend") {
		t.Errorf("hint not forwarded to LLM prompt")
	}
}

func TestExtractFiltersLowConfidence(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// Three facts: 0.9 keep, 0.2 drop (below 0.3 threshold), 0.5 keep.
	// Low-confidence filtering is the load-bearing guard against the
	// LLM padding its output with weak inferences; pin the threshold.
	srv.EnqueueText(`[
		{"fact": "Strong claim", "kind_hint": "semantic", "confidence": 0.9},
		{"fact": "Weak guess", "kind_hint": "episodic", "confidence": 0.2},
		{"fact": "Medium claim", "kind_hint": "procedural", "confidence": 0.5}
	]`)

	e := extract.New(srv.Client())
	facts := e.ExtractContext(context.Background(), longContent(), "")
	if len(facts) != 2 {
		t.Fatalf("got %d facts, want 2 (low-confidence dropped)", len(facts))
	}
	for _, f := range facts {
		if f.Confidence < 0.3 {
			t.Errorf("low-confidence fact survived: %+v", f)
		}
	}
}

func TestExtractDropsEmptyFacts(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// fact="" must be dropped even if confidence is high. Guards
	// against LLM emitting placeholder rows.
	srv.EnqueueText(`[
		{"fact": "", "kind_hint": "semantic", "confidence": 0.95},
		{"fact": "Real fact", "kind_hint": "semantic", "confidence": 0.9}
	]`)
	e := extract.New(srv.Client())
	facts := e.Extract(longContent(), "")
	if len(facts) != 1 || facts[0].Content != "Real fact" {
		t.Errorf("empty fact should be dropped, got %+v", facts)
	}
}

func TestExtractParsesJSONInsideMarkdownFences(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// Anthropic sometimes wraps JSON in ```json ... ```; the package's
	// ExtractJSONArray is supposed to strip that. Pin it.
	srv.EnqueueText("Sure! Here you go:\n```json\n" +
		`[{"fact": "Inside fences", "kind_hint": "semantic", "confidence": 0.8}]` +
		"\n```")
	e := extract.New(srv.Client())
	facts := e.Extract(longContent(), "")
	if len(facts) != 1 || facts[0].Content != "Inside fences" {
		t.Errorf("fenced JSON should parse: %+v", facts)
	}
}

func TestExtractReturnsNilOnLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusInternalServerError, "boom")
	e := extract.New(srv.Client())
	if got := e.Extract(longContent(), ""); got != nil {
		t.Errorf("LLM error should yield nil, got %+v", got)
	}
}

func TestExtractReturnsNilOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("not JSON at all, sorry")
	e := extract.New(srv.Client())
	if got := e.Extract(longContent(), ""); got != nil {
		t.Errorf("malformed response should yield nil, got %+v", got)
	}
}

func TestExtractTruncatesVeryLongContent(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText(`[{"fact":"ok","kind_hint":"semantic","confidence":0.9}]`)
	e := extract.New(srv.Client())
	// Build a 20KB input — the extractor caps at 16KB + truncation marker.
	huge := strings.Repeat("x", 20000)
	_ = e.Extract(huge, "")
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(reqs))
	}
	prompt := reqs[0].UserPrompt()
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("very long content should be truncated with marker")
	}
	if len(prompt) > 18000 {
		t.Errorf("prompt not capped: %d bytes", len(prompt))
	}
}
