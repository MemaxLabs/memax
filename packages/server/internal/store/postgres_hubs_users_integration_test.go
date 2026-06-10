package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresGetHubMemberCapNoSubscription(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Personal hub → no row in hub_subscriptions → cap = -1 (unlimited).
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@cap",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hub := &model.Hub{
		ID: uuid.NewString(), Name: "P", Slug: "cap-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: userID,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	cap, err := s.GetHubMemberCap(ctx, hub.ID)
	if err != nil {
		t.Fatalf("GetHubMemberCap: %v", err)
	}
	if cap != -1 {
		t.Errorf("personal hub cap = %d, want -1 (unlimited)", cap)
	}
}

func TestPostgresHubVisitRoundtrip(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'V', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@visit",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hub := &model.Hub{
		ID: uuid.NewString(), Name: "V", Slug: "v-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: userID,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Missing visit: GetHubVisit returns an error (pgx.ErrNoRows).
	if _, err := s.GetHubVisit(userID, hub.ID); err == nil {
		t.Error("GetHubVisit on absent row should error")
	}

	// First Upsert inserts; last_visited_at = first_visited_at.
	first := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.UpsertHubVisit(userID, hub.ID, first); err != nil {
		t.Fatalf("UpsertHubVisit (insert): %v", err)
	}
	got, err := s.GetHubVisit(userID, hub.ID)
	if err != nil {
		t.Fatalf("GetHubVisit: %v", err)
	}
	if !got.FirstVisitedAt.Equal(first) {
		t.Errorf("first_visited_at = %v, want %v", got.FirstVisitedAt, first)
	}
	if !got.LastVisitedAt.Equal(first) {
		t.Errorf("last_visited_at on insert = %v, want %v", got.LastVisitedAt, first)
	}

	// Second Upsert advances last_visited_at but NOT first_visited_at
	// (the conflict branch only touches last_visited_at).
	later := first.Add(time.Hour)
	if err := s.UpsertHubVisit(userID, hub.ID, later); err != nil {
		t.Fatalf("UpsertHubVisit (conflict): %v", err)
	}
	got, _ = s.GetHubVisit(userID, hub.ID)
	if !got.FirstVisitedAt.Equal(first) {
		t.Errorf("first_visited_at mutated on conflict: %v != %v", got.FirstVisitedAt, first)
	}
	if !got.LastVisitedAt.Equal(later) {
		t.Errorf("last_visited_at not advanced: %v", got.LastVisitedAt)
	}
}

func TestPostgresUpdateUserProfileAndGetUsersByIDs(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	var ids []string
	for i := 0; i < 3; i++ {
		id := uuid.NewString()
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, $3, 'personal_early_access', now(), now())`,
			id, id[:8]+"@byid", "User"+id[:4],
		); err != nil {
			t.Fatalf("seed user %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// UpdateUserProfile persists display_name.
	if err := s.UpdateUserProfile(ids[0], "Dr. Alice"); err != nil {
		t.Fatalf("UpdateUserProfile: %v", err)
	}

	// GetUsersByIDs returns a map keyed by ID, and includes display_name.
	m, err := s.GetUsersByIDs(ids)
	if err != nil {
		t.Fatalf("GetUsersByIDs: %v", err)
	}
	if len(m) != 3 {
		t.Errorf("got %d users, want 3", len(m))
	}
	if u, ok := m[ids[0]]; !ok || u.DisplayName != "Dr. Alice" {
		t.Errorf("display_name update didn't persist: %+v", u)
	}

	// Empty slice short-circuits (no SQL error, empty map).
	empty, err := s.GetUsersByIDs(nil)
	if err != nil {
		t.Errorf("GetUsersByIDs(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("GetUsersByIDs(nil) returned %d rows", len(empty))
	}
}

func TestPostgresUsageCountersIncrementAndDecrement(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'Use', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@usage",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Unknown field → error on all three methods (guard against typos
	// silently updating the wrong column).
	if err := s.IncrementUsage(userID, "bogus"); err == nil {
		t.Error("IncrementUsage with unknown field should error")
	}
	if _, err := s.IncrementUsageReturning(userID, "bogus"); err == nil {
		t.Error("IncrementUsageReturning with unknown field should error")
	}
	if err := s.DecrementUsage(userID, "bogus"); err == nil {
		t.Error("DecrementUsage with unknown field should error")
	}

	// Increment push_count: first call inserts, returning 1.
	n, err := s.IncrementUsageReturning(userID, "push_count")
	if err != nil {
		t.Fatalf("IncrementUsageReturning (insert): %v", err)
	}
	if n != 1 {
		t.Errorf("first increment returned %d, want 1", n)
	}

	// Second call hits ON CONFLICT, bumping to 2.
	n, err = s.IncrementUsageReturning(userID, "push_count")
	if err != nil {
		t.Fatalf("IncrementUsageReturning (update): %v", err)
	}
	if n != 2 {
		t.Errorf("second increment returned %d, want 2", n)
	}

	// Non-returning increment — just verify no error.
	if err := s.IncrementUsage(userID, "recall_count"); err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}

	// Decrement push_count from 2 → 1.
	if err := s.DecrementUsage(userID, "push_count"); err != nil {
		t.Fatalf("DecrementUsage: %v", err)
	}

	// Verify state by querying GetCurrentUsage.
	u, err := s.GetCurrentUsage(userID)
	if err != nil {
		t.Fatalf("GetCurrentUsage: %v", err)
	}
	if u.PushCount != 1 {
		t.Errorf("push_count after decrement = %d, want 1", u.PushCount)
	}
	if u.RecallCount != 1 {
		t.Errorf("recall_count = %d, want 1", u.RecallCount)
	}
}
