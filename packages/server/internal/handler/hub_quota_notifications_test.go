package handler

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// stubMembersStore returns a fixed hub member list for the recipient
// resolver. Only ListHubMembers is exercised; everything else falls
// through to the InMemoryStore base so the Store interface is
// satisfied without reimplementing it.
type stubMembersStore struct {
	*store.InMemoryStore
	members []model.HubMember
}

func (s *stubMembersStore) ListHubMembers(_ string) ([]model.HubMember, error) {
	return s.members, nil
}

func TestHubQuotaSourceID_CycleDedupes(t *testing.T) {
	cycle1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cycle2 := cycle1.Add(time.Hour) // a later cycle

	// Same hub, transition, recipient, but different cycle tokens →
	// the unique constraint on (source_kind, source_id) must not
	// collapse them. Asserts the cycle component of the key.
	sid1 := hubQuotaSourceID("hub-1", hubQuotaOverLimit, cycle1, "user-a")
	sid2 := hubQuotaSourceID("hub-1", hubQuotaOverLimit, cycle2, "user-a")
	if sid1 == sid2 {
		t.Fatalf("expected different source_ids for different cycles, got %s == %s", sid1, sid2)
	}

	// Same cycle → identical ids so retries within one cycle upsert.
	sid3 := hubQuotaSourceID("hub-1", hubQuotaOverLimit, cycle1, "user-a")
	if sid1 != sid3 {
		t.Fatalf("expected identical source_ids for same cycle, got %s != %s", sid1, sid3)
	}

	// Different recipients → different ids within the same cycle.
	sid4 := hubQuotaSourceID("hub-1", hubQuotaOverLimit, cycle1, "user-b")
	if sid1 == sid4 {
		t.Fatalf("expected different source_ids for different recipients, got %s == %s", sid1, sid4)
	}
}

func TestResolveHubQuotaRecipients_OwnerPlusAdmins(t *testing.T) {
	inMem := store.NewInMemoryStore()
	s := &stubMembersStore{
		InMemoryStore: inMem,
		members: []model.HubMember{
			{HubID: "h1", UserID: "owner-1", Role: "owner"},
			{HubID: "h1", UserID: "admin-1", Role: "admin"},
			{HubID: "h1", UserID: "admin-2", Role: "admin"},
			{HubID: "h1", UserID: "viewer-1", Role: "viewer"},
			{HubID: "h1", UserID: "contributor-1", Role: "contributor"},
		},
	}

	hub := &model.Hub{ID: "h1", OwnerID: "owner-1"}
	got := resolveHubQuotaRecipients(s, hub)

	// Expect owner + 2 admins; viewer + contributor filtered out.
	expectSet := map[string]struct{}{
		"owner-1": {}, "admin-1": {}, "admin-2": {},
	}
	if len(got) != len(expectSet) {
		t.Fatalf("expected %d recipients, got %d: %v", len(expectSet), len(got), got)
	}
	for _, id := range got {
		if _, ok := expectSet[id]; !ok {
			t.Errorf("unexpected recipient: %s", id)
		}
	}
	// Owner must come first for stable retries.
	if got[0] != "owner-1" {
		t.Errorf("expected owner first, got %s", got[0])
	}
}

func TestResolveHubQuotaRecipients_OwnerInMembersAsAdmin(t *testing.T) {
	// When the owner's own hub_members row has role=admin (possible
	// after an ownership pattern that promoted them), they MUST NOT
	// be duplicated in the recipient list.
	inMem := store.NewInMemoryStore()
	s := &stubMembersStore{
		InMemoryStore: inMem,
		members: []model.HubMember{
			{HubID: "h1", UserID: "owner-1", Role: "admin"},
			{HubID: "h1", UserID: "admin-2", Role: "admin"},
		},
	}
	hub := &model.Hub{ID: "h1", OwnerID: "owner-1"}
	got := resolveHubQuotaRecipients(s, hub)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique recipients, got %d: %v", len(got), got)
	}
}

// Ensure we compile against the store.Store interface — if any
// method is added to Store that InMemoryStore doesn't cover, this
// line will fail to build.
var _ store.Store = (*stubMembersStore)(nil)

// Silence unused-import warnings if the compile-time assertion
// alone isn't enough.
var _ = context.Background
