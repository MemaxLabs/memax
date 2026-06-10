package format

import (
	"strings"
	"testing"
)

func TestPrepareConvertsHTMLToMarkdown(t *testing.T) {
	input := `<!doctype html><html><body><h1>Deploy Guide</h1><p>Use the <strong>staging</strong> URL.</p><ul><li>Run migrations</li><li>Verify health</li></ul><p><a href="https://memax.app">Open Memax</a></p></body></html>`
	res := Prepare(input, "markdown", "deploy-guide.html")
	if !strings.Contains(res.Content, "# Deploy Guide") {
		t.Fatalf("expected heading markdown, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "**staging**") {
		t.Fatalf("expected bold markdown, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "- Run migrations") {
		t.Fatalf("expected list markdown, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "[Open Memax](https://memax.app)") {
		t.Fatalf("expected link markdown, got %q", res.Content)
	}
}

func TestPrepareCleansExtractedWrappedLines(t *testing.T) {
	input := "OAuth redirect\nconfiguration should\nuse the staging callback\nURL.\n\n- keep bullets\n- as bullets"
	res := Prepare(input, "pdf", "doc.pdf")
	if !strings.Contains(res.Content, "OAuth redirect configuration should use the staging callback URL.") {
		t.Fatalf("expected wrapped lines to join, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "- keep bullets") {
		t.Fatalf("expected bullets to remain, got %q", res.Content)
	}
}

func TestShouldLLMCleanup(t *testing.T) {
	if !shouldLLMCleanup("plain OCR text", "image", "scan.png") {
		t.Fatal("expected image content to use LLM cleanup")
	}
	if !shouldLLMCleanup("<html><body>hi</body></html>", "markdown", "page.html") {
		t.Fatal("expected html content to use LLM cleanup")
	}
	if shouldLLMCleanup("# Clean Note\n\nAlready tidy markdown.", "markdown", "note.md") {
		t.Fatal("did not expect clean markdown to use LLM cleanup")
	}
}
