package store_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// HubMembershipsForUser is on the chat hot path (every
// message-send for user_all_hubs/hub_subset scope resolution).
// Phase 3.7 step 1 widened it from IDs-only to (hub_id, role)
// pairs so the ScopeResolver can compute SessionScope's
// WritableHubIDs (hubs where role != "viewer") in the same
// query path it already used for HubIDs. The integration tests
// pin the contract:
//
//   - Returns exactly the hubs the user is a member of, no
//     others, with the role recorded in hub_members.
//   - Empty user → empty (non-nil) slice.
//   - Includes both owned and joined hubs.
//   - Excludes hubs the user has been kicked out of.
//   - Roles round-trip: an owner row reports "owner",
//     contributor reports "contributor", etc.

func TestPostgresHubMembershipsForUser_OwnerAndJoined(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	owner := seedUser(t, pool, "membership-owner")
	member := seedUser(t, pool, "membership-member")

	// Owner creates two team hubs (CreateTeamHub adds the owner
	// as a hub_members row automatically with role="owner").
	hubA := newTeamHub(t, s, pool, ctx, "hub-a", owner)
	hubB := newTeamHub(t, s, pool, ctx, "hub-b", owner)
	hubC := newTeamHub(t, s, pool, ctx, "hub-c", member)

	gotOwner, err := s.HubMembershipsForUser(ctx, owner)
	if err != nil {
		t.Fatalf("HubMembershipsForUser(owner): %v", err)
	}
	if !equalMembershipSets(gotOwner, []model.HubMembership{
		{HubID: hubA.ID, Role: "owner"},
		{HubID: hubB.ID, Role: "owner"},
	}) {
		t.Errorf("owner = %v, want owner roles on hubA + hubB", gotOwner)
	}

	gotMember, err := s.HubMembershipsForUser(ctx, member)
	if err != nil {
		t.Fatalf("HubMembershipsForUser(member): %v", err)
	}
	if !equalMembershipSets(gotMember, []model.HubMembership{
		{HubID: hubC.ID, Role: "owner"},
	}) {
		t.Errorf("member = %v, want owner role on hubC only", gotMember)
	}

	// Add member to hubA as contributor. AddHubMember writes
	// hub_members directly — the production path for invite-accept too.
	if err := s.AddHubMember(hubA.ID, member, "contributor"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}
	gotMember2, err := s.HubMembershipsForUser(ctx, member)
	if err != nil {
		t.Fatalf("HubMembershipsForUser(member after add): %v", err)
	}
	if !equalMembershipSets(gotMember2, []model.HubMembership{
		{HubID: hubA.ID, Role: "contributor"},
		{HubID: hubC.ID, Role: "owner"},
	}) {
		t.Errorf("member after add = %v, want contributor on hubA + owner on hubC", gotMember2)
	}
}

// Role round-trip: the four canonical hub_member roles all
// surface verbatim. The agent's WritableHubIDs depends on
// distinguishing viewer (read-only) from contributor (writable);
// pinning the round-trip here protects against a future migration
// silently casting the role column.
func TestPostgresHubMembershipsForUser_AllRolesRoundTrip(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	owner := seedUser(t, pool, "roles-owner")
	hub := newTeamHub(t, s, pool, ctx, "roles-hub", owner)

	roles := []string{"admin", "contributor", "viewer"}
	memberIDs := make([]string, 0, len(roles))
	for _, r := range roles {
		uid := seedUser(t, pool, "roles-"+r)
		if err := s.AddHubMember(hub.ID, uid, r); err != nil {
			t.Fatalf("AddHubMember(%s): %v", r, err)
		}
		memberIDs = append(memberIDs, uid)
	}

	for i, r := range roles {
		got, err := s.HubMembershipsForUser(ctx, memberIDs[i])
		if err != nil {
			t.Fatalf("HubMembershipsForUser(%s): %v", r, err)
		}
		if len(got) != 1 || got[0].HubID != hub.ID || got[0].Role != r {
			t.Errorf("role=%s got=%v, want [{%s, %s}]", r, got, hub.ID, r)
		}
	}
}

func TestPostgresHubMembershipsForUser_EmptyForUnknownUser(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	got, err := s.HubMembershipsForUser(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("HubMembershipsForUser(unknown): %v", err)
	}
	if got == nil {
		t.Error("got nil slice, want empty non-nil")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestPostgresHubMembershipsForUser_KickedOutUserLosesHub(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	owner := seedUser(t, pool, "kick-owner")
	target := seedUser(t, pool, "kick-target")
	hub := newTeamHub(t, s, pool, ctx, "kick-hub", owner)

	if err := s.AddHubMember(hub.ID, target, "contributor"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}
	got, _ := s.HubMembershipsForUser(ctx, target)
	if len(got) != 1 || got[0].HubID != hub.ID || got[0].Role != "contributor" {
		t.Fatalf("before kick: got %v, want [{%s, contributor}]", got, hub.ID)
	}

	// Remove via raw SQL (matches the production path for hub-kick).
	if _, err := pool.Exec(ctx,
		`DELETE FROM hub_members WHERE hub_id = $1::uuid AND user_id = $2::uuid`,
		hub.ID, target,
	); err != nil {
		t.Fatalf("DELETE hub_members: %v", err)
	}

	got, err := s.HubMembershipsForUser(ctx, target)
	if err != nil {
		t.Fatalf("HubMembershipsForUser(target after kick): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("after kick got %v, want empty", got)
	}
}

// newTeamHub creates a team hub for the test. Returns the persisted
// Hub; CreateTeamHub adds the owner to hub_members automatically
// with role="owner".
//
// CreateTeamHub takes a maxFreeTeamHubs cap (the per-user limit on
// owned free team hubs). 999 means "effectively unlimited for tests."
//
// We also bump can_create_hub to true via raw SQL — the shared
// seedUser helper inserts without it, and CreateTeamHub gates on
// the column.
func newTeamHub(t *testing.T, s storeForMembershipTests, pool *pgxpool.Pool, ctx context.Context, label, ownerID string) *model.Hub {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE users SET can_create_hub = true WHERE id = $1::uuid`,
		ownerID,
	); err != nil {
		t.Fatalf("set can_create_hub: %v", err)
	}
	hub := &model.Hub{
		ID:      uuid.NewString(),
		Name:    label,
		Slug:    label + "-" + uuid.NewString()[:8],
		HubType: "team",
		Plan:    model.HubFreeTeamPlanID,
		OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 999); err != nil {
		t.Fatalf("CreateTeamHub %s: %v", label, err)
	}
	return hub
}

// storeForMembershipTests is the subset of store.Store these
// tests touch. Tightening the surface here keeps the test
// file independent of unrelated Store interface changes.
type storeForMembershipTests interface {
	CreateTeamHub(ctx context.Context, hub *model.Hub, ownerID string, cap int) error
	HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error)
	AddHubMember(hubID, userID, role string) error
}

// equalMembershipSets reports whether two slices of
// HubMembership are equal as sets (order-insensitive). The
// store sorts by hub_id but the test compares against the
// sorted form anyway so a future ordering change doesn't
// cascade.
func equalMembershipSets(a, b []model.HubMembership) bool {
	if len(a) != len(b) {
		return false
	}
	cp := func(in []model.HubMembership) []model.HubMembership {
		out := append([]model.HubMembership(nil), in...)
		sort.Slice(out, func(i, j int) bool { return out[i].HubID < out[j].HubID })
		return out
	}
	a2, b2 := cp(a), cp(b)
	for i := range a2 {
		if a2[i] != b2[i] {
			return false
		}
	}
	return true
}
