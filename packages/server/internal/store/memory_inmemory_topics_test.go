package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// InMemoryStore topic CRUD + lookup smoke tests.

func TestInMemoryTopicCRUD(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	topic := &model.Topic{
		ID: "t1", HubID: "h1", OwnerID: "u1",
		Name: "Coffee", Icon: "☕",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	got, err := s.GetTopic("t1", "h1")
	if err != nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if got.Name != "Coffee" {
		t.Errorf("name = %q", got.Name)
	}

	list, err := s.ListTopics("h1")
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListTopics returned %d, want 1", len(list))
	}

	// GetTopicAccessible uses visibility scope.
	_, _ = s.GetTopicAccessible("t1", VisibilityScope{OwnerID: "u1", HubIDs: []string{"h1"}})

	// UpdateTopic.
	topic.Name = "Espresso"
	if err := s.UpdateTopic(topic); err != nil {
		t.Fatalf("UpdateTopic: %v", err)
	}

	// DeleteTopic.
	if err := s.DeleteTopic("t1", "h1"); err != nil {
		t.Fatalf("DeleteTopic: %v", err)
	}
}

func TestInMemoryTopicMemoryAssignmentStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	// Assignment stubs — exercise for no-panic.
	_ = s.AssignMemoryToTopic("m1", "t1", "h1", 0.9)
	_ = s.UnassignMemoryFromTopic("m1", "t1", "h1")
	_, _ = s.CountMemoriesByTopic(VisibilityScope{OwnerID: "u1"}, "h1")
	_, _ = s.CountUnassignedMemories(VisibilityScope{OwnerID: "u1"}, "h1")
	_, _, _ = s.CountTopicMemories(VisibilityScope{OwnerID: "u1"}, "h1")
	_ = s.ReorderTopics("h1", []model.ReorderOperation{{TopicID: "t1", Position: 0}})
}

func TestInMemoryTopicTreeHelpers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_, _ = s.GetTopicDepth("t1", "h1")
	_, _ = s.IsTopicDescendant("parent", "child", "h1")
	_, _ = s.GetSubtreeMaxDepth("t1", "h1")
	_, _ = s.SearchAccessibleTopics(context.Background(), "query",
		[]string{"h1"}, 5)
	_, _ = s.GetTopicActivitySummary(VisibilityScope{OwnerID: "u1"},
		"t1", "h1", time.Now().Add(-24*time.Hour), 3)
}

func TestInMemoryDreamRunListAndQueries(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	_, _ = s.ListDreamRuns("u1", 10)
	_, _ = s.ListDreamRunsByHub("h1", 10)
	_, _, _ = s.ListDreamRunsForUser(context.Background(),
		DreamRunListForUserOpts{UserID: "u1", Limit: 10})
	_, _ = s.GetDreamRunByID(context.Background(), "r1")
	_, _ = s.GetDreamActions("r1")
	_, _ = s.ListNotificationsByDreamRunID(context.Background(), "r1")
}

func TestInMemoryTxVariants(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Every *Tx helper should accept a nil tx in dev mode and not
	// panic. The WithTx call chains them to mirror production shape.
	_ = s.WithTx(context.Background(), func(tx pgx.Tx) error {
		_ = s.CreateDreamActionTx(context.Background(), tx, &model.DreamAction{ID: "a"})
		_ = s.UpdateDreamRunTx(context.Background(), tx, &model.DreamRun{ID: "r"})
		_, _ = s.MergeTopicsTx(context.Background(), tx, "h1", "target", []string{"src"})
		_ = s.ApplyTopicRestructureTx(context.Background(), tx, "h1", "src", "target")
		_, _ = s.AcceptHubInviteByIDTx(context.Background(), tx, "i1", "u1", 5)
		_ = s.DeclineHubInviteByIDTx(context.Background(), tx, "i1", "u1")
		return nil
	})

	// ApplyTopicRestructure (non-tx) is a stub too.
	_ = s.ApplyTopicRestructure(context.Background(), "h1", "src", "target")
}
