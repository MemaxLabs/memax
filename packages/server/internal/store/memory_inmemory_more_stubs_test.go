package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Final batch of InMemoryStore stub coverage — connected agents,
// plan lookups, usage sync.

func TestInMemoryConnectedAgents(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	_ = s.UpsertConnectedAgent(&model.ConnectedAgent{OwnerID: "u1", AgentName: "cc"})
	_, _ = s.GetConnectedAgent("u1", "cc")
	_, _ = s.ListConnectedAgentsWithStats("u1")
	_ = s.UpdateConnectedAgent(&model.ConnectedAgent{OwnerID: "u1", AgentName: "cc"})
	_ = s.DeleteConnectedAgent("u1", "cc")
	_, _ = s.CountAgentApiKeys("u1", "cc")
	_, _ = s.CountAgentConfigs("u1", "cc")
	_, _ = s.HealAPIKeyAgent(context.Background(), "key-1", "cc", "u1")
}

func TestInMemoryPlansAndOverrides(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_, _ = s.ListPlans(ctx)
	_, _ = s.ListPlansByScope(ctx, model.PlanScopePersonal)
	_, _ = s.GetPlan(ctx, "personal_free")
	_ = s.UpdatePlan(ctx, &model.Plan{ID: "personal_free"})
	_, _ = s.GetPlanOverride(ctx, "u1")
	_ = s.UpsertPlanOverride(ctx, &model.PlanOverride{UserID: "u1"})
	_ = s.DeletePlanOverride(ctx, "u1")
	_, _ = s.CountOwnedFreeTeamHubs(ctx, "u1")
}

func TestInMemoryUsageAndBilling(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	_, _ = s.ListUsageUserIDs(ctx, time.Now().Add(-30*24*time.Hour))
	_ = s.UpsertBillingSubscription(ctx, &model.BillingSubscription{})
	_ = s.InsertUsageEvent(ctx, &model.UsageEvent{UserID: "u1", Operation: "push"})
	_ = s.SetUsageCounters(ctx, "u1", time.Now(), time.Now().Add(time.Hour), 1, 2, 3)
}
