package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresCommsAuditAppendAndList(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Seed actor + resource (campaign) for FK integrity.
	actorID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'A', 'personal_early_access', now(), now())`,
		actorID, actorID[:8]+"@comms",
	); err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	campaignID := uuid.NewString()
	c := &model.Campaign{
		ID:        campaignID,
		Name:      "Audit target",
		Channel:   "email",
		Kind:      model.CampaignKindSystemNotice,
		Status:    model.CampaignStatusDraft,
		Content:   json.RawMessage(`{}`),
		CreatedBy: actorID,
	}
	if err := s.CreateCampaign(ctx, c); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}

	// --- Append two audit entries — one with metadata, one without.
	// The second tests the "empty metadata coerced to {}" branch.
	entries := []model.CommsAuditEntry{
		{
			ID:           uuid.NewString(),
			ResourceType: "campaign",
			ResourceID:   campaignID,
			Action:       "created",
			ActorID:      &actorID,
			Metadata:     json.RawMessage(`{"draft":true}`),
		},
		{
			ID:           uuid.NewString(),
			ResourceType: "campaign",
			ResourceID:   campaignID,
			Action:       "published",
			ActorID:      &actorID,
			// Leave Metadata empty — AppendCommsAudit should default to {}
		},
	}
	for i := range entries {
		if err := s.AppendCommsAudit(ctx, &entries[i]); err != nil {
			t.Fatalf("AppendCommsAudit[%d]: %v", i, err)
		}
		// created_at is set by the DB; the caller's CreatedAt field is
		// untouched by AppendCommsAudit (not a RETURNING contract).
	}

	// --- ListCommsAudit returns them newest-first.
	got, err := s.ListCommsAudit(ctx, "campaign", campaignID)
	if err != nil {
		t.Fatalf("ListCommsAudit: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(got))
	}
	// published came after created → should sort first (DESC by created_at).
	// Both rows land in the same transaction microsecond; order-by-time is
	// non-deterministic for same-instant inserts, so we only check that
	// both expected actions are present.
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.Action] = true
	}
	if !seen["created"] || !seen["published"] {
		t.Errorf("missing actions: %+v", seen)
	}

	// --- Filter leak check: unrelated resource returns empty.
	other, _ := s.ListCommsAudit(ctx, "campaign", uuid.NewString())
	if len(other) != 0 {
		t.Errorf("cross-resource list returned %d rows, want 0", len(other))
	}

	// Guard: metadata-less row shouldn't have scanned a nil RawMessage.
	for _, e := range got {
		if len(e.Metadata) == 0 {
			t.Errorf("metadata should be {} at minimum, got empty for %s", e.Action)
		}
	}

}
