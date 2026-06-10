package planresolver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// mockResolverStore wraps InMemoryStore with overrides for testing.
type mockResolverStore struct {
	store.InMemoryStore

	user              *model.User
	hubPlanID         string
	hubPlans          []model.HubPlanRef
	effectivePlan     *model.EffectivePlan
	hubMembers        []model.HubMember
	planOverride      *model.PlanOverride
	ownedFreeTeamHubs int

	effectivePlanSet    bool
	effectivePlanSetVal string
}

func (m *mockResolverStore) CountOwnedFreeTeamHubs(_ context.Context, _ string) (int, error) {
	return m.ownedFreeTeamHubs, nil
}

func (m *mockResolverStore) GetUser(_ string) (*model.User, error) {
	if m.user == nil {
		return &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID}, nil
	}
	return m.user, nil
}

func (m *mockResolverStore) GetHubPlanID(_ context.Context, _ string) (string, error) {
	if m.hubPlanID == "" {
		return "", fmt.Errorf("not found")
	}
	return m.hubPlanID, nil
}

func (m *mockResolverStore) GetUserHubPlans(_ context.Context, _ string) ([]model.HubPlanRef, error) {
	return m.hubPlans, nil
}

func (m *mockResolverStore) GetEffectivePlan(_ context.Context, _ string) (*model.EffectivePlan, error) {
	if m.effectivePlan == nil {
		return nil, fmt.Errorf("not found")
	}
	return m.effectivePlan, nil
}

func (m *mockResolverStore) SetEffectivePlan(_ context.Context, _, planID, _ string) error {
	m.effectivePlanSet = true
	m.effectivePlanSetVal = planID
	return nil
}

func (m *mockResolverStore) ListHubMembers(_ string) ([]model.HubMember, error) {
	return m.hubMembers, nil
}

func (m *mockResolverStore) GetPlanOverride(_ context.Context, _ string) (*model.PlanOverride, error) {
	if m.planOverride == nil {
		return nil, fmt.Errorf("not found")
	}
	return m.planOverride, nil
}

func setupTestRegistry(t *testing.T, planDefs ...model.Plan) *plans.Registry {
	t.Helper()
	ms := &mockListPlansStore{plans: planDefs}
	reg := plans.NewRegistry(ms)
	if err := reg.Load(context.Background()); err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return reg
}

type mockListPlansStore struct {
	store.InMemoryStore
	plans []model.Plan
}

func (m *mockListPlansStore) ListPlans(_ context.Context) ([]model.Plan, error) {
	return m.plans, nil
}

// testPlans uses scoped plan IDs with EntitlementRank for cross-scope comparison.
var testPlans = []model.Plan{
	{ID: model.PersonalFreePlanID, Scope: model.PlanScopePersonal, DisplayName: "Free", TierOrder: 0, EntitlementRank: 0, Active: true, PushLimit: 200, RecallLimit: 500, AskLimit: 10, AskModel: "haiku", MemoryLimit: 300, MaxOwnedFreeTeamHubs: 0},
	{ID: model.PersonalEarlyAccessPlanID, Scope: model.PlanScopePersonal, DisplayName: "Early Access", TierOrder: 15, EntitlementRank: 15, Active: true, PushLimit: 2000, RecallLimit: -1, AskLimit: 150, AskModel: "sonnet", MemoryLimit: 10000, MaxOwnedFreeTeamHubs: 1},
	{ID: model.PersonalProPlanID, Scope: model.PlanScopePersonal, DisplayName: "Pro", TierOrder: 20, EntitlementRank: 20, Active: true, PushLimit: 1000, RecallLimit: -1, AskLimit: 100, AskModel: "haiku", MemoryLimit: 5000, MaxOwnedFreeTeamHubs: 3},
	{ID: model.HubFreeTeamPlanID, Scope: model.PlanScopeHub, DisplayName: "Free Team", TierOrder: 0, EntitlementRank: 10, Active: true, PushLimit: 500, RecallLimit: 2000, AskLimit: 50, AskModel: "haiku", MemoryLimit: 300, MaxHubMembers: intPtr(3)},
	{ID: model.HubTeamPlanID, Scope: model.PlanScopeHub, DisplayName: "Team", TierOrder: 40, EntitlementRank: 40, Active: true, PushLimit: -1, RecallLimit: -1, AskLimit: 200, AskModel: "sonnet", MemoryLimit: -1, DreamsEnabled: true, SeatMinimum: 3, SeatBilled: true},
}

