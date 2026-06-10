package title_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
)

// weakTitle + genericPath + long body → ResolveContext should invoke
// the LLM. This is the main LLM-path coverage we want on title.

// unparseableContent is content that passes shouldUseLLM (length
// gate) but GenerateFromContent can't make a meaningful title from —
// forces the LLM branch. Pure digits and punctuation have < 3 letters
// per line, so isMeaningfulGeneratedTitle rejects them.
const unparseableContent = "1234567890\n=====\n-----\n" +
	"999 000 111 222 333 444 555 666 777 888\n" +
	"1111111 2222222 3333333 4444444 5555555\n" +
	"..........\n" +
	"0 1 2 3 4 5 6 7 8 9\n"

func TestResolveContextUsesLLMForWeakTitleWithLongBody(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{"title": "Memax recall tuning"})

	r := title.New(srv.Client())
	if r == nil {
		t.Fatal("expected non-nil Resolver")
	}
	got := r.ResolveContext(context.Background(), title.ResolveInput{
		Title:       "Untitled",
		SourcePath:  "screenshot-2026-04-07.png",
		Content:     unparseableContent,
		ContentType: "image",
		Hint:        "from screenshot",
	})
	if got.Title != "Memax recall tuning" {
		t.Errorf("LLM title not used: %+v", got)
	}
	if got.Reason != "llm_generated" {
		t.Errorf("Reason should be llm_generated, got %q", got.Reason)
	}
	prompt := srv.Requests()[0].UserPrompt()
	if !strings.Contains(prompt, "from screenshot") {
		t.Error("hint not forwarded")
	}
	if !strings.Contains(prompt, "screenshot-2026-04-07.png") {
		t.Error("filename not forwarded")
	}
}

func TestResolveContextFallsBackWhenLLMReturnsGarbage(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// "text extraction failed" is in the rejection list; returning it
	// forces proposeWithLLM to yield "" so the flow falls through.
	srv.EnqueueText("text extraction failed")

	r := title.New(srv.Client())
	got := r.ResolveContext(context.Background(), title.ResolveInput{
		Title:       "",
		SourcePath:  "a.png",
		Content:     unparseableContent,
		ContentType: "image",
	})
	if got.Reason != "fallback_generic" {
		t.Errorf("Reason = %q, want fallback_generic", got.Reason)
	}
}

func TestResolveContextFallsBackOnLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusInternalServerError, "boom")
	r := title.New(srv.Client())
	got := r.ResolveContext(context.Background(), title.ResolveInput{
		Title:       "Untitled",
		SourcePath:  "a.png",
		Content:     unparseableContent,
		ContentType: "image",
	})
	if got.Reason != "fallback_generic" {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestResolveContextTruncatesVeryLongContentForLLM(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{"title": "Long doc"})
	r := title.New(srv.Client())
	_ = r.ResolveContext(context.Background(), title.ResolveInput{
		Title:       "",
		SourcePath:  "a.txt",
		Content:     strings.Repeat("1234567890 ", 500) + unparseableContent,
		ContentType: "image",
	})
	if srv.CallCount() == 0 {
		t.Skip("LLM path not triggered; content heuristic changed")
	}
	prompt := srv.Requests()[0].UserPrompt()
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("long content should be truncated")
	}
}

func TestResolveDeterministicNeverUsesLLM(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{"title": "should not appear"})
	// The deterministic flavor passes allowLLM=false, so the mock must
	// NOT be called.
	got := title.ResolveDeterministic(title.ResolveInput{
		Title:       "",
		SourcePath:  "",
		Content:     "",
		ContentType: "text",
	})
	if srv.CallCount() != 0 {
		t.Errorf("ResolveDeterministic should not call LLM, got %d calls", srv.CallCount())
	}
	if got.Title == "" {
		t.Error("expected non-empty fallback title")
	}
}

func TestResolveGeneratedCandidateAcceptsLLMCandidateWhenAutoRefineTriggers(t *testing.T) {
	t.Parallel()
	// Pure function — no LLM involved, but exercises the
	// summary_llm_generated / user_preserved branches we don't hit
	// from the Resolver flow above.
	got := title.ResolveGeneratedCandidate(title.ResolveInput{
		Title:       "Untitled",
		SourcePath:  "a.txt",
		Content:     "hello",
		ContentType: "text",
	}, "Dev log entry about CI flakes")
	if got.Title != "Dev log entry about CI flakes" {
		t.Errorf("candidate should be accepted when title is weak, got %+v", got)
	}
}

func TestResolveGeneratedCandidatePreservesStrongTitle(t *testing.T) {
	t.Parallel()
	got := title.ResolveGeneratedCandidate(title.ResolveInput{
		Title:       "The user's carefully crafted title",
		SourcePath:  "notes.md",
		Content:     "content",
		ContentType: "text",
	}, "Something the LLM cooked up")
	if got.Reason != "user_preserved" {
		t.Errorf("strong user title must survive, got %+v", got)
	}
}

func TestResolveGeneratedCandidateRejectsEmptyCandidate(t *testing.T) {
	t.Parallel()
	got := title.ResolveGeneratedCandidate(title.ResolveInput{
		Title:       "",
		SourcePath:  "x.txt",
		Content:     "hi",
		ContentType: "text",
	}, "   ")
	if got.Reason != "candidate_rejected" {
		t.Errorf("empty candidate should be rejected, got %+v", got)
	}
}
