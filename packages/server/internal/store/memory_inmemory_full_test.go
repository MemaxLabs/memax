package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Broader coverage of the InMemoryStore's memory + chunk surfaces.
// These aren't used in production heavy paths but they ARE the backing
// store for many handler unit tests, so the semantics matter.

func seedMem(t *testing.T, s *InMemoryStore, id, owner, hub string) *model.Memory {
	t.Helper()
	m := &model.Memory{
		ID: id, OwnerID: owner, HubID: hub,
		Title: "T-" + id, Content: "C", ContentType: "text",
		ContentHash: "h-" + id, SourcePath: "path-" + id,
		State: "active", Kind: model.MemoryKindSemantic,
		Stability: model.MemoryStabilityEvolving,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateMemory(m); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	return m
}

func TestInMemoryMemoryCRUD(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	m1 := seedMem(t, s, "m1", "u1", "h1")
	m2 := seedMem(t, s, "m2", "u1", "h1")

	// GetMemory: own memory visible.
	got, err := s.GetMemory("m1", "u1")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.ID != "m1" {
		t.Errorf("got wrong memory: %+v", got)
	}

	// GetMemoryBySourcePath.
	bySrc, err := s.GetMemoryBySourcePath(m1.SourcePath, "u1", "h1")
	if err != nil {
		t.Fatalf("GetMemoryBySourcePath: %v", err)
	}
	if bySrc.ID != "m1" {
		t.Errorf("source-path lookup: %+v", bySrc)
	}

	// GetMemoryByContentHash.
	byHash, err := s.GetMemoryByContentHash("h-m1", "u1", "h1")
	if err != nil {
		t.Fatalf("GetMemoryByContentHash: %v", err)
	}
	if byHash.ID != "m1" {
		t.Errorf("hash lookup: %+v", byHash)
	}

	// ListMemories.
	list, err := s.ListMemories("u1", 50)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListMemories returned %d, want 2", len(list))
	}

	// UpdateMemory: edit title.
	m2.Title = "updated"
	if err := s.UpdateMemory(m2); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}
	got, _ = s.GetMemory("m2", "u1")
	if got.Title != "updated" {
		t.Errorf("update didn't persist: %q", got.Title)
	}

	// DeleteMemory.
	if err := s.DeleteMemory("m1", "u1"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if _, err := s.GetMemory("m1", "u1"); err == nil {
		t.Error("memory should be gone after delete")
	}

	// BatchDeleteMemories on the rest.
	if _, err := s.BatchDeleteMemories([]string{"m2"}, "u1"); err != nil {
		t.Fatalf("BatchDeleteMemories: %v", err)
	}
}

func TestInMemoryListMemoriesPaginated(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	for i := 0; i < 5; i++ {
		seedMem(t, s, string(rune('a'+i)), "u1", "h1")
	}
	memories, _, _, err := s.ListMemoriesPaginated(ListOptions{Scope: VisibilityScope{OwnerID: "u1"}, Limit: 3})
	if err != nil {
		t.Fatalf("ListMemoriesPaginated: %v", err)
	}
	if len(memories) > 3 {
		t.Errorf("limit ignored: %d rows", len(memories))
	}
}

func TestInMemoryAllChunksAndChunkCRUD(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	seedMem(t, s, "m1", "u1", "h1")

	// CreateChunks + GetChunksByMemory roundtrip.
	c := model.Chunk{
		ID: "c1", MemoryID: "m1", Content: "chunk body",
		ChunkIndex: 0, TokenCount: 2,
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		CreatedAt: time.Now(),
	}
	if err := s.CreateChunks([]model.Chunk{c}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}
	got, err := s.GetChunksByMemory("m1", "u1")
	if err != nil {
		t.Fatalf("GetChunksByMemory: %v", err)
	}
	if len(got) != 1 || got[0].ID != "c1" {
		t.Errorf("chunk round-trip: %+v", got)
	}

	// UpdateChunk mutates content.
	c.Content = "new body"
	if err := s.UpdateChunk(&c); err != nil {
		t.Fatalf("UpdateChunk: %v", err)
	}
	got, _ = s.GetChunksByMemory("m1", "u1")
	if got[0].Content != "new body" {
		t.Errorf("chunk update didn't persist: %+v", got[0])
	}

	// AllChunks returns everything.
	all := s.AllChunks()
	if len(all) == 0 {
		t.Error("AllChunks returned 0, want ≥ 1")
	}

	// DeleteChunksByMemory.
	if err := s.DeleteChunksByMemory("m1", "u1"); err != nil {
		t.Fatalf("DeleteChunksByMemory: %v", err)
	}
	got, _ = s.GetChunksByMemory("m1", "u1")
	if len(got) != 0 {
		t.Errorf("chunks not deleted: %d", len(got))
	}
}

func TestInMemoryMemoryAttachmentsRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	seedMem(t, s, "m1", "u1", "h1")

	a := &model.MemoryAttachment{
		ID: "a1", MemoryID: "m1", OwnerID: "u1",
		Filename: "file.txt", ContentType: "text/plain",
		SizeBytes: 5, CreatedAt: time.Now(),
	}
	if err := s.CreateMemoryAttachment(a); err != nil {
		t.Fatalf("CreateMemoryAttachment: %v", err)
	}

	list, err := s.ListMemoryAttachments("m1", "u1")
	if err != nil {
		t.Fatalf("ListMemoryAttachments: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListMemoryAttachments: %d", len(list))
	}

	got, err := s.GetMemoryAttachment("a1", "m1", "u1")
	if err != nil {
		t.Fatalf("GetMemoryAttachment: %v", err)
	}
	if got.Filename != "file.txt" {
		t.Errorf("Filename = %q", got.Filename)
	}

	byID, err := s.GetAttachmentByID("a1")
	if err != nil {
		t.Fatalf("GetAttachmentByID: %v", err)
	}
	if byID.ID != "a1" {
		t.Errorf("ID = %q", byID.ID)
	}

	// ListOwnerMemoryAttachments returns every attachment for a user.
	ownerList, err := s.ListOwnerMemoryAttachments("u1")
	if err != nil {
		t.Fatalf("ListOwnerMemoryAttachments: %v", err)
	}
	if len(ownerList) != 1 {
		t.Errorf("owner list: %d", len(ownerList))
	}

	// ListMemoryAttachmentsByIDs batch-loads attachments.
	batch, err := s.ListMemoryAttachmentsByIDs([]string{"m1"}, "u1")
	if err != nil {
		t.Fatalf("ListMemoryAttachmentsByIDs: %v", err)
	}
	if len(batch) != 1 {
		t.Errorf("batch should include 1 attachment, got %d: %+v", len(batch), batch)
	}
}

func TestInMemoryIncrementMemoryAccessedBatch(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	seedMem(t, s, "m1", "u1", "h1")
	seedMem(t, s, "m2", "u1", "h1")

	_ = s.IncrementMemoryAccessedBatch(context.Background(), []string{"m1", "m2"}, "u1", nil)
	got, _ := s.GetMemory("m1", "u1")
	if got.AccessCount == 0 {
		t.Error("m1 access count not incremented")
	}
	got, _ = s.GetMemory("m2", "u1")
	if got.AccessCount == 0 {
		t.Error("m2 access count not incremented")
	}
}

func TestInMemoryListActorCounts(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Empty store → empty map, no error.
	counts, err := s.ListActorCounts(ListOptions{Scope: VisibilityScope{OwnerID: "u1"}})
	if err != nil {
		t.Errorf("ListActorCounts: %v", err)
	}
	_ = counts
}

func TestInMemoryFindRelatedMemoriesAndForEnrichment(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// InMemory returns nil/empty — no embeddings in dev store. Just
	// exercise the function.
	got, err := s.FindRelatedMemories("m1", "h1", 5)
	if err != nil {
		t.Errorf("FindRelatedMemories: %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Errorf("dev store should return empty, got %+v", got)
	}

	enrich, err := s.FindRelatedForEnrichment(context.Background(), []float64{0.1}, "u1", []string{"h1"}, "m1", 3)
	if err != nil {
		t.Errorf("FindRelatedForEnrichment: %v", err)
	}
	if enrich != nil && len(enrich) != 0 {
		t.Errorf("dev store enrichment should return empty, got %+v", enrich)
	}
}

func TestInMemoryFindSuspiciousMetadata(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	got, err := s.FindSuspiciousMetadata(context.Background(), 10)
	if err != nil {
		t.Errorf("FindSuspiciousMetadata: %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Errorf("dev store should return empty, got %+v", got)
	}
}

func TestInMemoryBatchMoveOperations(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	seedMem(t, s, "m1", "u1", "h1")

	// InMemoryStore's batch-move methods are stubs returning 0 — just
	// assert no errors.
	if _, err := s.BatchMoveToTopic([]string{"m1"}, "t1", "u1", 0.8); err != nil {
		t.Errorf("BatchMoveToTopic: %v", err)
	}
	if _, err := s.BatchMoveToHub([]string{"m1"}, "h2", "u1"); err != nil {
		t.Errorf("BatchMoveToHub: %v", err)
	}
	if _, err := s.BatchMoveMemories([]string{"m1"}, "h2", "", "u1"); err != nil {
		t.Errorf("BatchMoveMemories: %v", err)
	}
}

func TestInMemoryDeleteHubMemories(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	seedMem(t, s, "m1", "u1", "h1")
	// DeleteHubMemory / BatchDeleteHubMemories — the dev-store variants.
	_ = s.DeleteHubMemory("m1", "h1")
	if _, err := s.BatchDeleteHubMemories([]string{"m1"}, "h1"); err != nil {
		t.Errorf("BatchDeleteHubMemories: %v", err)
	}
}
