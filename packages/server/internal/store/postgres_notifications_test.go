package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestSupportedKindsCoherence is a pure-Go drift-proofing assertion
// between supportedNotificationKinds and notificationKindBucket. Every
// kind that appears in the summary pre-populate list must resolve to
// exactly one bucket so the summary's NeedsActionPending /
// UpdatesPending totals stay correct as new kinds land.
func TestSupportedKindsCoherence(t *testing.T) {
	for _, k := range supportedNotificationKinds {
		bucket := notificationKindBucket(k)
		if bucket != bucketNeedsAction && bucket != bucketUpdates {
			t.Errorf("kind %q has no bucket classification; update notificationKindBucket", k)
		}
	}
}

// TestPostgresNotificationsVisibility exercises Phase 3a follow-up
// contract fixes against a real Postgres backend. Skipped when
// DATABASE_URL is unset (same pattern as field_lane_test.go /
// postgres_topics_merge_test.go). Requires migration 062 to be
// applied to the connected database.
//
// Four focused cases per the follow-up spec:
//   - hub-axis row in the selected hub → included
//   - hub-axis row in a different hub → excluded
//   - user-audience row when HubID is set → excluded (strict hub
//     narrowing; user-axis rows only surface in the unscoped view)
//   - GetNotificationSummary returns zero counts for every kind in
//     supportedNotificationKinds even when no rows exist
func TestPostgresNotificationsVisibility(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres notifications visibility test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	userID := "00000000-0000-0000-0000-aaaaaa300001"
	hubA := "00000000-0000-0000-0000-aaaaaa300002"
	hubB := "00000000-0000-0000-0000-aaaaaa300003"
	now := time.Now()

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM notifications WHERE recipient_user_id = $1::uuid OR hub_id = ANY($2::uuid[])`,
			userID, []string{hubA, hubB})
		pool.Exec(ctx, `DELETE FROM hub_members WHERE user_id = $1::uuid`, userID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = ANY($1::uuid[])`, []string{hubA, hubB})
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, 'notifs-test@test.local', 'Notifs Test', $2, $2)`,
		userID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	for _, h := range []struct{ id, slug string }{
		{hubA, "notifs-hub-a"},
		{hubB, "notifs-hub-b"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, $2, $2, 'team', $3::uuid, $4, $4)`,
			h.id, h.slug, userID, now); err != nil {
			t.Fatalf("insert hub %s: %v", h.slug, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
			h.id, userID); err != nil {
			t.Fatalf("insert hub member: %v", err)
		}
	}

	createHubNotification := func(t *testing.T, hubID, kind string) string {
		t.Helper()
		n := &model.Notification{
			ID:         uuidFor(t, hubID+kind),
			Audience:   model.AudienceHubMember,
			HubID:      hubID,
			Kind:       kind,
			Status:     model.NotificationStatusPending,
			Priority:   0,
			SourceKind: "test",
			SourceID:   hubID + ":" + kind,
			Payload:    json.RawMessage(`{}`),
			CreatedAt:  time.Now(),
		}
		if err := s.CreateNotification(n); err != nil {
			t.Fatalf("create hub notification (%s/%s): %v", hubID, kind, err)
		}
		return n.ID
	}
	createUserNotification := func(t *testing.T, recipient, kind string) string {
		t.Helper()
		n := &model.Notification{
			ID:              uuidFor(t, recipient+kind+"-user"),
			Audience:        model.AudienceUser,
			RecipientUserID: recipient,
			Kind:            kind,
			Status:          model.NotificationStatusPending,
			Priority:        0,
			SourceKind:      "test",
			SourceID:        recipient + ":" + kind,
			Payload:         json.RawMessage(`{}`),
			CreatedAt:       time.Now(),
		}
		if err := s.CreateNotification(n); err != nil {
			t.Fatalf("create user notification (%s/%s): %v", recipient, kind, err)
		}
		return n.ID
	}
	resetNotifications := func(t *testing.T) {
		t.Helper()
		pool.Exec(ctx, `DELETE FROM notifications WHERE recipient_user_id = $1::uuid OR hub_id = ANY($2::uuid[])`,
			userID, []string{hubA, hubB})
	}

	containsID := func(rows []model.Notification, id string) bool {
		for _, r := range rows {
			if r.ID == id {
				return true
			}
		}
		return false
	}

	t.Run("hub row in selected hub is included", func(t *testing.T) {
		resetNotifications(t)
		inA := createHubNotification(t, hubA, "review_contradiction")

		got, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
			HubID:  hubA,
		})
		if err != nil {
			t.Fatalf("list (hubA scope): %v", err)
		}
		if !containsID(got, inA) {
			t.Errorf("hub row in hubA missing from HubID=hubA query; got %d rows", len(got))
		}
	})

	t.Run("hub row in different hub is excluded", func(t *testing.T) {
		resetNotifications(t)
		inA := createHubNotification(t, hubA, "review_contradiction")
		inB := createHubNotification(t, hubB, "review_topic_merge")

		got, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
			HubID:  hubA,
		})
		if err != nil {
			t.Fatalf("list (hubA scope): %v", err)
		}
		if !containsID(got, inA) {
			t.Errorf("hubA row missing from HubID=hubA query")
		}
		if containsID(got, inB) {
			t.Errorf("hubB row leaked into HubID=hubA query; strict narrowing not enforced")
		}
	})

	t.Run("hub-scoped query includes only same-hub user-audience rows", func(t *testing.T) {
		resetNotifications(t)
		inA := createHubNotification(t, hubA, "review_contradiction")
		userRowOtherHub := createUserNotification(t, userID, "dream_run_completed")
		userRowSameHub := model.Notification{
			ID:              "99999999-9999-4999-8999-999999999991",
			Audience:        model.AudienceUser,
			RecipientUserID: userID,
			HubID:           hubA,
			Kind:            "dream_run_completed",
			Status:          model.NotificationStatusPending,
			Priority:        0,
			SourceKind:      "dream_run",
			SourceID:        "run-hub-a",
			CreatedAt:       time.Now(),
		}
		if err := s.CreateNotification(&userRowSameHub); err != nil {
			t.Fatalf("create same-hub user notification: %v", err)
		}

		// Unscoped query returns all visible rows.
		unscoped, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
		})
		if err != nil {
			t.Fatalf("list (unscoped): %v", err)
		}
		if !containsID(unscoped, inA) || !containsID(unscoped, userRowOtherHub) || !containsID(unscoped, userRowSameHub.ID) {
			t.Errorf("unscoped query missed rows: hubA=%v otherHubUser=%v sameHubUser=%v (rows=%d)",
				containsID(unscoped, inA), containsID(unscoped, userRowOtherHub), containsID(unscoped, userRowSameHub.ID), len(unscoped))
		}

		// Hub-scoped query includes same-hub direct rows only.
		scoped, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
			HubID:  hubA,
		})
		if err != nil {
			t.Fatalf("list (hubA scope): %v", err)
		}
		if !containsID(scoped, inA) {
			t.Errorf("hubA row missing from HubID=hubA query")
		}
		if !containsID(scoped, userRowSameHub.ID) {
			t.Errorf("same-hub user-audience row missing from hub-scoped query")
		}
		if containsID(scoped, userRowOtherHub) {
			t.Errorf("other-hub user-audience row leaked into hub-scoped query")
		}
	})

	t.Run("hub-scoped query from non-member returns empty", func(t *testing.T) {
		// Caller whose HubIDs do not include the requested hub must
		// receive an empty result (no leak of hub existence). The
		// helper returns "1 = 0" when the caller is not a member.
		resetNotifications(t)
		_ = createHubNotification(t, hubA, "review_contradiction")

		got, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{}, // user has no memberships in this call
			HubID:  hubA,
		})
		if err != nil {
			t.Fatalf("list (non-member scope): %v", err)
		}
		if len(got) != 0 {
			t.Errorf("non-member hub-scoped query returned %d rows; want 0", len(got))
		}
	})

	t.Run("summary returns zero counts for every supported kind", func(t *testing.T) {
		resetNotifications(t)
		summary, err := s.GetNotificationSummary(ctx, NotificationSummaryOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
		})
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if summary.NeedsActionPending != 0 || summary.UpdatesPending != 0 || summary.UpdatesUnseen != 0 {
			t.Errorf("totals non-zero on empty DB: needs=%d updates_p=%d updates_u=%d",
				summary.NeedsActionPending, summary.UpdatesPending, summary.UpdatesUnseen)
		}
		for _, k := range supportedNotificationKinds {
			entry, ok := summary.ByKind[k]
			if !ok {
				t.Errorf("summary.ByKind missing supported kind %q", k)
				continue
			}
			if entry.Pending != 0 || entry.Unseen != 0 {
				t.Errorf("summary.ByKind[%q] = %+v; want {0, 0}", k, entry)
			}
		}
	})

	t.Run("user-audience row addressed to someone else does not leak via hub axis", func(t *testing.T) {
		// REGRESSION: the pre-fix visibility predicate was
		// (hub_id = ANY(HubIDs) OR recipient_user_id = UserID),
		// audience-blind. An invite addressed to a different user
		// that carried hub_id as decoration was visible to every
		// hub member via the hub clause — the "why am I seeing my
		// own invite?" bug. The fixed predicate gates each axis on
		// the notification_audience enum: the user axis requires
		// audience='user' AND recipient match; the hub axis
		// requires audience IN ('hub','hub_member') AND hub_id
		// match. This test pins that behavior by seeding a
		// user-addressed row with hub_id set to a hub the caller
		// owns and asserting the caller does NOT see it.
		resetNotifications(t)

		// Create a second user (the real invitee) with a real
		// users row so the recipient_fkey holds.
		otherUserID := "00000000-0000-0000-0000-aaaaaa3000ff"
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, created_at, updated_at)
			 VALUES ($1::uuid, 'notifs-other@test.local', 'Other', $2, $2)
			 ON CONFLICT (id) DO NOTHING`,
			otherUserID, time.Now()); err != nil {
			t.Fatalf("insert other user: %v", err)
		}
		t.Cleanup(func() {
			pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, otherUserID)
		})

		leakRow := &model.Notification{
			ID:              "88888888-8888-4888-8888-888888888881",
			Audience:        model.AudienceUser,
			RecipientUserID: otherUserID, // addressed to someone else
			HubID:           hubA,        // decoration — caller owns hubA
			Kind:            "hub_invite",
			Status:          model.NotificationStatusPending,
			Priority:        1,
			SourceKind:      "hub_invite",
			SourceID:        "leak-scenario",
			Payload:         json.RawMessage(`{}`),
			CreatedAt:       time.Now(),
		}
		if err := s.CreateNotification(leakRow); err != nil {
			t.Fatalf("create leak row: %v", err)
		}

		// Unscoped query — caller must NOT see the row even though
		// they own hubA.
		unscoped, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
		})
		if err != nil {
			t.Fatalf("list (unscoped): %v", err)
		}
		if containsID(unscoped, leakRow.ID) {
			t.Errorf("user-audience row addressed to otherUserID leaked into caller's unscoped inbox via hub axis")
		}

		// Hub-scoped query (caller scoping to hubA) — same answer.
		scoped, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
			HubID:  hubA,
		})
		if err != nil {
			t.Fatalf("list (hubA scope): %v", err)
		}
		if containsID(scoped, leakRow.ID) {
			t.Errorf("user-audience row addressed to otherUserID leaked into caller's hubA-scoped inbox")
		}

		// Summary count — the row must NOT bump any of the caller's
		// counters (the "badge dot lights for rows you can't act
		// on" half of the bug).
		summary, err := s.GetNotificationSummary(ctx, NotificationSummaryOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
		})
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if summary.NeedsActionPending != 0 {
			t.Errorf("summary.NeedsActionPending bumped by leaked row: got %d, want 0", summary.NeedsActionPending)
		}
		if got := summary.ByKind["hub_invite"]; got.Pending != 0 {
			t.Errorf("summary.ByKind[hub_invite].Pending bumped by leaked row: got %d, want 0", got.Pending)
		}
	})

	t.Run("summary counts real rows and preserves zero fill", func(t *testing.T) {
		resetNotifications(t)
		createHubNotification(t, hubA, "review_contradiction")
		createHubNotification(t, hubA, "review_topic_merge")

		summary, err := s.GetNotificationSummary(ctx, NotificationSummaryOpts{
			UserID: userID,
			HubIDs: []string{hubA, hubB},
		})
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if got := summary.ByKind["review_contradiction"]; got.Pending != 1 {
			t.Errorf("review_contradiction pending: got %d, want 1", got.Pending)
		}
		if got := summary.ByKind["review_topic_merge"]; got.Pending != 1 {
			t.Errorf("review_topic_merge pending: got %d, want 1", got.Pending)
		}
		// Kinds without rows must still be present with zero counts.
		if got := summary.ByKind["dream_run_completed"]; got.Pending != 0 || got.Unseen != 0 {
			t.Errorf("dream_run_completed should be zero-filled; got %+v", got)
		}
		if summary.NeedsActionPending != 2 {
			t.Errorf("needs action pending: got %d, want 2", summary.NeedsActionPending)
		}
	})

	t.Run("expire sweep flips past-due rows and returns them", func(t *testing.T) {
		resetNotifications(t)
		// Seed three receipts:
		//   past       — pending, expires_at in the past   → must flip
		//   future     — pending, expires_at in the future → must stay
		//   already    — status already 'dismissed'        → must stay
		past := &model.Notification{
			ID:              uuidFor(t, "expire-past"),
			Audience:        model.AudienceUser,
			RecipientUserID: userID,
			Kind:            "dream_run_completed",
			Status:          model.NotificationStatusPending,
			SourceKind:      "dream_run",
			SourceID:        "expire-past",
			Payload:         json.RawMessage(`{}`),
			CreatedAt:       time.Now().Add(-48 * time.Hour),
		}
		pastExpires := time.Now().Add(-1 * time.Hour)
		past.ExpiresAt = &pastExpires
		if err := s.CreateNotification(past); err != nil {
			t.Fatalf("create past receipt: %v", err)
		}

		future := &model.Notification{
			ID:              uuidFor(t, "expire-future"),
			Audience:        model.AudienceUser,
			RecipientUserID: userID,
			Kind:            "dream_run_completed",
			Status:          model.NotificationStatusPending,
			SourceKind:      "dream_run",
			SourceID:        "expire-future",
			Payload:         json.RawMessage(`{}`),
			CreatedAt:       time.Now(),
		}
		futureExpires := time.Now().Add(24 * time.Hour)
		future.ExpiresAt = &futureExpires
		if err := s.CreateNotification(future); err != nil {
			t.Fatalf("create future receipt: %v", err)
		}

		already := &model.Notification{
			ID:              uuidFor(t, "expire-already"),
			Audience:        model.AudienceUser,
			RecipientUserID: userID,
			Kind:            "dream_run_completed",
			Status:          model.NotificationStatusDismissed,
			SourceKind:      "dream_run",
			SourceID:        "expire-already",
			Payload:         json.RawMessage(`{}`),
			CreatedAt:       time.Now().Add(-72 * time.Hour),
		}
		alreadyExpires := time.Now().Add(-24 * time.Hour)
		already.ExpiresAt = &alreadyExpires
		if err := s.CreateNotification(already); err != nil {
			t.Fatalf("create already-dismissed receipt: %v", err)
		}

		expired, err := s.ExpireNotifications(ctx, time.Now())
		if err != nil {
			t.Fatalf("expire: %v", err)
		}
		if len(expired) != 1 {
			t.Fatalf("expected exactly 1 expired row, got %d", len(expired))
		}
		if expired[0].ID != past.ID {
			t.Errorf("expired row id: got %s, want %s", expired[0].ID, past.ID)
		}
		if expired[0].Status != model.NotificationStatusExpired {
			t.Errorf("expired row status: got %s, want expired", expired[0].Status)
		}
		// Routing fields must be returned so the worker can publish
		// a notification.updated event to the right audience.
		if expired[0].RecipientUserID != userID {
			t.Errorf("expired row recipient: got %q, want %q", expired[0].RecipientUserID, userID)
		}

		// Future and already-dismissed rows must be untouched.
		futureRow, err := s.GetNotification(ctx, future.ID, userID, []string{hubA, hubB})
		if err != nil {
			t.Fatalf("get future: %v", err)
		}
		if futureRow.Status != model.NotificationStatusPending {
			t.Errorf("future row must stay pending, got %s", futureRow.Status)
		}
		alreadyRow, err := s.GetNotification(ctx, already.ID, userID, []string{hubA, hubB})
		if err != nil {
			t.Fatalf("get already: %v", err)
		}
		if alreadyRow.Status != model.NotificationStatusDismissed {
			t.Errorf("already-dismissed row must stay dismissed, got %s", alreadyRow.Status)
		}

		// Running the sweep again with the same `now` must be a
		// no-op — the expired row no longer matches the WHERE clause.
		second, err := s.ExpireNotifications(ctx, time.Now())
		if err != nil {
			t.Fatalf("second expire: %v", err)
		}
		if len(second) != 0 {
			t.Errorf("second sweep must be a no-op, got %d rows", len(second))
		}
	})
}

// uuidFor returns a deterministic v5-like UUID string derived from
// seed. Using a stable derivation keeps the test fixture identifiers
// predictable across reruns so cleanup DELETE statements find the
// right rows even if a prior run crashed mid-test.
func uuidFor(t *testing.T, seed string) string {
	t.Helper()
	// Pack the seed into the last 12 hex digits of a fixed prefix.
	// Simple, reversible, and unique enough for test fixtures.
	h := uint64(0)
	for _, c := range seed {
		h = h*131 + uint64(c)
	}
	return "00000000-0000-0000-0000-" + hex12(h)
}

func hex12(n uint64) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, 12)
	for i := 11; i >= 0; i-- {
		out[i] = alphabet[n&0xf]
		n >>= 4
	}
	return string(out)
}
