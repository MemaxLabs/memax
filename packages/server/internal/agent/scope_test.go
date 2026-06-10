package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeMembership is a deterministic MembershipReader used across
// the resolver tests. Real production store hits Postgres; that's
// tested separately at the integration boundary. Here we want
// focused coverage of the resolver's logic — intersection
// ordering, membership-respecting redaction, role → WritableHubIDs
// derivation, error propagation — without DB I/O.
//
// The `hubs` map carries lists of (hubID, role) pairs so each
// scope_test can pick the role distribution it wants. Tests that
// only care about ID matching can leave Role empty (which falls
// outside the writable-roles set, so WritableHubIDs comes back
// empty for those entries — matches the "viewer-equivalent"
// fall-through behavior).
type fakeMembership struct {
	hubs map[string][]model.HubMembership
	err  error
}

func (f *fakeMembership) HubMembershipsForUser(_ context.Context, userID string) ([]model.HubMembership, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.hubs[userID], nil
}

// hubsAsOwner is a small helper for tests that want every hub to
// resolve to a writable role. Equivalent to fakeMembership{
// hubs: { userID: [{h1,owner}, {h2,owner}, ...] } }.
func hubsAsOwner(userID string, hubIDs ...string) map[string][]model.HubMembership {
	out := make([]model.HubMembership, len(hubIDs))
	for i, h := range hubIDs {
		out[i] = model.HubMembership{HubID: h, Role: "owner"}
	}
	return map[string][]model.HubMembership{userID: out}
}

func TestScopeTypeValid(t *testing.T) {
	for _, tc := range []struct {
		in   ScopeType
		want bool
	}{
		{ScopeTypeUserAllHubs, true},
		{ScopeTypeSingleHub, true},
		{ScopeTypeHubSubset, true},
		{"", false},
		{"unknown", false},
	} {
		if got := tc.in.Valid(); got != tc.want {
			t.Errorf("ScopeType(%q).Valid() = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSessionDescriptorValidate(t *testing.T) {
	cases := []struct {
		name    string
		d       SessionDescriptor
		wantErr string // substring; empty = expect nil
	}{
		{
			name: "valid_user_all_hubs",
			d:    SessionDescriptor{OwnerID: "u1", Type: ScopeTypeUserAllHubs},
		},
		{
			name: "valid_single_hub",
			d:    SessionDescriptor{OwnerID: "u1", Type: ScopeTypeSingleHub, HubIDs: []string{"h1"}},
		},
		{
			name: "valid_hub_subset",
			d:    SessionDescriptor{OwnerID: "u1", Type: ScopeTypeHubSubset, HubIDs: []string{"h1", "h2"}},
		},
		{
			name:    "missing_owner",
			d:       SessionDescriptor{Type: ScopeTypeUserAllHubs},
			wantErr: "OwnerID is required",
		},
		{
			name:    "invalid_type",
			d:       SessionDescriptor{OwnerID: "u1", Type: "bogus"},
			wantErr: `invalid SessionDescriptor.Type "bogus"`,
		},
		{
			name:    "user_all_hubs_with_hubs",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeUserAllHubs, HubIDs: []string{"h1"}},
			wantErr: "ScopeTypeUserAllHubs must have empty HubIDs",
		},
		{
			name:    "single_hub_with_zero",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeSingleHub},
			wantErr: "must have exactly 1 HubID",
		},
		{
			name:    "single_hub_with_two",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeSingleHub, HubIDs: []string{"h1", "h2"}},
			wantErr: "must have exactly 1 HubID",
		},
		{
			name:    "hub_subset_empty",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeHubSubset, HubIDs: []string{}},
			wantErr: "must have at least 1 HubID",
		},
		{
			name:    "empty_hub_id_in_list",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeHubSubset, HubIDs: []string{"h1", ""}},
			wantErr: "HubIDs[1] is empty",
		},
		{
			name:    "duplicate_hub_id_in_list",
			d:       SessionDescriptor{OwnerID: "u1", Type: ScopeTypeHubSubset, HubIDs: []string{"h1", "h2", "h1"}},
			wantErr: `duplicate "h1" at index 2`,
		},
		{
			name: "valid_write_hub_in_subset",
			d: SessionDescriptor{
				OwnerID: "u1", Type: ScopeTypeHubSubset,
				HubIDs: []string{"h1", "h2"}, WriteHubID: "h2",
			},
		},
		{
			name: "valid_write_hub_user_all_hubs",
			d: SessionDescriptor{
				OwnerID: "u1", Type: ScopeTypeUserAllHubs,
				WriteHubID: "h-anywhere",
			},
		},
		{
			name: "write_hub_not_in_subset",
			d: SessionDescriptor{
				OwnerID: "u1", Type: ScopeTypeHubSubset,
				HubIDs: []string{"h1", "h2"}, WriteHubID: "h3",
			},
			wantErr: `WriteHubID "h3" not in HubIDs`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSessionScopeIsHubAllowed(t *testing.T) {
	s := SessionScope{HubIDs: []string{"h1", "h2", "h3"}}
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"h1", true},
		{"h2", true},
		{"h3", true},
		{"", false},
		{"h4", false},
		{"H1", false}, // case-sensitive — UUIDs are canonical lowercase
	} {
		if got := s.IsHubAllowed(tc.in); got != tc.want {
			t.Errorf("IsHubAllowed(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNewScopeResolverNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil MembershipReader")
		}
	}()
	NewScopeResolver(nil)
}

