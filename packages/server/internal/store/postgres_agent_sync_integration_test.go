package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupAgentSyncFixture seeds a user (no hub needed — agent configs
// are user-scoped, not hub-scoped) and returns the store + userID.
func setupAgentSyncFixture(t *testing.T) (store.Store, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@agsync",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return s, userID
}

func TestPostgresAgentConfigCRUD(t *testing.T) {
	t.Parallel()
	s, userID := setupAgentSyncFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	cfg := &model.AgentConfig{
		ID:          uuid.NewString(),
		OwnerID:     userID,
		Agent:       "claude-code",
		FilePath:    "CLAUDE.md",
		Scope:       "global",
		Content:     "# original",
		ContentHash: "hash-1",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// --- UpsertAgentConfig (insert) ---
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig (insert): %v", err)
	}

	// --- GetAgentConfig (by ID, owner-scoped) ---
	got, err := s.GetAgentConfig(cfg.ID, userID)
	if err != nil {
		t.Fatalf("GetAgentConfig: %v", err)
	}
	if got.Content != "# original" || got.Version != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// --- GetAgentConfigByPath ---
	got, err = s.GetAgentConfigByPath("claude-code", "CLAUDE.md", "global", userID)
	if err != nil {
		t.Fatalf("GetAgentConfigByPath: %v", err)
	}
	if got.ID != cfg.ID {
		t.Errorf("lookup returned wrong config")
	}

	// Cross-owner lookup must fail.
	other := uuid.NewString()
	if _, err := s.GetAgentConfig(cfg.ID, other); err == nil {
		t.Error("GetAgentConfig must not leak across owners")
	}

	// --- Upsert again: ON CONFLICT DO UPDATE should increment version
	// and overwrite content — this is the hot path the CLI hits on
	// every sync.
	cfg.Content = "# updated"
	cfg.ContentHash = "hash-2"
	cfg.UpdatedAt = now.Add(time.Minute)
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig (conflict): %v", err)
	}
	got, _ = s.GetAgentConfigByPath("claude-code", "CLAUDE.md", "global", userID)
	if got.Content != "# updated" || got.Version != 2 {
		t.Errorf("ON CONFLICT update didn't bump version: %+v", got)
	}

	// --- ListAgentConfigs ---
	list, err := s.ListAgentConfigs(userID)
	if err != nil {
		t.Fatalf("ListAgentConfigs: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListAgentConfigs returned %d, want 1", len(list))
	}

	// --- DeleteAgentConfig with cross-owner guard ---
	if err := s.DeleteAgentConfig(cfg.ID, other); err != nil {
		// Delete as a different owner should be a no-op (affects 0
		// rows but returns nil). If it errored, that's fine too —
		// we just care the row wasn't actually deleted.
		t.Logf("cross-owner delete returned: %v (acceptable)", err)
	}
	if got, _ := s.GetAgentConfigByPath("claude-code", "CLAUDE.md", "global", userID); got == nil {
		t.Error("cross-owner delete actually deleted the row")
	}

	// Legitimate delete.
	if err := s.DeleteAgentConfig(cfg.ID, userID); err != nil {
		t.Fatalf("DeleteAgentConfig: %v", err)
	}
	if _, err := s.GetAgentConfig(cfg.ID, userID); err == nil {
		t.Error("DeleteAgentConfig should remove the row")
	}
}

func TestPostgresAgentConfigTombstone(t *testing.T) {
	t.Parallel()
	s, userID := setupAgentSyncFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(24 * time.Hour)

	tomb := &model.AgentConfigTombstone{
		OwnerID:            userID,
		Agent:              "cursor",
		FilePath:           ".cursorrules",
		Scope:              "project:github.com/me/repo",
		Version:            7,
		DeletedAt:          now,
		DeletedContent:     "prior content",
		DeletedContentHash: "h",
		ContentExpiresAt:   &exp,
	}

	// Create.
	if err := s.CreateAgentConfigTombstone(tomb); err != nil {
		t.Fatalf("CreateAgentConfigTombstone: %v", err)
	}

	// Get.
	got, err := s.GetAgentConfigTombstone("cursor", ".cursorrules", "project:github.com/me/repo", userID)
	if err != nil {
		t.Fatalf("GetAgentConfigTombstone: %v", err)
	}
	if got.Version != 7 {
		t.Errorf("version mismatch: %d", got.Version)
	}

	// List.
	list, err := s.ListAgentConfigTombstones(userID)
	if err != nil {
		t.Fatalf("ListAgentConfigTombstones: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListAgentConfigTombstones returned %d, want 1", len(list))
	}

	// --- PurgeExpiredAgentConfigTombstoneContent zeros deleted_content
	// for rows past content_expires_at. Advance the clock past `exp`.
	if err := s.PurgeExpiredAgentConfigTombstoneContent(exp.Add(time.Hour)); err != nil {
		t.Fatalf("PurgeExpiredAgentConfigTombstoneContent: %v", err)
	}
	got, _ = s.GetAgentConfigTombstone("cursor", ".cursorrules", "project:github.com/me/repo", userID)
	if got.DeletedContent != "" {
		t.Errorf("purge didn't null deleted_content: %q", got.DeletedContent)
	}

	// Delete — should return nil error, row gone.
	if err := s.DeleteAgentConfigTombstone("cursor", ".cursorrules", "project:github.com/me/repo", userID); err != nil {
		t.Fatalf("DeleteAgentConfigTombstone: %v", err)
	}
	if _, err := s.GetAgentConfigTombstone("cursor", ".cursorrules", "project:github.com/me/repo", userID); err == nil {
		t.Error("tombstone should be gone after delete")
	}
}
