package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// disconnectTestStore wraps InMemoryStore to implement just enough of the
// ConnectedAgent + count-query + delete surface for the Disconnect handler
// tests. InMemoryStore's connected-agent methods are stubs so we can't use
// it directly — this wrapper provides predictable responses plus hooks to
// inject store errors on the delete path.
type disconnectTestStore struct {
	*store.InMemoryStore
	agents       map[string]*model.ConnectedAgent // key = ownerID + "|" + slug
	keyCounts    map[string]int                   // key = ownerID + "|" + slug
	configCounts map[string]int                   // key = ownerID + "|" + slug
	failDelete   bool                             // when true, DeleteConnectedAgent errors
}

func newDisconnectTestStore() *disconnectTestStore {
	return &disconnectTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		agents:        map[string]*model.ConnectedAgent{},
		keyCounts:     map[string]int{},
		configCounts:  map[string]int{},
	}
}

func (s *disconnectTestStore) key(ownerID, slug string) string {
	return ownerID + "|" + slug
}

func (s *disconnectTestStore) seedAgent(ownerID, slug string, keys, configs int) {
	k := s.key(ownerID, slug)
	s.agents[k] = &model.ConnectedAgent{
		OwnerID:   ownerID,
		AgentName: slug,
	}
	s.keyCounts[k] = keys
	s.configCounts[k] = configs
}

func (s *disconnectTestStore) GetConnectedAgent(ownerID, slug string) (*model.ConnectedAgent, error) {
	if agent, ok := s.agents[s.key(ownerID, slug)]; ok {
		return agent, nil
	}
	return nil, fmt.Errorf("not found")
}

func (s *disconnectTestStore) CountAgentApiKeys(ownerID, slug string) (int, error) {
	return s.keyCounts[s.key(ownerID, slug)], nil
}

func (s *disconnectTestStore) CountAgentConfigs(ownerID, slug string) (int, error) {
	return s.configCounts[s.key(ownerID, slug)], nil
}

func (s *disconnectTestStore) DeleteConnectedAgent(ownerID, slug string) error {
	if s.failDelete {
		return fmt.Errorf("injected cascade failure")
	}
	delete(s.agents, s.key(ownerID, slug))
	delete(s.keyCounts, s.key(ownerID, slug))
	delete(s.configCounts, s.key(ownerID, slug))
	return nil
}

func callDisconnect(t *testing.T, h *AgentsHandler, ownerID, slug string) model.AgentDisconnectResult {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/v1/agents/"+slug, bytes.NewReader(nil))
	req.SetPathValue("slug", slug)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, ownerID))
	rec := httptest.NewRecorder()
	h.Disconnect(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.AgentDisconnectResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Data.Skipped == nil {
		resp.Data.Skipped = []model.SkippedMemory{}
	}
	return resp.Data
}

// TestDisconnectHappyPathReportsCounts — agent exists, cascade succeeds,
// result carries the pre-queried counts so the client can show
// "disconnected claude-code — revoked 3 keys, forgot 12 configs".
func TestDisconnectHappyPathReportsCounts(t *testing.T) {
	s := newDisconnectTestStore()
	s.seedAgent("owner-a", "claude-code", 3, 12)
	h := NewAgentsHandler(s, nil)

	result := callDisconnect(t, h, "owner-a", "claude-code")

	if !result.Disconnected {
		t.Fatalf("expected disconnected=true, got %+v", result)
	}
	if result.KeysRevoked != 3 {
		t.Fatalf("expected keys_revoked=3, got %d", result.KeysRevoked)
	}
	if result.ConfigsTombstoned != 12 {
		t.Fatalf("expected configs_tombstoned=12, got %d", result.ConfigsTombstoned)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected empty skipped, got %+v", result.Skipped)
	}
	// Agent row must be gone from the store.
	if _, err := s.GetConnectedAgent("owner-a", "claude-code"); err == nil {
		t.Fatalf("expected agent to be deleted")
	}
}

// TestDisconnectNotFoundReturns200WithSkip — unknown slug (or another
// user's agent). Client must NOT roll back the optimistic removal —
// target state is already reached.
func TestDisconnectNotFoundReturns200WithSkip(t *testing.T) {
	s := newDisconnectTestStore()
	h := NewAgentsHandler(s, nil)

	result := callDisconnect(t, h, "owner-a", "ghost-agent")

	if result.Disconnected {
		t.Fatalf("expected disconnected=false, got %+v", result)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	skip := result.Skipped[0]
	if skip.ID != "ghost-agent" {
		t.Fatalf("expected skipped id=ghost-agent, got %s", skip.ID)
	}
	if skip.Reason != model.AgentDisconnectSkipNotFound {
		t.Fatalf("expected reason=not_found, got %s", skip.Reason)
	}
}

// TestDisconnectCascadeFailedRollsBack — agent exists but the cascade
// errors. Server must surface `cascade_failed` so the web hook throws
// `AgentDisconnectIncompleteError` and rolls back the optimistic cache.
func TestDisconnectCascadeFailedRollsBack(t *testing.T) {
	s := newDisconnectTestStore()
	s.seedAgent("owner-a", "claude-code", 1, 2)
	s.failDelete = true
	h := NewAgentsHandler(s, nil)

	result := callDisconnect(t, h, "owner-a", "claude-code")

	if result.Disconnected {
		t.Fatalf("expected disconnected=false on cascade failure, got %+v", result)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	skip := result.Skipped[0]
	if skip.Reason != model.AgentDisconnectSkipCascadeFailed {
		t.Fatalf("expected reason=cascade_failed, got %s", skip.Reason)
	}
	// Agent row must still exist on the server — the test wrapper
	// returned an error without mutating state.
	if _, err := s.GetConnectedAgent("owner-a", "claude-code"); err != nil {
		t.Fatalf("expected agent to still exist after cascade failure, got err=%v", err)
	}
}

// TestDisconnectMissingSlugReturns400 — the BadRequest path is still a
// hard error since there's no recoverable interpretation of a missing
// path parameter.
func TestDisconnectMissingSlugReturns400(t *testing.T) {
	s := newDisconnectTestStore()
	h := NewAgentsHandler(s, nil)

	req := httptest.NewRequest(http.MethodDelete, "/v1/agents/", bytes.NewReader(nil))
	req.SetPathValue("slug", "")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "owner-a"))
	rec := httptest.NewRecorder()
	h.Disconnect(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing slug, got %d: %s", rec.Code, rec.Body.String())
	}
}
