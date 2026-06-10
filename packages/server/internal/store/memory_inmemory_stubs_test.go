package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Bulk coverage of the InMemoryStore's remaining "stub" surface —
// methods that return nil or an empty value without mutating state.
// They exist only to satisfy the Store interface for dev-mode; the
// Postgres variants have their own integration tests.
//
// We exercise every one so regressions that panic on nil receivers
// or zero arguments surface.

func TestInMemoryStubs_HubInvitesAndOwnership(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	_ = s.RevokeHubInvite("i1", "h1", "u1")
	_, _ = s.AcceptHubOwnershipTransfer("t1", "u1", "u2", 3)
	_ = s.CancelHubOwnershipTransfer("t1", "u1")
	_, _ = s.AcceptHubInviteByID(context.Background(), "i1", "u1", 5)
	_ = s.DeclineHubInviteByID(context.Background(), "i1", "u1")
}

func TestInMemoryStubs_WithTxExecutesCallback(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	called := false
	err := s.WithTx(context.Background(), func(_ pgx.Tx) error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("WithTx: %v", err)
	}
	if !called {
		t.Error("WithTx didn't invoke the callback")
	}
}

func TestInMemoryStubs_DreamLifecycle(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Every dream-lifecycle stub should exit cleanly.
	_ = s.CreateDreamRun(&model.DreamRun{ID: "r1"})
	_ = s.UpdateDreamRun(&model.DreamRun{ID: "r1"})
	_ = s.HeartbeatDreamRun(context.Background(), "r1")
	_, _ = s.ClaimStaleDreamRun(context.Background(), "h1", time.Minute)
	_ = s.CreateDreamAction(&model.DreamAction{ID: "a1"})
	_, _ = s.GetLatestDreamRun("u1")
	_, _ = s.GetLatestDreamRunByHub("h1")
	_ = s.ExecuteDreamMerge(&model.Memory{ID: "keeper"}, "absorbed", "u1", nil)
	_ = s.ExecuteDreamMergeInHub(&model.Memory{ID: "keeper"}, "absorbed", "h1", nil)
}

func TestInMemoryStubs_ArchiveAndDelete(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_ = s.ArchiveMemory("m1", "u1")
	_ = s.ArchiveMemoryInHub("m1", "h1")
	_, _ = s.ListArchiveCandidates("u1", 30, 10)
	_ = s.DeleteAllUserData("u1")
}

func TestInMemoryStubs_RetrievalHelpers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_, _ = s.FindRelevantTopicIDs("u1", []string{"m1"}, 3)
	_, _ = s.FindRelevantTopicIDsByHub("h1", []string{"m1"}, 3)
	_, _ = s.FindSimilarMemories("u1", 0.85, 10)
	_, _ = s.FindSimilarMemoriesByHub("h1", 0.85, 10)
}
