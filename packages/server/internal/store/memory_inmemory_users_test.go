package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Tests for the InMemoryStore user + hub-invite + usage-counter
// stubs. These are all dev-mode helpers used by handler unit
// tests; regressions silently break those.

func TestInMemoryUserCRUD(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "a@b", Name: "Alice"})

	got, err := s.GetUser("u1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.ID != "u1" {
		t.Errorf("got %+v", got)
	}

	// Missing user → error.
	if _, err := s.GetUser("missing"); err == nil {
		t.Error("unknown user should error")
	}
}

func TestInMemoryGetUsersByIDs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "a@b", Name: "A"})

	// The dev-store GetUsersByIDs is a stub returning an empty map.
	// Exercise both the loaded + empty slices so regressions that panic
	// surface.
	m, err := s.GetUsersByIDs([]string{"u1"})
	if err != nil {
		t.Errorf("GetUsersByIDs: %v", err)
	}
	_ = m
	empty, err := s.GetUsersByIDs(nil)
	if err != nil {
		t.Errorf("empty: %v", err)
	}
	_ = empty
}

func TestInMemoryUpdateUserProfile(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1", Email: "a@b", Name: "Alice"})

	// InMemoryStore stub doesn't mutate — just assert no panic/error.
	if err := s.UpdateUserProfile("u1", "Dr. Alice"); err != nil {
		t.Fatalf("UpdateUserProfile: %v", err)
	}
}

func TestInMemoryUsageCounters(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1"})

	// The dev-store variants are no-op stubs that don't validate the
	// field argument or maintain counters. Exercise them so panics
	// surface, but don't assert on the values — the Postgres variants
	// (which DO enforce the contract) already have their own test in
	// postgres_hubs_users_integration_test.go.
	_ = s.IncrementUsage("u1", "push_count")
	_, _ = s.IncrementUsageReturning("u1", "push_count")
	_ = s.DecrementUsage("u1", "push_count")
	_, _ = s.GetCurrentUsage("u1")
}

func TestInMemoryUserPreferences(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.AddUser(&model.User{ID: "u1"})

	// Exercise the getter/upsert pair. Exact shape of UserPreferences
	// varies; we just care these don't panic.
	_, _ = s.GetUserPreferences("u1")
	_ = s.UpsertUserPreferences("u1", map[string]any{"theme": "dark"})
}

func TestInMemoryHubInviteStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	// Exercise every hub-invite stub so regressions that panic on nil
	// receivers surface. The dev-store returns nil/empty for these.
	_, _ = s.ListOutstandingHubInvites("h1")
	_, _ = s.CountActiveHubInvites("h1")
	_ = s.UpdateHubInviteEmailEnqueuedAt("i1", time.Now())
	_, _ = s.ListExpiringHubInvites(24)
	_, _ = s.AcceptHubInvite("token-x", "u1", 3)
}

func TestInMemoryHubOwnershipTransfers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Exercise the ownership transfer stubs. Signatures differ from
	// PostgresStore; we just ensure none panic.
	_ = s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{ID: "t1"})
	_, _ = s.GetHubOwnershipTransfer("t1")
	_, _ = s.GetActiveHubOwnershipTransfer("h1")
}

func TestInMemoryAddHubMemberWithCapCheck(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.CreateHub(&model.Hub{ID: "h1", OwnerID: "u1", HubType: "team", Slug: "t"})
	// Pass a large cap so the dev store never rejects on count.
	if err := s.AddHubMemberWithCapCheck(context.Background(), "h1", "u2", "member", 100); err != nil {
		t.Errorf("AddHubMemberWithCapCheck: %v", err)
	}
}

func TestInMemoryListHubMembersPaginated(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_, _, _, err := s.ListHubMembersPaginated(context.Background(), "h1",
		model.AdminHubMemberListOpts{Limit: 10})
	if err != nil {
		t.Errorf("ListHubMembersPaginated: %v", err)
	}
}

func TestInMemoryListDreamableHubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	if _, err := s.ListDreamableHubs(); err != nil {
		t.Errorf("ListDreamableHubs: %v", err)
	}
}

func TestInMemoryMergeTopics(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Stub that should not panic. Signature: (ctx, hubID, targetID, sourceIDs).
	_, _ = s.MergeTopics(context.Background(), "h1", "target", []string{"src"})
}

func TestInMemorySearchChunksForHubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Empty store → empty results, no error.
	got, err := s.SearchChunksForHubs(context.Background(), "query", nil, "u1",
		[]string{"h1"}, 10, nil, nil)
	if err != nil {
		t.Errorf("SearchChunksForHubs: %v", err)
	}
	_ = got
}
