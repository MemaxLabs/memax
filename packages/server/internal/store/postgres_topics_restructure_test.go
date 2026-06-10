package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresApplyTopicRestructure exercises the reparent primitive
// against a real Postgres backend. Skipped when DATABASE_URL is unset.
func TestPostgresApplyTopicRestructure(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres restructure test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	ownerID := "00000000-0000-0000-0000-aaaaaa100001"
	hubA := "00000000-0000-0000-0000-aaaaaa100002"
	hubB := "00000000-0000-0000-0000-aaaaaa100003"
	now := time.Now()

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id = $1::uuid`, ownerID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id IN ($1::uuid, $2::uuid)`, hubA, hubB)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'restructure-test@test.local', 'Restructure Test', $2, $2)`,
		ownerID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	for _, h := range []struct{ id, slug string }{
		{hubA, "restructure-test-hub-a"},
		{hubB, "restructure-test-hub-b"},
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

	createTopic := func(t *testing.T, id, hubID, name string, parent *string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO topics (id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, '', 'folder', 0, false, false, now(), now())`,
			id, ownerID, hubID, parent, name); err != nil {
			t.Fatalf("create topic %s: %v", name, err)
		}
	}
	resetTopics := func(t *testing.T) {
		t.Helper()
		pool.Exec(ctx, `DELETE FROM topics WHERE owner_id = $1::uuid`, ownerID)
	}
	parentOf := func(t *testing.T, id string) string {
		t.Helper()
		var p *string
		if err := pool.QueryRow(ctx, `SELECT parent_id::text FROM topics WHERE id = $1::uuid`, id).Scan(&p); err != nil {
			t.Fatalf("scan parent: %v", err)
		}
		if p == nil {
			return ""
		}
		return *p
	}

	t.Run("happy path: reparent under new parent", func(t *testing.T) {
		resetTopics(t)
		oldParent := "00000000-0000-0000-0000-bbbbbb100001"
		child := "00000000-0000-0000-0000-bbbbbb100002"
		newParent := "00000000-0000-0000-0000-bbbbbb100003"
		createTopic(t, oldParent, hubA, "Old", nil)
		createTopic(t, newParent, hubA, "New", nil)
		createTopic(t, child, hubA, "Child", &oldParent)

		if err := s.ApplyTopicRestructure(ctx, hubA, child, newParent); err != nil {
			t.Fatalf("restructure: %v", err)
		}
		if got := parentOf(t, child); got != newParent {
			t.Errorf("child.parent: got %s, want %s", got, newParent)
		}
	})

	t.Run("happy path: promote child to root", func(t *testing.T) {
		resetTopics(t)
		oldParent := "00000000-0000-0000-0000-cccccc100001"
		child := "00000000-0000-0000-0000-cccccc100002"
		createTopic(t, oldParent, hubA, "Old", nil)
		createTopic(t, child, hubA, "Child", &oldParent)

		if err := s.ApplyTopicRestructure(ctx, hubA, child, ""); err != nil {
			t.Fatalf("restructure to root: %v", err)
		}
		if got := parentOf(t, child); got != "" {
			t.Errorf("child.parent: got %q, want empty (root)", got)
		}
	})

	t.Run("refuse self-parent", func(t *testing.T) {
		resetTopics(t)
		topic := "00000000-0000-0000-0000-dddddd100001"
		createTopic(t, topic, hubA, "Solo", nil)
		err := s.ApplyTopicRestructure(ctx, hubA, topic, topic)
		if !errors.Is(err, ErrTopicRestructureSelfParent) {
			t.Errorf("got %v, want ErrTopicRestructureSelfParent", err)
		}
	})

	t.Run("refuse cycle: new parent is descendant of child", func(t *testing.T) {
		resetTopics(t)
		root := "00000000-0000-0000-0000-eeeeee100001"
		mid := "00000000-0000-0000-0000-eeeeee100002"
		leaf := "00000000-0000-0000-0000-eeeeee100003"
		createTopic(t, root, hubA, "Root", nil)
		createTopic(t, mid, hubA, "Mid", &root)
		createTopic(t, leaf, hubA, "Leaf", &mid)
		// Try to reparent root under leaf — leaf is in root's subtree.
		err := s.ApplyTopicRestructure(ctx, hubA, root, leaf)
		if !errors.Is(err, ErrTopicRestructureCycle) {
			t.Errorf("got %v, want ErrTopicRestructureCycle", err)
		}
		if got := parentOf(t, root); got != "" {
			t.Errorf("root.parent should still be empty after rollback, got %s", got)
		}
	})

	t.Run("refuse cross-hub child", func(t *testing.T) {
		resetTopics(t)
		childInB := "00000000-0000-0000-0000-ffffff100001"
		parentInA := "00000000-0000-0000-0000-ffffff100002"
		createTopic(t, childInB, hubB, "Child in B", nil)
		createTopic(t, parentInA, hubA, "Parent in A", nil)
		err := s.ApplyTopicRestructure(ctx, hubA, childInB, parentInA)
		if !errors.Is(err, ErrTopicRestructureChildNotInHub) {
			t.Errorf("got %v, want ErrTopicRestructureChildNotInHub", err)
		}
	})

	t.Run("refuse cross-hub parent", func(t *testing.T) {
		resetTopics(t)
		childInA := "00000000-0000-0000-0000-aabbcc100001"
		parentInB := "00000000-0000-0000-0000-aabbcc100002"
		createTopic(t, childInA, hubA, "Child in A", nil)
		createTopic(t, parentInB, hubB, "Parent in B", nil)
		err := s.ApplyTopicRestructure(ctx, hubA, childInA, parentInB)
		if !errors.Is(err, ErrTopicRestructureParentNotInHub) {
			t.Errorf("got %v, want ErrTopicRestructureParentNotInHub", err)
		}
	})
}
