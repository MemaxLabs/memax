package agent

import (
	"context"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// hubMembershipStore is the focused store-side capability the
// runtime needs for membership lookups. Defined here as a narrow
// interface so the agent package's dependency on store/ stays
// minimal and the test surface is small. The full store.Store
// satisfies it directly.
type hubMembershipStore interface {
	HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error)
}

// StoreMembership adapts a store implementation to the
// MembershipReader interface the ScopeResolver expects. Use this
// when wiring the resolver in production: a single line in
// serverapp/workerapp constructs the runtime against the real
// Postgres store, identical signature for the in-memory store in
// tests that exercise the integration boundary.
//
// Compile-time check: any *store.PostgresStore (and any other
// implementation of store.Store) implements hubMembershipStore via
// the HubMembershipsForUser method.
type StoreMembership struct {
	s hubMembershipStore
}

// NewStoreMembership wraps s as a MembershipReader. s must not be nil.
func NewStoreMembership(s hubMembershipStore) *StoreMembership {
	if s == nil {
		panic("agent: NewStoreMembership requires non-nil store")
	}
	return &StoreMembership{s: s}
}

// HubMembershipsForUser delegates to the underlying store. The
// store's implementation already filters by user_id and returns a
// deterministic-ordered slice; we don't transform here — keeping
// the adapter a thin pass-through means the resolver test fake and
// the production path observe identical contracts.
func (m *StoreMembership) HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error) {
	return m.s.HubMembershipsForUser(ctx, userID)
}

// Compile-time assertion: store.Store satisfies hubMembershipStore.
// If a future refactor renames or removes HubMembershipsForUser
// from the Store interface, this fails to compile and we catch it
// at build time rather than at runtime when the resolver tries to
// look up a hub set.
var _ hubMembershipStore = (store.Store)(nil)
