package chunker

import (
	"strings"
	"testing"
)

func TestChunkMarkdownPreservesCodeFenceComments(t *testing.T) {
	content := `# Implementation Notes

This memory explains the deployment script and why the shell comments below matter for future debugging.

` + "```bash" + `
# This is a shell comment, not a Markdown heading.
fly deploy -c fly.worker.toml
` + "```" + `

The final paragraph keeps the chunk above the minimum size so it is indexed.`

	chunks := ChunkMarkdown(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0].HeadingChain != "Implementation Notes" {
		t.Fatalf("heading chain = %q, want Implementation Notes", chunks[0].HeadingChain)
	}
	if !strings.Contains(chunks[0].Content, "# This is a shell comment") {
		t.Fatalf("chunk content lost code fence comment: %q", chunks[0].Content)
	}
}

func TestChunkMarkdownSplitsLongSingleParagraph(t *testing.T) {
	content := "# Long Note\n\n" + strings.Repeat("retrieval precision depends on bounded semantic chunks ", 700)

	chunks := ChunkMarkdown(content)
	if len(chunks) < 2 {
		t.Fatalf("expected long paragraph to split, got %d chunk(s)", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.TokenCount > targetChunkTokens {
			t.Fatalf("chunk %d has %d tokens, want <= %d", chunk.Index, chunk.TokenCount, targetChunkTokens)
		}
		if chunk.HeadingChain != "Long Note" {
			t.Fatalf("chunk %d heading chain = %q, want Long Note", chunk.Index, chunk.HeadingChain)
		}
	}
}

func TestEstimateTokensCountsCJKMoreAccurately(t *testing.T) {
	cjk := strings.Repeat("记", 80)
	english := strings.Repeat("a", 80)

	if estimateTokens(cjk) <= estimateTokens(english) {
		t.Fatalf("expected CJK token estimate to exceed ASCII estimate, got cjk=%d ascii=%d", estimateTokens(cjk), estimateTokens(english))
	}
}
