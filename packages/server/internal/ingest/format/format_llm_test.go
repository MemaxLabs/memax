package format_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/format"
)

// messyOCR is many short lines without markdown → looksStructurallyMessy
// returns true and shouldLLMCleanup approves the LLM pass.
const messyOCR = `Title
Some body
More body
Another
Line here
short
more
short
still more
end`

func TestNormalizeContextUsesLLMForPDF(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("# Clean title\n\nBody paragraph.")

	f := format.New(srv.Client())
	got := f.NormalizeContext(context.Background(), messyOCR, "pdf", "doc.pdf")
	if got.Content != "# Clean title\n\nBody paragraph." {
		t.Errorf("LLM-formatted content not used: %q", got.Content)
	}
	if !strings.Contains(got.Reason, "llm_cleanup") {
		t.Errorf("Reason = %q, want llm_cleanup suffix", got.Reason)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(reqs))
	}
	if !strings.Contains(reqs[0].UserPrompt(), "doc.pdf") {
		t.Error("source path not forwarded to LLM prompt")
	}
}

func TestNormalizeContextUsesLLMForImage(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("screenshot text cleaned up")
	f := format.New(srv.Client())
	got := f.NormalizeContext(context.Background(), messyOCR, "image", "s.png")
	if got.Content != "screenshot text cleaned up" {
		t.Errorf("image branch didn't use LLM output: %q", got.Content)
	}
}

func TestNormalizeContextUsesLLMForHTML(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("# Converted markdown")
	f := format.New(srv.Client())
	got := f.NormalizeContext(context.Background(),
		"<html><body><h1>Title</h1><p>Body</p></body></html>",
		"html", "page.html")
	if !strings.Contains(got.Content, "Converted markdown") {
		t.Errorf("HTML branch didn't use LLM output: %q", got.Content)
	}
}

func TestNormalizeContextSkipsLLMForShortCleanText(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("should not appear")
	f := format.New(srv.Client())
	// Short clean text, markdown-y contentType → shouldLLMCleanup=false.
	got := f.NormalizeContext(context.Background(),
		"# Hello\n\nJust a little note.\n", "text", "notes.md")
	if srv.CallCount() != 0 {
		t.Errorf("LLM should be skipped for short clean text, got %d calls",
			srv.CallCount())
	}
	if got.Reason != "normalize_whitespace" {
		t.Errorf("Reason = %q, want normalize_whitespace", got.Reason)
	}
}

func TestNormalizeContextFallsBackOnLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusInternalServerError, "boom")
	f := format.New(srv.Client())
	got := f.NormalizeContext(context.Background(), messyOCR, "pdf", "a.pdf")
	if !strings.Contains(got.Reason, "llm_failed") {
		t.Errorf("Reason = %q, want llm_failed suffix", got.Reason)
	}
	// Content falls back to the pre-LLM prepared text — must not be empty.
	if got.Content == "" {
		t.Error("fallback should preserve prepared content")
	}
}

func TestNormalizeContextSkipsLLMForVeryLargeContent(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("should not appear")
	f := format.New(srv.Client())
	huge := strings.Repeat("x ", 15000) // > maxCleanupChars (20000)
	got := f.NormalizeContext(context.Background(), huge, "pdf", "huge.pdf")
	if srv.CallCount() != 0 {
		t.Errorf("LLM should be skipped for very large input, got %d calls",
			srv.CallCount())
	}
	if !strings.Contains(got.Reason, "skip_large") {
		t.Errorf("Reason = %q, want skip_large suffix", got.Reason)
	}
}

func TestNormalizeContextStripsMarkdownFencesFromLLM(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("```markdown\n# Title\n\nBody.\n```")
	f := format.New(srv.Client())
	got := f.NormalizeContext(context.Background(), messyOCR, "pdf", "doc.pdf")
	if strings.Contains(got.Content, "```") {
		t.Errorf("fences should be stripped: %q", got.Content)
	}
}

func TestNormalizeContextSkipsLLMWhenClientNil(t *testing.T) {
	t.Parallel()
	// Without a client, Formatter still normalizes whitespace but
	// never reaches the LLM path — pin that fast-path.
	f := format.New(nil)
	got := f.NormalizeContext(context.Background(), messyOCR, "pdf", "a.pdf")
	if got.Reason != "normalize_whitespace" {
		t.Errorf("nil-client should fall through to prepare reason, got %q", got.Reason)
	}
}
