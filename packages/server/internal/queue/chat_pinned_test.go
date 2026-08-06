package queue

import (
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// The pinned-context block is what makes "continue in memax" feel like
// the agent was already in the room: it must quote the exact memories
// the card cited, and must never leak one the owner can't read.
func TestBuildPinnedMemoryBlock(t *testing.T) {
	t.Parallel()
	s := store.NewInMemoryStore()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, m := range []*model.Memory{
		{ID: "m1", OwnerID: "u1", HubID: "h1", Title: "部署决策", Summary: "staging 由 CI 自动部署", CreatedAt: now},
		{ID: "m2", OwnerID: "u1", HubID: "h1", Title: "", Content: "无标题但有正文", CreatedAt: now},
		{ID: "other", OwnerID: "u2", HubID: "h2", Title: "别人的记忆", Summary: "secret", CreatedAt: now},
	} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	w := &ChatMessageRunWorker{Store: s}

	t.Run("no pinned ids renders nothing", func(t *testing.T) {
		if got := w.buildPinnedMemoryBlock("u1", []string{"h1"}, &model.ChatSession{}); got != "" {
			t.Fatalf("expected empty block, got %q", got)
		}
		if got := w.buildPinnedMemoryBlock("u1", []string{"h1"}, nil); got != "" {
			t.Fatalf("nil session must render nothing, got %q", got)
		}
	})

	t.Run("renders titles, dates and bodies in pin order", func(t *testing.T) {
		block := w.buildPinnedMemoryBlock("u1", []string{"h1"}, &model.ChatSession{
			PinnedMemoryIDs: []string{"m1", "m2"},
		})
		for _, want := range []string{"## Pinned context", "部署决策", "staging 由 CI 自动部署", "2026-08-05", "(untitled)", "无标题但有正文"} {
			if !strings.Contains(block, want) {
				t.Fatalf("block missing %q:\n%s", want, block)
			}
		}
		if strings.Index(block, "部署决策") > strings.Index(block, "无标题但有正文") {
			t.Fatal("pinned memories must render in pin order")
		}
	})

	t.Run("drops memories the owner cannot access", func(t *testing.T) {
		block := w.buildPinnedMemoryBlock("u1", []string{"h1"}, &model.ChatSession{
			PinnedMemoryIDs: []string{"m1", "other"},
		})
		if strings.Contains(block, "secret") || strings.Contains(block, "别人的记忆") {
			t.Fatalf("inaccessible memory leaked into the prompt:\n%s", block)
		}
		if !strings.Contains(block, "部署决策") {
			t.Fatal("accessible memory should still render")
		}
	})

	t.Run("all-inaccessible degrades to no block", func(t *testing.T) {
		if got := w.buildPinnedMemoryBlock("u1", []string{"h1"}, &model.ChatSession{
			PinnedMemoryIDs: []string{"other"},
		}); got != "" {
			t.Fatalf("expected empty block, got %q", got)
		}
	})
}

func TestTruncateRunesForPrompt(t *testing.T) {
	t.Parallel()
	// CJK content is common; a byte-slice cut would ship invalid UTF-8.
	cjk := strings.Repeat("汉", 100) // 3 bytes each
	got := truncateRunesForPrompt(cjk, 50, "…")
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected marker suffix, got %q", got)
	}
	body := strings.TrimSuffix(got, "…")
	for _, r := range body {
		if r != '汉' {
			t.Fatalf("truncation split a rune: %q", got)
		}
	}
	if short := truncateRunesForPrompt("short", 100, "…"); short != "short" {
		t.Fatalf("under-cap strings must pass through, got %q", short)
	}
}
