package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestPostgresListDreamableHubs_UsesScopedPersonalPlan verifies that the
// nightly dream sweep uses users.personal_plan_id as the canonical source for
// personal dream eligibility, with a legacy users.plan fallback only for rows
// that have not been backfilled yet.
func TestPostgresListDreamableHubs_UsesScopedPersonalPlan(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres dreamable hubs test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	now := time.Now()

	type userFixture struct {
		id             string
		email          string
		legacyPlan     string
		personalPlanID string
	}

	paidScoped := userFixture{
		id:             "00000000-0000-0000-0000-eeeeee100001",
		email:          "dream-paid-scoped@test.local",
		legacyPlan:     "free",
		personalPlanID: "personal_pro",
	}
	legacyPaid := userFixture{
		id:             "00000000-0000-0000-0000-eeeeee100002",
		email:          "dream-paid-second@test.local",
		legacyPlan:     "free",
		personalPlanID: "personal_early_access",
	}
	freeScoped := userFixture{
		id:             "00000000-0000-0000-0000-eeeeee100003",
		email:          "dream-free-scoped@test.local",
		legacyPlan:     "pro",
		personalPlanID: "personal_free",
	}

	personalHubIDs := map[string]string{
		paidScoped.id: "00000000-0000-0000-0000-eeeeee200001",
		legacyPaid.id: "00000000-0000-0000-0000-eeeeee200002",
		freeScoped.id: "00000000-0000-0000-0000-eeeeee200003",
	}
	teamHubID := "00000000-0000-0000-0000-eeeeee200004"

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = ANY($1::uuid[])`, []string{
			personalHubIDs[paidScoped.id],
			personalHubIDs[legacyPaid.id],
			personalHubIDs[freeScoped.id],
			teamHubID,
		})
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = ANY($1::uuid[])`, []string{
			personalHubIDs[paidScoped.id],
			personalHubIDs[legacyPaid.id],
			personalHubIDs[freeScoped.id],
			teamHubID,
		})
		pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, []string{
			paidScoped.id,
			legacyPaid.id,
			freeScoped.id,
		})
	}
	cleanup()
	t.Cleanup(cleanup)

	insertUser := func(t *testing.T, u userFixture) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
			 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)`,
			u.id, u.email, u.email, u.legacyPlan, u.personalPlanID, now,
		); err != nil {
			t.Fatalf("insert user %s: %v", u.email, err)
		}
	}

	for _, u := range []userFixture{paidScoped, legacyPaid, freeScoped} {
		insertUser(t, u)
	}

	insertPersonalHub := func(t *testing.T, hubID string, ownerID string, slug string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5)`,
			hubID, slug, slug, ownerID, now,
		); err != nil {
			t.Fatalf("insert personal hub %s: %v", slug, err)
		}
	}

	insertPersonalHub(t, personalHubIDs[paidScoped.id], paidScoped.id, "dream-paid-scoped")
	insertPersonalHub(t, personalHubIDs[legacyPaid.id], legacyPaid.id, "dream-legacy-paid")
	insertPersonalHub(t, personalHubIDs[freeScoped.id], freeScoped.id, "dream-free-scoped")

	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, plan, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'dream-team', 'dream-team', 'team', $2, $3::uuid, $4, $4)`,
		teamHubID, model.HubFreeTeamPlanID, paidScoped.id, now,
	); err != nil {
		t.Fatalf("insert team hub: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hub_subscriptions (hub_id, plan_id, seat_count, provider, status, billing_user_id, current_period_start)
		 VALUES ($1::uuid, $2, 3, 'admin', 'active', $3::uuid, $4)`,
		teamHubID, model.HubTeamPlanID, paidScoped.id, now,
	); err != nil {
		t.Fatalf("insert team hub subscription: %v", err)
	}

	hubs, err := s.ListDreamableHubs()
	if err != nil {
		t.Fatalf("ListDreamableHubs: %v", err)
	}

	got := map[string]bool{}
	for _, hub := range hubs {
		got[hub.ID] = true
	}

	if !got[personalHubIDs[paidScoped.id]] {
		t.Fatalf("scoped paid personal hub should be dreamable")
	}
	if !got[personalHubIDs[legacyPaid.id]] {
		t.Fatalf("second scoped paid personal hub should be dreamable")
	}
	if got[personalHubIDs[freeScoped.id]] {
		t.Fatalf("scoped free personal hub must not be dreamable")
	}
	if !got[teamHubID] {
		t.Fatalf("team hub with active hub_team subscription should be dreamable even if hubs.plan is stale")
	}
}
