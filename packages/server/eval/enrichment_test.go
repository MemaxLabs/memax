package eval

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbmigrate "github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

const (
	enrichTestOwner  = "00000000-0000-0000-0000-eeeeeeeeee01"
	enrichTestHubA   = "00000000-0000-0000-0000-eeeeeeeeee10"
	enrichTestHubB   = "00000000-0000-0000-0000-eeeeeeeeee20"
	enrichVectorDims = 1024
)

// TestFindRelatedForEnrichment validates the dedicated enrichment search
// against Postgres/pgvector with deterministic embeddings.
// Requires DATABASE_URL — skips otherwise.
func TestFindRelatedForEnrichment(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping pgvector enrichment test")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := dbmigrate.Run(dbURL, migrationsPath()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx := context.Background()
	s := store.NewPostgresStore(pool)
	now := time.Now()

	// Ensure test user and hubs
	ensureUser(t, pool, enrichTestOwner, "enrich-test@memax.local", "Enrich Tester")
	ensureHub(t, pool, enrichTestHubA, "enrich-hub-a", "personal", enrichTestOwner, now)
	ensureHub(t, pool, enrichTestHubB, "enrich-hub-b", "team", enrichTestOwner, now)

	// Clean up any previous test data
	cleanEnrichmentTestData(t, pool)

	// Seed 3 memories in hub A, 1 in hub B.
	// Embeddings are deterministic unit vectors in specific dimensions:
	//   mem-a1: strong signal in dim 0 → "pricing" topic
	//   mem-a2: strong signal in dim 0+1 → "pricing + competitive" topic
	//   mem-a3: strong signal in dim 2 → "deployment" topic (different)
	//   mem-b1: strong signal in dim 0 → "pricing" topic but in hub B
	memA1 := seedEnrichmentMemory(t, s, fixtureUUID("enrich-mem-a1"), enrichTestHubA, enrichTestOwner, "Pricing Decision", "Decided on $9/$19 tiers", now)
	memA2 := seedEnrichmentMemory(t, s, fixtureUUID("enrich-mem-a2"), enrichTestHubA, enrichTestOwner, "Competitive Analysis", "Mem0 comparison notes", now)
	memA3 := seedEnrichmentMemory(t, s, fixtureUUID("enrich-mem-a3"), enrichTestHubA, enrichTestOwner, "Deploy Guide", "Fly.io deployment steps", now)
	memB1 := seedEnrichmentMemory(t, s, fixtureUUID("enrich-mem-b1"), enrichTestHubB, enrichTestOwner, "Pricing B", "Hub B pricing notes", now)
	memB2 := seedEnrichmentMemory(t, s, fixtureUUID("enrich-mem-b2"), enrichTestHubB, enrichTestOwner, "Roadmap B", "Hub B roadmap", now)

	// Create chunks with deterministic embeddings
	seedEnrichmentChunk(t, s, memA1, unitVector(0))          // dim 0
	seedEnrichmentChunk(t, s, memA2, blendVector(0, 1, 0.8)) // dim 0+1, close to mem-a1
	seedEnrichmentChunk(t, s, memA3, unitVector(2))          // dim 2, far from mem-a1
	seedEnrichmentChunk(t, s, memB1, unitVector(0))          // dim 0, same as mem-a1 but different hub
	seedEnrichmentChunk(t, s, memB2, blendVector(0, 1, 0.5)) // dim 0+1, in hub B

	// Query embedding: strong signal in dim 0 (like "pricing")
	queryEmbed := unitVector(0)

	t.Run("finds same-hub candidates sorted by similarity", func(t *testing.T) {
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{enrichTestHubA}, memA1, 5)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		if len(candidates) < 2 {
			t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
		}
		// mem-a2 (blended dim 0+1) should be more similar than mem-a3 (dim 2)
		if candidates[0].MemoryID != memA2 {
			t.Fatalf("expected first candidate to be mem-a2, got %s", candidates[0].MemoryID)
		}
		if candidates[0].Similarity <= candidates[len(candidates)-1].Similarity {
			t.Fatal("expected candidates sorted by descending similarity")
		}
		// Verify title and summary are populated
		if candidates[0].Title == "" {
			t.Fatal("expected candidate title to be populated")
		}
	})

	t.Run("excludes the memory being ingested", func(t *testing.T) {
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{enrichTestHubA}, memA1, 5)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		for _, c := range candidates {
			if c.MemoryID == memA1 {
				t.Fatal("excluded memory appeared in candidates")
			}
		}
	})

	t.Run("does not return cross-hub memories", func(t *testing.T) {
		// Search hub A only — mem-b1 (in hub B) should NOT appear
		// even though it has identical embedding to the query.
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{enrichTestHubA}, memA1, 10)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		for _, c := range candidates {
			if c.MemoryID == memB1 {
				t.Fatal("cross-hub memory appeared in candidates")
			}
		}
	})

	t.Run("respects readHubIDs scoping", func(t *testing.T) {
		// Search hub B only, excluding mem-b1. mem-b2 should appear
		// as it's also in hub B. No hub-A memories should appear.
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{enrichTestHubB}, memB1, 5)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		if len(candidates) != 1 {
			t.Fatalf("expected 1 candidate (mem-b2), got %d", len(candidates))
		}
		if candidates[0].MemoryID != memB2 {
			t.Fatalf("expected mem-b2, got %s", candidates[0].MemoryID)
		}
		// Verify no hub-A memories leaked in
		for _, c := range candidates {
			if c.MemoryID == memA1 || c.MemoryID == memA2 || c.MemoryID == memA3 {
				t.Fatalf("hub-A memory %s appeared in hub-B-only search", c.MemoryID)
			}
		}
	})

	t.Run("returns empty for no matching hubs", func(t *testing.T) {
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{"00000000-0000-0000-0000-000000000000"}, "00000000-0000-0000-0000-ffffffffffff", 5)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("expected 0 candidates for nonexistent hub, got %d", len(candidates))
		}
	})

	t.Run("returns empty for empty embedding", func(t *testing.T) {
		candidates, err := s.FindRelatedForEnrichment(ctx, nil, enrichTestOwner, []string{enrichTestHubA}, memA1, 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("expected 0 candidates for nil embedding, got %d", len(candidates))
		}
	})

	t.Run("similarity values are correct", func(t *testing.T) {
		candidates, err := s.FindRelatedForEnrichment(ctx, queryEmbed, enrichTestOwner, []string{enrichTestHubA}, memA1, 5)
		if err != nil {
			t.Fatalf("FindRelatedForEnrichment: %v", err)
		}
		for _, c := range candidates {
			if c.Similarity < 0 || c.Similarity > 1.0 {
				t.Fatalf("similarity %.4f out of [0, 1] range for %s", c.Similarity, c.MemoryID)
			}
		}
		// mem-a2 has blended vector (cos sim ~0.8 with unit(0))
		// mem-a3 has orthogonal vector (cos sim ~0.0 with unit(0))
		if len(candidates) >= 2 {
			if candidates[0].Similarity < 0.5 {
				t.Fatalf("expected high similarity for mem-a2, got %.4f", candidates[0].Similarity)
			}
		}
	})
}

