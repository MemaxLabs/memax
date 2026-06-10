package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupMemoriesCreateFixture seeds a user + personal hub and returns
// a MemoriesHandler whose enqueue hook captures processing calls
// without running the pipeline. This lets Create tests cover every
// synchronous branch (validation, provenance, hub lookup, dedup,
// insert) while skipping the LLM-heavy background work.
func setupMemoriesCreateFixture(t *testing.T) (*handler.MemoriesHandler, string, string, *atomic.Int32) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@mc",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "P", Slug: "mc-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}

	h := handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var enqueued atomic.Int32
	h.SetEnqueue(func(memoryID, ownerID string, req model.PushRequest) {
		enqueued.Add(1)
	})
	return h, ownerID, hubID, &enqueued
}

func postCreate(t *testing.T, h *handler.MemoriesHandler, ownerID, hubID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBuffer(buf))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req = handler.InjectWriteHubIDForTest(req, hubID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

func TestMemoriesCreate_HappyPathTextContent(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, enqueued := setupMemoriesCreateFixture(t)

	rec := postCreate(t, h, ownerID, hubID, map[string]any{
		"title":        "Research note",
		"content":      "Plenty of content to ingest here. Nothing special, just text.",
		"content_type": "text",
		"tags":         []string{"research"},
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Memory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID == "" {
		t.Fatal("ID should be set on response")
	}
	if resp.Data.Title != "Research note" {
		t.Errorf("Title = %q", resp.Data.Title)
	}
	if resp.Data.HubID != hubID {
		t.Errorf("HubID mismatch")
	}
	// Background processing should have been enqueued.
	if enqueued.Load() != 1 {
		t.Errorf("enqueue count = %d, want 1", enqueued.Load())
	}
}

func TestMemoriesCreate_RejectsEmptyContent(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, enqueued := setupMemoriesCreateFixture(t)
	rec := postCreate(t, h, ownerID, hubID, map[string]any{"title": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if enqueued.Load() != 0 {
		t.Errorf("no enqueue expected, got %d", enqueued.Load())
	}
}

func TestMemoriesCreate_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/memories",
		bytes.NewBufferString("not json"))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesCreate_RejectsMissingHubContext(t *testing.T) {
	t.Parallel()
	h, ownerID, _, _ := setupMemoriesCreateFixture(t)

	body, _ := json.Marshal(map[string]any{"content": "some content long enough"})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	// NOT injecting hub ID — handler must refuse.
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing hub: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesCreate_RefusesWriteForNonMember(t *testing.T) {
	t.Parallel()
	h, _, hubID, _ := setupMemoriesCreateFixture(t)

	other := uuid.NewString()
	rec := postCreate(t, h, other, hubID, map[string]any{
		"content": "attempting to write to someone else's hub",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-member write: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesUpdate_PatchesTitleAndContent(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)

	// Seed a memory via Create.
	rec := postCreate(t, h, ownerID, hubID, map[string]any{
		"title":   "Before",
		"content": "Original body text that is long enough to skip short-content guards",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data model.Memory `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Patch title + content.
	patchBody, _ := json.Marshal(map[string]any{
		"title":   "After",
		"content": "New body text that is also sufficiently long to pass guards",
	})
	patchReq := httptest.NewRequest(http.MethodPatch,
		"/v1/memories/"+created.Data.ID, bytes.NewBuffer(patchBody))
	patchReq = handler.InjectUserIDForTest(patchReq, ownerID)
	patchReq = handler.InjectHubIDForTest(patchReq, hubID)
	patchReq = handler.InjectAccessibleHubIDsForTest(patchReq, []string{hubID})
	patchReq.SetPathValue("id", created.Data.ID)
	patchRec := httptest.NewRecorder()
	h.Update(patchRec, patchReq)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("Update: %d %s", patchRec.Code, patchRec.Body.String())
	}
	var updated struct {
		Data model.Memory `json:"data"`
	}
	_ = json.Unmarshal(patchRec.Body.Bytes(), &updated)
	if updated.Data.Title != "After" {
		t.Errorf("title not updated: %q", updated.Data.Title)
	}
	if updated.Data.Content == "" || updated.Data.Content == created.Data.Content {
		t.Errorf("content not updated: %q", updated.Data.Content)
	}
}

func TestMemoriesUpdate_NotFoundForUnknownID(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)
	body, _ := json.Marshal(map[string]any{"title": "x"})
	req := httptest.NewRequest(http.MethodPatch, "/v1/memories/missing", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req.SetPathValue("id", uuid.NewString())
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestMemoriesBatchDelete_RemovesOwnedMemories(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)

	// Seed three memories.
	var ids []string
	for i := 0; i < 3; i++ {
		rec := postCreate(t, h, ownerID, hubID, map[string]any{
			"title":   "Item",
			"content": "Content with enough body to pass short-content guards at the ingest boundary",
		})
		if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
			t.Fatalf("seed %d: %d %s", i, rec.Code, rec.Body.String())
		}
		var m struct {
			Data model.Memory `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		ids = append(ids, m.Data.ID)
	}

	body, _ := json.Marshal(map[string]any{"ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("BatchDelete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesBatchDelete_RejectsEmpty(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)
	body, _ := json.Marshal(map[string]any{"ids": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty batch: expected 400, got %d", rec.Code)
	}
}

func TestMemoriesBatchDelete_RejectsTooMany(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, _ := setupMemoriesCreateFixture(t)
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	body, _ := json.Marshal(map[string]any{"ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf(">100 batch: expected 400, got %d", rec.Code)
	}
}

func TestMemoriesCreate_DedupsBySourcePath(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID, enqueued := setupMemoriesCreateFixture(t)

	body := map[string]any{
		"title":        "Dedup target",
		"content":      "Some body content",
		"source_path":  "/home/me/note.md",
		"content_type": "text",
	}
	// First push creates.
	rec1 := postCreate(t, h, ownerID, hubID, body)
	if rec1.Code != http.StatusOK && rec1.Code != http.StatusCreated {
		t.Fatalf("first push: %d %s", rec1.Code, rec1.Body.String())
	}
	var first struct {
		Data model.Memory `json:"data"`
	}
	_ = json.Unmarshal(rec1.Body.Bytes(), &first)

	// Second push with IDENTICAL content → dedup, same ID returned (or
	// 200 with existing row). enqueued count should NOT grow by 2.
	firstEnqueues := enqueued.Load()
	rec2 := postCreate(t, h, ownerID, hubID, body)
	if rec2.Code != http.StatusOK && rec2.Code != http.StatusCreated {
		t.Fatalf("second push: %d %s", rec2.Code, rec2.Body.String())
	}
	var second struct {
		Data model.Memory `json:"data"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)

	if second.Data.ID != first.Data.ID {
		t.Errorf("dedup should reuse ID: first=%s, second=%s", first.Data.ID, second.Data.ID)
	}
	// The dedup (unchanged) branch skips re-processing → enqueued stays flat.
	if enqueued.Load() > firstEnqueues {
		t.Errorf("identical re-push should not re-enqueue; enqueued went from %d → %d",
			firstEnqueues, enqueued.Load())
	}
}