func TestResolveSingleHubIntersectsMembership(t *testing.T) {
	// SingleHub MUST intersect against live membership — chat
	// sessions can pick single_hub, and a session created for a
	// hub the user has since been removed from must NOT keep
	// returning that hub. (codex review caught the original
	// no-DB-shortcut as a correctness bug.)
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h1", "h-other")}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeSingleHub, HubIDs: []string{"h1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 1 || scope.HubIDs[0] != "h1" {
		t.Errorf("scope.HubIDs = %v, want [h1]", scope.HubIDs)
	}
	if scope.OwnerID != "u1" {
		t.Errorf("scope.OwnerID = %q, want u1", scope.OwnerID)
	}
}

func TestResolveSingleHubKickedOutReturnsEmpty(t *testing.T) {
	// The case codex caught: user had a chat session for h1, then
	// got removed from h1. Resolver must return empty HubIDs so
	// downstream tool wrappers (which check IsHubAllowed) reject
	// every recall/list/mutate against h1. No silent leak.
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h-other")} // h1 not in membership
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeSingleHub, HubIDs: []string{"h1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 0 {
		t.Errorf("scope.HubIDs = %v, want empty (user lost h1 access)", scope.HubIDs)
	}
}

func TestResolveSingleHubCopiesHubIDs(t *testing.T) {
	// Defense against a tool wrapper accidentally mutating the input
	// descriptor's HubIDs slice. Resolve returns its own copy.
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h1")}
	r := NewScopeResolver(m)
	d := SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeSingleHub, HubIDs: []string{"h1"},
	}
	scope, err := r.Resolve(context.Background(), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 1 {
		t.Fatalf("expected 1 resolved hub, got %v", scope.HubIDs)
	}
	scope.HubIDs[0] = "MUTATED"
	if d.HubIDs[0] != "h1" {
		t.Errorf("descriptor.HubIDs[0] = %q after mutation, want h1", d.HubIDs[0])
	}
}

func TestResolveUserAllHubs(t *testing.T) {
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h-personal", "h-work", "h-side-project")}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeUserAllHubs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 3 {
		t.Errorf("scope.HubIDs len = %d, want 3", len(scope.HubIDs))
	}
}

func TestResolveUserAllHubsZeroMemberships(t *testing.T) {
	// Edge case: a user who belongs to no team hubs (only their
	// personal hub, but that's outside hub_members in the current
	// schema). Resolver should return an empty HubIDs list rather
	// than error — the chat agent then has nothing to recall over,
	// which the agent surfaces as "I don't have any context yet."
	m := &fakeMembership{hubs: map[string][]model.HubMembership{"u-isolated": {}}}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u-isolated", Type: ScopeTypeUserAllHubs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 0 {
		t.Errorf("scope.HubIDs len = %d, want 0", len(scope.HubIDs))
	}
}

func TestResolveHubSubsetIntersectsWithMembership(t *testing.T) {
	// User created a session over [h1, h2, h3] but has since been
	// removed from h2. Resolve must drop h2 from the result while
	// keeping descriptor order for the survivors.
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h1", "h3", "h4")} // h4 added post-create, h2 lost
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1",
		Type:    ScopeTypeHubSubset,
		HubIDs:  []string{"h1", "h2", "h3"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := scope.HubIDs
	want := []string{"h1", "h3"} // descriptor order preserved
	if !slicesEqual(got, want) {
		t.Errorf("scope.HubIDs = %v, want %v", got, want)
	}
}

func TestResolveHubSubsetAllRemoved(t *testing.T) {
	// Edge case: user lost membership of every hub in their
	// chat_session.scope_hub_ids. Scope is empty. Tool calls will
	// have no targets; the agent should surface this gracefully.
	m := &fakeMembership{hubs: hubsAsOwner("u1", "h99")} // disjoint from session's pinned hubs
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1",
		Type:    ScopeTypeHubSubset,
		HubIDs:  []string{"h1", "h2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.HubIDs) != 0 {
		t.Errorf("scope.HubIDs = %v, want empty", scope.HubIDs)
	}
}

func TestResolvePropagatesMembershipError(t *testing.T) {
	// Postgres outage / connection drop. Resolve wraps the error so
	// callers can identify it as a scope-resolution failure.
	sentinel := errors.New("postgres: connection refused")
	m := &fakeMembership{err: sentinel}
	r := NewScopeResolver(m)
	_, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeUserAllHubs,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain doesn't include sentinel: %v", err)
	}
	if !contains(err.Error(), "scope resolve") {
		t.Errorf("error message missing 'scope resolve': %v", err)
	}
}

