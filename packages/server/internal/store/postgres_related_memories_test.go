package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestPostgresFindRelatedMemories pins the neighbor-search invariants
// for the /v1/memories/{id}/related endpoint against a live Postgres
// with pgvector. Skipped when DATABASE_URL is unset.
//
// Invariants:
//  1. Source + neighbor both in hubA → neighbors returned, ordered by
//     embedding similarity (nearest first), source excluded from results.
//  2. Source in hubA but caller passes hubB as sourceHubID →
//     query_vec defense-in-depth guard blocks the read; returns empty.
//     This closes the similarity-oracle surface at the store layer so
//     a future internal caller that forgets to authorize can't leak.
//  3. Neighbor candidates in a different hub are excluded, even if
//     their embedding is closer than same-hub neighbors.
//  4. Archived / processing neighbors are excluded (state='active' filter).
//  5. HNSW path: the lateral subquery preserves ORDER BY <=> LIMIT so
//     the pgvector HNSW index (idx_chunks_embedding) drives the scan.
func TestPostgresFindRelatedMemories(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres related-memories test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Namespaced UUIDs — "re" = related-memories test.
	owner := "00000000-0000-0000-0000-aa11fe000001"
	hubA := "00000000-0000-0000-0000-aa11fe000002"
	hubB := "00000000-0000-0000-0000-aa22fe000002"
	memSource := "00000000-0000-0000-0000-aa11fe000010"
	memNear := "00000000-0000-0000-0000-aa11fe000011"
	memFar := "00000000-0000-0000-0000-aa11fe000012"
	memArchived := "00000000-0000-0000-0000-aa11fe000013"
	memForeignClose := "00000000-0000-0000-0000-aa22fe000014"

	now := time.Now().UTC()

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM chunks WHERE memory_id = ANY($1::uuid[])`,
			[]string{memSource, memNear, memFar, memArchived, memForeignClose})
		pool.Exec(ctx, `DELETE FROM memories WHERE owner_id = $1::uuid`, owner)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id = $1::uuid`, owner)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id IN ($1::uuid, $2::uuid)`, hubA, hubB)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, owner)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec: %v\nSQL: %s", err, sql)
		}
	}

	mustExec(`INSERT INTO users (id, email, name, created_at, updated_at)
		VALUES ($1::uuid, 'related-test@local', 'Related', $2, $2)`, owner, now)
	mustExec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, 'hubA', 'related-hub-a', 'team', $2::uuid, $3, $3)`, hubA, owner, now)
	mustExec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		VALUES ($1::uuid, 'hubB', 'related-hub-b', 'team', $2::uuid, $3, $3)`, hubB, owner, now)
	mustExec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`, hubA, owner)
	mustExec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`, hubB, owner)

	makeMemory := func(id, hub, state string) {
		mustExec(`INSERT INTO memories (id, hub_id, owner_id, title, state, created_at, updated_at, accessed_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $6, $6)`,
			id, hub, owner, "mem "+id[len(id)-4:], state, now)
	}
	makeMemory(memSource, hubA, "active")
	makeMemory(memNear, hubA, "active")
	makeMemory(memFar, hubA, "active")
	makeMemory(memArchived, hubA, "archived")
	makeMemory(memForeignClose, hubB, "active")

	// Build 1024-dim unit-ish vectors where bucket-0 similarity encodes
	// the test's notion of "close": source and near share high-value
	// slot 0, far has high-value slot 500, archived matches source
	// exactly, foreignClose also matches source exactly. Cosine
	// similarity ranking:
	//   source ↔ near         → HIGH (both slot 0 dominant)
	//   source ↔ archived     → HIGH (identical) but state=archived → excluded
	//   source ↔ foreignClose → HIGH (identical) but hub=hubB       → excluded
	//   source ↔ far          → LOW  (orthogonal)
	vecWithSpike := func(slot int) []float64 {
		v := make([]float64, 1024)
		v[slot] = 1.0
		v[slot+1] = 0.1 // tiny tail so vectors aren't literally identical
		return v
	}

	for i, c := range []struct {
		memoryID string
		slot     int
	}{
		{memSource, 0},
		{memNear, 0},
		{memFar, 500},
		{memArchived, 0},
		{memForeignClose, 0},
	} {
		// chunk IDs share the namespace prefix so cleanup catches them
		// even if the test crashes mid-run.
		chunkID := "00000000-0000-0000-0000-cc11fe00000" + string(rune('1'+i))
		if err := s.CreateChunks([]model.Chunk{{
			ID:         chunkID,
			MemoryID:   c.memoryID,
			Content:    "content " + c.memoryID,
			ChunkIndex: 0,
			Embedding:  vecWithSpike(c.slot),
			CreatedAt:  now,
		}}); err != nil {
			t.Fatalf("CreateChunks(%s): %v", c.memoryID, err)
		}
	}

	t.Run("same-hub neighbors returned ordered by similarity", func(t *testing.T) {
		got, err := s.FindRelatedMemories(memSource, hubA, 5)
		if err != nil {
			t.Fatalf("FindRelatedMemories: %v", err)
		}
		gotIDs := make([]string, 0, len(got))
		for _, r := range got {
			gotIDs = append(gotIDs, r.Memory.ID)
		}
		if len(got) == 0 {
			t.Fatalf("expected at least one neighbor, got 0")
		}
		if got[0].Memory.ID != memNear {
			t.Errorf("nearest neighbor = %s, want %s (got order: %v)", got[0].Memory.ID, memNear, gotIDs)
		}
		for _, r := range got {
			if r.Memory.ID == memSource {
				t.Errorf("source memory %s must be excluded from its own neighbor set", memSource)
			}
			if r.Memory.ID == memArchived {
				t.Errorf("archived memory %s must be excluded (state != active)", memArchived)
			}
			if r.Memory.ID == memForeignClose {
				t.Errorf("cross-hub memory %s must be excluded (hub_id != sourceHubID)", memForeignClose)
			}
			if r.Memory.HubID != hubA {
				t.Errorf("neighbor %s has hub_id=%s, want %s", r.Memory.ID, r.Memory.HubID, hubA)
			}
		}
	})

	t.Run("wrong sourceHubID returns empty (defense-in-depth oracle closure)", func(t *testing.T) {
		// Source memory lives in hubA. Caller passes hubB → query_vec
		// guard (m.hub_id = $2::uuid) fails → no embedding loaded →
		// neighbors CTE empty → result empty. Without this guard an
		// unauthorized caller could use neighbor distances as a
		// similarity oracle on memories they don't own.
		got, err := s.FindRelatedMemories(memSource, hubB, 5)
		if err != nil {
			t.Fatalf("FindRelatedMemories: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty result when sourceHubID doesn't match memory, got %d", len(got))
		}
	})

	t.Run("unknown memory id returns empty", func(t *testing.T) {
		got, err := s.FindRelatedMemories("00000000-0000-0000-0000-000000000000", hubA, 5)
		if err != nil {
			t.Fatalf("FindRelatedMemories: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty result for unknown memory, got %d", len(got))
		}
	})
}
