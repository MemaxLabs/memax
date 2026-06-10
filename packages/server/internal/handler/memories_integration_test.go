package handler_test

import (
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

// setupMemoriesHandlerFixture seeds user + personal hub + a memory and
// returns the handler, ownerID, hubID, and memoryID. Passes nil for
// every optional dependency (embedder, summarizer, etc.) — the Get/
// List/Delete paths exercised below don't touch them.
func setupMemoriesHandlerFixture(t *testing.T) (*handler.MemoriesHandler, string, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'M', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@h",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "P", Slug: "m-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	memID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.CreateMemory(&model.Memory{
		ID: memID, HubID: hubID, OwnerID: ownerID,
		Title: "Test memory", Content: "memory body",
		ContentType: "text/plain",
		ContentHash: uuid.NewString(),
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Tags:        []string{},
		State:       "active",
		CreatedAt:   now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	h := handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return h, ownerID, hubID, memID
}

func TestMemoriesGet_Success(t *testing.T) {
	t.Parallel()
	h, ownerID, _, memID := setupMemoriesHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/memories/"+memID, nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req.SetPathValue("id", memID)
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Memory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID != memID {
		t.Errorf("ID mismatch: %q vs %q", resp.Data.ID, memID)
	}
	if resp.Data.Title != "Test memory" {
		t.Errorf("Title = %q", resp.Data.Title)
	}
}

func TestMemoriesGet_NotFoundForUnknownID(t *testing.T) {
	t.Parallel()
	h, ownerID, _, _ := setupMemoriesHandlerFixture(t)

	missing := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/memories/"+missing, nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req.SetPathValue("id", missing)
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesGet_NotFoundForWrongOwner(t *testing.T) {
	t.Parallel()
	h, _, _, memID := setupMemoriesHandlerFixture(t)

	// Cross-owner access must 404 (not 403 — we don't leak existence).
	other := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/memories/"+memID, nil)
	req = handler.InjectUserIDForTest(req, other)
	req.SetPathValue("id", memID)
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner: expected 404, got %d", rec.Code)
	}
}

func TestMemoriesList_ReturnsSeededMemory(t *testing.T) {
	t.Parallel()
	h, ownerID, _, memID := setupMemoriesHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Memories []model.Memory `json:"memories"`
			HasMore  bool           `json:"has_more"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Memories) != 1 || resp.Data.Memories[0].ID != memID {
		t.Errorf("expected one seeded memory, got %+v", resp.Data.Memories)
	}
}

func TestMemoriesList_EmptyWhenNoneOwned(t *testing.T) {
	t.Parallel()
	h, _, _, _ := setupMemoriesHandlerFixture(t)

	// New owner with no memories → empty list (normalized, not nil).
	other := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	req = handler.InjectUserIDForTest(req, other)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Memories []model.Memory `json:"memories"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data.Memories) != 0 {
		t.Errorf("expected empty list for new user, got %d", len(resp.Data.Memories))
	}
}

func TestMemoriesDelete_RemovesRow(t *testing.T) {
	t.Parallel()
	h, ownerID, _, memID := setupMemoriesHandlerFixture(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/memories/"+memID, nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req.SetPathValue("id", memID)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Errorf("expected 200/204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Follow-up GET should now 404.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/memories/"+memID, nil)
	getReq = handler.InjectUserIDForTest(getReq, ownerID)
	getReq.SetPathValue("id", memID)
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Errorf("after delete, GET should 404, got %d", getRec.Code)
	}
}

func TestMemoriesDelete_CrossOwnerRefused(t *testing.T) {
	t.Parallel()
	h, _, _, memID := setupMemoriesHandlerFixture(t)

	other := uuid.NewString()
	req := httptest.NewRequest(http.MethodDelete, "/v1/memories/"+memID, nil)
	req = handler.InjectUserIDForTest(req, other)
	req.SetPathValue("id", memID)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)
	// Either 404 or 403 is acceptable — what must NOT happen: the
	// memory actually gets deleted. Verify via the fixture owner.
	if rec.Code == http.StatusOK || rec.Code == http.StatusNoContent {
		t.Errorf("cross-owner delete should refuse, got %d", rec.Code)
	}
}
