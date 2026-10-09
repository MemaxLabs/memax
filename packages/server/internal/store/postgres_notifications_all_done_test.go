package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// TryAutoResolveChecklist keeps the finished checklist PENDING: it
// stamps all_done_at, marks the row seen and caps expires_at at one
// day out (founder spec 2026-09-21 — the card shows its finished state
// for a day, then the expiry sweep retires it).
func TestPostgresTryAutoResolveChecklist_StaysPendingForADay(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'N', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@alldone",
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

	done := time.Now().UTC().Add(-time.Hour)
	weekOut := time.Now().UTC().Add(7 * 24 * time.Hour)
	payload, _ := json.Marshal(model.ChecklistPayload{
		Title: "first week",
		Items: []model.ChecklistItem{
			{Item: model.Item{ID: "welcome", Title: "welcome"}},
			{Item: model.Item{ID: "connect_agent", Title: "connect"}, CompletedAt: &done},
			{Item: model.Item{ID: "first_dream", Title: "dream"}, CompletedAt: &done},
		},
		RequiredIDs: []string{"connect_agent", "first_dream"},
	})
	n := &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		HubID:           hubID,
		RecipientUserID: userID,
		Kind:            "checklist",
		Status:          model.NotificationStatusPending,
		SourceKind:      "onboarding",
		SourceID:        uuid.NewString(),
		Payload:         payload,
		ExpiresAt:       &weekOut,
		CreatedAt:       time.Now().Truncate(time.Microsecond),
	}
	if err := s.CreateNotification(n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	flipped, post, err := s.TryAutoResolveChecklist(ctx, n.ID, userID, []string{hubID})
	if err != nil {
		t.Fatalf("TryAutoResolveChecklist: %v", err)
	}
	if !flipped {
		t.Fatal("expected flipped=true on first call")
	}
	if post.Status != model.NotificationStatusPending {
		t.Fatalf("row must stay pending, got %q", post.Status)
	}
	if post.SeenAt == nil {
		t.Fatal("expected seen_at to be stamped")
	}
	if post.ExpiresAt == nil || post.ExpiresAt.After(time.Now().Add(24*time.Hour+time.Minute)) {
		t.Fatalf("expected expires_at capped at ~1 day, got %v", post.ExpiresAt)
	}
	var got model.ChecklistPayload
	if err := json.Unmarshal(post.Payload, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if got.AllDoneAt == nil {
		t.Fatal("expected all_done_at in payload")
	}

	// Idempotent: the stamp is already there.
	flipped, _, err = s.TryAutoResolveChecklist(ctx, n.ID, userID, []string{hubID})
	if err != nil {
		t.Fatalf("second TryAutoResolveChecklist: %v", err)
	}
	if flipped {
		t.Fatal("expected flipped=false on second call")
	}

	// Cross-user: another user must not be able to stamp it.
	otherID := uuid.NewString()
	if _, _, err := s.TryAutoResolveChecklist(ctx, n.ID, otherID, nil); err == nil {
		t.Fatal("expected an error for a non-owner")
	}
}
