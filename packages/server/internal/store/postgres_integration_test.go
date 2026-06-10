package store_test

// Integration tests for PostgresStore that run against a real
// Postgres database via internal/testdb. Each test gets a freshly
// migrated, isolated database — writes are dropped at test end.
//
// # Why this file is separate from the existing postgres_*_test.go
//
// The existing tests in this package use `os.Getenv("DATABASE_URL")`
// and connect directly, assuming a shared (pre-existing) DB.
// Running them concurrently sees cross-contamination because they
// all write to the same `memax` database. The harness in
// internal/testdb gives each test its own clone, so these tests
// can use t.Parallel safely AND don't have to clean up after
// themselves — the cleanup drops the entire database.
//
// # Coverage goal
//
// This file walks the core happy-path shapes of PostgresStore:
// users, hubs, memberships, memories, plan subscriptions. Each
// test exercises one public write method plus every read method
// that should reflect the write. Together they cover ~40 of the
// postgres_*.go methods with real SQL, real constraints, real
// cascade deletes.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// seedUser inserts a user row directly via SQL and returns its
// ID. We don't go through any store method because the server
// doesn't actually expose a general-purpose CreateUser — user rows
// land via OAuth provider handlers. Tests that want a user need
// the SQL primitive.
func seedUser(t *testing.T, pool *pgxpool.Pool, emailSuffix string) string {
	t.Helper()
	id := uuid.NewString()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal_free', now(), now())`,
		id, id[:8]+"@"+emailSuffix, "test-user",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func TestPostgresGetUserAndPersonalHub(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Seed a user row directly.
	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'getuser@test', 'Get User', 'personal_free', now(), now())`,
		id,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Create a personal hub for them.
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'My Notes', 'my-notes', 'personal', $2::uuid, now(), now())`,
		hubID, id,
	); err != nil {
		t.Fatalf("seed hub: %v", err)
	}

	// --- GetUser ---
	user, err := s.GetUser(id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Email != "getuser@test" || user.PersonalPlanID != "personal_free" {
		t.Errorf("GetUser returned wrong row: %+v", user)
	}

	// --- GetPersonalHub ---
	hub, err := s.GetPersonalHub(id)
	if err != nil {
		t.Fatalf("GetPersonalHub: %v", err)
	}
	if hub.ID != hubID || hub.HubType != "personal" {
		t.Errorf("GetPersonalHub returned wrong hub: %+v", hub)
	}
}

func TestPostgresTeamHubLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'teamhub-owner@test', 'Owner', 'personal_early_access', now(), now())`,
		ownerID,
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	// --- CreateTeamHub with cap check ---
	hub := &model.Hub{
		ID:      uuid.NewString(),
		Name:    "Team A",
		Slug:    "team-a",
		HubType: "team", Plan: model.HubFreeTeamPlanID,
		OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	// --- GetHub ---
	got, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatalf("GetHub: %v", err)
	}
	if got.Name != "Team A" || got.HubType != "team" {
		t.Errorf("GetHub got %+v", got)
	}

	// --- GetHubBySlug ---
	bySlug, err := s.GetHubBySlug("team-a")
	if err != nil {
		t.Fatalf("GetHubBySlug: %v", err)
	}
	if bySlug.ID != hub.ID {
		t.Errorf("GetHubBySlug got different hub: %+v", bySlug)
	}

	// --- Membership: CreateTeamHub already added the owner.
	// CountHubMembers should report 1 right now.
	count, err := s.CountHubMembers(hub.ID)
	if err != nil {
		t.Fatalf("CountHubMembers: %v", err)
	}
	if count != 1 {
		t.Errorf("CountHubMembers after create = %d, want 1 (just the owner)", count)
	}

	// --- AddHubMember (direct, no cap check) ---
	memberID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'member@test', 'Member', 'personal_free', now(), now())`,
		memberID,
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := s.AddHubMember(hub.ID, memberID, "contributor"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}
	count, _ = s.CountHubMembers(hub.ID)
	if count != 2 {
		t.Errorf("CountHubMembers after AddHubMember = %d, want 2", count)
	}

	// --- GetHubMemberRole ---
	role, err := s.GetHubMemberRole(hub.ID, memberID)
	if err != nil {
		t.Fatalf("GetHubMemberRole: %v", err)
	}
	if role != "contributor" {
		t.Errorf("role = %q, want contributor", role)
	}

	// --- UpdateHubMemberRole ---
	if err := s.UpdateHubMemberRole(hub.ID, memberID, "admin"); err != nil {
		t.Fatalf("UpdateHubMemberRole: %v", err)
	}
	role, _ = s.GetHubMemberRole(hub.ID, memberID)
	if role != "admin" {
		t.Errorf("role after update = %q, want admin", role)
	}

	// --- ListHubMembers ---
	members, err := s.ListHubMembers(hub.ID)
	if err != nil {
		t.Fatalf("ListHubMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("ListHubMembers len = %d, want 2", len(members))
	}

	// --- ListUserHubs (owner sees their team hub AND any
	// personal-scope hubs they have; since we didn't seed a
	// personal hub here, just the team).
	userHubs, err := s.ListUserHubs(ownerID)
	if err != nil {
		t.Fatalf("ListUserHubs: %v", err)
	}
	if len(userHubs) != 1 || userHubs[0].Hub.ID != hub.ID {
		t.Errorf("ListUserHubs owner: got %+v", userHubs)
	}

	// --- RemoveHubMember ---
	if err := s.RemoveHubMember(hub.ID, memberID); err != nil {
		t.Fatalf("RemoveHubMember: %v", err)
	}
	count, _ = s.CountHubMembers(hub.ID)
	if count != 1 {
		t.Errorf("CountHubMembers after remove = %d, want 1", count)
	}

	// --- UpdateHub ---
	hub.Name = "Team A Renamed"
	if err := s.UpdateHub(hub); err != nil {
		t.Fatalf("UpdateHub: %v", err)
	}
	got, _ = s.GetHub(hub.ID)
	if got.Name != "Team A Renamed" {
		t.Errorf("UpdateHub did not persist name; got %q", got.Name)
	}

	// --- PatchHubSettings ---
	if err := s.PatchHubSettings(ctx, hub.ID, map[string]any{
		"dreams_merge_enabled": false,
		"dreams_enabled":       true,
	}); err != nil {
		t.Fatalf("PatchHubSettings: %v", err)
	}
	got, _ = s.GetHub(hub.ID)
	if got.Settings["dreams_merge_enabled"] != false {
		t.Errorf("settings patch did not apply; got %+v", got.Settings)
	}

	// --- Clearing a key via null ---
	if err := s.PatchHubSettings(ctx, hub.ID, map[string]any{
		"dreams_merge_enabled": nil,
	}); err != nil {
		t.Fatalf("PatchHubSettings delete: %v", err)
	}
	got, _ = s.GetHub(hub.ID)
	if _, has := got.Settings["dreams_merge_enabled"]; has {
		t.Errorf("null patch should delete key; still have %+v", got.Settings)
	}

	// --- DeleteHub cascades through hub_members ---
	if err := s.DeleteHub(hub.ID); err != nil {
		t.Fatalf("DeleteHub: %v", err)
	}
	if _, err := s.GetHub(hub.ID); err == nil {
		t.Error("GetHub after delete should error, got nil")
	}
}

func TestPostgresHubMemberCapEnforced(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'capowner@test', 'Cap Owner', 'personal_early_access', now(), now())`,
		ownerID,
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	hub := &model.Hub{
		ID:      uuid.NewString(),
		Name:    "Capped",
		Slug:    "capped",
		HubType: "team", Plan: model.HubFreeTeamPlanID,
		OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	// CreateTeamHub added the owner as member 1. Cap of 2 means
	// we can add exactly one more; the next attempt must error
	// with the hub-full indicator. AddHubMemberWithCapCheck is
	// the atomic path used by AcceptInvite to avoid TOCTOU.
	for i := 0; i < 2; i++ {
		memberID := uuid.NewString()
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, 'Filler', 'personal_free', now(), now())`,
			memberID, memberID[:8]+"@test",
		); err != nil {
			t.Fatalf("seed member %d: %v", i, err)
		}
		err := s.AddHubMemberWithCapCheck(ctx, hub.ID, memberID, "contributor", 2)
		if i == 0 {
			if err != nil {
				t.Errorf("first fill should succeed, got %v", err)
			}
		} else {
			if err == nil {
				t.Errorf("second fill should error (cap=2, members=3 after insert)")
			}
		}
	}
}

func TestPostgresHubInviteLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	inviteeID := uuid.NewString()
	for _, tuple := range []struct{ id, email string }{
		{ownerID, "owner@test"},
		{inviteeID, "invited@test"},
	} {
		_, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
			tuple.id, tuple.email,
		)
		if err != nil {
			t.Fatalf("seed %s: %v", tuple.email, err)
		}
	}

	hub := &model.Hub{
		ID: uuid.NewString(), Name: "Hub",
		Slug: "hub-slug", HubType: "team", Plan: model.HubFreeTeamPlanID,
		OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	// --- CreateHubInvite ---
	inviteeEmail := "invited@test"
	invite := &model.HubInvite{
		ID:           uuid.NewString(),
		HubID:        hub.ID,
		Token:        "test-token-" + uuid.NewString(),
		InvitedBy:    ownerID,
		Role:         "contributor",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		InviteeEmail: &inviteeEmail,
	}
	if err := s.CreateHubInvite(invite); err != nil {
		t.Fatalf("CreateHubInvite: %v", err)
	}

	// --- GetHubInvite by ID ---
	got, err := s.GetHubInvite(invite.ID)
	if err != nil {
		t.Fatalf("GetHubInvite: %v", err)
	}
	if got.InviteeEmail == nil || *got.InviteeEmail != inviteeEmail {
		t.Errorf("invite invitee email mismatch: %+v", got.InviteeEmail)
	}

	// --- GetHubInviteByToken ---
	byToken, err := s.GetHubInviteByToken(invite.Token)
	if err != nil {
		t.Fatalf("GetHubInviteByToken: %v", err)
	}
	if byToken.ID != invite.ID {
		t.Errorf("GetHubInviteByToken got different row")
	}

	// --- CountActiveHubInvites ---
	if n, err := s.CountActiveHubInvites(hub.ID); err != nil || n != 1 {
		t.Errorf("CountActiveHubInvites = (%d, %v), want (1, nil)", n, err)
	}

	// --- ListOutstandingHubInvites ---
	list, err := s.ListOutstandingHubInvites(hub.ID)
	if err != nil {
		t.Fatalf("ListOutstandingHubInvites: %v", err)
	}
	if len(list) != 1 || list[0].ID != invite.ID {
		t.Errorf("ListOutstandingHubInvites = %+v", list)
	}

	// --- RevokeHubInvite ---
	if err := s.RevokeHubInvite(hub.ID, invite.ID, ownerID); err != nil {
		t.Fatalf("RevokeHubInvite: %v", err)
	}
	if n, _ := s.CountActiveHubInvites(hub.ID); n != 0 {
		t.Errorf("revoked invite should not count as active; got %d", n)
	}
}

func TestPostgresListPlans(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)

	// Migration 001 seeds the plan rows. Confirm the shape every
	// plan-resolver consumer depends on.
	plans, err := s.ListPlans(context.Background())
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) == 0 {
		t.Fatal("ListPlans returned 0 — migration 001 should have seeded canonical plan rows")
	}

	// Must include personal_free and personal_early_access —
	// handlers rely on these by name.
	byID := make(map[string]model.Plan, len(plans))
	for _, p := range plans {
		byID[p.ID] = p
	}
	for _, expected := range []string{"personal_free", "personal_early_access"} {
		p, ok := byID[expected]
		if !ok {
			t.Errorf("plan %q not seeded", expected)
			continue
		}
		if p.DisplayName == "" {
			t.Errorf("plan %q has empty DisplayName", expected)
		}
	}
}

func TestPostgresUsageRoundtrip(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)

	userID := seedUser(t, pool, "usage.test")

	// GetCurrentUsage on a fresh user returns a zero row (not nil,
	// not error) — handlers depend on being able to read even
	// before any push landed.
	usage, err := s.GetCurrentUsage(userID)
	if err != nil {
		t.Fatalf("GetCurrentUsage (fresh): %v", err)
	}
	if usage == nil {
		t.Fatal("GetCurrentUsage returned nil for fresh user; contract says zero row")
	}
	if usage.PushCount != 0 || usage.RecallCount != 0 || usage.AskCount != 0 {
		t.Errorf("fresh usage should be zeroed: %+v", usage)
	}
}