func intPtr(v int) *int { return &v }

func TestResolveForRequest_PersonalHub_FreeUser(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{user: &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID}}
	r := New(nil, ms, reg)

	limits := r.ResolveForRequest(context.Background(), "user-1", "")
	if limits.PushLimit != 200 {
		t.Errorf("expected PushLimit 200, got %d", limits.PushLimit)
	}
	if limits.AskModel != "haiku" {
		t.Errorf("expected haiku, got %s", limits.AskModel)
	}
}

func TestResolveForRequest_TeamHub_FreeMember(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user:      &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID},
		hubPlanID: model.HubTeamPlanID,
		hubPlans:  []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubTeamPlanID}},
	}
	r := New(nil, ms, reg)

	limits := r.ResolveForRequest(context.Background(), "user-1", "hub-1")
	// Hub plan limits for push/recall/ask
	if limits.PushLimit != -1 {
		t.Errorf("expected PushLimit -1 (unlimited from hub_team), got %d", limits.PushLimit)
	}
	if limits.AskModel != "sonnet" {
		t.Errorf("expected sonnet, got %s", limits.AskModel)
	}
	if !limits.DreamsEnabled {
		t.Error("expected DreamsEnabled from hub plan")
	}
	// MemoryLimit stays personal — the effective personal plan is boosted to hub_team
	// because the user is a member of a team hub (entitlement_rank 40 > 0)
	if limits.MemoryLimit != -1 {
		t.Errorf("expected MemoryLimit -1 (team via personal boost), got %d", limits.MemoryLimit)
	}
}

func TestResolveForRequest_FreeHub_TeamMember(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		// User is on personal_pro (no team personal plan in our test set)
		user:      &model.User{Plan: "pro", PersonalPlanID: model.PersonalProPlanID},
		hubPlanID: "", // no subscription on this hub
	}
	r := New(nil, ms, reg)

	// Operating in a hub with no subscription — falls to personal plan
	limits := r.ResolveForRequest(context.Background(), "user-1", "hub-2")
	if limits.PushLimit != 1000 {
		t.Errorf("pro user should get pro limits personally, got PushLimit %d", limits.PushLimit)
	}
}

func TestResolveForRequest_PersonalBoost(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user:     &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID},
		hubPlans: []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubTeamPlanID}},
	}
	r := New(nil, ms, reg)

	// Personal hub — gets boosted to hub_team via max(personal_free rank=0, hub_team rank=40)
	limits := r.ResolveForRequest(context.Background(), "user-1", "")
	if limits.PlanID != model.HubTeamPlanID {
		t.Errorf("expected effective plan %s, got %s", model.HubTeamPlanID, limits.PlanID)
	}
	if limits.PushLimit != -1 {
		t.Errorf("expected unlimited push from team boost, got %d", limits.PushLimit)
	}
}

func TestComputeEffectivePlan_PersonalHigher(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		// User personal plan (Pro, rank 20) is higher than hub plan (hub_free_team, rank 10)
		user:     &model.User{Plan: "pro", PersonalPlanID: model.PersonalProPlanID},
		hubPlans: []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubFreeTeamPlanID}},
	}
	r := New(nil, ms, reg)

	planID, source := r.computeEffectivePlan(context.Background(), "user-1")
	if planID != model.PersonalProPlanID {
		t.Errorf("personal_pro (rank 20) should win over hub_free_team (rank 10), got %s", planID)
	}
	if source != "personal" {
		t.Errorf("source should be personal, got %s", source)
	}
}

