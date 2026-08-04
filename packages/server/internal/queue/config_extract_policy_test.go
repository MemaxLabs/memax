package queue

import "testing"

func TestExtractKnowledgeItemsSkipsIdentityFiles(t *testing.T) {
	soul := `# Soul

## Personality
Warm, direct, a little playful. Always explains reasoning.

## Values
Honesty over comfort. The user's architecture decisions and deploy
workflow matter more than being agreeable.
`
	if items := extractKnowledgeItems(soul, "openclaw", "SOUL.md"); len(items) != 0 {
		t.Errorf("expected no knowledge items from SOUL.md, got %d", len(items))
	}
	// Same content in a memory file DOES extract (contains durable signals).
	if items := extractKnowledgeItems(soul, "openclaw", "memory/notes.md"); len(items) == 0 {
		t.Errorf("expected knowledge items from memory/notes.md, got none")
	}
}
