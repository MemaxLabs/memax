package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestListDreamRunsForUser_PaginatesAcrossMembershipHubs covers the
// admin list endpoint's real query path:
//
//   - "User" dream runs span both personal hub (owner_id = user) AND
//     any team hub where the user is a hub_member. A user can legitimately
//     care about runs on hubs they don't own.
//   - Keyset pagination by (started_at DESC, id DESC).
//   - Optional hubID scope narrows to one hub.
//
// Fixture: one user who owns hub_p (personal) and is a contributor
// on hub_t (team). Three dream runs, two hubs, varied timestamps.
func TestListDreamRunsForUser_PaginatesAcrossMembershipHubs(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping admin dream list test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	userID := "00000000-0000-0000-0000-ad00aaaa0001"
	ownerOfTeam := "00000000-0000-0000-0000-ad00aaaa0002"
	hubP := "00000000-0000-0000-0000-ad00bbbb0001"
	hubT := "00000000-0000-0000-0000-ad00bbbb0002"
	now := time.Now().UTC().Truncate(time.Millisecond) // dodge nanosecond rounding

	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM dream_runs WHERE hub_id IN ($1::uuid, $2::uuid)`, hubP, hubT)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM hub_members WHERE hub_id IN ($1::uuid, $2::uuid)`, hubP, hubT)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM hubs WHERE id IN ($1::uuid, $2::uuid)`, hubP, hubT)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM users WHERE id IN ($1::uuid, $2::uuid)`, userID, ownerOfTeam)
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, u := range []struct{ id, email string }{
		{userID, "admin-dream-list-user@test.local"},
		{ownerOfTeam, "admin-dream-list-owner@test.local"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
			 VALUES ($1::uuid, $2, $2, 'free', 'personal_free', true, $3, $3)`,
			u.id, u.email, now); err != nil {
			t.Fatalf("seed user %s: %v", u.email, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5)`,
		hubP, "user-personal", "user-personal", userID, now); err != nil {
		t.Fatalf("seed personal hub: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'team', $4::uuid, $5, $5)`,
		hubT, "team-hub", "team-hub-admin-list", ownerOfTeam, now); err != nil {
		t.Fatalf("seed team hub: %v", err)
	}
	// User is a contributor on the team hub; should appear in their
	// dream-run list even though they don't own the hub.
	if _, err := pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'contributor')`,
		hubT, userID); err != nil {
		t.Fatalf("seed hub member: %v", err)
	}

	seedRun := func(id, hub string, startedAt time.Time) {
		t.Helper()
		owner := userID
		if hub == hubT {
			owner = ownerOfTeam
		}
		if err := s.CreateDreamRun(&model.DreamRun{
			ID: id, OwnerID: owner, HubID: hub,
			Mode: "maintenance", Status: "completed", StartedAt: startedAt,
		}); err != nil {
			t.Fatalf("CreateDreamRun %s: %v", id, err)
		}
		// Move out of `running` so the next seed doesn't hit the
		// unique index.
		finished := startedAt.Add(time.Second)
		if _, err := pool.Exec(ctx,
			`UPDATE dream_runs SET status = 'completed', finished_at = $1 WHERE id = $2::uuid`,
			finished, id); err != nil {
			t.Fatalf("finish run %s: %v", id, err)
		}
	}
	runNew := "00000000-0000-0000-0000-ad00cccc0001"
	runMid := "00000000-0000-0000-0000-ad00cccc0002"
	runOld := "00000000-0000-0000-0000-ad00cccc0003"
	seedRun(runOld, hubP, now.Add(-24*time.Hour))
	seedRun(runMid, hubT, now.Add(-12*time.Hour))
	seedRun(runNew, hubP, now.Add(-1*time.Hour))

	// All runs — ordered newest → oldest.
	all, nextCursor, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 runs across personal + team membership, got %d", len(all))
	}
	if all[0].ID != runNew || all[1].ID != runMid || all[2].ID != runOld {
		t.Fatalf("order drift: %v / %v / %v", all[0].ID, all[1].ID, all[2].ID)
	}
	if nextCursor != "" {
		t.Fatalf("next cursor should be empty when under limit, got %q", nextCursor)
	}

	// HubID scope narrows to one hub.
	teamOnly, _, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, HubID: hubT, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list team only: %v", err)
	}
	if len(teamOnly) != 1 || teamOnly[0].ID != runMid {
		t.Fatalf("team-scoped list wrong: %+v", teamOnly)
	}

	// Keyset pagination: limit=1 returns newest, next_cursor is
	// the composite (started_at|id). Next page uses cursor and
	// returns the middle row.
	page1, cursor1, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 1,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 1 || page1[0].ID != runNew || cursor1 == "" {
		t.Fatalf("unexpected page 1 state: rows=%+v cursor=%q", page1, cursor1)
	}
	page2, cursor2, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 1, Cursor: cursor1,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID != runMid || cursor2 == "" {
		t.Fatalf("unexpected page 2 state: rows=%+v cursor=%q", page2, cursor2)
	}

	// Composite cursor correctness: seed two runs sharing an exact
	// started_at (realistic when dreams are enqueued in batch).
	// A started_at-only cursor would skip the second one on page
	// boundary; (started_at, id) tuple comparison keeps it in.
	collisionTS := now.Add(-36 * time.Hour)
	runCollisionA := "00000000-0000-0000-0000-ad00cccc00a0"
	runCollisionB := "00000000-0000-0000-0000-ad00cccc00b0" // >A lexicographically
	seedRun(runCollisionA, hubP, collisionTS)
	seedRun(runCollisionB, hubP, collisionTS)
	// With ORDER BY started_at DESC, id DESC, runCollisionB comes
	// first among the two.
	colPage1, colCursor1, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 4,
	})
	if err != nil {
		t.Fatalf("collision page 1: %v", err)
	}
	// Expect 5 rows total now (3 original + 2 collision). limit=4
	// returns 4 + a non-empty cursor.
	if len(colPage1) != 4 || colCursor1 == "" {
		t.Fatalf("unexpected collision page 1: rows=%d cursor=%q", len(colPage1), colCursor1)
	}
	// The 4th row on page 1 should be one of the collision pair
	// (older than the 3 original rows). Page 2 must surface the
	// second collision row, not skip it.
	colPage2, _, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 4, Cursor: colCursor1,
	})
	if err != nil {
		t.Fatalf("collision page 2: %v", err)
	}
	if len(colPage2) != 1 {
		t.Fatalf("expected exactly 1 row on collision page 2 (the other collision row), got %d: %+v", len(colPage2), colPage2)
	}
	if colPage2[0].ID != runCollisionA && colPage2[0].ID != runCollisionB {
		t.Fatalf("collision page 2 returned unrelated row: %+v", colPage2[0])
	}

	// Invalid cursor → error, not silent empty.
	if _, _, err := s.ListDreamRunsForUser(ctx, DreamRunListForUserOpts{
		UserID: userID, Limit: 4, Cursor: "not-a-cursor",
	}); err == nil {
		t.Fatalf("expected error on malformed cursor")
	}
}

// TestListNotificationsByDreamRunID returns every notification
// tagged with dream_run_id and none without. Partial index
// idx_notifications_dream_run_id (migration 017) makes this cheap
// at scale; correctness is verified here.
func TestListNotificationsByDreamRunID(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ad11aaaa0001"
	hubID := "00000000-0000-0000-0000-ad11bbbb0001"
	runID := "00000000-0000-0000-0000-ad11cccc0001"
	otherRunID := "00000000-0000-0000-0000-ad11cccc0002"
	now := time.Now().UTC()

	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM notifications WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $2, 'free', 'personal_free', true, $3, $3)`,
		ownerID, "admin-dream-notif@test.local", now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, 'h', 'admin-dream-notif-h', 'personal', $2::uuid, $3, $3)`,
		hubID, ownerID, now); err != nil {
		t.Fatalf("seed hub: %v", err)
	}

	mkNotif := func(id, source string, dreamRun *string) *model.Notification {
		return &model.Notification{
			ID: id, Audience: model.AudienceUser, RecipientUserID: ownerID,
			HubID: hubID, Kind: model.NotificationKindDreamRunCompleted,
			Status:     model.NotificationStatusPending,
			SourceKind: "dream_run", SourceID: source,
			DreamRunID: dreamRun,
			CreatedAt:  now,
		}
	}
	if err := s.CreateNotification(mkNotif("00000000-0000-0000-0000-ad11dddd0001", "src-for-run", &runID)); err != nil {
		t.Fatalf("insert tagged 1: %v", err)
	}
	if err := s.CreateNotification(mkNotif("00000000-0000-0000-0000-ad11dddd0002", "src-for-run-2", &runID)); err != nil {
		t.Fatalf("insert tagged 2: %v", err)
	}
	if err := s.CreateNotification(mkNotif("00000000-0000-0000-0000-ad11dddd0003", "src-other", &otherRunID)); err != nil {
		t.Fatalf("insert tagged other: %v", err)
	}
	if err := s.CreateNotification(mkNotif("00000000-0000-0000-0000-ad11dddd0004", "src-untagged", nil)); err != nil {
		t.Fatalf("insert untagged: %v", err)
	}

	got, err := s.ListNotificationsByDreamRunID(ctx, runID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 notifications for runID=%s, got %d", runID, len(got))
	}
	for _, n := range got {
		if n.DreamRunID == nil || *n.DreamRunID != runID {
			t.Fatalf("list returned wrong row: %+v", n)
		}
	}

	// Other run and untagged row remain invisible.
	other, err := s.ListNotificationsByDreamRunID(ctx, otherRunID)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(other) != 1 {
		t.Fatalf("expected 1 row for otherRunID, got %d", len(other))
	}

	// Missing id → empty list, not an error.
	missing, err := s.ListNotificationsByDreamRunID(ctx, "00000000-0000-0000-0000-aaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("list missing: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected empty for missing run id, got %d", len(missing))
	}
}