func TestEffectivePlanCache_Freshness(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	stale := time.Now().Add(-45 * time.Minute) // older than 30-min limit
	ms := &mockResolverStore{
		user:     &model.User{Plan: "pro", PersonalPlanID: model.PersonalProPlanID},
		hubPlans: nil,
		effectivePlan: &model.EffectivePlan{
			PlanID:     model.PersonalFreePlanID, // stale value
			Source:     "personal",
			ComputedAt: stale,
		},
	}
	r := New(nil, ms, reg)

	planID := r.GetEffectivePlanID(context.Background(), "user-1")
	if planID != model.PersonalProPlanID {
		t.Errorf("stale cache should be bypassed, got %s instead of %s", planID, model.PersonalProPlanID)
	}
	if !ms.effectivePlanSet || ms.effectivePlanSetVal != model.PersonalProPlanID {
		t.Error("should have written recomputed plan to cache")
	}
}

// ─── Scoped Resolver Tests ──────────────────────────────────────────────────

func TestResolveReadEntitlements_MaxPersonalAndHub(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user:     &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID},
		hubPlans: []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubTeamPlanID}},
	}
	r := New(nil, ms, reg)

	ent := r.ResolveReadEntitlements(context.Background(), "user-1")
	if ent.Context != model.EntitlementRead {
		t.Errorf("expected context read, got %s", ent.Context)
	}
	// Free user + Team hub membership → boosted to hub_team (rank 40 > 0)
	if ent.Limits.PlanID != model.HubTeamPlanID {
		t.Errorf("expected plan %s, got %s", model.HubTeamPlanID, ent.Limits.PlanID)
	}
	if ent.Source.Reason != "best_hub" {
		t.Errorf("expected source reason best_hub, got %s", ent.Source.Reason)
	}
	if ent.Source.HubID != "hub-1" {
		t.Errorf("expected source hub_id hub-1, got %s", ent.Source.HubID)
	}
}

func TestResolveReadEntitlements_PersonalOnly(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user: &model.User{Plan: model.LegacyProPlanID, PersonalPlanID: model.PersonalProPlanID},
	}
	r := New(nil, ms, reg)

	ent := r.ResolveReadEntitlements(context.Background(), "user-1")
	if ent.Limits.PlanID != model.PersonalProPlanID {
		t.Errorf("expected plan %s, got %s", model.PersonalProPlanID, ent.Limits.PlanID)
	}
	if ent.Source.Reason != "personal" {
		t.Errorf("expected personal source, got %s", ent.Source.Reason)
	}
}

func TestResolvePersonalWriteEntitlements_SameAsRead(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user:     &model.User{Plan: model.LegacyFreePlanID, PersonalPlanID: model.PersonalFreePlanID},
		hubPlans: []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubTeamPlanID}},
	}
	r := New(nil, ms, reg)

	ent := r.ResolvePersonalWriteEntitlements(context.Background(), "user-1")
	if ent.Context != model.EntitlementPersonalWrite {
		t.Errorf("expected context personal_write, got %s", ent.Context)
	}
	// Same resolution as read — max(personal, best_hub)
	if ent.Limits.PlanID != model.HubTeamPlanID {
		t.Errorf("expected boosted plan %s, got %s", model.HubTeamPlanID, ent.Limits.PlanID)
	}
}

func TestResolveHubWriteEntitlements_UsesTargetHub(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		// Pro+ user writing into a free team hub
		user:      &model.User{Plan: model.LegacyProPlanID, PersonalPlanID: model.PersonalProPlanID},
		hubPlanID: model.HubFreeTeamPlanID,
		hubPlans:  []model.HubPlanRef{{HubID: "hub-1", PlanID: model.HubFreeTeamPlanID}},
	}
	r := New(nil, ms, reg)

	ent := r.ResolveHubWriteEntitlements(context.Background(), "user-1", "hub-1")
	if ent.Context != model.EntitlementHubWrite {
		t.Errorf("expected context hub_write, got %s", ent.Context)
	}
	// Even though user is Pro, the hub write uses the TARGET hub's plan
	if ent.Limits.PlanID != model.HubFreeTeamPlanID {
		t.Errorf("expected hub_free_team limits, got %s", ent.Limits.PlanID)
	}
	if ent.Limits.PushLimit != 500 {
		t.Errorf("expected hub_free_team push_limit 500, got %d", ent.Limits.PushLimit)
	}
	// MemoryLimit should come from user's effective personal plan (pro)
	if ent.Limits.MemoryLimit != 5000 {
		t.Errorf("expected personal memory_limit 5000, got %d", ent.Limits.MemoryLimit)
	}
	if ent.Source.Reason != "target_hub" {
		t.Errorf("expected target_hub source, got %s", ent.Source.Reason)
	}
}

