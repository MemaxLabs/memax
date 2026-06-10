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

// TestNotificationDreamRunIDRoundTrip locks in migration 017: a
// dream-produced notification that sets DreamRunID persists the
// column, and scan paths surface it back. Admin queries like
// "all rows from run X" depend on this pairing.
//
// Non-dream notifications (hub invites, etc.) leave DreamRunID nil
// and the column stays NULL — no spurious tagging of unrelated rows.
func TestNotificationDreamRunIDRoundTrip(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping notification dream_run_id round-trip test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00dddd0001"
	hubID := "00000000-0000-0000-0000-ff00eeee0001"
	runID := "00000000-0000-0000-0000-ff00ffff0001"
	now := time.Now().UTC()

	// Seed fixtures.
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "dream-run-id@test.local", "dream-run-id@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, $5, $5) ON CONFLICT (id) DO NOTHING`,
		hubID, "dream-run-id-hub", "dream-run-id-hub", ownerID, now,
	)
	if err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	// Clean up from prior runs so uniqueness doesn't bite.
	if _, err := pool.Exec(ctx,
		`DELETE FROM notifications WHERE source_id IN ($1, $2)`,
		"dream-run-id-test-with", "dream-run-id-test-without"); err != nil {
		t.Fatalf("cleanup notifications: %v", err)
	}

	// Create a dream-produced notification with DreamRunID set.
	payload, _ := json.Marshal(map[string]any{"body": "test"})
	tagged := &model.Notification{
		ID:              "00000000-0000-0000-0000-ff00bbbb0101",
		Audience:        model.AudienceUser,
		RecipientUserID: ownerID,
		HubID:           hubID,
		Kind:            model.NotificationKindDreamRunCompleted,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceDreamRun,
		SourceID:        "dream-run-id-test-with",
		DreamRunID:      &runID,
		Payload:         payload,
		CreatedAt:       now,
	}
	if err := s.CreateNotification(tagged); err != nil {
		t.Fatalf("CreateNotification (tagged): %v", err)
	}

	// Create a non-dream notification with DreamRunID nil.
	untagged := &model.Notification{
		ID:              "00000000-0000-0000-0000-ff00bbbb0102",
		Audience:        model.AudienceUser,
		RecipientUserID: ownerID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubInvite,
		Status:          model.NotificationStatusPending,
		SourceKind:      "hub_invite",
		SourceID:        "dream-run-id-test-without",
		Payload:         payload,
		CreatedAt:       now,
	}
	if err := s.CreateNotification(untagged); err != nil {
		t.Fatalf("CreateNotification (untagged): %v", err)
	}

	// Read both back via the admin-style raw query to prove the
	// column is stored + scannable. Using raw SQL rather than the
	// list helpers because the tagged path is the interesting
	// assertion.
	var taggedRunID, untaggedRunID *string
	err = pool.QueryRow(ctx,
		`SELECT dream_run_id::text FROM notifications WHERE source_id = $1`,
		"dream-run-id-test-with").Scan(&taggedRunID)
	if err != nil {
		t.Fatalf("read tagged: %v", err)
	}
	if taggedRunID == nil || *taggedRunID != runID {
		t.Fatalf("tagged notification dream_run_id = %v, want %s", taggedRunID, runID)
	}

	err = pool.QueryRow(ctx,
		`SELECT dream_run_id::text FROM notifications WHERE source_id = $1`,
		"dream-run-id-test-without").Scan(&untaggedRunID)
	if err != nil {
		t.Fatalf("read untagged: %v", err)
	}
	if untaggedRunID != nil {
		t.Fatalf("untagged notification dream_run_id = %v, want nil", *untaggedRunID)
	}

	// Prove the full read path (notificationCols + scanNotification)
	// surfaces DreamRunID — raw SQL reads bypass those helpers, so
	// this is the only thing guarding SDK/web consumers from a nil
	// DreamRunID. ListNotificationsForUser is the canonical read
	// path used by GET /v1/notifications.
	notifs, _, err := s.ListNotificationsForUser(ctx, NotificationListOpts{
		UserID: ownerID,
		HubIDs: []string{hubID},
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListNotificationsForUser: %v", err)
	}
	var sawTagged, sawUntagged bool
	for _, n := range notifs {
		switch n.SourceID {
		case "dream-run-id-test-with":
			if n.DreamRunID == nil || *n.DreamRunID != runID {
				t.Fatalf("list path lost DreamRunID: %+v", n.DreamRunID)
			}
			sawTagged = true
		case "dream-run-id-test-without":
			if n.DreamRunID != nil {
				t.Fatalf("list path set DreamRunID on untagged row: %+v", n.DreamRunID)
			}
			sawUntagged = true
		}
	}
	if !sawTagged || !sawUntagged {
		t.Fatalf("expected both rows from ListNotificationsForUser, tagged=%v untagged=%v", sawTagged, sawUntagged)
	}

	// Prove the "first run wins" upsert contract: a second
	// CreateNotification with the same (source_kind, source_id) but
	// a DIFFERENT dream_run_id must NOT overwrite the stored
	// dream_run_id. Critical for idempotent producers — if run B
	// re-detects the same contradiction pair that run A already
	// wrote, the audit trail should credit run A.
	overwriteAttempt := "00000000-0000-0000-0000-ff00ffff0002"
	upsert := &model.Notification{
		ID:              "00000000-0000-0000-0000-ff00bbbb0103",
		Audience:        model.AudienceUser,
		RecipientUserID: ownerID,
		HubID:           hubID,
		Kind:            model.NotificationKindDreamRunCompleted,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceDreamRun,
		SourceID:        "dream-run-id-test-with",
		DreamRunID:      &overwriteAttempt,
		Payload:         payload,
		CreatedAt:       now,
	}
	if err := s.CreateNotification(upsert); err != nil {
		t.Fatalf("upsert CreateNotification: %v", err)
	}
	var afterUpsert *string
	if err := pool.QueryRow(ctx,
		`SELECT dream_run_id::text FROM notifications WHERE source_id = $1`,
		"dream-run-id-test-with").Scan(&afterUpsert); err != nil {
		t.Fatalf("read after upsert: %v", err)
	}
	if afterUpsert == nil || *afterUpsert != runID {
		t.Fatalf("upsert overwrote dream_run_id: got %v, want %s (first-run-wins contract)", afterUpsert, runID)
	}

	// Cleanup.
	if _, err := pool.Exec(ctx,
		`DELETE FROM notifications WHERE source_id IN ($1, $2)`,
		"dream-run-id-test-with", "dream-run-id-test-without"); err != nil {
		t.Logf("cleanup notifications: %v", err)
	}
}
