package store

import (
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestInMemorySearchChunksMatchesTagsText(t *testing.T) {
	s := NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:              "mem-tags",
		HubID:           "hub-1",
		OwnerID:         "user-1",
		Title:           "Unrelated title",
		Content:         "plain content",
		ContentType:     "text",
		ContentHash:     "hash",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Boundary:        "private",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{{
		ID:         "chunk-tags",
		MemoryID:   memory.ID,
		Content:    "alpha beta gamma",
		TagsText:   "person:ziyang person ziyang ziyang",
		ChunkIndex: 0,
		CreatedAt:  now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	chunks, err := s.SearchChunks(t.Context(), "Ziyang", nil, memory.OwnerID, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected one tag-matched chunk, got %d", len(chunks))
	}
	if chunks[0].ID != "chunk-tags" {
		t.Fatalf("expected chunk-tags, got %q", chunks[0].ID)
	}
}

func TestInMemorySearchChunksMatchesMetadataText(t *testing.T) {
	s := NewInMemoryStore()
	now := time.Now()
	memory := &model.Memory{
		ID:              "mem-author",
		HubID:           "hub-1",
		OwnerID:         "user-1",
		Title:           "Unrelated title",
		Content:         "plain content",
		ContentType:     "text",
		ContentHash:     "hash",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Boundary:        "private",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{{
		ID:           "chunk-author",
		MemoryID:     memory.ID,
		Content:      "alpha beta gamma",
		MetadataText: "author:jiahao-ye author jiahao ye jiahao ye",
		ChunkIndex:   0,
		CreatedAt:    now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	chunks, err := s.SearchChunks(t.Context(), "Jiahao", nil, memory.OwnerID, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected one metadata-matched chunk, got %d", len(chunks))
	}
	if chunks[0].ID != "chunk-author" {
		t.Fatalf("expected chunk-author, got %q", chunks[0].ID)
	}
}

func TestIncrementMemoryShownBatch(t *testing.T) {
	s := NewInMemoryStore()
	now := time.Now()
	mem := &model.Memory{
		ID: "mem-1", OwnerID: "owner-a", HubID: "hub-1",
		CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	}
	_ = s.CreateMemory(mem)

	// Increment shown_count
	err := s.IncrementMemoryShownBatch(t.Context(), []string{"mem-1"}, "owner-a", []string{"hub-1"})
	if err != nil {
		t.Fatalf("IncrementMemoryShownBatch: %v", err)
	}

	got, _ := s.GetMemory("mem-1", "owner-a")
	if got.ShownCount != 1 {
		t.Fatalf("shown_count = %d, want 1", got.ShownCount)
	}
	if got.AccessCount != 0 {
		t.Fatalf("access_count = %d, want 0 (shown should not affect access)", got.AccessCount)
	}
}

func TestIncrementMemoryAccessed(t *testing.T) {
	s := NewInMemoryStore()
	now := time.Now()
	mem := &model.Memory{
		ID: "mem-1", OwnerID: "owner-a", HubID: "hub-1",
		CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	}
	_ = s.CreateMemory(mem)

	// Increment access_count
	err := s.IncrementMemoryAccessed(t.Context(), "mem-1", "owner-a", []string{"hub-1"})
	if err != nil {
		t.Fatalf("IncrementMemoryAccessed: %v", err)
	}

	got, _ := s.GetMemory("mem-1", "owner-a")
	if got.AccessCount != 1 {
		t.Fatalf("access_count = %d, want 1", got.AccessCount)
	}
	if got.ShownCount != 0 {
		t.Fatalf("shown_count = %d, want 0 (access should not affect shown)", got.ShownCount)
	}
}

// TestInMemoryHubSettingsNotAliased guards the deep-copy invariant
// on InMemoryStore.CreateHub / GetHub / UpdateHub. Hub.Settings is
// a map, so a plain struct copy would alias the backing map between
// the store's row and every caller. A caller mutating returned
// Settings would silently mutate the stored value and bypass
// UpdateHub — exactly the kind of bug that masks real issues in
// hub-settings tests.
func TestInMemoryHubSettingsNotAliased(t *testing.T) {
	s := NewInMemoryStore()
	hub := &model.Hub{
		ID:       "hub-alias",
		Name:     "Alias test",
		Slug:     "hub-alias",
		HubType:  "team",
		OwnerID:  "owner",
		Settings: map[string]any{"dreams_merge_enabled": false},
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Mutating the input map after CreateHub must not touch the
	// stored row — the store's internal copy should own its own
	// map.
	hub.Settings["dreams_merge_enabled"] = true
	got, err := s.GetHub("hub-alias")
	if err != nil {
		t.Fatalf("GetHub: %v", err)
	}
	if got.Settings["dreams_merge_enabled"] != false {
		t.Fatalf("CreateHub input mutation leaked into stored row: %v", got.Settings)
	}

	// Mutating the returned map must not touch the stored row
	// either — callers shouldn't be able to edit store state
	// without UpdateHub.
	got.Settings["dreams_merge_enabled"] = true
	got2, err := s.GetHub("hub-alias")
	if err != nil {
		t.Fatalf("GetHub (second): %v", err)
	}
	if got2.Settings["dreams_merge_enabled"] != false {
		t.Fatalf("GetHub return mutation leaked into stored row: %v", got2.Settings)
	}
}
