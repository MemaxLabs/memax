package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresMemoryCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@memtest",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hub := &model.Hub{
		ID: hubID, Name: "P", Slug: "p-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	now := time.Now().Truncate(time.Microsecond)
	mem := &model.Memory{
		ID:              uuid.NewString(),
		HubID:           hubID,
		OwnerID:         userID,
		Title:           "First memory",
		Content:         "Hello world",
		ContentType:     "text/plain",
		ContentHash:     "hash-" + uuid.NewString(),
		Summary:         "A greeting",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Tags:            []string{"greeting", "test"},
		Boundary:        "private",
		State:           "active",
		Source:          "test",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// --- CreateMemory ---
	if err := s.CreateMemory(mem); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	// --- GetMemory by id+owner (the primary ownership-scoped read) ---
	got, err := s.GetMemory(mem.ID, userID)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Title != "First memory" || got.ContentHash != mem.ContentHash {
		t.Errorf("GetMemory returned wrong row: %+v", got)
	}

	// --- GetMemory by wrong owner returns nothing (owner isolation) ---
	other := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'other@t', 'O', 'personal_free', now(), now())`,
		other,
	); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	if got, err := s.GetMemory(mem.ID, other); err == nil && got != nil {
		t.Error("GetMemory should isolate by owner_id — other user should not see the memory")
	}

	// --- GetMemoryByContentHash ---
	byHash, err := s.GetMemoryByContentHash(mem.ContentHash, userID, hubID)
	if err != nil {
		t.Fatalf("GetMemoryByContentHash: %v", err)
	}
	if byHash == nil || byHash.ID != mem.ID {
		t.Errorf("GetMemoryByContentHash returned wrong row: %+v", byHash)
	}

	// --- GetMemoryForAdmin (no owner filter) ---
	adminGet, err := s.GetMemoryForAdmin(mem.ID)
	if err != nil {
		t.Fatalf("GetMemoryForAdmin: %v", err)
	}
	if adminGet.ID != mem.ID {
		t.Error("admin read returned different row")
	}

	// --- ListMemories ---
	list, err := s.ListMemories(userID, 10)
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(list) != 1 || list[0].ID != mem.ID {
		t.Errorf("ListMemories = %+v", list)
	}

	// --- UpdateMemory ---
	mem.Title = "Updated title"
	mem.Tags = []string{"updated"}
	if err := s.UpdateMemory(mem); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}
	got, _ = s.GetMemory(mem.ID, userID)
	if got.Title != "Updated title" {
		t.Errorf("UpdateMemory did not persist; title = %q", got.Title)
	}

	// --- DeleteMemory ---
	if err := s.DeleteMemory(mem.ID, userID); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if got, err := s.GetMemory(mem.ID, userID); err == nil && got != nil {
		t.Error("DeleteMemory should remove the row")
	}
}

func TestPostgresCountMemoriesByOwnerAndHub(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@countmem",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "P", Slug: "p-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Fresh state: zero counts.
	if n, err := s.CountMemoriesByOwner(ctx, userID); err != nil || n != 0 {
		t.Errorf("CountMemoriesByOwner fresh = (%d, %v), want (0, nil)", n, err)
	}
	if n, err := s.CountMemoriesByHub(ctx, hubID); err != nil || n != 0 {
		t.Errorf("CountMemoriesByHub fresh = (%d, %v), want (0, nil)", n, err)
	}

	// Insert 3 active memories + 1 archived.
	now := time.Now().Truncate(time.Microsecond)
	for i := 0; i < 4; i++ {
		state := "active"
		if i == 3 {
			state = "archived"
		}
		mem := &model.Memory{
			ID:              uuid.NewString(),
			HubID:           hubID,
			OwnerID:         userID,
			Title:           "mem",
			Content:         "c",
			ContentType:     "text/plain",
			ContentHash:     uuid.NewString(),
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Tags:            []string{},
			State:           state,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.CreateMemory(mem); err != nil {
			t.Fatalf("CreateMemory %d: %v", i, err)
		}
	}

	// Owner count should be 3 (excludes archived — the canonical
	// behavior the quota enforcement depends on; if archived got
	// counted every deleted memory would keep taking quota).
	got, err := s.CountMemoriesByOwner(ctx, userID)
	if err != nil {
		t.Fatalf("CountMemoriesByOwner: %v", err)
	}
	if got != 3 {
		t.Errorf("CountMemoriesByOwner = %d, want 3 (archived excluded)", got)
	}

	// Hub count: same invariant, per-hub scope.
	got, err = s.CountMemoriesByHub(ctx, hubID)
	if err != nil {
		t.Fatalf("CountMemoriesByHub: %v", err)
	}
	if got != 3 {
		t.Errorf("CountMemoriesByHub = %d, want 3 (archived excluded)", got)
	}
}

func TestPostgresUpsertUserPreferencesMergesJSONB(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'pref@t', 'P', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// First upsert: set theme.
	if err := s.UpsertUserPreferences(userID, map[string]any{
		"theme": "dark",
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	prefs, err := s.GetUserPreferences(userID)
	if err != nil {
		t.Fatalf("GetUserPreferences: %v", err)
	}
	if prefs.Settings["theme"] != "dark" {
		t.Errorf("theme not persisted: %+v", prefs.Settings)
	}

	// Second upsert: set notifications — merge semantics means
	// theme survives (JSONB concat `||` on the key, not full
	// replace).
	if err := s.UpsertUserPreferences(userID, map[string]any{
		"notifications_enabled": false,
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	prefs, _ = s.GetUserPreferences(userID)
	if prefs.Settings["theme"] != "dark" {
		t.Errorf("theme LOST on second upsert — merge semantics broken: %+v", prefs.Settings)
	}
	if prefs.Settings["notifications_enabled"] != false {
		t.Errorf("notifications not persisted: %+v", prefs.Settings)
	}

	// Third upsert: overwrite existing key.
	if err := s.UpsertUserPreferences(userID, map[string]any{
		"theme": "light",
	}); err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	prefs, _ = s.GetUserPreferences(userID)
	if prefs.Settings["theme"] != "light" {
		t.Errorf("theme not overwritten: %+v", prefs.Settings)
	}
}

func TestPostgresUpdateUserPlan(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'up@t', 'U', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// UpdateUserPlan writes personal_plan_id (legacy users.plan is
	// NOT written per phase-6 convention).
	if err := s.UpdateUserPlan(ctx, userID, "personal_early_access"); err != nil {
		t.Fatalf("UpdateUserPlan: %v", err)
	}
	user, _ := s.GetUser(userID)
	if user.PersonalPlanID != "personal_early_access" {
		t.Errorf("personal_plan_id = %q, want personal_early_access", user.PersonalPlanID)
	}

	// Accepts legacy ID too — maps to scoped via
	// LegacyToScopedPlanID.
	if err := s.UpdateUserPlan(ctx, userID, "free"); err != nil {
		t.Fatalf("legacy UpdateUserPlan: %v", err)
	}
	user, _ = s.GetUser(userID)
	if user.PersonalPlanID != model.PersonalFreePlanID {
		t.Errorf("legacy mapping failed; got %q", user.PersonalPlanID)
	}

	// Unknown user returns error.
	if err := s.UpdateUserPlan(ctx, uuid.NewString(), "personal_free"); err == nil {
		t.Error("UpdateUserPlan should error for unknown user")
	}
}

func TestPostgresHubSubscriptionLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'sub-owner@t', 'O', 'personal_early_access', now(), now())`,
		ownerID,
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	hub := &model.Hub{
		ID: uuid.NewString(), Name: "Sub Hub", Slug: "sub-hub",
		HubType: "team", Plan: model.HubFreeTeamPlanID, OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, hub, ownerID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	// CreateTeamHub already seeded a hub_free_team subscription.
	// GetActiveHubSubscription should find it.
	sub, err := s.GetActiveHubSubscription(ctx, hub.ID)
	if err != nil {
		t.Fatalf("GetActiveHubSubscription: %v", err)
	}
	if sub == nil {
		t.Fatal("expected an active subscription from CreateTeamHub")
	}
	if sub.PlanID != model.HubFreeTeamPlanID {
		t.Errorf("initial plan = %q, want hub_free_team", sub.PlanID)
	}

	// UpdateHubSubscription: bump seats.
	sub.SeatCount = 5
	if err := s.UpdateHubSubscription(ctx, sub); err != nil {
		t.Fatalf("UpdateHubSubscription: %v", err)
	}
	fresh, _ := s.GetActiveHubSubscription(ctx, hub.ID)
	if fresh.SeatCount != 5 {
		t.Errorf("seat update didn't persist; got %d", fresh.SeatCount)
	}

	// ListHubSubscriptionsByBillingUser: must include this one.
	list, err := s.ListHubSubscriptionsByBillingUser(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListHubSubscriptionsByBillingUser: %v", err)
	}
	found := false
	for _, item := range list {
		if item.HubID == hub.ID {
			found = true
		}
	}
	if !found {
		t.Error("subscription missing from billing user's list")
	}
}

func TestPostgresAuthIdentityCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'auth@t', 'A', 'personal_free', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// --- CreateAuthIdentity ---
	if err := s.CreateAuthIdentity(userID, "github", "99999", "auth@t", "A", ""); err != nil {
		t.Fatalf("CreateAuthIdentity: %v", err)
	}

	// --- GetUserByAuthIdentity ---
	u, err := s.GetUserByAuthIdentity("github", "99999")
	if err != nil {
		t.Fatalf("GetUserByAuthIdentity: %v", err)
	}
	if u.ID != userID {
		t.Errorf("lookup returned wrong user: %q", u.ID)
	}

	// --- GetUserByCanonicalEmail (case-insensitive, whitespace-
	// tolerant lookup used by the invite consume path).
	u, err = s.GetUserByCanonicalEmail(" AUTH@t ")
	if err != nil {
		t.Fatalf("GetUserByCanonicalEmail: %v", err)
	}
	if u == nil || u.ID != userID {
		t.Error("canonical email lookup failed")
	}

	// --- ListAuthIdentities ---
	ids, err := s.ListAuthIdentities(userID)
	if err != nil {
		t.Fatalf("ListAuthIdentities: %v", err)
	}
	if len(ids) != 1 || ids[0].Provider != "github" {
		t.Errorf("identities = %+v", ids)
	}

	// --- DeleteAuthIdentity ---
	if err := s.DeleteAuthIdentity(userID, "github"); err != nil {
		t.Fatalf("DeleteAuthIdentity: %v", err)
	}
	ids, _ = s.ListAuthIdentities(userID)
	if len(ids) != 0 {
		t.Errorf("after delete, identities = %+v", ids)
	}
}
