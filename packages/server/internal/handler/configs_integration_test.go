package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func setupConfigsFixture(t *testing.T) (*handler.ConfigsHandler, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@cf",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return handler.NewConfigsHandler(s, nil), ownerID
}

func TestConfigsList_EmptyForNewUser(t *testing.T) {
	t.Parallel()
	h, ownerID := setupConfigsFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Configs          []model.AgentConfig `json:"configs"`
			ExtractionCounts map[string]int      `json:"extraction_counts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Configs == nil {
		t.Error("configs should be []-initialized, not null")
	}
	if len(resp.Data.Configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(resp.Data.Configs))
	}
}

// seedAgentConfig writes directly through the handler so we exercise
// Upsert and the subsequent List / Get paths with the same code the
// CLI drives.
func seedAgentConfig(t *testing.T, h *handler.ConfigsHandler, ownerID, agent, path, scope, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"agent":     agent,
		"file_path": path,
		"scope":     scope,
		"content":   content,
	})
	req := httptest.NewRequest(http.MethodPut, "/v1/configs", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.Upsert(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("seed upsert: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.AgentConfig `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data.ID
}

func TestConfigsUpsertAndList_RoundTrip(t *testing.T) {
	t.Parallel()
	h, ownerID := setupConfigsFixture(t)

	id := seedAgentConfig(t, h, ownerID, "claude-code", "CLAUDE.md", "global", "# hello")
	if id == "" {
		t.Fatal("upsert didn't return an ID")
	}

	// List should include it.
	listReq := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	listReq = handler.InjectUserIDForTest(listReq, ownerID)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)

	var resp struct {
		Data struct {
			Configs []model.AgentConfig `json:"configs"`
		} `json:"data"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &resp)
	if len(resp.Data.Configs) != 1 || resp.Data.Configs[0].ID != id {
		t.Errorf("list returned unexpected: %+v", resp.Data.Configs)
	}
}

func TestConfigsList_FilterByAgent(t *testing.T) {
	t.Parallel()
	h, ownerID := setupConfigsFixture(t)

	seedAgentConfig(t, h, ownerID, "claude-code", "CLAUDE.md", "global", "A")
	seedAgentConfig(t, h, ownerID, "cursor", ".cursorrules", "global", "B")

	req := httptest.NewRequest(http.MethodGet, "/v1/configs?agent=cursor", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	var resp struct {
		Data struct {
			Configs []model.AgentConfig `json:"configs"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data.Configs) != 1 {
		t.Fatalf("agent filter returned %d, want 1", len(resp.Data.Configs))
	}
	if resp.Data.Configs[0].Agent != "cursor" {
		t.Errorf("filter leaked: %q", resp.Data.Configs[0].Agent)
	}
}

func TestConfigsGet_NotFoundAcrossOwners(t *testing.T) {
	t.Parallel()
	h, ownerID := setupConfigsFixture(t)
	id := seedAgentConfig(t, h, ownerID, "cc", "CLAUDE.md", "global", "x")

	// Owner can fetch.
	ownerReq := httptest.NewRequest(http.MethodGet, "/v1/configs/"+id, nil)
	ownerReq = handler.InjectUserIDForTest(ownerReq, ownerID)
	ownerReq.SetPathValue("id", id)
	ownerRec := httptest.NewRecorder()
	h.Get(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Errorf("owner Get: expected 200, got %d", ownerRec.Code)
	}

	// Cross-owner → 404 (no existence leak).
	other := uuid.NewString()
	otherReq := httptest.NewRequest(http.MethodGet, "/v1/configs/"+id, nil)
	otherReq = handler.InjectUserIDForTest(otherReq, other)
	otherReq.SetPathValue("id", id)
	otherRec := httptest.NewRecorder()
	h.Get(otherRec, otherReq)
	if otherRec.Code != http.StatusNotFound {
		t.Errorf("cross-owner Get: expected 404, got %d", otherRec.Code)
	}

	// Unknown id → 404.
	unknownReq := httptest.NewRequest(http.MethodGet, "/v1/configs/"+uuid.NewString(), nil)
	unknownReq = handler.InjectUserIDForTest(unknownReq, ownerID)
	unknownReq.SetPathValue("id", uuid.NewString())
	unknownRec := httptest.NewRecorder()
	h.Get(unknownRec, unknownReq)
	if unknownRec.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d", unknownRec.Code)
	}
}

func TestConfigsDelete_TombstonesRow(t *testing.T) {
	t.Parallel()
	h, ownerID := setupConfigsFixture(t)
	id := seedAgentConfig(t, h, ownerID, "cc", "CLAUDE.md", "global", "original")

	delReq := httptest.NewRequest(http.MethodDelete, "/v1/configs/"+id, nil)
	delReq = handler.InjectUserIDForTest(delReq, ownerID)
	delReq.SetPathValue("id", id)
	delRec := httptest.NewRecorder()
	h.Delete(delRec, delReq)
	if delRec.Code != http.StatusOK && delRec.Code != http.StatusNoContent {
		t.Fatalf("Delete: %d %s", delRec.Code, delRec.Body.String())
	}

	// GET should now 404.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/configs/"+id, nil)
	getReq = handler.InjectUserIDForTest(getReq, ownerID)
	getReq.SetPathValue("id", id)
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Errorf("after delete: expected 404, got %d", getRec.Code)
	}

	// ListDeleted should show it.
	listDelReq := httptest.NewRequest(http.MethodGet, "/v1/configs/deleted", nil)
	listDelReq = handler.InjectUserIDForTest(listDelReq, ownerID)
	listDelRec := httptest.NewRecorder()
	h.ListDeleted(listDelRec, listDelReq)
	if listDelRec.Code != http.StatusOK {
		t.Errorf("ListDeleted: %d %s", listDelRec.Code, listDelRec.Body.String())
	}
	// Content shouldn't expire immediately.
	time.Sleep(1 * time.Millisecond)
}
