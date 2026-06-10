package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresNotificationCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Seed user + personal hub.
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'notif@t', 'N', 'personal_early_access', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "H", Slug: "h-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// --- CreateNotification (user audience) ---
	payload, _ := json.Marshal(map[string]string{"msg": "hi"})
	n := &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		HubID:           hubID,
		RecipientUserID: userID,
		Kind:            "dream_run_completed",
		Status:          model.NotificationStatusPending,
		SourceKind:      "dream_run",
		SourceID:        uuid.NewString(),
		Priority:        5,
		Payload:         payload,
		CreatedAt:       time.Now().Truncate(time.Microsecond),
	}
	if err := s.CreateNotification(n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	// --- GetNotification ---
	got, err := s.GetNotification(ctx, n.ID, userID, []string{hubID})
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got.Kind != "dream_run_completed" {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.RecipientUserID != userID {
		t.Errorf("RecipientUserID = %q", got.RecipientUserID)
	}

	// --- Cross-user isolation: a different userID must not see
	// a user-audience row addressed to someone else.
	other := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'notif-other@t', 'O', 'personal_free', now(), now())`,
		other,
	); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	if got, _ := s.GetNotification(ctx, n.ID, other, nil); got != nil {
		t.Error("user-audience notification leaked to a non-recipient")
	}

	// --- MarkNotificationSeen ---
	if err := s.MarkNotificationSeen(ctx, n.ID, userID, []string{hubID}); err != nil {
		t.Fatalf("MarkNotificationSeen: %v", err)
	}
	got, _ = s.GetNotification(ctx, n.ID, userID, []string{hubID})
	if got.SeenAt == nil {
		t.Error("SeenAt not set after MarkNotificationSeen")
	}

	// --- DismissNotification ---
	if err := s.DismissNotification(ctx, n.ID, userID, []string{hubID}); err != nil {
		t.Fatalf("DismissNotification: %v", err)
	}
	got, _ = s.GetNotification(ctx, n.ID, userID, []string{hubID})
	if got.Status != model.NotificationStatusDismissed {
		t.Errorf("Status after dismiss = %q, want dismissed", got.Status)
	}
}

func TestPostgresCreateNotificationIfAbsentIdempotent(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'dedup@t', 'D', 'personal_early_access', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "H", Slug: "dedup-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// The shared SourceID + SourceKind is what makes the second
	// call dedup — a production regression here would create
	// duplicate inbox rows on retried dream cycles.
	sourceID := uuid.NewString()
	make := func() *model.Notification {
		return &model.Notification{
			ID:              uuid.NewString(),
			Audience:        model.AudienceUser,
			HubID:           hubID,
			RecipientUserID: userID,
			Kind:            "review_contradiction",
			Status:          model.NotificationStatusPending,
			SourceKind:      "contradiction",
			SourceID:        sourceID,
			CreatedAt:       time.Now().Truncate(time.Microsecond),
		}
	}

	created1, err := s.CreateNotificationIfAbsent(make())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !created1 {
		t.Error("first call should return created=true")
	}

	created2, err := s.CreateNotificationIfAbsent(make())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created2 {
		t.Error("second call (same source) should return created=false — dedup invariant")
	}
}

func TestPostgresListNotificationsForUserPagination(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	hubID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, 'listn@t', 'L', 'personal_early_access', now(), now())`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "H", Slug: "listn-" + userID[:8],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Seed a small pile of rows with monotonically increasing
	// created_at so the cursor order is predictable.
	base := time.Now().Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		n := &model.Notification{
			ID:              uuid.NewString(),
			Audience:        model.AudienceUser,
			HubID:           hubID,
			RecipientUserID: userID,
			Kind:            "dream_run_completed",
			Status:          model.NotificationStatusPending,
			SourceKind:      "dream_run",
			SourceID:        uuid.NewString(),
			CreatedAt:       base.Add(time.Duration(i) * time.Second),
		}
		if err := s.CreateNotification(n); err != nil {
			t.Fatalf("seed notif %d: %v", i, err)
		}
	}

	first, nextCursor, err := s.ListNotificationsForUser(ctx, store.NotificationListOpts{
		UserID: userID,
		HubIDs: []string{hubID},
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(first) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(first))
	}
	if nextCursor == "" {
		t.Error("page 1 should produce a next_cursor for more rows")
	}

	// Walk the next page via cursor.
	second, _, err := s.ListNotificationsForUser(ctx, store.NotificationListOpts{
		UserID: userID,
		HubIDs: []string{hubID},
		Limit:  2,
		Cursor: nextCursor,
	})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	// Union of both pages shouldn't overlap.
	seen := make(map[string]bool, 4)
	for _, n := range append(first, second...) {
		if seen[n.ID] {
			t.Errorf("cursor paging returned duplicate %q", n.ID)
		}
		seen[n.ID] = true
	}
}
