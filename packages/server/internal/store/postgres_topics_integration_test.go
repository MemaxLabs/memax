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

// setupTopicFixture lays down a user + personal hub combo so each
// topics test starts from the same shape. Returns store, hub id,
// user id — the three things every topic test needs to scope
// its writes.
func setupTopicFixture(t *testing.T) (store.Store, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@topic",
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
	return s, hubID, userID
}

func TestPostgresTopicCRUD(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupTopicFixture(t)

	now := time.Now().Truncate(time.Microsecond)
	topic := &model.Topic{
		ID:        uuid.NewString(),
		OwnerID:   userID,
		HubID:     hubID,
		Name:      "Coffee",
		Icon:      "☕",
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// --- CreateTopic ---
	if err := s.CreateTopic(topic); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	// --- GetTopic ---
	got, err := s.GetTopic(topic.ID, hubID)
	if err != nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if got.Name != "Coffee" {
		t.Errorf("GetTopic name = %q", got.Name)
	}

	// --- ListTopics ---
	list, err := s.ListTopics(hubID)
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListTopics returned %d topics, want 1", len(list))
	}

	// --- UpdateTopic ---
	topic.Name = "Espresso"
	topic.Icon = "☕️"
	if err := s.UpdateTopic(topic); err != nil {
		t.Fatalf("UpdateTopic: %v", err)
	}
	got, _ = s.GetTopic(topic.ID, hubID)
	if got.Name != "Espresso" {
		t.Errorf("UpdateTopic didn't persist; got %q", got.Name)
	}

	// --- DeleteTopic ---
	if err := s.DeleteTopic(topic.ID, hubID); err != nil {
		t.Fatalf("DeleteTopic: %v", err)
	}
	if got, _ := s.GetTopic(topic.ID, hubID); got != nil {
		t.Error("DeleteTopic should remove the row")
	}
}

