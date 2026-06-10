package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestPostgresLifecycleResolvers exercises the memory + topic lifecycle
// resolvers against a real Postgres backend. Skipped when DATABASE_URL
// is unset (same pattern as TestPostgresMergeTopics). All fixture rows
// carry the lifecycle_test_ UUID prefix so cleanup is deterministic.
//
// Invariants covered (per the Phase 2 contract):
//   - Clear-on-visit for memory pending_dream_action + topic
//     delta_since_visit. topic_visits.last_visited_at advancing past
//     the action's created_at removes the signal without any client
//     mutation.
//   - Visibility scoping: cross-tenant memory lookup returns empty;
//     topic-name joins return nil for topics the viewer can't see.
//   - Historical null degradation: dream_actions rows with null
//     from_topic_id / to_topic_id (pre-migration-069) scan cleanly and
//     produce nil FromTopic / ToTopic on DreamActionRef.
//   - Action-type filter: contradictions never enter lifecycle (they
//     stay on the notification surface — explicit product split).
func TestPostgresLifecycleResolvers(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres lifecycle test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	// Two owners + two hubs to cover cross-tenant scope. Owner A sees
	// hub A; owner B sees hub B. Each owner owns their respective
	// memories/topics.
	// Hex-only UUIDs per the same convention the merge/restructure
	// tests use — "lc" = lifecycle test namespace, distinct per row.
	ownerA := "00000000-0000-0000-0000-aa11cc000001"
	ownerB := "00000000-0000-0000-0000-aa22cc000001"
	hubA := "00000000-0000-0000-0000-aa11cc000002"
	hubB := "00000000-0000-0000-0000-aa22cc000002"
	memoryA := "00000000-0000-0000-0000-aa11cc000003"
	topicA := "00000000-0000-0000-0000-aa11cc000004"
	topicA2 := "00000000-0000-0000-0000-aa11cc000005"
	topicB := "00000000-0000-0000-0000-aa22cc000003"
	runID := "00000000-0000-0000-0000-aa11cc00000a"

	now := time.Now().UTC()
	past := now.Add(-48 * time.Hour)
	actionAt := now.Add(-24 * time.Hour)

	cleanup := func() {
		// Order: topic_visits → memory_topics → memories → topics →
		// dream_actions → dream_runs → hub_members → hubs → users.
		pool.Exec(ctx, `DELETE FROM topic_visits WHERE user_id IN ($1::uuid, $2::uuid)`, ownerA, ownerB)
		pool.Exec(ctx, `DELETE FROM memory_topics WHERE memory_id = $1::uuid`, memoryA)
		pool.Exec(ctx, `DELETE FROM memories WHERE owner_id IN ($1::uuid, $2::uuid)`, ownerA, ownerB)
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id IN ($1::uuid, $2::uuid)`, ownerA, ownerB)
		pool.Exec(ctx, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		pool.Exec(ctx, `DELETE FROM dream_runs WHERE id = $1::uuid`, runID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id IN ($1::uuid, $2::uuid)`, ownerA, ownerB)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id IN ($1::uuid, $2::uuid)`, hubA, hubB)
		pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1::uuid, $2::uuid)`, ownerA, ownerB)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec: %v\nSQL: %s", err, sql)
		}
	}

	// Users + hubs.
	mustExec(t,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'lfcyc-a@test.local', 'A', $2, $2)`,
		ownerA, past)
	mustExec(t,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'lfcyc-b@test.local', 'B', $2, $2)`,
		ownerB, past)
	mustExec(t,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'hub-a', 'lfcyc-hub-a', 'team', $2::uuid, $3, $3)`,
		hubA, ownerA, past)
	mustExec(t,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'hub-b', 'lfcyc-hub-b', 'team', $2::uuid, $3, $3)`,
		hubB, ownerB, past)
	mustExec(t,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
		hubA, ownerA)
	mustExec(t,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
		hubB, ownerB)

	// Topics: two in hubA (topicA current, topicA2 former/other), one
	// in hubB (topicB — viewer A should never see its name).
	for _, tp := range []struct{ id, hub, owner, name string }{
		{topicA, hubA, ownerA, "Topic A"},
		{topicA2, hubA, ownerA, "Topic A2"},
		{topicB, hubB, ownerB, "Topic B (cross-tenant)"},
	} {
		mustExec(t,
			`INSERT INTO topics (id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, $4, '', 'folder', 0, false, false, $5, $5)`,
			tp.id, tp.owner, tp.hub, tp.name, past)
	}

	// Memory in hubA assigned to topicA.
	mustExec(t,
		`INSERT INTO memories (id, hub_id, owner_id, title) VALUES ($1::uuid, $2::uuid, $3::uuid, 'lifecycle test memory')`,
		memoryA, hubA, ownerA)
	mustExec(t,
		`INSERT INTO memory_topics (memory_id, topic_id, confidence) VALUES ($1::uuid, $2::uuid, 0.7)`,
		memoryA, topicA)

	// Dream run fixture so action rows can satisfy run_id FK.
	mustExec(t,
		`INSERT INTO dream_runs (id, owner_id, hub_id, status, started_at, finished_at) VALUES ($1::uuid, $2::uuid, $3::uuid, 'completed', $4, $4)`,
		runID, ownerA, hubA, actionAt)

	scopeA := VisibilityScope{OwnerID: ownerA, HubIDs: []string{hubA}}
	scopeB := VisibilityScope{OwnerID: ownerB, HubIDs: []string{hubB}}

	// ── Memory list: clear-on-visit + action-type filter + scoping ──
	t.Run("memory lifecycle: organize action becomes pending, clears on visit", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Insert organize action: memory moved into topicA at actionAt.
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, result_memory_id, from_topic_id, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'organize', ARRAY[$2::text], $3, $4::uuid, $5::uuid, 'auto-organize', $6)`,
			runID, memoryA, topicA, topicA2, topicA, actionAt)

		got, err := s.ResolveMemoryLifecycleForList(ctx, scopeA, ownerA, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list: %v", err)
		}
		lc := got[memoryA]
		if lc == nil || lc.PendingDreamAction == nil {
			t.Fatalf("expected pending_dream_action before visit, got nil")
		}
		if lc.PendingDreamAction.ActionType != "organize" {
			t.Errorf("action_type = %q, want organize", lc.PendingDreamAction.ActionType)
		}
		if lc.PendingDreamAction.FromTopic == nil || lc.PendingDreamAction.FromTopic.Name != "Topic A2" {
			t.Errorf("from_topic name = %+v, want Topic A2", lc.PendingDreamAction.FromTopic)
		}
		if lc.PendingDreamAction.ToTopic == nil || lc.PendingDreamAction.ToTopic.Name != "Topic A" {
			t.Errorf("to_topic name = %+v, want Topic A", lc.PendingDreamAction.ToTopic)
		}

		// Upsert topic_visits.last_visited_at past the action's time.
		if err := s.UpsertTopicVisit(ownerA, topicA, hubA, actionAt.Add(1*time.Hour)); err != nil {
			t.Fatalf("upsert visit: %v", err)
		}

		got2, err := s.ResolveMemoryLifecycleForList(ctx, scopeA, ownerA, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list post-visit: %v", err)
		}
		if lc := got2[memoryA]; lc != nil && lc.PendingDreamAction != nil {
			t.Errorf("expected pending to clear after visit; got %+v", lc.PendingDreamAction)
		}
	})

	t.Run("memory lifecycle: cross-tenant scope returns empty", func(t *testing.T) {
		// Same fixture: dream action exists for memoryA in hubA. Viewer B
		// (scopeB) should not see it — they have no access to hubA.
		got, err := s.ResolveMemoryLifecycleForList(ctx, scopeB, ownerB, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map for out-of-scope viewer; got %d entries", len(got))
		}
	})

	t.Run("memory lifecycle: topic-name leak blocked by scoped joins", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Dream action references topicB (in hubB — outside viewer A's
		// scope). The memory is still in hubA (viewer A can see the
		// memory), but the topic name should NOT leak.
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, result_memory_id, from_topic_id, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'organize', ARRAY[$2::text], $3, $4::uuid, $5::uuid, 'leak test', $6)`,
			runID, memoryA, topicB, topicB, topicA, actionAt)

		got, err := s.ResolveMemoryLifecycleForList(ctx, scopeA, ownerA, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list: %v", err)
		}
		lc := got[memoryA]
		if lc == nil || lc.PendingDreamAction == nil {
			t.Fatalf("expected pending visible (memory is in scope), got nil")
		}
		// from_topic is topicB (out-of-scope) → should be nil via scoped join.
		if lc.PendingDreamAction.FromTopic != nil {
			t.Errorf("from_topic should be nil for out-of-scope topic; got %+v", lc.PendingDreamAction.FromTopic)
		}
		// to_topic is topicA (in-scope) → should resolve normally.
		if lc.PendingDreamAction.ToTopic == nil || lc.PendingDreamAction.ToTopic.Name != "Topic A" {
			t.Errorf("to_topic = %+v, want Topic A", lc.PendingDreamAction.ToTopic)
		}
	})

	t.Run("memory lifecycle: historical null from/to degrades gracefully", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Pre-migration-069 shape: both from_topic_id and to_topic_id NULL.
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, result_memory_id, from_topic_id, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'merge', ARRAY[$2::text], NULL, NULL, NULL, 'legacy merge', $3)`,
			runID, memoryA, actionAt)

		got, err := s.ResolveMemoryLifecycleForList(ctx, scopeA, ownerA, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list: %v", err)
		}
		lc := got[memoryA]
		if lc == nil || lc.PendingDreamAction == nil {
			t.Fatalf("expected pending action even with null lineage, got nil")
		}
		if lc.PendingDreamAction.FromTopic != nil || lc.PendingDreamAction.ToTopic != nil {
			t.Errorf("FromTopic/ToTopic should be nil for historical null row; got from=%+v to=%+v",
				lc.PendingDreamAction.FromTopic, lc.PendingDreamAction.ToTopic)
		}
		if lc.PendingDreamAction.Reason != "legacy merge" {
			t.Errorf("reason preserved = %q, want 'legacy merge'", lc.PendingDreamAction.Reason)
		}
	})

	t.Run("memory lifecycle: contradiction actions excluded from pending + history", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Only a contradiction action — should never surface as lifecycle.
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, result_memory_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'contradiction', ARRAY[$2::text], NULL, 'possible conflict', $3)`,
			runID, memoryA, actionAt)

		got, err := s.ResolveMemoryLifecycleForList(ctx, scopeA, ownerA, []string{memoryA})
		if err != nil {
			t.Fatalf("resolve list: %v", err)
		}
		if lc := got[memoryA]; lc != nil && lc.PendingDreamAction != nil {
			t.Errorf("contradictions must not surface as pending; got %+v", lc.PendingDreamAction)
		}

		// Detail resolver: history must also exclude contradictions.
		detail, err := s.ResolveMemoryLifecycleForDetail(ctx, scopeA, ownerA, memoryA)
		if err != nil {
			t.Fatalf("resolve detail: %v", err)
		}
		if detail.PendingDreamAction != nil {
			t.Errorf("detail pending should be nil for contradiction-only memory; got %+v", detail.PendingDreamAction)
		}
		if len(detail.DreamHistory) != 0 {
			t.Errorf("detail history should be empty for contradiction-only memory; got %d entries", len(detail.DreamHistory))
		}
	})

	t.Run("memory lifecycle: detail fetches history unscoped by visit", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Three organize actions, oldest → newest. result_memory_id
		// is text while to_topic_id is uuid — pass the topic id as
		// two explicit params ($3 for text, $4 for uuid cast) so the
		// driver doesn't have to deduce a single type for the two
		// columns (avoids SQLSTATE 42P08).
		for _, at := range []time.Time{actionAt.Add(-48 * time.Hour), actionAt.Add(-24 * time.Hour), actionAt} {
			mustExec(t,
				`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, result_memory_id, from_topic_id, to_topic_id, reason, created_at)
				 VALUES (gen_random_uuid(), $1::uuid, 'organize', ARRAY[$2::text], $3, NULL, $4::uuid, 'auto', $5)`,
				runID, memoryA, topicA, topicA, at)
		}

		// Visit topicA AFTER all actions — should clear pending but NOT
		// history (history is durable, unscoped by visit).
		if err := s.UpsertTopicVisit(ownerA, topicA, hubA, actionAt.Add(1*time.Hour)); err != nil {
			t.Fatalf("upsert visit: %v", err)
		}

		detail, err := s.ResolveMemoryLifecycleForDetail(ctx, scopeA, ownerA, memoryA)
		if err != nil {
			t.Fatalf("resolve detail: %v", err)
		}
		if detail.PendingDreamAction != nil {
			t.Errorf("pending should be nil after visit; got %+v", detail.PendingDreamAction)
		}
		if len(detail.DreamHistory) != 3 {
			t.Errorf("expected 3 history entries (durable), got %d", len(detail.DreamHistory))
		}
	})

	// ── Topic lifecycle: clear-on-visit + action-type filter ──
	t.Run("topic lifecycle: added count clears after visit, contradictions filtered", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		mustExec(t, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerA)

		// Two organize actions arriving into topicA (from NULL), plus
		// one contradiction referencing topicA via from_topic_id that
		// must NOT contribute to any count.
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'organize', ARRAY[$2::text], $3::uuid, 'a', $4)`,
			runID, memoryA, topicA, actionAt)
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'organize', ARRAY[$2::text], $3::uuid, 'b', $4)`,
			runID, memoryA, topicA, actionAt)
		mustExec(t,
			`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids, from_topic_id, to_topic_id, reason, created_at)
			 VALUES (gen_random_uuid(), $1::uuid, 'contradiction', ARRAY[$2::text], $3::uuid, $3::uuid, 'should not count', $4)`,
			runID, memoryA, topicA, actionAt)

		got, err := s.ResolveTopicLifecycle(ctx, scopeA, ownerA, []string{topicA})
		if err != nil {
			t.Fatalf("resolve topic: %v", err)
		}
		tl := got[topicA]
		if tl == nil || tl.DeltaSinceVisit == nil {
			t.Fatalf("expected delta_since_visit before visit, got nil")
		}
		if tl.DeltaSinceVisit.Added != 2 {
			t.Errorf("added = %d, want 2 (organize only, contradiction excluded)", tl.DeltaSinceVisit.Added)
		}

		// Visit clears it.
		if err := s.UpsertTopicVisit(ownerA, topicA, hubA, actionAt.Add(1*time.Hour)); err != nil {
			t.Fatalf("upsert visit: %v", err)
		}
		got2, err := s.ResolveTopicLifecycle(ctx, scopeA, ownerA, []string{topicA})
		if err != nil {
			t.Fatalf("resolve topic post-visit: %v", err)
		}
		if tl := got2[topicA]; tl != nil && tl.DeltaSinceVisit != nil {
			t.Errorf("expected delta to clear after visit; got %+v", tl.DeltaSinceVisit)
		}
	})

	t.Run("topic lifecycle: cross-tenant scope returns empty", func(t *testing.T) {
		got, err := s.ResolveTopicLifecycle(ctx, scopeB, ownerB, []string{topicA})
		if err != nil {
			t.Fatalf("resolve topic: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map for out-of-scope viewer; got %d entries", len(got))
		}
	})

	// Smoke: verify the DreamAction model writes and reads back the
	// new from/to columns. Belt-and-suspenders for the migration.
	t.Run("dream action: store + retrieve preserves from/to topic ids", func(t *testing.T) {
		mustExec(t, `DELETE FROM dream_actions WHERE run_id = $1::uuid`, runID)
		action := &model.DreamAction{
			ID:              "00000000-0000-0000-0000-aa11cc000020",
			RunID:           runID,
			ActionType:      "organize",
			SourceMemoryIDs: []string{memoryA},
			ResultMemoryID:  topicA,
			FromTopicID:     topicA2,
			ToTopicID:       topicA,
			Reason:          "roundtrip",
			CreatedAt:       actionAt,
		}
		if err := s.CreateDreamAction(action); err != nil {
			t.Fatalf("create: %v", err)
		}
		actions, err := s.GetDreamActions(runID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		var found bool
		for _, a := range actions {
			if a.ID == action.ID {
				found = true
				if a.FromTopicID != topicA2 || a.ToTopicID != topicA {
					t.Errorf("roundtrip mismatch: from=%q to=%q", a.FromTopicID, a.ToTopicID)
				}
			}
		}
		if !found {
			t.Errorf("inserted action not returned by GetDreamActions")
		}
	})
}
