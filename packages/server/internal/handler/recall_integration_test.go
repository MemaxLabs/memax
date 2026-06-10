package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func setupRecallFixture(t *testing.T) (*handler.RecallHandler, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'R', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@re",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "P", Slug: "re-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}

	// nil embedder/distiller/reranker/cache is fine — Recall gracefully
	// falls back to the lexical-only path and returns empty results.
	return handler.NewRecallHandler(s, nil, nil, nil, nil), ownerID, hubID
}

func TestRecall_RejectsEmptyQuery(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupRecallFixture(t)
	body, _ := json.Marshal(map[string]any{"query": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty query: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRecall_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupRecallFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBufferString("not json"))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: expected 400, got %d", rec.Code)
	}
}

func TestRecall_RejectsInvalidTopicID(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupRecallFixture(t)
	body, _ := json.Marshal(map[string]any{
		"query":    "search query",
		"topic_id": "not-a-uuid",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid topic_id: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRecall_EmptyResultsForEmptyStore(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupRecallFixture(t)
	body, _ := json.Marshal(map[string]any{"query": "anything goes here"})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.RecallResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Memories) != 0 {
		t.Errorf("empty store: expected 0 memories, got %d", len(resp.Data.Memories))
	}
}

func TestRecall_SetRerankPolicyAcceptsNil(t *testing.T) {
	t.Parallel()
	h, _, _ := setupRecallFixture(t)
	// SetRerankPolicy(nil) must install the default "allow all" policy,
	// not panic on a nil callback.
	h.SetRerankPolicy(nil)
	h.SetRerankPolicy(func(ownerID, source string) bool { return false })
}
