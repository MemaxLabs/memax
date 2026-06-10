package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupCampaignFixture seeds a user and returns store + userID. The
// user FKs into campaigns.created_by so every test needs one.
func setupCampaignFixture(t *testing.T) (store.Store, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@camp",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return s, userID
}

func TestPostgresCampaignCRUDAndStateMachine(t *testing.T) {
	t.Parallel()
	s, userID := setupCampaignFixture(t)
	ctx := context.Background()

	snap := &model.AudienceSnapshot{
		RuleType: "static",
		Rule:     model.AudienceRule{},
	}
	c := &model.Campaign{
		ID:               uuid.NewString(),
		Name:             "Launch",
		Channel:          "email",
		Kind:             model.CampaignKindSystemNotice,
		Status:           model.CampaignStatusDraft,
		AudienceSnapshot: snap,
		Content:          json.RawMessage(`{"subject":"Hello","body":"<p>test</p>"}`),
		CreatedBy:        userID,
	}

	// --- CreateCampaign ---
	if err := s.CreateCampaign(ctx, c); err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}

	// --- GetCampaign round-trips fields correctly.
	got, err := s.GetCampaign(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetCampaign: %v", err)
	}
	if got.Name != "Launch" {
		t.Errorf("name lost: %q", got.Name)
	}
	if got.Status != model.CampaignStatusDraft {
		t.Errorf("status = %q, want draft", got.Status)
	}

	// --- Unknown ID returns ErrCampaignNotFound.
	if _, err := s.GetCampaign(ctx, uuid.NewString()); !errors.Is(err, store.ErrCampaignNotFound) {
		t.Errorf("unknown ID: got %v, want ErrCampaignNotFound", err)
	}

	// --- ListCampaigns returns our draft.
	list, cursor, err := s.ListCampaigns(ctx, model.CampaignListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListCampaigns: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListCampaigns returned %d, want 1", len(list))
	}
	_ = cursor

	// --- ListCampaigns with status filter matches draft.
	byStatus, _, _ := s.ListCampaigns(ctx, model.CampaignListOpts{
		Status: model.CampaignStatusDraft, Limit: 10,
	})
	if len(byStatus) != 1 {
		t.Errorf("status filter returned %d, want 1", len(byStatus))
	}

	// --- UpdateCampaignDraft ---
	newName := "Launch v2"
	newContent := json.RawMessage(`{"subject":"Hello v2","body":"<p>v2</p>"}`)
	if err := s.UpdateCampaignDraft(ctx, c.ID, store.CampaignDraftPatch{
		Name:      &newName,
		Content:   newContent,
		UpdatedBy: userID,
	}); err != nil {
		t.Fatalf("UpdateCampaignDraft: %v", err)
	}
	got, _ = s.GetCampaign(ctx, c.ID)
	if got.Name != newName {
		t.Errorf("update didn't persist name: %q", got.Name)
	}

	// --- ScheduleCampaign: draft → scheduled.
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if err := s.ScheduleCampaign(ctx, c.ID, at, userID); err != nil {
		t.Fatalf("ScheduleCampaign: %v", err)
	}
	got, _ = s.GetCampaign(ctx, c.ID)
	if got.Status != model.CampaignStatusScheduled {
		t.Errorf("after schedule, status = %q", got.Status)
	}

	// --- UpdateCampaignDraft on non-draft must reject with
	// ErrCampaignStateMismatch (state-machine guard).
	if err := s.UpdateCampaignDraft(ctx, c.ID, store.CampaignDraftPatch{
		Name: &newName, UpdatedBy: userID,
	}); !errors.Is(err, store.ErrCampaignStateMismatch) {
		t.Errorf("update after schedule: got %v, want ErrCampaignStateMismatch", err)
	}

	// --- CancelCampaign: scheduled → cancelled ---
	if err := s.CancelCampaign(ctx, c.ID, userID); err != nil {
		t.Fatalf("CancelCampaign: %v", err)
	}
	got, _ = s.GetCampaign(ctx, c.ID)
	if got.Status != model.CampaignStatusCancelled {
		t.Errorf("after cancel, status = %q", got.Status)
	}
	if got.CancelledBy == nil || *got.CancelledBy != userID {
		t.Errorf("cancelled_by not set: %v", got.CancelledBy)
	}

	// --- CancelCampaign again: already cancelled, but the UPDATE's
	// WHERE clause filters draft/scheduled, so zero rows affected →
	// stateOrNotFound decides. Row still exists → ErrCampaignStateMismatch.
	if err := s.CancelCampaign(ctx, c.ID, userID); !errors.Is(err, store.ErrCampaignStateMismatch) {
		t.Errorf("re-cancel: got %v, want ErrCampaignStateMismatch", err)
	}

	// --- DeleteCampaign on cancelled is allowed.
	if err := s.DeleteCampaign(ctx, c.ID, userID); err != nil {
		t.Fatalf("DeleteCampaign: %v", err)
	}
	if _, err := s.GetCampaign(ctx, c.ID); !errors.Is(err, store.ErrCampaignNotFound) {
		t.Errorf("after delete, got %v, want ErrCampaignNotFound", err)
	}
	// Delete of missing ID → ErrCampaignNotFound.
	if err := s.DeleteCampaign(ctx, c.ID, userID); !errors.Is(err, store.ErrCampaignNotFound) {
		t.Errorf("delete missing: got %v, want ErrCampaignNotFound", err)
	}
}

func TestPostgresCampaignClaimAndRevert(t *testing.T) {
	t.Parallel()
	s, userID := setupCampaignFixture(t)
	ctx := context.Background()

	c := &model.Campaign{
		ID:        uuid.NewString(),
		Name:      "Claim",
		Channel:   "email",
		Kind:      model.CampaignKindSystemNotice,
		Status:    model.CampaignStatusDraft,
		Content:   json.RawMessage(`{}`),
		CreatedBy: userID,
	}
	if err := s.CreateCampaign(ctx, c); err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}

	// ClaimCampaignForSend expects status=draft, flips to sending.
	batchID := "batch-" + uuid.NewString()[:8]
	if err := s.ClaimCampaignForSend(ctx, c.ID, model.CampaignStatusDraft,
		time.Now().UTC(), batchID); err != nil {
		t.Fatalf("ClaimCampaignForSend: %v", err)
	}
	got, _ := s.GetCampaign(ctx, c.ID)
	if got.Status != model.CampaignStatusSending {
		t.Errorf("after claim, status = %q", got.Status)
	}
	if got.BatchID == nil || *got.BatchID != batchID {
		t.Errorf("batch_id not set: %v", got.BatchID)
	}

	// Double-claim from the wrong expected status fails.
	if err := s.ClaimCampaignForSend(ctx, c.ID, model.CampaignStatusDraft,
		time.Now().UTC(), "ignored"); !errors.Is(err, store.ErrCampaignStateMismatch) {
		t.Errorf("double-claim: got %v, want ErrCampaignStateMismatch", err)
	}

	// RevertCampaignToDraft pulls it back out of sending.
	if err := s.RevertCampaignToDraft(ctx, c.ID, model.CampaignStatusSending); err != nil {
		t.Fatalf("RevertCampaignToDraft: %v", err)
	}
	got, _ = s.GetCampaign(ctx, c.ID)
	if got.Status != model.CampaignStatusDraft {
		t.Errorf("after revert, status = %q", got.Status)
	}
	if got.BatchID != nil {
		t.Errorf("batch_id should be cleared on revert, got %v", got.BatchID)
	}
}
