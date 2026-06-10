package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func newBarTestHandler(t *testing.T) (*BarHandler, *store.InMemoryStore) {
	t.Helper()
	s := store.NewInMemoryStore()
	memH := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return NewBarHandler(s, memH), s
}

func seedBarHub(t *testing.T, s *store.InMemoryStore, hubID, ownerID, name string) {
	t.Helper()
	hub := &model.Hub{
		ID:      hubID,
		OwnerID: ownerID,
		Name:    name,
		HubType: "personal",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}
}

func seedBarTopic(t *testing.T, s *store.InMemoryStore, hubID, ownerID, name, description string) *model.Topic {
	t.Helper()
	now := time.Now()
	topic := &model.Topic{
		ID:          "topic-" + name,
		OwnerID:     ownerID,
		HubID:       hubID,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	return topic
}

func barSearchRequest(t *testing.T, h *BarHandler, query, ownerID string, hubIDs []string) (*httptest.ResponseRecorder, barSearchResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q="+query, nil)
	ctx := context.WithValue(req.Context(), userIDKey, ownerID)
	ctx = context.WithValue(ctx, hubIDsKey, hubIDs)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data barSearchResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec, envelope.Data
}

func TestBarSearch_EmptyQuery_ReturnsEmptyPayload(t *testing.T) {
	h, s := newBarTestHandler(t)
	seedBarHub(t, s, "hub-1", "owner-a", "Personal")

	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=", nil)
	ctx := context.WithValue(req.Context(), userIDKey, "owner-a")
	ctx = context.WithValue(ctx, hubIDsKey, []string{"hub-1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty query, got %d", rec.Code)
	}
	var envelope struct {
		Data barSearchResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Data.Query != "" {
		t.Errorf("expected empty query echoed back, got %q", envelope.Data.Query)
	}
	if len(envelope.Data.Memories) != 0 || len(envelope.Data.Topics) != 0 || len(envelope.Data.Hubs) != 0 {
		t.Errorf("expected all empty arrays, got %+v", envelope.Data)
	}
}

func TestBarSearch_InvalidTopicID_Returns400(t *testing.T) {
	h, s := newBarTestHandler(t)
	seedBarHub(t, s, "hub-1", "owner-a", "Personal")

	req := httptest.NewRequest(http.MethodGet, "/v1/bar/search?q=foo&topic_id=not-a-uuid", nil)
	ctx := context.WithValue(req.Context(), userIDKey, "owner-a")
	ctx = context.WithValue(ctx, hubIDsKey, []string{"hub-1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBarSearch_TopicMultiTokenAND(t *testing.T) {
	h, s := newBarTestHandler(t)
	seedBarHub(t, s, "hub-1", "owner-a", "Personal")
	// Two tokens required: both must appear in name OR description.
	seedBarTopic(t, s, "hub-1", "owner-a", "Auth middleware", "session token storage notes")
	seedBarTopic(t, s, "hub-1", "owner-a", "Auth design", "just auth stuff")
	seedBarTopic(t, s, "hub-1", "owner-a", "Database migrations", "alembic history")

	_, data := barSearchRequest(t, h, "auth+middleware", "owner-a", []string{"hub-1"})
	// "auth middleware" tokenizes into [auth, middleware] via the server.
	// Only one topic has both. Second has only 'auth'.
	if len(data.Topics) != 1 {
		t.Fatalf("expected exactly one topic match for multi-token AND, got %d: %+v", len(data.Topics), data.Topics)
	}
	if data.Topics[0].Name != "Auth middleware" {
		t.Errorf("expected 'Auth middleware', got %q", data.Topics[0].Name)
	}
}

func TestBarSearch_HubNameMatch(t *testing.T) {
	h, s := newBarTestHandler(t)
	seedBarHub(t, s, "hub-1", "owner-a", "Engineering")
	seedBarHub(t, s, "hub-2", "owner-a", "Marketing")

	_, data := barSearchRequest(t, h, "engine", "owner-a", []string{"hub-1", "hub-2"})
	if len(data.Hubs) != 1 {
		t.Fatalf("expected one hub match, got %d: %+v", len(data.Hubs), data.Hubs)
	}
	if data.Hubs[0].Name != "Engineering" {
		t.Errorf("expected 'Engineering', got %q", data.Hubs[0].Name)
	}
}

// NOTE: a meaningful `topic_id` filter test requires Postgres — the
// InMemoryStore's SearchChunks intentionally ignores `SearchFilters`
// for both the owner-only and hub-scoped paths. A happy-path "returns
// 200" check would not catch a regression where the handler silently
// stopped threading the filter through, so it is not included here.
// Topic-id plumbing is exercised end-to-end via the Postgres-backed
// eval harness (`packages/server/eval`).

func TestBarSearch_HubScopeExcludesUnauthorizedMembership(t *testing.T) {
	// The user is a member of both hubs, but the request's authorized
	// hub scope (hubIDs) only permits hub-1. Hub rows must honor that
	// scope — otherwise a restricted grant could probe `q` to
	// enumerate hubs outside its allowlist.
	h, s := newBarTestHandler(t)
	seedBarHub(t, s, "hub-1", "owner-a", "Engineering")
	seedBarHub(t, s, "hub-2", "owner-a", "Engineering pod")

	_, data := barSearchRequest(t, h, "engineering", "owner-a", []string{"hub-1"})
	if len(data.Hubs) != 1 {
		t.Fatalf("expected only the authorized hub in results, got %d: %+v", len(data.Hubs), data.Hubs)
	}
	if data.Hubs[0].ID != "hub-1" {
		t.Errorf("expected hub-1 (authorized), got %q", data.Hubs[0].ID)
	}
}