func TestPostgresTopicMemoryAssignment(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupTopicFixture(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)

	// Create a topic + a memory. Use AssignMemoryToTopic to link
	// them; Count methods must reflect the linkage.
	topic := &model.Topic{
		ID: uuid.NewString(), OwnerID: userID, HubID: hubID,
		Name: "Work", Icon: "💼", Position: 0,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}

	mem := &model.Memory{
		ID: uuid.NewString(), HubID: hubID, OwnerID: userID,
		Title: "note", Content: "c", ContentType: "text/plain",
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

	// Unassigned count before linking: 1.
	scope := store.VisibilityScope{OwnerID: userID, HubIDs: []string{hubID}}
	unassigned, err := s.CountUnassignedMemories(scope, hubID)
	if err != nil {
		t.Fatalf("CountUnassignedMemories: %v", err)
	}
	if unassigned != 1 {
		t.Errorf("pre-assign unassigned = %d, want 1", unassigned)
	}

	// --- AssignMemoryToTopic ---
	if err := s.AssignMemoryToTopic(mem.ID, topic.ID, hubID, 0.95); err != nil {
		t.Fatalf("AssignMemoryToTopic: %v", err)
	}

	// Unassigned count after: 0.
	unassigned, _ = s.CountUnassignedMemories(scope, hubID)
	if unassigned != 0 {
		t.Errorf("post-assign unassigned = %d, want 0", unassigned)
	}

	// Topic memory count: 1.
	counts, err := s.CountMemoriesByTopic(scope, hubID)
	if err != nil {
		t.Fatalf("CountMemoriesByTopic: %v", err)
	}
	if counts[topic.ID] != 1 {
		t.Errorf("topic count for %q = %d, want 1 (full map: %+v)", topic.ID, counts[topic.ID], counts)
	}

	// --- UnassignMemoryFromTopic ---
	if err := s.UnassignMemoryFromTopic(mem.ID, topic.ID, hubID); err != nil {
		t.Fatalf("UnassignMemoryFromTopic: %v", err)
	}
	unassigned, _ = s.CountUnassignedMemories(scope, hubID)
	if unassigned != 1 {
		t.Errorf("after unassign, unassigned = %d, want 1", unassigned)
	}
	// Guard: unusing the same request ID is a safe no-op, not
	// an error (idempotent retry path).
	if err := s.UnassignMemoryFromTopic(mem.ID, topic.ID, hubID); err != nil {
		t.Errorf("re-unassign should be idempotent, got %v", err)
	}
	_ = ctx
}

// TestPostgresTopicArchiveRestoreSubtree exercises the recursive-CTE
// archive/restore primitives against real Postgres: cascade down the
// subtree, idempotency, partial-branch restore with root re-planting,
// and the archived-list ordering contract.
func TestPostgresTopicArchiveRestoreSubtree(t *testing.T) {
	t.Parallel()
	s, hubID, userID := setupTopicFixture(t)

	now := time.Now().Truncate(time.Microsecond)
	mk := func(name string, parentID *string, position int) *model.Topic {
		topic := &model.Topic{
			ID: uuid.NewString(), OwnerID: userID, HubID: hubID,
			ParentID: parentID, Name: name, Icon: "folder",
			Position: position, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.CreateTopic(topic); err != nil {
			t.Fatalf("CreateTopic(%s): %v", name, err)
		}
		return topic
	}

	root := mk("Root", nil, 0)
	child := mk("Child", &root.ID, 0)
	grandchild := mk("Grandchild", &child.ID, 0)
	sibling := mk("Sibling", nil, 1)

	// Archive root: whole subtree goes, sibling stays.
	archived, err := s.ArchiveTopicSubtree(root.ID, hubID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ArchiveTopicSubtree: %v", err)
	}
	if archived != 3 {
		t.Fatalf("expected 3 archived, got %d", archived)
	}
	active, err := s.ListTopics(hubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != sibling.ID {
		t.Fatalf("expected only sibling active, got %d topics", len(active))
	}
	archivedList, err := s.ListArchivedTopics(hubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(archivedList) != 3 {
		t.Fatalf("expected 3 archived topics, got %d", len(archivedList))
	}
	for _, topic := range archivedList {
		if topic.ArchivedAt == nil {
			t.Fatalf("archived topic %s missing archived_at", topic.Name)
		}
	}

	// Idempotent: nothing left to archive.
	archived, err = s.ArchiveTopicSubtree(root.ID, hubID, time.Now().UTC())
	if err != nil {
		t.Fatalf("re-archive: %v", err)
	}
	if archived != 0 {
		t.Fatalf("expected 0 newly archived, got %d", archived)
	}

	// Partial restore: child branch only → child re-planted at root,
	// grandchild follows, root stays archived.
	restored, err := s.RestoreTopicSubtree(child.ID, hubID, time.Now().UTC())
	if err != nil {
		t.Fatalf("RestoreTopicSubtree: %v", err)
	}
	if restored != 2 {
		t.Fatalf("expected 2 restored, got %d", restored)
	}
	gotChild, err := s.GetTopic(child.ID, hubID)
	if err != nil {
		t.Fatal(err)
	}
	if gotChild.ArchivedAt != nil {
		t.Fatal("child should be active after restore")
	}
	if gotChild.ParentID != nil {
		t.Fatalf("child should be re-planted at root, got parent_id=%v", gotChild.ParentID)
	}
	if gotChild.Position <= sibling.Position {
		t.Fatalf("re-planted child should take next root position after sibling, got %d", gotChild.Position)
	}
	gotGrandchild, err := s.GetTopic(grandchild.ID, hubID)
	if err != nil {
		t.Fatal(err)
	}
	if gotGrandchild.ParentID == nil || *gotGrandchild.ParentID != child.ID {
		t.Fatalf("grandchild should stay under child, got parent_id=%v", gotGrandchild.ParentID)
	}
	gotRoot, err := s.GetTopic(root.ID, hubID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot.ArchivedAt == nil {
		t.Fatal("root should remain archived after branch restore")
	}

	// Full restore of the remaining root: restores in place (no parent).
	restored, err = s.RestoreTopicSubtree(root.ID, hubID, time.Now().UTC())
	if err != nil {
		t.Fatalf("restore root: %v", err)
	}
	if restored != 1 {
		t.Fatalf("expected 1 restored, got %d", restored)
	}
	active, err = s.ListTopics(hubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 4 {
		t.Fatalf("expected all 4 topics active after full restore, got %d", len(active))
	}
}
