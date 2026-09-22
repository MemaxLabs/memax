package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// ListRecentMemoryActivityByHub — the 动静 receipt rows against real
// Postgres: hub isolation (memories AND topics), window, ordering,
// limit, archived / onboarding-seed exclusion, owner + agent + topic
// enrichment.
func TestPostgresRecentMemoryActivityByHub(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, display_name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'derek', 'Derek', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@activity",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	mkHub := func(slug string) string {
		id := uuid.NewString()
		if err := s.CreateHub(&model.Hub{
			ID: id, Name: slug, Slug: slug + "-" + userID[:8],
			HubType: "personal", OwnerID: userID,
		}); err != nil {
			t.Fatalf("CreateHub %s: %v", slug, err)
		}
		return id
	}
	hubID := mkHub("a")
	otherHub := mkHub("b")

	now := time.Now().UTC().Truncate(time.Microsecond)
	mk := func(hub, agent, title string, age time.Duration, mutate func(*model.Memory)) string {
		m := &model.Memory{
			ID: uuid.NewString(), HubID: hub, OwnerID: userID,
			Title: title, Content: title, ContentType: "text/plain",
			ContentHash: uuid.NewString(),
			Kind:        model.MemoryKindSemantic,
			Stability:   model.MemoryStabilityEvolving,
			Tags:        []string{},
			State:       "active",
			SourceAgent: agent,
			CreatedAt:   now.Add(-age), UpdatedAt: now.Add(-age),
		}
		if mutate != nil {
			mutate(m)
		}
		if err := s.CreateMemory(m); err != nil {
			t.Fatalf("CreateMemory %s: %v", title, err)
		}
		return m.ID
	}
	newest := mk(hubID, "cursor", "newest", time.Hour, nil)
	older := mk(hubID, "claude-code", "older", 3*time.Hour, nil)
	mk(hubID, "claude-code", "outside window", 40*time.Hour, nil)
	mk(otherHub, "claude-code", "other hub", time.Hour, nil)
	mk(hubID, "", "seed", time.Hour, func(m *model.Memory) { m.SourceKind = "onboarding-seed" })
	mk(hubID, "", "archived", time.Hour, func(m *model.Memory) { m.State = "archived" })

	// Topic in THIS hub on the older memory; a topic from the OTHER hub
	// wired onto the newest memory by a raw row — the receipt must not
	// borrow it (hub predicate inside the LATERAL join).
	topicID := uuid.NewString()
	if err := s.CreateTopic(&model.Topic{ID: topicID, OwnerID: userID, HubID: hubID, Name: "部署", Icon: "rocket"}); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	if err := s.AssignMemoryToTopic(older, topicID, hubID, 1.0); err != nil {
		t.Fatalf("AssignMemoryToTopic: %v", err)
	}
	foreignTopic := uuid.NewString()
	if err := s.CreateTopic(&model.Topic{ID: foreignTopic, OwnerID: userID, HubID: otherHub, Name: "别家的", Icon: "bug"}); err != nil {
		t.Fatalf("CreateTopic foreign: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO memory_topics (memory_id, topic_id, confidence, position, created_at)
		 VALUES ($1::uuid, $2::uuid, 1.0, 0, now())`,
		newest, foreignTopic,
	); err != nil {
		t.Fatalf("seed foreign memory_topics row: %v", err)
	}

	items, err := s.ListRecentMemoryActivityByHub(hubID, now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("ListRecentMemoryActivityByHub: %v", err)
	}
	if len(items) != 2 || items[0].MemoryID != newest || items[1].MemoryID != older {
		t.Fatalf("expected [newest, older], got %#v", items)
	}
	if items[0].AuthorID != userID || items[0].AuthorName != "Derek" || items[0].AgentSlug != "cursor" {
		t.Fatalf("attribution wrong: %#v", items[0])
	}
	if items[0].TopicID != "" {
		t.Fatalf("another hub's topic leaked into the receipt: %#v", items[0])
	}
	if items[1].TopicID != topicID || items[1].TopicName != "部署" || items[1].TopicIcon != "rocket" {
		t.Fatalf("topic enrichment wrong: %#v", items[1])
	}

	capped, err := s.ListRecentMemoryActivityByHub(hubID, now.Add(-24*time.Hour), 1)
	if err != nil || len(capped) != 1 || capped[0].MemoryID != newest {
		t.Fatalf("limit not honoured: %#v %v", capped, err)
	}
}