func TestResolveValidationErrorBeforeMembershipLookup(t *testing.T) {
	// Defense: if the descriptor is malformed, Resolve must reject it
	// BEFORE making any lookup calls. Avoids a wasted DB roundtrip
	// when the caller passed bad input.
	called := false
	m := &trackingMembership{
		inner:  &fakeMembership{},
		called: &called,
	}
	r := NewScopeResolver(m)
	_, err := r.Resolve(context.Background(), SessionDescriptor{
		// Missing OwnerID
		Type: ScopeTypeUserAllHubs,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if called {
		t.Errorf("HubMembershipsForUser was called despite validation failure")
	}
}

// trackingMembership wraps an inner reader and records whether it
// was called. Used to verify the resolver's
// reject-before-DB-on-bad-input invariant.
type trackingMembership struct {
	inner  MembershipReader
	called *bool
}

func (t *trackingMembership) HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error) {
	*t.called = true
	return t.inner.HubMembershipsForUser(ctx, userID)
}

// --- small helpers ---

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── Phase 3.7 step 1: WritableHubIDs derivation ────────────
//
// SessionScope.WritableHubIDs is the subset of HubIDs where the
// user has owner / admin / contributor (not viewer). The
// resolver derives it in the same single-pass loop that builds
// HubIDs. Tests pin the contract:
//
//   - Owner / admin / contributor → in WritableHubIDs.
//   - Viewer → in HubIDs (still readable) but NOT in WritableHubIDs.
//   - Unknown role string → NOT in WritableHubIDs (fail-closed).
//   - Order matches HubIDs order so debugging stays predictable.
//   - Empty HubIDs → empty WritableHubIDs.

func TestResolveWritableHubIDs_RoleMatrix(t *testing.T) {
	t.Parallel()
	m := &fakeMembership{hubs: map[string][]model.HubMembership{
		"u1": {
			{HubID: "h-owner", Role: "owner"},
			{HubID: "h-admin", Role: "admin"},
			{HubID: "h-contrib", Role: "contributor"},
			{HubID: "h-viewer", Role: "viewer"},
			{HubID: "h-unknown", Role: "totally-bogus-role"},
		},
	}}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1", Type: ScopeTypeUserAllHubs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// HubIDs includes ALL hubs (read-eligible).
	wantAllHubs := []string{"h-owner", "h-admin", "h-contrib", "h-viewer", "h-unknown"}
	if !slicesEqual(scope.HubIDs, wantAllHubs) {
		t.Errorf("HubIDs = %v, want %v", scope.HubIDs, wantAllHubs)
	}
	// WritableHubIDs excludes viewer + unknown role.
	wantWritable := []string{"h-owner", "h-admin", "h-contrib"}
	if !slicesEqual(scope.WritableHubIDs, wantWritable) {
		t.Errorf("WritableHubIDs = %v, want %v", scope.WritableHubIDs, wantWritable)
	}
}

func TestResolveWritableHubIDs_EmptyWhenNoMemberships(t *testing.T) {
	t.Parallel()
	m := &fakeMembership{hubs: map[string][]model.HubMembership{"u-isolated": {}}}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u-isolated", Type: ScopeTypeUserAllHubs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope.WritableHubIDs) != 0 {
		t.Errorf("WritableHubIDs = %v, want empty", scope.WritableHubIDs)
	}
}

// HubSubset preserves descriptor order for both HubIDs and
// WritableHubIDs. A pinned scope of [a, b, c] with viewer on b
// resolves to HubIDs=[a,b,c] and WritableHubIDs=[a,c] —
// descriptor order, viewer dropped.
func TestResolveWritableHubIDs_HubSubsetPreservesOrderAndFiltersViewer(t *testing.T) {
	t.Parallel()
	m := &fakeMembership{hubs: map[string][]model.HubMembership{
		"u1": {
			{HubID: "h-a", Role: "owner"},
			{HubID: "h-b", Role: "viewer"},
			{HubID: "h-c", Role: "contributor"},
		},
	}}
	r := NewScopeResolver(m)
	scope, err := r.Resolve(context.Background(), SessionDescriptor{
		OwnerID: "u1",
		Type:    ScopeTypeHubSubset,
		HubIDs:  []string{"h-a", "h-b", "h-c"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slicesEqual(scope.HubIDs, []string{"h-a", "h-b", "h-c"}) {
		t.Errorf("HubIDs = %v, want [h-a h-b h-c]", scope.HubIDs)
	}
	if !slicesEqual(scope.WritableHubIDs, []string{"h-a", "h-c"}) {
		t.Errorf("WritableHubIDs = %v, want [h-a h-c]", scope.WritableHubIDs)
	}
}

func TestSessionScope_IsHubWritable(t *testing.T) {
	t.Parallel()
	s := SessionScope{
		HubIDs:         []string{"h-a", "h-b", "h-c"},
		WritableHubIDs: []string{"h-a", "h-c"}, // h-b is viewer
	}
	cases := map[string]bool{
		"h-a":  true,
		"h-b":  false, // in HubIDs (readable) but not writable
		"h-c":  true,
		"h-z":  false, // not in scope at all
		"":     false, // empty hub id never writable
	}
	for in, want := range cases {
		if got := s.IsHubWritable(in); got != want {
			t.Errorf("IsHubWritable(%q) = %v, want %v", in, got, want)
		}
	}
}
