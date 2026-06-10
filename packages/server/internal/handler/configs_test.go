package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestConfigsSyncPushesWhenLocalChangedSinceLastSync(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	cfg := &model.AgentConfig{
		ID:          "cfg-1",
		OwnerID:     "owner-a",
		Agent:       "claude-code",
		FilePath:    "CLAUDE.md",
		Scope:       "global",
		Content:     "old",
		ContentHash: "old-hash",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           cfg.Agent,
		FilePath:        cfg.FilePath,
		Scope:           cfg.Scope,
		LocalPath:       "/home/test/.claude/CLAUDE.md",
		LastSeenVersion: 1,
		LastSeenHash:    "old-hash",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[{"agent":"claude-code","file_path":"CLAUDE.md","scope":"global","content_hash":"new-hash","updated_at":"2026-01-01T00:00:00Z"}]}`)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0]["action"] != "push" {
		t.Fatalf("expected push action, got %#v", actions[0])
	}
}

func TestConfigsSyncPullsWhenCloudChangedSinceLastSync(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	cfg := &model.AgentConfig{
		ID:          "cfg-1",
		OwnerID:     "owner-a",
		Agent:       "claude-code",
		FilePath:    "CLAUDE.md",
		Scope:       "global",
		Content:     "old",
		ContentHash: "old-hash",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	cfg.Content = "cloud-new"
	cfg.ContentHash = "cloud-new-hash"
	cfg.UpdatedAt = now.Add(time.Minute)
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig update: %v", err)
	}
	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           cfg.Agent,
		FilePath:        cfg.FilePath,
		Scope:           cfg.Scope,
		LocalPath:       "/home/test/.claude/CLAUDE.md",
		LastSeenVersion: 1,
		LastSeenHash:    "old-hash",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[{"agent":"claude-code","file_path":"CLAUDE.md","scope":"global","content_hash":"old-hash","updated_at":"2026-01-01T00:00:00Z"}]}`)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0]["action"] != "pull" {
		t.Fatalf("expected pull action, got %#v", actions[0])
	}
}

func TestConfigsSyncConflictsWhenBothChanged(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	cfg := &model.AgentConfig{
		ID:          "cfg-1",
		OwnerID:     "owner-a",
		Agent:       "claude-code",
		FilePath:    "CLAUDE.md",
		Scope:       "global",
		Content:     "old",
		ContentHash: "old-hash",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	cfg.Content = "cloud-new"
	cfg.ContentHash = "cloud-new-hash"
	cfg.UpdatedAt = now.Add(time.Minute)
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig update: %v", err)
	}
	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           cfg.Agent,
		FilePath:        cfg.FilePath,
		Scope:           cfg.Scope,
		LocalPath:       "/home/test/.claude/CLAUDE.md",
		LastSeenVersion: 1,
		LastSeenHash:    "old-hash",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[{"agent":"claude-code","file_path":"CLAUDE.md","scope":"global","content_hash":"local-new-hash","updated_at":"2026-01-01T00:00:00Z"}]}`)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0]["action"] != "conflict" {
		t.Fatalf("expected conflict action, got %#v", actions[0])
	}
}

func TestConfigsUpsertPersistsDeviceState(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/configs", bytes.NewBufferString(`{
		"agent":"gemini",
		"file_path":"GEMINI.md",
		"scope":"global",
		"content":"hello",
		"device_id":"device-1",
		"local_path":"/home/test/.gemini/GEMINI.md"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.Upsert(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	states, err := s.ListAgentConfigSyncStates("owner-a", "device-1")
	if err != nil {
		t.Fatalf("ListAgentConfigSyncStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 sync state, got %d", len(states))
	}
	if states[0].LocalPath != "/home/test/.gemini/GEMINI.md" {
		t.Fatalf("unexpected local path: %#v", states[0])
	}
}

func TestConfigsSyncDeletesLocalWhenCloudTombstoned(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           "codex",
		FilePath:        "AGENTS.md",
		Scope:           "global",
		LocalPath:       "/home/test/.codex/AGENTS.md",
		LastSeenVersion: 2,
		LastSeenHash:    "old-hash",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}
	if err := s.CreateAgentConfigTombstone(&model.AgentConfigTombstone{
		OwnerID:   "owner-a",
		Agent:     "codex",
		FilePath:  "AGENTS.md",
		Scope:     "global",
		Version:   3,
		DeletedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateAgentConfigTombstone: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[]}`)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0]["action"] != "delete_local" {
		t.Fatalf("expected delete_local action, got %#v", actions[0])
	}
}

func TestConfigsSyncSkipsSuppressedCloudOnlyConfig(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	cfg := &model.AgentConfig{
		ID:          "cfg-1",
		OwnerID:     "owner-a",
		Agent:       "gemini",
		FilePath:    "GEMINI.md",
		Scope:       "global",
		Content:     "hello",
		ContentHash: "hello-hash",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           "gemini",
		FilePath:        "GEMINI.md",
		Scope:           "global",
		LocalPath:       "/home/test/.gemini/GEMINI.md",
		LastSeenVersion: 1,
		LastSeenHash:    "hello-hash",
		Suppressed:      true,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[]}`)
	if len(actions) != 0 {
		t.Fatalf("expected no actions for suppressed config, got %#v", actions)
	}
}