func TestResolveHubWriteEntitlements_NoSubscription_FallsToHubFreeTeam(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		user:      &model.User{Plan: model.LegacyProPlanID, PersonalPlanID: model.PersonalProPlanID},
		hubPlanID: "", // no subscription
	}
	r := New(nil, ms, reg)

	ent := r.ResolveHubWriteEntitlements(context.Background(), "user-1", "hub-no-sub")
	// Falls back to hub_free_team (NOT personal entitlements — that would
	// violate the "writes use target hub plan" rule)
	if ent.Limits.PlanID != model.HubFreeTeamPlanID {
		t.Errorf("expected fallback to hub_free_team, got %s", ent.Limits.PlanID)
	}
	if ent.Limits.PushLimit != 500 {
		t.Errorf("expected hub_free_team push_limit 500, got %d", ent.Limits.PushLimit)
	}
	// MemoryLimit should still come from the user's personal plan
	if ent.Limits.MemoryLimit != 5000 {
		t.Errorf("expected personal memory_limit 5000, got %d", ent.Limits.MemoryLimit)
	}
}

func TestResolveHubManagementEntitlements(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	ms := &mockResolverStore{
		hubPlanID: model.HubTeamPlanID,
	}
	r := New(nil, ms, reg)

	ent := r.ResolveHubManagementEntitlements(context.Background(), "hub-1")
	if ent.Context != model.EntitlementHubManage {
		t.Errorf("expected context hub_manage, got %s", ent.Context)
	}
	if ent.Limits.PlanID != model.HubTeamPlanID {
		t.Errorf("expected hub_team limits, got %s", ent.Limits.PlanID)
	}
	if !ent.Limits.DreamsEnabled {
		t.Error("expected DreamsEnabled from hub_team plan")
	}
}

func TestResolveOwnershipEntitlements_UnderCap(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	// personal_pro has max_owned_free_team_hubs = 3
	ms := &mockResolverStore{
		user:              &model.User{Plan: model.LegacyProPlanID, PersonalPlanID: model.PersonalProPlanID},
		ownedFreeTeamHubs: 1,
	}
	r := New(nil, ms, reg)

	ent, err := r.ResolveOwnershipEntitlements(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ent.MaxOwnedFreeTeamHubs != 3 {
		t.Errorf("expected max 3, got %d", ent.MaxOwnedFreeTeamHubs)
	}
	if ent.CurrentOwnedCount != 1 {
		t.Errorf("expected current 1, got %d", ent.CurrentOwnedCount)
	}
	if !ent.CanCreateFreeTeamHub {
		t.Error("should be allowed to create (1 < 3)")
	}
}

func TestResolveOwnershipEntitlements_AtCap(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	// personal_early_access has max_owned_free_team_hubs = 1
	ms := &mockResolverStore{
		user:              &model.User{PersonalPlanID: model.PersonalEarlyAccessPlanID},
		ownedFreeTeamHubs: 1,
	}
	r := New(nil, ms, reg)

	ent, err := r.ResolveOwnershipEntitlements(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ent.CanCreateFreeTeamHub {
		t.Error("should NOT be allowed to create (1 >= 1)")
	}
}

func TestResolveOwnershipEntitlements_FreeUserCannotCreate(t *testing.T) {
	reg := setupTestRegistry(t, testPlans...)
	// personal_free has max_owned_free_team_hubs = 0
	ms := &mockResolverStore{
		user:              &model.User{PersonalPlanID: model.PersonalFreePlanID},
		ownedFreeTeamHubs: 0,
	}
	r := New(nil, ms, reg)

	ent, err := r.ResolveOwnershipEntitlements(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ent.CanCreateFreeTeamHub {
		t.Error("free user should NOT be allowed to create (0 < 0 is false)")
	}
	if ent.MaxOwnedFreeTeamHubs != 0 {
		t.Errorf("expected max 0, got %d", ent.MaxOwnedFreeTeamHubs)
	}
}
