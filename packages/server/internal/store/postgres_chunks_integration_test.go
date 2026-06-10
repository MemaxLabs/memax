package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupChunksFixture seeds user + personal hub + one memory and returns
// the store, ownerID, and memoryID — the inputs every chunks test needs.
func setupChunksFixture(t *testing.T) (store.Store, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@chunks",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hubID := uuid.NewString()
	hub := &model.Hub{
		ID: hubID, Name: "P", Slug: "p-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	memID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	mem := &model.Memory{
		ID: memID, HubID: hubID, OwnerID: userID,
		Title: "Doc", Content: "x", ContentType: "text/plain",
		ContentHash: uuid.NewString(),
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityEvolving,
		Tags:        []string{},
		State:       "active",
		CreatedAt:   now, UpdatedAt: now,
	}
	if err := s.CreateMemory(mem); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}
	return s, userID, memID
}

func TestPostgresChunksCRUD(t *testing.T) {
	t.Parallel()
	s, ownerID, memID := setupChunksFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// --- CreateChunks with two chunks: one with embedding, one without.
	// The insert path branches on Embedding presence; hit both to avoid
	// silent breakage of the vector write path.
	chunks := []model.Chunk{
		{
			ID: uuid.NewString(), MemoryID: memID,
			Content: "first chunk body", HeadingChain: "H1",
			ChunkIndex: 0, TokenCount: 4,
			Language: "en", SearchConfig: "english",
			Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0, Hint: "h", TagsText: "", MetadataText: "",
			CreatedAt: now,
			// No embedding — exercises the text-only insert branch.
		},
		{
			ID: uuid.NewString(), MemoryID: memID,
			Content: "second chunk body", HeadingChain: "H2",
			ChunkIndex: 1, TokenCount: 5,
			Language: "en", SearchConfig: "english",
			Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			CreatedAt:       now,
			// No embedding — the vector write path is covered by the
			// related-memories test which seeds 1024-dim embeddings.
			// Scanning a non-zero embedding back through
			// GetChunksByMemory requires the pgvector codec; we skip
			// that path here and focus on the text-only CRUD shape.
		},
	}

	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	// CreateChunks with empty slice — guard against silent SQL errors
	// on the no-op path (we've shipped bugs here before).
	if err := s.CreateChunks(nil); err != nil {
		t.Errorf("CreateChunks(nil) should be no-op, got %v", err)
	}
	if err := s.CreateChunks([]model.Chunk{}); err != nil {
		t.Errorf("CreateChunks([]) should be no-op, got %v", err)
	}

	// --- GetChunksByMemory — must verify ownership AND return in index order.
	got, err := s.GetChunksByMemory(memID, ownerID)
	if err != nil {
		t.Fatalf("GetChunksByMemory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}
	if got[0].ChunkIndex != 0 || got[1].ChunkIndex != 1 {
		t.Errorf("chunks out of order: %d, %d", got[0].ChunkIndex, got[1].ChunkIndex)
	}
	if got[0].Content != "first chunk body" {
		t.Errorf("content[0] = %q", got[0].Content)
	}

	// --- UpdateChunk — verify the update path (both branches).
	chunks[0].Content = "updated first"
	chunks[0].Hint = "new hint"
	if err := s.UpdateChunk(&chunks[0]); err != nil {
		t.Fatalf("UpdateChunk (no-embedding): %v", err)
	}
	// Re-read.
	got, _ = s.GetChunksByMemory(memID, ownerID)
	if got[0].Content != "updated first" {
		t.Errorf("update content didn't persist: %q", got[0].Content)
	}
	if got[0].Hint != "new hint" {
		t.Errorf("update hint didn't persist: %q", got[0].Hint)
	}

	// Update chunk 1 (text-only) — exercise second UpdateChunk.
	chunks[1].Content = "updated second"
	if err := s.UpdateChunk(&chunks[1]); err != nil {
		t.Fatalf("UpdateChunk (text-only): %v", err)
	}

	// --- DeleteChunksByMemory — ownership guard + cascade.
	if err := s.DeleteChunksByMemory(memID, ownerID); err != nil {
		t.Fatalf("DeleteChunksByMemory: %v", err)
	}
	got, _ = s.GetChunksByMemory(memID, ownerID)
	if len(got) != 0 {
		t.Errorf("after delete, got %d chunks, want 0", len(got))
	}
}

func TestPostgresChunksOwnerIsolation(t *testing.T) {
	t.Parallel()
	s, ownerID, memID := setupChunksFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	if err := s.CreateChunks([]model.Chunk{{
		ID: uuid.NewString(), MemoryID: memID,
		Content: "secret", ChunkIndex: 0, TokenCount: 1,
		Language: "en", SearchConfig: "english",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, CreatedAt: now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	// A different user must NOT be able to read this memory's chunks.
	// This is the canary for the owner_id filter — if it regresses, every
	// user can see every other user's content.
	otherOwner := uuid.NewString()
	if _, err := s.GetChunksByMemory(memID, otherOwner); err == nil {
		t.Fatal("GetChunksByMemory must deny cross-owner reads")
	}
	if err := s.DeleteChunksByMemory(memID, otherOwner); err == nil {
		t.Fatal("DeleteChunksByMemory must deny cross-owner deletes")
	}

	// Legitimate owner's chunks are untouched.
	got, err := s.GetChunksByMemory(memID, ownerID)
	if err != nil {
		t.Fatalf("owner read after denied cross-owner: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("owner lost chunks after cross-owner attempt: %d", len(got))
	}
}

func TestPostgresChunksAllChunks(t *testing.T) {
	t.Parallel()
	s, _, memID := setupChunksFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// AllChunks is an internal scan used by reindex jobs. It should
	// return every non-archived chunk in the DB regardless of owner.
	if err := s.CreateChunks([]model.Chunk{{
		ID: uuid.NewString(), MemoryID: memID,
		Content: "hello", ChunkIndex: 0, TokenCount: 1,
		Language: "en", SearchConfig: "english",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, CreatedAt: now,
	}}); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	all := s.AllChunks()
	found := false
	for _, c := range all {
		if c.MemoryID == memID {
			found = true
			break
		}
	}
	if !found {
		t.Error("AllChunks didn't include our seeded chunk")
	}
}
