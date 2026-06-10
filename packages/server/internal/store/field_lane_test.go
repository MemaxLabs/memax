package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestFieldLaneCandidateRescue verifies that the title-matching field lane
// rescues a short-body memory whose title contains query terms but whose
// chunk embedding is too weak to compete in vector/FTS/trigram lanes.
//
// Requires DATABASE_URL — skipped when not set (CI runs this via eval).
func TestFieldLaneCandidateRescue(t *testing.T) {
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
	ownerID := "00000000-0000-0000-0000-ffffffffffff"
	hubID := "00000000-0000-0000-0000-eeeeeeeeeeee"
	targetMemID := "00000000-0000-0000-0000-dddddddddd01"
	targetChunkID := "00000000-0000-0000-0000-cccccccccc01"
	distractorMemID := "00000000-0000-0000-0000-dddddddddd02"
	distractorChunkID := "00000000-0000-0000-0000-cccccccccc02"
	now := time.Now()

	// Cleanup first (idempotent)
	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM chunks WHERE id IN ($1::uuid, $2::uuid)`, targetChunkID, distractorChunkID)
		pool.Exec(ctx, `DELETE FROM memories WHERE id IN ($1::uuid, $2::uuid)`, targetMemID, distractorMemID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Setup: user, hub, membership
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'field-test@test.local', 'Field Test', $2, $2)`, ownerID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'field-test-hub', 'field-test-hub', 'team', $2::uuid, $3, $3)`, hubID, ownerID, now); err != nil {
		t.Fatalf("insert hub: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`, hubID, ownerID); err != nil {
		t.Fatalf("insert hub member: %v", err)
	}

	// Target: short body, distinctive title with "email" and "选型"
	if _, err := pool.Exec(ctx, `INSERT INTO memories (id, title, content, kind, stability, owner_id, hub_id, source, created_at, updated_at, accessed_at)
		VALUES ($1::uuid, 'memax email 选型: Resend', '选了 Resend。DX 最好。', 'rationale', 'stable', $2::uuid, $3::uuid, 'web', $4, $4, $4)`,
		targetMemID, ownerID, hubID, now); err != nil {
		t.Fatalf("insert target memory: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, kind, stability, retrieval_weight, hint, tags_text, metadata_text, created_at)
		VALUES ($1::uuid, $2::uuid, '选了 Resend。DX 最好。', '', 0, 10, 'rationale', 'stable', 1.0, 'memax email 选型: Resend', '', '', $3)`,
		targetChunkID, targetMemID, now); err != nil {
		t.Fatalf("insert target chunk: %v", err)
	}

	// Distractor: similar topic, longer body, no "email" in title
	if _, err := pool.Exec(ctx, `INSERT INTO memories (id, title, content, kind, stability, owner_id, hub_id, source, created_at, updated_at, accessed_at)
		VALUES ($1::uuid, 'memax infrastructure decisions', 'We chose Fly.io for Go services and Vercel for Next.js. Redis is Upstash serverless. Queue is River.', 'semantic', 'evolving', $2::uuid, $3::uuid, 'web', $4, $4, $4)`,
		distractorMemID, ownerID, hubID, now); err != nil {
		t.Fatalf("insert distractor memory: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, kind, stability, retrieval_weight, hint, tags_text, metadata_text, created_at)
		VALUES ($1::uuid, $2::uuid, 'We chose Fly.io for Go services and Vercel for Next.js.', '', 0, 20, 'semantic', 'evolving', 1.0, 'memax infrastructure decisions', '', '', $3)`,
		distractorChunkID, distractorMemID, now); err != nil {
		t.Fatalf("insert distractor chunk: %v", err)
	}

	// Search WITHOUT field terms — only FTS/trigram, no vector (embedding is nil)
	baseResults, err := s.SearchChunksForHubs(ctx, "email provider selection", nil, ownerID, []string{hubID}, 10, nil, nil)
	if err != nil {
		t.Fatalf("search without field terms: %v", err)
	}

	// Search WITH field terms — field lane should rescue the target
	fieldResults, err := s.SearchChunksForHubs(ctx, "email provider selection", nil, ownerID, []string{hubID}, 10, nil, &SearchOptions{
		FieldTerms: []string{"email", "选型"},
	})
	if err != nil {
		t.Fatalf("search with field terms: %v", err)
	}

	// Field lane must rescue the target memory
	if !containsMemory(fieldResults, targetMemID) {
		t.Errorf("field lane did not rescue target memory %s; got %d results: %v",
			targetMemID, len(fieldResults), memoryIDsFromChunks(fieldResults))
	}

	t.Logf("without field terms: %d results, target present: %v",
		len(baseResults), containsMemory(baseResults, targetMemID))
	t.Logf("with field terms: %d results, target present: %v",
		len(fieldResults), containsMemory(fieldResults, targetMemID))
}

func containsMemory(chunks []model.Chunk, memoryID string) bool {
	for _, c := range chunks {
		if c.MemoryID == memoryID {
			return true
		}
	}
	return false
}

func memoryIDsFromChunks(chunks []model.Chunk) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, c := range chunks {
		if !seen[c.MemoryID] {
			seen[c.MemoryID] = true
			ids = append(ids, c.MemoryID)
		}
	}
	return ids
}
