package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresTopicVisits covers the topic_visits upsert/read surface
// that anchors the clear-on-visit semantics for scan-surface lifecycle
// signals. Skipped when DATABASE_URL is unset.
func TestPostgresTopicVisits(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres topic-visits test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	ownerID := "00000000-0000-0000-0000-cc11cc000001"
	hubID := "00000000-0000-0000-0000-cc11cc000002"
	topicID := "00000000-0000-0000-0000-cc11cc000003"
	absentTopicID := "00000000-0000-0000-0000-cc11cc000099"
	past := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Millisecond)
	later := past.Add(24 * time.Hour)

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM topic_visits WHERE user_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}

	mustExec(t,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'tvisit@test.local', 'TV', $2, $2)`,
		ownerID, past)
	mustExec(t,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'tv-hub', 'tv-hub', 'team', $2::uuid, $3, $3)`,
		hubID, ownerID, past)
	mustExec(t,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
		hubID, ownerID)
	mustExec(t,
		`INSERT INTO topics (id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, 'tv topic', '', 'folder', 0, false, false, $4, $4)`,
		topicID, ownerID, hubID, past)

	t.Run("not found returns error for missing (user, topic) pair", func(t *testing.T) {
		if _, err := s.GetTopicVisit(ownerID, absentTopicID); err == nil {
			t.Errorf("expected error for missing visit row, got nil")
		}
	})

	t.Run("upsert creates row and get roundtrips", func(t *testing.T) {
		if err := s.UpsertTopicVisit(ownerID, topicID, hubID, past); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		got, err := s.GetTopicVisit(ownerID, topicID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.UserID != ownerID || got.TopicID != topicID || got.HubID != hubID {
			t.Errorf("unexpected IDs: %+v", got)
		}
		if !got.FirstVisitedAt.Equal(past) {
			t.Errorf("FirstVisitedAt = %v, want %v", got.FirstVisitedAt, past)
		}
		if !got.LastVisitedAt.Equal(past) {
			t.Errorf("LastVisitedAt = %v, want %v", got.LastVisitedAt, past)
		}
	})

	t.Run("upsert advances last_visited_at, preserves first_visited_at", func(t *testing.T) {
		if err := s.UpsertTopicVisit(ownerID, topicID, hubID, later); err != nil {
			t.Fatalf("upsert 2nd: %v", err)
		}
		got, err := s.GetTopicVisit(ownerID, topicID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !got.FirstVisitedAt.Equal(past) {
			t.Errorf("FirstVisitedAt should stay at %v (preserved); got %v", past, got.FirstVisitedAt)
		}
		if !got.LastVisitedAt.Equal(later) {
			t.Errorf("LastVisitedAt should advance to %v; got %v", later, got.LastVisitedAt)
		}
	})
}