func TestConfigsDeleteCreatesTombstone(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	cfg := &model.AgentConfig{
		ID:          "cfg-1",
		OwnerID:     "owner-a",
		Agent:       "claude-code",
		FilePath:    "CLAUDE.md",
		Scope:       "global",
		Content:     "hello",
		ContentHash: "hello-hash",
		Version:     2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/v1/configs/cfg-1", nil)
	req.SetPathValue("id", "cfg-1")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.Delete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tombstones, err := s.ListAgentConfigTombstones("owner-a")
	if err != nil {
		t.Fatalf("ListAgentConfigTombstones: %v", err)
	}
	if len(tombstones) != 1 {
		t.Fatalf("expected 1 tombstone, got %d", len(tombstones))
	}
	if tombstones[0].Version != 3 {
		t.Fatalf("expected tombstone version 3, got %#v", tombstones[0])
	}
	if tombstones[0].DeletedContent != "hello" {
		t.Fatalf("expected deleted content backup, got %#v", tombstones[0])
	}
	if tombstones[0].ContentExpiresAt == nil {
		t.Fatalf("expected content retention expiry on tombstone")
	}
}

func TestConfigsListDeletedReturnsRecoverableBackups(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	if err := s.CreateAgentConfigTombstone(&model.AgentConfigTombstone{
		OwnerID:            "owner-a",
		Agent:              "claude-code",
		FilePath:           "CLAUDE.md",
		Scope:              "global",
		Version:            2,
		DeletedAt:          now,
		DeletedContent:     "backup",
		DeletedContentHash: "hash-a",
		ContentExpiresAt:   &expiresAt,
	}); err != nil {
		t.Fatalf("CreateAgentConfigTombstone: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/configs/deleted", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.ListDeleted(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Configs []model.RecoverableAgentConfigTombstone `json:"configs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data.Configs) != 1 {
		t.Fatalf("expected 1 recoverable config, got %#v", resp.Data.Configs)
	}
	if resp.Data.Configs[0].FilePath != "CLAUDE.md" {
		t.Fatalf("unexpected deleted config payload: %#v", resp.Data.Configs[0])
	}
}

func TestConfigsRestoreRecreatesDeletedConfig(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	if err := s.CreateAgentConfigTombstone(&model.AgentConfigTombstone{
		OwnerID:            "owner-a",
		Agent:              "codex",
		FilePath:           "AGENTS.md",
		Scope:              "global",
		Version:            4,
		DeletedAt:          now,
		DeletedContent:     "restored content",
		DeletedContentHash: "restored-hash",
		ContentExpiresAt:   &expiresAt,
	}); err != nil {
		t.Fatalf("CreateAgentConfigTombstone: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/configs/restore", bytes.NewBufferString(`{
		"agent":"codex",
		"file_path":"AGENTS.md",
		"scope":"global",
		"device_id":"device-1",
		"local_path":"/home/test/.codex/AGENTS.md"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.Restore(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	cfg, err := s.GetAgentConfigByPath("codex", "AGENTS.md", "global", "owner-a")
	if err != nil {
		t.Fatalf("GetAgentConfigByPath: %v", err)
	}
	if cfg.Content != "restored content" {
		t.Fatalf("expected restored content, got %#v", cfg)
	}
	tombstones, err := s.ListAgentConfigTombstones("owner-a")
	if err != nil {
		t.Fatalf("ListAgentConfigTombstones: %v", err)
	}
	if len(tombstones) != 0 {
		t.Fatalf("expected tombstone to be cleared after restore, got %#v", tombstones)
	}
	states, err := s.ListAgentConfigSyncStates("owner-a", "device-1")
	if err != nil {
		t.Fatalf("ListAgentConfigSyncStates: %v", err)
	}
	if len(states) != 1 || states[0].LastSeenVersion != cfg.Version {
		t.Fatalf("expected sync state to advance, got %#v", states)
	}
}

func TestConfigsAckDeletedAdvancesDeviceState(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           "codex",
		FilePath:        "AGENTS.md",
		Scope:           "global",
		LocalPath:       "/home/test/.codex/AGENTS.md",
		LastSeenVersion: 2,
		LastSeenHash:    "old-hash",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}
	if err := s.CreateAgentConfigTombstone(&model.AgentConfigTombstone{
		OwnerID:   "owner-a",
		Agent:     "codex",
		FilePath:  "AGENTS.md",
		Scope:     "global",
		Version:   3,
		DeletedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateAgentConfigTombstone: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/configs/ack", bytes.NewBufferString(`{
		"device_id":"device-1",
		"configs":[{"agent":"codex","file_path":"AGENTS.md","scope":"global","version":3,"deleted":true,"local_path":"/home/test/.codex/AGENTS.md"}]
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.Ack(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[]}`)
	if len(actions) != 0 {
		t.Fatalf("expected no actions after delete ack, got %#v", actions)
	}
}

func TestConfigsSyncPullsRecreatedCloudConfigAfterDeleteAck(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)
	now := time.Now()

	if err := s.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         "owner-a",
		DeviceID:        "device-1",
		Agent:           "codex",
		FilePath:        "AGENTS.md",
		Scope:           "global",
		LocalPath:       "/home/test/.codex/AGENTS.md",
		LastSeenVersion: 3,
		LastSeenHash:    "",
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertAgentConfigSyncState: %v", err)
	}
	cfg := &model.AgentConfig{
		ID:          "cfg-2",
		OwnerID:     "owner-a",
		Agent:       "codex",
		FilePath:    "AGENTS.md",
		Scope:       "global",
		Content:     "recreated",
		ContentHash: "recreated-hash",
		Version:     4,
		CreatedAt:   now,
		UpdatedAt:   now.Add(time.Minute),
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}

	actions := runConfigSync(t, h, `{"device_id":"device-1","configs":[]}`)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0]["action"] != "pull" {
		t.Fatalf("expected pull action, got %#v", actions[0])
	}
}

// failingAgentConfigStore wraps InMemoryStore so tests can inject store
// errors on DeleteAgentConfig or CreateAgentConfigTombstone for a targeted
// subset of ids, without rewriting the whole store. Used to lock in the
// delete_failed skip-reason path.
type failingAgentConfigStore struct {
	*store.InMemoryStore
	failDeleteIDs     map[string]bool
	failTombstoneIDs  map[string]bool
	failAllDelete     bool
	failAllTombstones bool
}

func (f *failingAgentConfigStore) DeleteAgentConfig(id, ownerID string) error {
	if f.failAllDelete || f.failDeleteIDs[id] {
		return fmt.Errorf("injected delete failure for id=%s", id)
	}
	return f.InMemoryStore.DeleteAgentConfig(id, ownerID)
}

func (f *failingAgentConfigStore) CreateAgentConfigTombstone(t *model.AgentConfigTombstone) error {
	// Tombstones are keyed by (agent, file_path, scope) rather than config id,
	// but the test wrappers key failures by the original config id, so we
	// read the id off the already-looked-up config path. The test provides
	// a pre-built lookup map.
	if f.failAllTombstones {
		return fmt.Errorf("injected tombstone failure")
	}
	return f.InMemoryStore.CreateAgentConfigTombstone(t)
}

// makeAgentConfig creates and persists a config in the in-memory store.
func makeAgentConfig(t *testing.T, s store.Store, id, ownerID, agent, filePath string, version int) *model.AgentConfig {
	t.Helper()
	now := time.Now()
	cfg := &model.AgentConfig{
		ID:          id,
		OwnerID:     ownerID,
		Agent:       agent,
		FilePath:    filePath,
		Scope:       "global",
		Content:     "content-" + id,
		ContentHash: "hash-" + id,
		Version:     version,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertAgentConfig(cfg); err != nil {
		t.Fatalf("UpsertAgentConfig(%s): %v", id, err)
	}
	return cfg
}

func callBatchDelete(t *testing.T, h *ConfigsHandler, ownerID string, ids []string) model.BatchDeleteResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/configs/batch-delete", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, ownerID))
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.BatchDeleteResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	// Defensive: API contract says skipped is always an array, never null.
	if resp.Data.Skipped == nil {
		resp.Data.Skipped = []model.SkippedMemory{}
	}
	return resp.Data
}

// TestConfigsBatchDeleteFullSuccess — every id committed, empty skipped,
// both tombstones persisted, both rows removed. Happy path.
func TestConfigsBatchDeleteFullSuccess(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	cfg1 := makeAgentConfig(t, s, "cfg-1", "owner-a", "claude-code", "CLAUDE.md", 2)
	cfg2 := makeAgentConfig(t, s, "cfg-2", "owner-a", "cursor", "AGENTS.md", 1)

	result := callBatchDelete(t, h, "owner-a", []string{cfg1.ID, cfg2.ID})

	if result.Deleted != 2 {
		t.Fatalf("expected deleted=2, got %d", result.Deleted)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected empty skipped, got %+v", result.Skipped)
	}

	for _, id := range []string{cfg1.ID, cfg2.ID} {
		if _, err := s.GetAgentConfig(id, "owner-a"); err == nil {
			t.Fatalf("expected config %s to be deleted", id)
		}
	}
	tombstones, err := s.ListAgentConfigTombstones("owner-a")
	if err != nil {
		t.Fatalf("ListAgentConfigTombstones: %v", err)
	}
	if len(tombstones) != 2 {
		t.Fatalf("expected 2 tombstones, got %d", len(tombstones))
	}
}

// TestConfigsBatchDeleteMixedWithNotFound — one real id + one unknown id.
// Real id is committed (deleted > 0), unknown id is skipped as not_found.
// This is the partial-success case the web hook reads as "target reached"
// and the CLI reads as "still run local cleanup."
func TestConfigsBatchDeleteMixedWithNotFound(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	cfg := makeAgentConfig(t, s, "cfg-real", "owner-a", "claude-code", "CLAUDE.md", 1)

	result := callBatchDelete(t, h, "owner-a", []string{cfg.ID, "cfg-ghost"})

	if result.Deleted != 1 {
		t.Fatalf("expected deleted=1, got %d", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %+v", result.Skipped)
	}
	skip := result.Skipped[0]
	if skip.ID != "cfg-ghost" {
		t.Fatalf("expected skipped id=cfg-ghost, got %s", skip.ID)
	}
	if skip.Reason != model.BatchDeleteSkipNotFound {
		t.Fatalf("expected reason=not_found, got %s", skip.Reason)
	}
	if _, err := s.GetAgentConfig(cfg.ID, "owner-a"); err == nil {
		t.Fatalf("expected cfg-real to be deleted")
	}
}

// TestConfigsBatchDeleteMixedWithDeleteFailed — one real id committed, one
// real id that fails at the store layer. The committed id advances Deleted,
// the failed id surfaces as delete_failed so the client can distinguish
// infra failure from already-gone.
func TestConfigsBatchDeleteMixedWithDeleteFailed(t *testing.T) {
	inner := store.NewInMemoryStore()
	s := &failingAgentConfigStore{
		InMemoryStore: inner,
		failDeleteIDs: map[string]bool{"cfg-fail": true},
	}
	h := NewConfigsHandler(s, nil)

	makeAgentConfig(t, inner, "cfg-ok", "owner-a", "claude-code", "CLAUDE.md", 1)
	makeAgentConfig(t, inner, "cfg-fail", "owner-a", "cursor", "AGENTS.md", 1)

	result := callBatchDelete(t, h, "owner-a", []string{"cfg-ok", "cfg-fail"})

	if result.Deleted != 1 {
		t.Fatalf("expected deleted=1 (cfg-ok committed), got %d", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %+v", result.Skipped)
	}
	skip := result.Skipped[0]
	if skip.ID != "cfg-fail" {
		t.Fatalf("expected skipped id=cfg-fail, got %s", skip.ID)
	}
	if skip.Reason != model.BatchDeleteSkipDeleteFailed {
		t.Fatalf("expected reason=delete_failed, got %s", skip.Reason)
	}
	// cfg-ok must be removed; cfg-fail must still exist on the server so
	// the client's onError rollback reveals it to the user.
	if _, err := inner.GetAgentConfig("cfg-ok", "owner-a"); err == nil {
		t.Fatalf("expected cfg-ok to be deleted")
	}
	if _, err := inner.GetAgentConfig("cfg-fail", "owner-a"); err != nil {
		t.Fatalf("expected cfg-fail to still exist on server, got err=%v", err)
	}
}

// TestConfigsBatchDeleteFullSkipNotFound — every requested id is unknown.
// Client treats this as idempotent success (nothing to roll back, the user's
// target state is already reached) and does NOT throw.
func TestConfigsBatchDeleteFullSkipNotFound(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewConfigsHandler(s, nil)

	result := callBatchDelete(t, h, "owner-a", []string{"ghost-1", "ghost-2"})

	if result.Deleted != 0 {
		t.Fatalf("expected deleted=0, got %d", result.Deleted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %+v", result.Skipped)
	}
	for _, entry := range result.Skipped {
		if entry.Reason != model.BatchDeleteSkipNotFound {
			t.Fatalf("expected all skipped reasons=not_found, got %+v", result.Skipped)
		}
	}
}

// TestConfigsBatchDeleteFullSkipDeleteFailed — every attempted delete
// errors at the store layer. Client treats this as a real failure and rolls
// back the optimistic list removal via onError.
func TestConfigsBatchDeleteFullSkipDeleteFailed(t *testing.T) {
	inner := store.NewInMemoryStore()
	s := &failingAgentConfigStore{
		InMemoryStore: inner,
		failAllDelete: true,
	}
	h := NewConfigsHandler(s, nil)

	makeAgentConfig(t, inner, "cfg-1", "owner-a", "claude-code", "CLAUDE.md", 1)
	makeAgentConfig(t, inner, "cfg-2", "owner-a", "cursor", "AGENTS.md", 1)

	result := callBatchDelete(t, h, "owner-a", []string{"cfg-1", "cfg-2"})

	if result.Deleted != 0 {
		t.Fatalf("expected deleted=0, got %d", result.Deleted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %+v", result.Skipped)
	}
	for _, entry := range result.Skipped {
		if entry.Reason != model.BatchDeleteSkipDeleteFailed {
			t.Fatalf("expected all skipped reasons=delete_failed, got %+v", result.Skipped)
		}
	}
	// Both configs must still exist on the server — delete_failed must
	// not silently drop rows that the store refused to delete.
	for _, id := range []string{"cfg-1", "cfg-2"} {
		if _, err := inner.GetAgentConfig(id, "owner-a"); err != nil {
			t.Fatalf("expected %s to still exist, got err=%v", id, err)
		}
	}
}

func runConfigSync(t *testing.T, h *ConfigsHandler, body string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/configs/sync", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()

	h.Sync(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Actions []map[string]any `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return resp.Data.Actions
}
