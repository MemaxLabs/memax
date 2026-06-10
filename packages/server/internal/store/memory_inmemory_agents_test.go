package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// InMemoryStore agent config stubs.
//
// These stubs are used by the CLI sync tests that exercise the
// handler without a real DB. Regressions panic those tests.

func TestInMemoryAgentConfigCRUD(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	cfg := &model.AgentConfig{
		ID: "c1", OwnerID: "u1", Agent: "claude-code",
		FilePath: "CLAUDE.md", Scope: "global",
		Content: "# hi", ContentHash: "h1", Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	_, _ = s.GetAgentConfig("c1", "u1")
	_, _ = s.GetAgentConfigByPath("claude-code", "CLAUDE.md", "global", "u1")
	_, _ = s.ListAgentConfigs("u1")
	_ = s.DeleteAgentConfig("c1", "u1")

	// Sync state helpers.
	_, _ = s.ListAgentConfigSyncStates("u1", "dev-1")
	_ = s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID: "u1", DeviceID: "dev-1", Agent: "cc", FilePath: "CLAUDE.md",
	})

	// Tombstones.
	_, _ = s.ListAgentConfigTombstones("u1")
	_, _ = s.GetAgentConfigTombstone("cc", "CLAUDE.md", "global", "u1")
	_ = s.CreateAgentConfigTombstone(&model.AgentConfigTombstone{
		OwnerID: "u1", Agent: "cc", FilePath: "CLAUDE.md", Scope: "global",
	})
	_ = s.DeleteAgentConfigTombstone("cc", "CLAUDE.md", "global", "u1")
	_ = s.PurgeExpiredAgentConfigTombstoneContent(time.Now().Add(time.Hour))

	_, _ = s.CountExtractedMemories("u1")
}

func TestInMemoryTopicMemoryLookupHelpers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	scope := VisibilityScope{OwnerID: "u1"}
	_, _ = s.ListUnassignedMemories(scope, 10)
	_, _ = s.ListUnassignedMemoriesByHub("h1", 10)
	_, _ = s.GetMemoryTopicNameMap(scope)
	_, _ = s.GetMemoryTopicNameMapForMemories(scope, []string{"m1"})
	_, _ = s.GetMemoryTopicIDMap(scope)
	_, _ = s.GetMemoryTopicIDMapForMemories(scope, []string{"m1"})
	_, _ = s.GetTopicKinds("h1")
	_, _, _ = s.ListMemoriesByTopic(scope, "t1", 10, "")
}

func TestInMemoryAgentConfigContextNote(t *testing.T) {
	// Some of these methods take context via handler paths even though
	// the dev-store signatures drop it. The smoke test above exercises
	// every path. This entry just documents the intentional gap —
	// nothing to assert.
	_ = context.Background()
}
