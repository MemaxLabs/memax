package link

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
)

// These tests exercise summarizeWithLLM directly because Process's
// URL-fetch path refuses loopback addresses (SSRF guard). Internal
// package lets us bypass the fetch and exercise the LLM plumbing.

func TestSummarizeWithLLMParsesJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{
		"title":          "Memax",
		"summary":        "A memory hub for AI agents.",
		"retrieval_hint": "Retrieve when the user asks about Memax.",
		"key_topics":     []string{"memory", "AI agents", "retrieval"},
	})

	p := New(srv.Client())
	if p == nil {
		t.Fatal("expected non-nil Processor")
	}
	result, err := p.summarizeWithLLM(context.Background(),
		"https://memax.app",
		"Memax is a shared memory layer for AI agents and humans.")
	if err != nil {
		t.Fatalf("summarizeWithLLM: %v", err)
	}
	if result.Title != "Memax" {
		t.Errorf("Title = %q", result.Title)
	}
	if len(result.KeyTopics) != 3 {
		t.Errorf("KeyTopics = %v", result.KeyTopics)
	}
	// Verify URL + content reached the LLM prompt.
	req := srv.Requests()[0]
	if !strings.Contains(req.UserPrompt(), "https://memax.app") {
		t.Error("URL not in prompt")
	}
}

func TestSummarizeWithLLMParsesJSONInFences(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// Anthropic sometimes wraps JSON. The helper must unwrap.
	srv.EnqueueText("```json\n" +
		`{"title":"A","summary":"B","retrieval_hint":"C","key_topics":["x"]}` +
		"\n```")
	p := New(srv.Client())
	got, err := p.summarizeWithLLM(context.Background(), "https://x", "content")
	if err != nil {
		t.Fatalf("summarizeWithLLM: %v", err)
	}
	if got.Title != "A" || got.Summary != "B" {
		t.Errorf("fence-wrapped JSON not parsed: %+v", got)
	}
}

func TestSummarizeWithLLMReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("I cannot help with that.")
	p := New(srv.Client())
	_, err := p.summarizeWithLLM(context.Background(), "https://x", "content")
	if err == nil {
		t.Fatal("malformed response should error so callers fall back")
	}
}

func TestSummarizeWithLLMReturnsErrorOnUpstreamFailure(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusBadGateway, "upstream fail")
	p := New(srv.Client())
	_, err := p.summarizeWithLLM(context.Background(), "https://x", "content")
	if err == nil {
		t.Fatal("upstream 5xx should propagate")
	}
}