// --- Test helpers ---

func ensureUser(t *testing.T, pool *pgxpool.Pool, id, email, name string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name, display_name, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $3, now(), now())
		ON CONFLICT (id) DO NOTHING`, id, email, name)
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
}

func ensureHub(t *testing.T, pool *pgxpool.Pool, id, slug, hubType, ownerID string, now time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6, $6)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug`,
		id, slug, slug, hubType, ownerID, now)
	if err != nil {
		t.Fatalf("ensure hub: %v", err)
	}
	// Ensure owner membership
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO hub_members (hub_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, 'owner')
		ON CONFLICT (hub_id, user_id) DO NOTHING`, id, ownerID)
}

func seedEnrichmentMemory(t *testing.T, s *store.PostgresStore, id, hubID, ownerID, title, content string, now time.Time) string {
	t.Helper()
	_ = s.DeleteChunksByMemory(id, ownerID)
	_ = s.DeleteMemory(id, ownerID)
	mem := &model.Memory{
		ID: id, HubID: hubID, OwnerID: ownerID,
		Title: title, Content: content, ContentType: "markdown",
		ContentHash: id, Summary: title + " summary",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Tags: []string{}, Boundary: "private",
		State: "active", Source: "test", Version: 1,
		CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	}
	if err := s.CreateMemory(mem); err != nil {
		t.Fatalf("seed memory %s: %v", id, err)
	}
	return id
}

func seedEnrichmentChunk(t *testing.T, s *store.PostgresStore, memoryID string, embedding []float64) {
	t.Helper()
	chunk := model.Chunk{
		ID: fixtureUUID(memoryID + ":chunk:0"), MemoryID: memoryID,
		Content: "test content", ChunkIndex: 0, TokenCount: 10,
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, CreatedAt: time.Now(),
		Embedding: embedding,
	}
	if err := s.CreateChunks([]model.Chunk{chunk}); err != nil {
		t.Fatalf("seed chunk for %s: %v", memoryID, err)
	}
}

func cleanEnrichmentTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM chunks WHERE memory_id IN (
		SELECT id FROM memories WHERE owner_id = $1::uuid
	)`, enrichTestOwner)
	_, _ = pool.Exec(ctx, `DELETE FROM memories WHERE owner_id = $1::uuid`, enrichTestOwner)
}

// unitVector returns a 1024-dim unit vector with 1.0 in the specified
// dimension and 0.0 elsewhere. Useful for deterministic similarity tests.
func unitVector(dim int) []float64 {
	v := make([]float64, enrichVectorDims)
	if dim < enrichVectorDims {
		v[dim] = 1.0
	}
	return v
}

// blendVector returns a normalized blend of two unit vectors.
// weight controls the balance: 1.0 = pure dimA, 0.0 = pure dimB.
func blendVector(dimA, dimB int, weight float64) []float64 {
	v := make([]float64, enrichVectorDims)
	if dimA < enrichVectorDims {
		v[dimA] = weight
	}
	if dimB < enrichVectorDims {
		v[dimB] = 1.0 - weight
	}
	// Normalize to unit length
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}
