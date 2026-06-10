package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresMergeTopics exercises the MergeTopics primitive against a
// real Postgres backend. Skipped when DATABASE_URL is unset (the same
// pattern used by field_lane_test.go and temporal_lane_test.go). All
// fixture rows use the merge_topics_test_ prefix so cleanup is
// deterministic even if a sub-test panics partway through.
func TestPostgresMergeTopics(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres merge-topics test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Fixture identities. Distinct UUIDs per test so a leftover from a
	// previous failing run can't masquerade as fresh state.
	ownerID := "00000000-0000-0000-0000-aaaaaa000001"
	hubA := "00000000-0000-0000-0000-aaaaaa000002"
	hubB := "00000000-0000-0000-0000-aaaaaa000003"
	now := time.Now()

	cleanup := func() {
		// Order matters: memory_topics → memories → topics → hubs → users.
		// FK cascades would handle most of this, but explicit deletes
		// make the cleanup trace easy to read in test logs.
		pool.Exec(ctx, `DELETE FROM memory_topics WHERE memory_id IN (SELECT id FROM memories WHERE owner_id = $1::uuid)`, ownerID)
		pool.Exec(ctx, `DELETE FROM memories WHERE owner_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id IN ($1::uuid, $2::uuid)`, hubA, hubB)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// User + two hubs.
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'merge-test@test.local', 'Merge Test', $2, $2)`,
		ownerID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	for _, h := range []struct{ id, slug string }{
		{hubA, "merge-test-hub-a"},
		{hubB, "merge-test-hub-b"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, $2, $2, 'team', $3::uuid, $4, $4)`,
			h.id, h.slug, ownerID, now); err != nil {
			t.Fatalf("insert hub %s: %v", h.slug, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
			h.id, ownerID); err != nil {
			t.Fatalf("insert hub member: %v", err)
		}
	}

	// Helpers: create a topic in a hub, assign a memory to a topic.
	createTopic := func(t *testing.T, id, hubID, name string, parent *string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO topics (id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, '', 'folder', 0, false, false, now(), now())`,
			id, ownerID, hubID, parent, name); err != nil {
			t.Fatalf("create topic %s: %v", name, err)
		}
	}
	createMemoryInTopic := func(t *testing.T, memID, hubID, topicID string, confidence float64) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO memories (id, hub_id, owner_id, title) VALUES ($1::uuid, $2::uuid, $3::uuid, 'merge-test')`,
			memID, hubID, ownerID); err != nil {
			t.Fatalf("create memory: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO memory_topics (memory_id, topic_id, confidence) VALUES ($1::uuid, $2::uuid, $3)`,
			memID, topicID, confidence); err != nil {
			t.Fatalf("assign memory to topic: %v", err)
		}
	}
	resetTopicsAndMemories := func(t *testing.T) {
		t.Helper()
		pool.Exec(ctx, `DELETE FROM memory_topics WHERE memory_id IN (SELECT id FROM memories WHERE owner_id = $1::uuid)`, ownerID)
		pool.Exec(ctx, `DELETE FROM memories WHERE owner_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id = $1::uuid`, ownerID)
	}

	t.Run("happy path: rehome memories, reparent children, delete sources", func(t *testing.T) {
		resetTopicsAndMemories(t)
		target := "00000000-0000-0000-0000-bbbbbb000001"
		s1 := "00000000-0000-0000-0000-bbbbbb000002"
		s2 := "00000000-0000-0000-0000-bbbbbb000003"
		s1Child := "00000000-0000-0000-0000-bbbbbb000004"
		s2Child := "00000000-0000-0000-0000-bbbbbb000005"
		createTopic(t, target, hubA, "Target", nil)
		createTopic(t, s1, hubA, "Source 1", nil)
		createTopic(t, s2, hubA, "Source 2", nil)
		createTopic(t, s1Child, hubA, "S1 child", &s1)
		createTopic(t, s2Child, hubA, "S2 child", &s2)

		mem1 := "00000000-0000-0000-0000-bbbbbb000011"
		mem2 := "00000000-0000-0000-0000-bbbbbb000012"
		mem3 := "00000000-0000-0000-0000-bbbbbb000013"
		mem4 := "00000000-0000-0000-0000-bbbbbb000014"
		createMemoryInTopic(t, mem1, hubA, s1, 0.91)
		createMemoryInTopic(t, mem2, hubA, s1, 0.62)
		createMemoryInTopic(t, mem3, hubA, s2, 0.83)
		createMemoryInTopic(t, mem4, hubA, s2, 0.55)

		moved, err := s.MergeTopics(ctx, hubA, target, []string{s1, s2})
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if moved != 4 {
			t.Errorf("moved count: got %d, want 4", moved)
		}

		// Every memory should now point at target.
		var inTarget int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM memory_topics WHERE memory_id = ANY($1::uuid[]) AND topic_id = $2::uuid`,
			[]string{mem1, mem2, mem3, mem4}, target,
		).Scan(&inTarget); err != nil {
			t.Fatalf("count target memories: %v", err)
		}
		if inTarget != 4 {
			t.Errorf("memories in target: got %d, want 4", inTarget)
		}

		// Confidence preserved on rehomed rows.
		var conf float64
		if err := pool.QueryRow(ctx,
			`SELECT confidence FROM memory_topics WHERE memory_id = $1::uuid`, mem1,
		).Scan(&conf); err != nil {
			t.Fatalf("scan confidence: %v", err)
		}
		if conf < 0.905 || conf > 0.915 {
			t.Errorf("mem1 confidence: got %f, want ~0.91", conf)
		}

		// Children reparented to target.
		var s1ChildParent, s2ChildParent string
		if err := pool.QueryRow(ctx, `SELECT parent_id::text FROM topics WHERE id = $1::uuid`, s1Child).Scan(&s1ChildParent); err != nil {
			t.Fatalf("scan s1Child parent: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT parent_id::text FROM topics WHERE id = $1::uuid`, s2Child).Scan(&s2ChildParent); err != nil {
			t.Fatalf("scan s2Child parent: %v", err)
		}
		if s1ChildParent != target || s2ChildParent != target {
			t.Errorf("children not reparented: s1Child.parent=%s s2Child.parent=%s want=%s", s1ChildParent, s2ChildParent, target)
		}

		// Source topics deleted.
		var srcCount int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM topics WHERE id = ANY($1::uuid[])`, []string{s1, s2},
		).Scan(&srcCount); err != nil {
			t.Fatalf("count sources after merge: %v", err)
		}
		if srcCount != 0 {
			t.Errorf("source topics not deleted: %d remain", srcCount)
		}
	})

	t.Run("refuse cycle: target is descendant of source", func(t *testing.T) {
		resetTopicsAndMemories(t)
		root := "00000000-0000-0000-0000-cccccc000001"
		mid := "00000000-0000-0000-0000-cccccc000002"
		leaf := "00000000-0000-0000-0000-cccccc000003"
		createTopic(t, root, hubA, "Root", nil)
		createTopic(t, mid, hubA, "Mid", &root)
		createTopic(t, leaf, hubA, "Leaf", &mid)

		// Try to merge root into leaf — root is leaf's transitive ancestor.
		_, err := s.MergeTopics(ctx, hubA, leaf, []string{root})
		if !errors.Is(err, ErrTopicMergeCycle) {
			t.Errorf("got %v, want ErrTopicMergeCycle", err)
		}
		// Confirm topics still exist (rolled back).
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM topics WHERE id = ANY($1::uuid[])`, []string{root, mid, leaf}).Scan(&count); err != nil {
			t.Fatalf("count after rollback: %v", err)
		}
		if count != 3 {
			t.Errorf("expected 3 topics intact after rollback, got %d", count)
		}
	})

	t.Run("refuse cross-hub target", func(t *testing.T) {
		resetTopicsAndMemories(t)
		targetInB := "00000000-0000-0000-0000-dddddd000001"
		s1InA := "00000000-0000-0000-0000-dddddd000002"
		createTopic(t, targetInB, hubB, "Target in B", nil)
		createTopic(t, s1InA, hubA, "Source in A", nil)
		_, err := s.MergeTopics(ctx, hubA, targetInB, []string{s1InA})
		if !errors.Is(err, ErrTopicMergeTargetNotInHub) {
			t.Errorf("got %v, want ErrTopicMergeTargetNotInHub", err)
		}
	})

	t.Run("refuse cross-hub source", func(t *testing.T) {
		resetTopicsAndMemories(t)
		target := "00000000-0000-0000-0000-eeeeee000001"
		s1 := "00000000-0000-0000-0000-eeeeee000002"
		s2InB := "00000000-0000-0000-0000-eeeeee000003"
		createTopic(t, target, hubA, "Target", nil)
		createTopic(t, s1, hubA, "S1", nil)
		createTopic(t, s2InB, hubB, "S2 in B", nil)
		_, err := s.MergeTopics(ctx, hubA, target, []string{s1, s2InB})
		if !errors.Is(err, ErrTopicMergeSourceNotInHub) {
			t.Errorf("got %v, want ErrTopicMergeSourceNotInHub", err)
		}
		// S1 must still exist — rollback.
		var s1Exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM topics WHERE id = $1::uuid)`, s1).Scan(&s1Exists); err != nil {
			t.Fatalf("check s1: %v", err)
		}
		if !s1Exists {
			t.Errorf("s1 was deleted despite rollback")
		}
	})

	t.Run("refuse target ∈ sources", func(t *testing.T) {
		resetTopicsAndMemories(t)
		target := "00000000-0000-0000-0000-ffffff000001"
		createTopic(t, target, hubA, "Target", nil)
		_, err := s.MergeTopics(ctx, hubA, target, []string{target})
		if !errors.Is(err, ErrTopicMergeTargetIsSource) {
			t.Errorf("got %v, want ErrTopicMergeTargetIsSource", err)
		}
	})

	t.Run("refuse empty sources", func(t *testing.T) {
		resetTopicsAndMemories(t)
		target := "00000000-0000-0000-0000-aabbcc000001"
		createTopic(t, target, hubA, "Target", nil)
		_, err := s.MergeTopics(ctx, hubA, target, nil)
		if !errors.Is(err, ErrTopicMergeEmptySources) {
			t.Errorf("got %v, want ErrTopicMergeEmptySources", err)
		}
	})

	t.Run("dedupe duplicate sources", func(t *testing.T) {
		resetTopicsAndMemories(t)
		target := "00000000-0000-0000-0000-112233000001"
		s1 := "00000000-0000-0000-0000-112233000002"
		createTopic(t, target, hubA, "Target", nil)
		createTopic(t, s1, hubA, "S1", nil)
		mem := "00000000-0000-0000-0000-112233000010"
		createMemoryInTopic(t, mem, hubA, s1, 0.7)
		moved, err := s.MergeTopics(ctx, hubA, target, []string{s1, s1, s1})
		if err != nil {
			t.Fatalf("dedupe merge: %v", err)
		}
		if moved != 1 {
			t.Errorf("moved count: got %d, want 1", moved)
		}
		var s1Exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM topics WHERE id = $1::uuid)`, s1).Scan(&s1Exists); err != nil {
			t.Fatalf("check s1: %v", err)
		}
		if s1Exists {
			t.Errorf("s1 should be deleted after dedupe merge")
		}
	})
}
