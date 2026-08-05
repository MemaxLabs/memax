package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// stubMembershipStore is a small fake satisfying the focused
// hubMembershipStore interface used by StoreMembership. The
// adapter is a thin pass-through, but we still pin its contract
// (delegation, error propagation, nil-receiver safety) so a
// future refactor can't silently drop a behavior.
type stubMembershipStore struct {
	hubs map[string][]model.HubMembership
	err  error
}

func (s *stubMembershipStore) HubMembershipsForUser(_ context.Context, userID string) ([]model.HubMembership, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.hubs[userID], nil
}

func TestNewStoreMembershipNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil store")
		}
	}()
	NewStoreMembership(nil)
}

func TestStoreMembershipDelegates(t *testing.T) {
	s := &stubMembershipStore{hubs: map[string][]model.HubMembership{
		"u1": {
			{HubID: "h-a", Role: "owner"},
			{HubID: "h-b", Role: "viewer"},
		},
	}}
	m := NewStoreMembership(s)
	got, err := m.HubMembershipsForUser(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].HubID != "h-a" || got[0].Role != "owner" ||
		got[1].HubID != "h-b" || got[1].Role != "viewer" {
		t.Errorf("got %v, want [{h-a, owner}, {h-b, viewer}]", got)
	}
}

func TestStoreMembershipPropagatesError(t *testing.T) {
	sentinel := errors.New("postgres: query failed")
	s := &stubMembershipStore{err: sentinel}
	m := NewStoreMembership(s)
	_, err := m.HubMembershipsForUser(context.Background(), "u1")
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error in chain, got %v", err)
	}
}

func TestStoreMembershipUnknownUserReturnsEmpty(t *testing.T) {
	// hubMembershipStore for an unknown user returns nil/empty
	// (the Postgres impl returns []HubMembership{} explicitly to
	// keep callers' slices.Contains/range correct). The adapter
	// must not transform either shape into the other; both must
	// pass through unchanged so the resolver's downstream
	// behavior stays consistent across in-memory and real-DB.
	s := &stubMembershipStore{hubs: map[string][]model.HubMembership{}}
	m := NewStoreMembership(s)
	got, err := m.HubMembershipsForUser(context.Background(), "u-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestStoreMembershipResolverRoundTrip(t *testing.T) {
	// End-to-end: production wiring is StoreMembership wrapping a
	// store, fed into NewScopeResolver. Verify a hub_subset session
	// resolves correctly through the full chain — including the
	// WritableHubIDs derivation from the role column.
	s := &stubMembershipStore{hubs: map[string][]model.HubMembership{
		"u1": {
			{HubID: "h-a", Role: "owner"},       // writable
			{HubID: "h-b", Role: "viewer"},      // readable, NOT writable
			{HubID: "h-c", Role: "contributor"}, // writable
		},
	}}
	r := NewScopeResolver(NewStoreMembership(s))
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1",
		Type:    ScopeTypeHubSubset,
		HubIDs:  []string{"h-a", "h-b", "h-z"}, // h-z lost (not in membership)
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(scope.HubIDs) != 2 || scope.HubIDs[0] != "h-a" || scope.HubIDs[1] != "h-b" {
		t.Errorf("scope.HubIDs = %v, want [h-a h-b]", scope.HubIDs)
	}
	// h-b is in HubIDs (readable) but NOT in WritableHubIDs (viewer).
	if len(scope.WritableHubIDs) != 1 || scope.WritableHubIDs[0] != "h-a" {
		t.Errorf("scope.WritableHubIDs = %v, want [h-a]", scope.WritableHubIDs)
	}
}
