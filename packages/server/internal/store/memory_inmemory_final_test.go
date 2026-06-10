package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Final pass — effective plan, audiences, campaigns, remaining tx
// and hub-plan helpers.

func TestInMemoryEffectivePlanAndHubPlans(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_, _ = s.GetEffectivePlan(ctx, "u1")
	_ = s.SetEffectivePlan(ctx, "u1", "personal_free", "personal")
	_, _ = s.GetHubPlanID(ctx, "h1")
	_, _ = s.GetUserHubPlans(ctx, "u1")
}

func TestInMemoryAudiencesStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_ = s.CreateAudience(ctx, &model.Audience{ID: "a1"})
	_, _ = s.GetAudience(ctx, "a1")
	_, _ = s.ListAudiences(ctx)
	_ = s.UpdateAudience(ctx, &model.Audience{ID: "a1"})
	_ = s.DeleteAudience(ctx, "a1")
}

func TestInMemoryCampaignsStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_ = s.CreateCampaign(ctx, &model.Campaign{ID: "c1", Content: json.RawMessage(`{}`)})
	_, _ = s.GetCampaign(ctx, "c1")
	_, _, _ = s.ListCampaigns(ctx, model.CampaignListOpts{})
	_ = s.UpdateCampaignDraft(ctx, "c1", CampaignDraftPatch{})
	_ = s.DeleteCampaign(ctx, "c1", "u1")
	_ = s.ScheduleCampaign(ctx, "c1", time.Now().Add(time.Hour), "u1")
	_ = s.CancelCampaign(ctx, "c1", "u1")
	_ = s.ClaimCampaignForSend(ctx, "c1", "draft", time.Now(), "batch-1")
	_ = s.RevertCampaignToDraft(ctx, "c1", "sending")
	_ = s.FinalizeCampaignSend(ctx, "c1", 1, 0, time.Now(), "")
}

func TestInMemoryCreateNotificationTx(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	_ = s.WithTx(context.Background(), func(tx pgx.Tx) error {
		return s.CreateNotificationTx(context.Background(), tx, &model.Notification{ID: "n1"})
	})
}
