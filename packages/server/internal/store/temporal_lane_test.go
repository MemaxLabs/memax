package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestTemporalLaneParameterBinding verifies that the temporal search lane
// correctly binds its SQL parameters without leaving unused placeholders.
// The previous implementation prepended an unreferenced $1 placeholder,
// causing pgx to fail with "could not determine data type of parameter $1".
//
// Requires DATABASE_URL — skipped when not set (CI runs this via eval).
func TestTemporalLaneParameterBinding(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres store test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-aaaaaaaaaa01"
	hubID := "00000000-0000-0000-0000-aaaaaaaaaa02"
	targetMemID := "00000000-0000-0000-0000-aaaaaaaaaa03"
	targetChunkID := "00000000-0000-0000-0000-aaaaaaaaaa04"
	now := time.Now()
	targetCreatedAt := now.Add(-48 * time.Hour) // 2 days ago

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM chunks WHERE id = $1::uuid`, targetChunkID)
		pool.Exec(ctx, `DELETE FROM memories WHERE id = $1::uuid`, targetMemID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'temporal-test@test.local', 'Temporal Test', $2, $2)`, ownerID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'temporal-test-hub', 'temporal-test-hub', 'team', $2::uuid, $3, $3)`, hubID, ownerID, now); err != nil {
		t.Fatalf("insert hub: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`, hubID, ownerID); err != nil {
		t.Fatalf("insert hub member: %v", err)
	}

	// Memory created 2 days ago. Content is deliberately unique gibberish
	// so BM25/trigram/field lanes cannot find it — only the temporal lane
	// can match this memory. This proves the temporal lane actually works,
	// rather than the test passing via other lanes.
	if _, err := pool.Exec(ctx, `INSERT INTO memories (id, title, content, kind, stability, owner_id, hub_id, source, created_at, updated_at, accessed_at)
		VALUES ($1::uuid, 'xkcd7291 zqfmgb', 'Unique temporal test content xkcd7291.', 'episodic', 'volatile', $2::uuid, $3::uuid, 'web', $4, $5, $5)`,
		targetMemID, ownerID, hubID, targetCreatedAt, now); err != nil {
		t.Fatalf("insert target memory: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, kind, stability, retrieval_weight, hint, tags_text, metadata_text, created_at)
		VALUES ($1::uuid, $2::uuid, 'Unique temporal test content xkcd7291.', '', 0, 8, 'episodic', 'volatile', 1.0, 'xkcd7291 zqfmgb', '', '', $3)`,
		targetChunkID, targetMemID, targetCreatedAt); err != nil {
		t.Fatalf("insert target chunk: %v", err)
	}

	// Query text deliberately does NOT match the memory content.
	// Only the temporal lane can surface this memory.
	nonMatchingQuery := "unrelated search terms foobar"

	// Include range: 3 days ago to now — target (2 days old) is inside.
	temporalStart := now.Add(-72 * time.Hour)
	temporalEnd := now
	results, err := s.SearchChunksForHubs(ctx, nonMatchingQuery, nil, ownerID, []string{hubID}, 10,
		&model.SearchFilters{
			TemporalStart: &temporalStart,
			TemporalEnd:   &temporalEnd,
		}, nil)
	if err != nil {
		t.Fatalf("temporal search failed: %v", err)
	}

	if !containsMemory(results, targetMemID) {
		t.Errorf("temporal lane did not find target memory %s; got %d results: %v",
			targetMemID, len(results), memoryIDsFromChunks(results))
	}

	// Exclude range: 10 to 5 days ago — target (2 days old) is outside.
	// With non-matching query text, no lane can find this memory.
	excludeStart := now.Add(-240 * time.Hour)
	excludeEnd := now.Add(-120 * time.Hour)
	excludeResults, err := s.SearchChunksForHubs(ctx, nonMatchingQuery, nil, ownerID, []string{hubID}, 10,
		&model.SearchFilters{
			TemporalStart: &excludeStart,
			TemporalEnd:   &excludeEnd,
		}, nil)
	if err != nil {
		t.Fatalf("temporal search (exclude range) failed: %v", err)
	}

	if containsMemory(excludeResults, targetMemID) {
		t.Errorf("temporal lane should NOT return target for excluded range; got %d results: %v",
			len(excludeResults), memoryIDsFromChunks(excludeResults))
	}

	// Owner-only path (no hub scope) — same include range.
	ownerResults, err := s.SearchChunks(ctx, nonMatchingQuery, nil, ownerID, 10,
		&model.SearchFilters{
			TemporalStart: &temporalStart,
			TemporalEnd:   &temporalEnd,
		}, nil)
	if err != nil {
		t.Fatalf("temporal search (owner-only) failed: %v", err)
	}

	if !containsMemory(ownerResults, targetMemID) {
		t.Errorf("temporal lane (owner-only) did not find target; got %d results: %v",
			len(ownerResults), memoryIDsFromChunks(ownerResults))
	}

	t.Logf("include range: %d results, target present: %v",
		len(results), containsMemory(results, targetMemID))
	t.Logf("exclude range: %d results, target present: %v",
		len(excludeResults), containsMemory(excludeResults, targetMemID))
	t.Logf("owner-only: %d results, target present: %v",
		len(ownerResults), containsMemory(ownerResults, targetMemID))
}
