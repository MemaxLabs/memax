package title

import "testing"

func TestPrepareIncomingMeaningfulFilename(t *testing.T) {
	title := PrepareIncoming("deploy-runbook_v2.md", "deploy-runbook_v2.md", "ignored", "markdown")
	if title != "Deploy Runbook V2" {
		t.Fatalf("expected cleaned filename title, got %q", title)
	}
}

func TestPrepareIncomingJunkScreenshotUsesFallback(t *testing.T) {
	title := PrepareIncoming("Screenshot 2026-04-07 at 10.31.22 AM.png", "Screenshot 2026-04-07 at 10.31.22 AM.png", "", "image")
	if title != "Screenshot" {
		t.Fatalf("expected generic screenshot fallback, got %q", title)
	}
}

func TestPrepareIncomingJunkTextFilenameUsesContent(t *testing.T) {
	content := "# Fly Release Command Timeout\n\nDetails about release machines."
	title := PrepareIncoming("IMG_4921.txt", "IMG_4921.txt", content, "markdown")
	if title != "Fly Release Command Timeout" {
		t.Fatalf("expected content-derived title, got %q", title)
	}
}

func TestIsMeaningfulFilename(t *testing.T) {
	if !isMeaningfulFilename("q2-pricing-notes.pdf") {
		t.Fatal("expected meaningful filename to be detected")
	}
	if isMeaningfulFilename("IMG_4921.png") {
		t.Fatal("expected screenshot-style filename to be treated as junk")
	}
}

func TestResolveContextUsesContentForJunkFilename(t *testing.T) {
	resolver := (*Resolver)(nil)
	result := resolver.ResolveContext(t.Context(), ResolveInput{
		Title:       "Screenshot 2026-04-07 at 10.31.22 AM",
		SourcePath:  "Screenshot 2026-04-07 at 10.31.22 AM.png",
		Content:     "# OAuth Redirect Fix\n\nUse the staging callback URL.",
		ContentType: "image",
	})
	if result.Title != "OAuth Redirect Fix" {
		t.Fatalf("expected content-generated title, got %q", result.Title)
	}
	if result.Reason != "content_generated" {
		t.Fatalf("expected content_generated reason, got %q", result.Reason)
	}
}

func TestResolveContextPreservesUserTitle(t *testing.T) {
	resolver := (*Resolver)(nil)
	result := resolver.ResolveContext(t.Context(), ResolveInput{
		Title:       "Staging deploy follow-up",
		SourcePath:  "IMG_4921.png",
		Content:     "some content",
		ContentType: "image",
	})
	if result.Title != "Staging deploy follow-up" {
		t.Fatalf("expected user title to be preserved, got %q", result.Title)
	}
	if result.Reason != "user_preserved" {
		t.Fatalf("expected user_preserved reason, got %q", result.Reason)
	}
}

func TestResolveGeneratedCandidateAppliesToGenericTitle(t *testing.T) {
	result := ResolveGeneratedCandidate(ResolveInput{
		Title:       "Document",
		SourcePath:  "scan-2026-04-11.pdf",
		Content:     "quarterly planning notes",
		ContentType: "pdf",
	}, "Quarterly Planning Notes")

	if result.Title != "Quarterly Planning Notes" {
		t.Fatalf("expected generated title, got %q", result.Title)
	}
	if result.Reason != "summary_llm_generated" {
		t.Fatalf("expected summary_llm_generated reason, got %q", result.Reason)
	}
}

func TestResolveGeneratedCandidatePreservesUserTitle(t *testing.T) {
	result := ResolveGeneratedCandidate(ResolveInput{
		Title:       "Staging deploy follow-up",
		SourcePath:  "scan-2026-04-11.pdf",
		Content:     "quarterly planning notes",
		ContentType: "pdf",
	}, "Quarterly Planning Notes")

	if result.Title != "Staging deploy follow-up" {
		t.Fatalf("expected user title to be preserved, got %q", result.Title)
	}
	if result.Reason != "user_preserved" {
		t.Fatalf("expected user_preserved reason, got %q", result.Reason)
	}
}

func TestResolveGeneratedCandidateAppliesContextUpgrade(t *testing.T) {
	result := ResolveGeneratedCandidate(ResolveInput{
		Title:       "CLI hub references now support personal alias",
		Content:     "Memax CLI hub references now support the personal alias.",
		ContentType: "markdown",
	}, "Memax CLI: personal hub alias support")

	if result.Title != "Memax CLI: personal hub alias support" {
		t.Fatalf("expected contextual title upgrade, got %q", result.Title)
	}
	if result.Reason != "summary_llm_context_upgrade" {
		t.Fatalf("expected summary_llm_context_upgrade reason, got %q", result.Reason)
	}
}

func TestResolveGeneratedCandidateRejectsUnrelatedContextUpgrade(t *testing.T) {
	result := ResolveGeneratedCandidate(ResolveInput{
		Title:       "Staging deploy follow-up",
		Content:     "notes about staging deploy follow-up",
		ContentType: "markdown",
	}, "Memax CLI: personal hub alias support")

	if result.Title != "Staging deploy follow-up" {
		t.Fatalf("expected user title to be preserved, got %q", result.Title)
	}
	if result.Reason != "user_preserved" {
		t.Fatalf("expected user_preserved reason, got %q", result.Reason)
	}
}

func TestWeakTitleDetection(t *testing.T) {
	tests := []struct {
		title string
		weak  bool
	}{
		{title: "Session capture — 2026-04-11", weak: true},
		{title: "Body: Hi [Mobility Contact / Fragomen Attorney", weak: true},
		{title: "Document", weak: true},
		{title: "Untitled", weak: true},
		{title: "Memax CLI: personal hub alias support", weak: false},
		{title: "Fragomen attorney requests mobility contact intro", weak: false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := IsWeakTitle(tt.title); got != tt.weak {
				t.Fatalf("expected weak=%v, got %v", tt.weak, got)
			}
		})
	}
}

func TestResolveGeneratedCandidateAppliesToSessionCaptureWithoutSourcePath(t *testing.T) {
	result := ResolveGeneratedCandidate(ResolveInput{
		Title:       "Session capture — 2026-04-11",
		Content:     "Discussion about tuning Memax AI Ask source context windows.",
		ContentType: "markdown",
	}, "Memax AI Ask context tuning")

	if result.Title != "Memax AI Ask context tuning" {
		t.Fatalf("expected generated title, got %q", result.Title)
	}
	if result.Reason != "summary_llm_generated" {
		t.Fatalf("expected summary_llm_generated reason, got %q", result.Reason)
	}
}

func TestGenerateFromContentSkipsEmailBoilerplate(t *testing.T) {
	content := `Body:
Hi [Mobility Contact / Fragomen Attorney],

Subject: Fragomen attorney requests mobility contact intro

I am introducing the mobility contact for the immigration case.`

	if got := GenerateFromContent(content); got != "Fragomen attorney requests mobility contact intro" {
		t.Fatalf("expected subject-derived title after boilerplate, got %q", got)
	}
}
