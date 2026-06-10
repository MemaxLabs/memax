package handler_test

import (
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

func setupBarFixture(t *testing.T) (*handler.BarHandler, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'B', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@b",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "BarHub", Slug: "b-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}

	memories := handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return handler.NewBarHandler(s, memories), ownerID, hubID
}

func TestBarSearch_EmptyQueryReturnsEmptyShape(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupBarFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Query    string `json:"query"`
			Memories []any  `json:"memories"`
			Topics   []any  `json:"topics"`
			Hubs     []any  `json:"hubs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Query != "" {
		t.Errorf("Query echo = %q", resp.Data.Query)
	}
	if resp.Data.Memories == nil || resp.Data.Topics == nil || resp.Data.Hubs == nil {
		t.Error("empty arrays should be []-initialized, not null")
	}
}

func TestBarSearch_RejectsInvalidTopicID(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupBarFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=test&topic_id=bad", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid topic_id: expected 400, got %d", rec.Code)
	}
}

func TestBarSearch_HubNameMatchesForOwnedHub(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupBarFixture(t)
	// Hub is named "BarHub" — query matches prefix.
	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=Bar", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hubID})
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Hubs []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"hubs"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	found := false
	for _, h := range resp.Data.Hubs {
		if h.ID == hubID && h.Name == "BarHub" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("owned hub not in results: %+v", resp.Data.Hubs)
	}
}

func TestBarSearch_HubsRespectAccessScope(t *testing.T) {
	t.Parallel()
	h, ownerID, _ := setupBarFixture(t)
	// Caller authorized for NO hubs — hub-name match should return empty
	// even though the user technically owns one. This is the defense
	// against API-key scope leak via name enumeration.
	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=Bar", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{}) // empty scope
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Hubs []any `json:"hubs"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data.Hubs) != 0 {
		t.Errorf("empty scope: expected 0 hubs, got %d", len(resp.Data.Hubs))
	}
}
