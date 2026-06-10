package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestPostgresListUsers_NullPlanRowIncluded guards the regression
// where a user row with users.plan = NULL aborted the whole Scan
// loop in PostgresStore.ListUsers with an error, causing the admin
// users page to render "No users found" even though COUNT(*) and
// the stats endpoint still reported every user. The fix wraps
// u.plan in COALESCE(”) in the SELECT list.
//
// This test inserts a user with an explicit NULL legacy plan and
// asserts ListUsers returns the row without error. Uses a stable
// id/email so re-runs clean deterministically.
func TestPostgresListUsers_NullPlanRowIncluded(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres null-plan ListUsers test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	const (
		userID = "00000000-0000-0000-0000-eeeeee900001"
		email  = "null-plan-user@test.local"
	)

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Insert a user with an explicit NULL legacy plan. personal_plan_id
	// still gets the schema default ('personal_free') so the row is
	// otherwise well-formed.
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan)
		 VALUES ($1::uuid, $2, 'Null Plan', NULL)`,
		userID, email,
	)
	if err != nil {
		t.Fatalf("insert null-plan user: %v", err)
	}

	users, _, _, err := s.ListUsers(ctx, model.AdminUserListOpts{
		Query: email,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	found := false
	for _, u := range users {
		if u.ID == userID {
			found = true
			// Sanity: the legacy plan column coalesced to the empty string
			// (not the personal_plan_id fallback) — confirms the COALESCE
			// is in the SELECT, not hiding a different code path.
			if u.Plan != "" {
				t.Errorf("expected empty Plan after COALESCE, got %q", u.Plan)
			}
			// personal_plan_id keeps the schema default.
			if u.PersonalPlanID != "personal_free" {
				t.Errorf("expected personal_plan_id=personal_free, got %q", u.PersonalPlanID)
			}
		}
	}
	if !found {
		t.Fatalf("NULL-plan user not returned by ListUsers (got %d users)", len(users))
	}
}
