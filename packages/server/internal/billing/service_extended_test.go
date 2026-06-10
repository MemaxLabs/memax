package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// extMockStore is the richer fake used by this extended suite. It
// tracks every store method the billing Service touches so tests
// can assert "was this called? with what?", and exposes error-
// injection knobs (via fields ending in Err) so we can drive each
// non-fatal vs. fatal branch explicitly.
//
// Kept separate from the original mockStore in service_test.go to
// avoid breaking those tests if we tweak its API — the two can
// coexist.
type extMockStore struct {
	*store.InMemoryStore

	// Plan set — ListPlans returns this slice verbatim.
	plans []model.Plan

	// UpdateUserPlan
	updateUserPlanCalled bool
	updateUserPlanUserID string
	updateUserPlanPlan   string
	updateUserPlanErr    error

	// UpsertBillingSubscription
	upsertBillingSubCalled bool
	upsertBillingSubErr    error

	// InsertUsageEvent
	usageEvents    []*model.UsageEvent
	insertUsageErr error

	// GetUser
	users map[string]*model.User

	// Hub support
	hubs                           map[string]*model.Hub
	hubMemberCount                 int
	countHubMembersErr             error
	activeHubSubs                  map[string]*model.HubSubscription
	getActiveHubSubErr             error
	createHubSubCalled             bool
	createHubSubErr                error
	updateHubSubCalled             bool
	updateHubSubErr                error
	hubSubs                        []model.HubSubscription
	listHubSubsByBillingUserCalled bool

	// Memory count / clear-over-limit
	memoryCountByHub      int
	countMemoriesByHubErr error
	clearHubOverLimitOK   bool
	clearHubOverLimitErr  error

	// Reset
	resetUsageErr error
}

func newExtMock() *extMockStore {
	return &extMockStore{
		InMemoryStore: store.NewInMemoryStore(),
		users:         map[string]*model.User{},
		hubs:          map[string]*model.Hub{},
		activeHubSubs: map[string]*model.HubSubscription{},
	}
}

func (m *extMockStore) ListPlans(_ context.Context) ([]model.Plan, error) {
	return m.plans, nil
}

func (m *extMockStore) UpdateUserPlan(_ context.Context, userID, planID string) error {
	m.updateUserPlanCalled = true
	m.updateUserPlanUserID = userID
	m.updateUserPlanPlan = planID
	return m.updateUserPlanErr
}

func (m *extMockStore) UpsertBillingSubscription(_ context.Context, _ *model.BillingSubscription) error {
	m.upsertBillingSubCalled = true
	return m.upsertBillingSubErr
}

func (m *extMockStore) InsertUsageEvent(_ context.Context, event *model.UsageEvent) error {
	m.usageEvents = append(m.usageEvents, event)
	return m.insertUsageErr
}

func (m *extMockStore) GetUser(userID string) (*model.User, error) {
	if u, ok := m.users[userID]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (m *extMockStore) GetHub(hubID string) (*model.Hub, error) {
	if h, ok := m.hubs[hubID]; ok {
		return h, nil
	}
	return nil, errors.New("not found")
}

func (m *extMockStore) CountHubMembers(_ string) (int, error) {
	if m.countHubMembersErr != nil {
		return 0, m.countHubMembersErr
	}
	return m.hubMemberCount, nil
}

func (m *extMockStore) GetActiveHubSubscription(_ context.Context, hubID string) (*model.HubSubscription, error) {
	if m.getActiveHubSubErr != nil {
		return nil, m.getActiveHubSubErr
	}
	return m.activeHubSubs[hubID], nil
}

func (m *extMockStore) CreateHubSubscription(_ context.Context, sub *model.HubSubscription) error {
	m.createHubSubCalled = true
	if m.createHubSubErr != nil {
		return m.createHubSubErr
	}
	m.activeHubSubs[sub.HubID] = sub
	return nil
}

func (m *extMockStore) UpdateHubSubscription(_ context.Context, sub *model.HubSubscription) error {
	m.updateHubSubCalled = true
	if m.updateHubSubErr != nil {
		return m.updateHubSubErr
	}
	m.activeHubSubs[sub.HubID] = sub
	return nil
}

func (m *extMockStore) ListHubSubscriptionsByBillingUser(_ context.Context, _ string) ([]model.HubSubscription, error) {
	m.listHubSubsByBillingUserCalled = true
	return m.hubSubs, nil
}

func (m *extMockStore) CountMemoriesByHub(_ context.Context, _ string) (int, error) {
	return m.memoryCountByHub, m.countMemoriesByHubErr
}

func (m *extMockStore) ClearHubOverLimit(_ context.Context, _ string) (bool, error) {
	return m.clearHubOverLimitOK, m.clearHubOverLimitErr
}

func (m *extMockStore) ResetUsageCounters(_ context.Context, _ string, _, _ time.Time) error {
	return m.resetUsageErr
}

// buildService stands up a Service with a registry pre-loaded from
// the given plans. Keeps every test's setup tiny: one call produces
// a working Service + mock pair ready for the specific scenario.
func buildService(t *testing.T, plansFixture []model.Plan) (*Service, *extMockStore) {
	t.Helper()
	ms := newExtMock()
	ms.plans = plansFixture
	registry := plans.NewRegistry(ms)
	if err := registry.Load(context.Background()); err != nil {
		t.Fatalf("registry.Load: %v", err)
	}
	return NewService(ms, registry), ms
}

func personalFreePlan() model.Plan {
	return model.Plan{
		ID:          model.PersonalFreePlanID,
		Scope:       model.PlanScopePersonal,
		DisplayName: "Free",
		MemoryLimit: 300,
		AskLimit:    10,
		Active:      true,
	}
}

func personalEarlyAccessPlan() model.Plan {
	return model.Plan{
		ID:          "personal_early_access",
		Scope:       model.PlanScopePersonal,
		DisplayName: "Early Access",
		MemoryLimit: 10000,
		AskLimit:    150,
		Active:      true,
	}
}

func hubTeamPlan() model.Plan {
	return model.Plan{
		ID:          "hub_team",
		Scope:       model.PlanScopeHub,
		DisplayName: "Team",
		MemoryLimit: 50000,
		AskLimit:    500,
		Active:      true,
	}
}

// --- ChangePlan error paths ---

func TestChangePlanRejectsInactivePlan(t *testing.T) {
	t.Parallel()

	inactive := personalEarlyAccessPlan()
	inactive.Active = false
	svc, _ := buildService(t, []model.Plan{inactive})

	err := svc.ChangePlan(context.Background(), "u1", inactive.ID, "test")
	if err == nil || !contains(err.Error(), "not active") {
		t.Fatalf("want 'not active' error, got %v", err)
	}
}

func TestChangePlanRejectsHubPlan(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{hubTeamPlan()})
	err := svc.ChangePlan(context.Background(), "u1", "hub_team", "test")
	if err == nil || !contains(err.Error(), "hub plan") {
		t.Fatalf("want hub-plan rejection, got %v", err)
	}
}

func TestChangePlanAcceptsLegacyIDViaMapping(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	err := svc.ChangePlan(context.Background(), "u1", model.LegacyEarlyAccessPlanID, "test")
	if err != nil {
		t.Fatalf("legacy-mapped ID should succeed, got %v", err)
	}
	if !ms.updateUserPlanCalled {
		t.Error("UpdateUserPlan never fired despite successful lookup")
	}
}

func TestChangePlanReturnsUpdateError(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	ms.updateUserPlanErr = errors.New("db down")

	err := svc.ChangePlan(context.Background(), "u1", "personal_early_access", "test")
	if err == nil || !contains(err.Error(), "db down") {
		t.Fatalf("want UpdateUserPlan error to propagate, got %v", err)
	}
}

func TestChangePlanSwallowsSubscriptionUpsertError(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	ms.upsertBillingSubErr = errors.New("audit table offline")

	// The plan change should succeed even though the audit-trail
	// subscription upsert failed — the docstring says this is
	// intentionally non-fatal.
	if err := svc.ChangePlan(context.Background(), "u1", "personal_early_access", "test"); err != nil {
		t.Fatalf("subscription upsert failure should be non-fatal, got %v", err)
	}
	if !ms.updateUserPlanCalled {
		t.Error("plan update still should have happened")
	}
}

func TestChangePlanSwallowsUsageEventError(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	ms.insertUsageErr = errors.New("event log offline")

	if err := svc.ChangePlan(context.Background(), "u1", "personal_early_access", "test"); err != nil {
		t.Fatalf("usage event failure should be non-fatal, got %v", err)
	}
}

func TestChangePlanCallsInvalidateUserPlan(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	called := false
	svc.SetInvalidateUserPlan(func(_ context.Context, userID string) error {
		called = true
		if userID != "u1" {
			t.Errorf("invalidate got user %q, want u1", userID)
		}
		return nil
	})
	if err := svc.ChangePlan(context.Background(), "u1", "personal_early_access", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if !called {
		t.Fatal("invalidateUserPlan callback was never invoked")
	}
}

func TestChangePlanFailsIfInvalidateReturnsError(t *testing.T) {
	t.Parallel()

	// Per the implementation comment: cache invalidation failure
	// MUST surface as an error because stale-cache + new-DB leaves
	// the user with old limits despite a successful plan change.
	// This test is the regression guard for the Codex finding #2
	// fix — if a future refactor makes this non-fatal the bug
	// recurs silently.
	svc, _ := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	svc.SetInvalidateUserPlan(func(_ context.Context, _ string) error {
		return errors.New("redis down")
	})
	err := svc.ChangePlan(context.Background(), "u1", "personal_early_access", "test")
	if err == nil || !contains(err.Error(), "cache invalidation failed") {
		t.Fatalf("want cache-invalidation error, got %v", err)
	}
}

// --- ResetUsageCounters paths ---

func TestResetUsageCountersFailsWhenPostgresFails(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalFreePlan()})
	ms.resetUsageErr = errors.New("schema lock")

	err := svc.ResetUsageCounters(context.Background(), "u1", "admin-1")
	if err == nil || !contains(err.Error(), "reset postgres usage") {
		t.Fatalf("want postgres reset error to surface, got %v", err)
	}
}

func TestResetUsageCountersSwallowsRedisFailure(t *testing.T) {
	t.Parallel()

	// Postgres is the authoritative source of truth; Redis is a
	// hot-cache that resyncs on its own tick. A Redis reset
	// failure after Postgres is zeroed must not fail the whole
	// operation — the comment in service.go calls this out
	// explicitly.
	svc, _ := buildService(t, []model.Plan{personalFreePlan()})
	svc.SetResetCounters(func(_ context.Context, _ string) error {
		return errors.New("redis flap")
	})
	if err := svc.ResetUsageCounters(context.Background(), "u1", "admin-1"); err != nil {
		t.Fatalf("redis error must be non-fatal, got %v", err)
	}
}

func TestResetUsageCountersSwallowsAuditEventFailure(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalFreePlan()})
	ms.insertUsageErr = errors.New("event table locked")

	if err := svc.ResetUsageCounters(context.Background(), "u1", "admin-1"); err != nil {
		t.Fatalf("audit failure must be non-fatal, got %v", err)
	}
}

// --- GetActivePlan paths ---

func TestGetActivePlanPrefersPersonalPlanID(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalFreePlan(), personalEarlyAccessPlan()})
	ms.users["u1"] = &model.User{
		ID:             "u1",
		Plan:           "personal_free",
		PersonalPlanID: "personal_early_access",
	}

	p := svc.GetActivePlan("u1")
	if p == nil || p.ID != "personal_early_access" {
		t.Fatalf("expected personal_early_access (scoped wins), got %+v", p)
	}
}

func TestGetActivePlanFallsBackToLegacyPlanColumn(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{personalFreePlan(), personalEarlyAccessPlan()})
	ms.users["u1"] = &model.User{
		ID:             "u1",
		Plan:           "personal_early_access",
		PersonalPlanID: "",
	}

	p := svc.GetActivePlan("u1")
	if p == nil || p.ID != "personal_early_access" {
		t.Fatalf("legacy fallback should resolve, got %+v", p)
	}
}

func TestGetActivePlanFallsBackToFreeWhenUserMissing(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{personalFreePlan()})
	p := svc.GetActivePlan("nonexistent-user")
	if p == nil || p.ID != model.PersonalFreePlanID {
		t.Fatalf("unknown user should fall back to free, got %+v", p)
	}
}

func TestGetActivePlanFallsBackToFreeWhenPlanMissing(t *testing.T) {
	t.Parallel()

	// User exists but their plan ID isn't in the registry (maybe
	// a plan was deactivated and removed between the write and the
	// read). Must not return nil — callers dereference .AskLimit
	// etc. unconditionally.
	svc, ms := buildService(t, []model.Plan{personalFreePlan()})
	ms.users["u1"] = &model.User{ID: "u1", Plan: "deleted-plan"}

	p := svc.GetActivePlan("u1")
	if p == nil || p.ID != model.PersonalFreePlanID {
		t.Fatalf("missing plan should fall back to free, got %+v", p)
	}
}

// --- ChangeHubPlan paths ---

func TestChangeHubPlanRejectsUnknownPlan(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{hubTeamPlan()})
	err := svc.ChangeHubPlan(context.Background(), "hub-1", "nope", "test")
	if err == nil || !contains(err.Error(), "not found") {
		t.Fatalf("want 'not found' error, got %v", err)
	}
}

func TestChangeHubPlanRejectsInactivePlan(t *testing.T) {
	t.Parallel()

	inactive := hubTeamPlan()
	inactive.Active = false
	svc, _ := buildService(t, []model.Plan{inactive})

	err := svc.ChangeHubPlan(context.Background(), "hub-1", inactive.ID, "test")
	if err == nil || !contains(err.Error(), "not active") {
		t.Fatalf("want 'not active' error, got %v", err)
	}
}

func TestChangeHubPlanRejectsPersonalPlan(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{personalEarlyAccessPlan()})
	err := svc.ChangeHubPlan(context.Background(), "hub-1", "personal_early_access", "test")
	if err == nil || !contains(err.Error(), "personal plan") {
		t.Fatalf("want personal-plan rejection, got %v", err)
	}
}

func TestChangeHubPlanRejectsMissingHub(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, []model.Plan{hubTeamPlan()})
	err := svc.ChangeHubPlan(context.Background(), "missing-hub", "hub_team", "test")
	if err == nil || !contains(err.Error(), "not found") {
		t.Fatalf("want hub-not-found error, got %v", err)
	}
}

func TestChangeHubPlanRejectsPersonalHub(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["personal-hub"] = &model.Hub{ID: "personal-hub", HubType: "personal", OwnerID: "u1"}

	err := svc.ChangeHubPlan(context.Background(), "personal-hub", "hub_team", "test")
	if err == nil || !contains(err.Error(), "not a team hub") {
		t.Fatalf("want 'not a team hub' error, got %v", err)
	}
}

func TestChangeHubPlanAppliesSeatFloor(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 1 // below the floor

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	sub := ms.activeHubSubs["hub-1"]
	if sub == nil {
		t.Fatal("no subscription was created")
	}
	if sub.SeatCount != 3 {
		t.Errorf("seat_count = %d, want 3 (minimum floor)", sub.SeatCount)
	}
}

func TestChangeHubPlanUsesActualMemberCountAboveFloor(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 7

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if ms.activeHubSubs["hub-1"].SeatCount != 7 {
		t.Errorf("seat_count = %d, want 7", ms.activeHubSubs["hub-1"].SeatCount)
	}
}

func TestChangeHubPlanUpdatesExistingSubscription(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 5
	// Pre-seed an existing active subscription so the code takes
	// the update-path instead of create-path.
	ms.activeHubSubs["hub-1"] = &model.HubSubscription{
		ID:            "sub-1",
		HubID:         "hub-1",
		PlanID:        "hub_free_team",
		SeatCount:     3,
		Status:        "active",
		BillingUserID: "u1",
	}

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if !ms.updateHubSubCalled {
		t.Error("expected UpdateHubSubscription (existing sub path)")
	}
	if ms.createHubSubCalled {
		t.Error("CreateHubSubscription should NOT be called when one already exists")
	}
	if ms.activeHubSubs["hub-1"].PlanID != "hub_team" {
		t.Errorf("plan not updated, got %q", ms.activeHubSubs["hub-1"].PlanID)
	}
}

func TestChangeHubPlanFailsIfInvalidateMembersFails(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 3
	svc.SetInvalidateHubMembers(func(_ context.Context, _ string) error {
		return errors.New("redis down")
	})

	err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test")
	if err == nil || !contains(err.Error(), "member cache invalidation failed") {
		t.Fatalf("want invalidation error, got %v", err)
	}
}

func TestChangeHubPlanFiresHubRestoredWhenCleared(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 3
	ms.memoryCountByHub = 100     // within team plan's 50000 limit
	ms.clearHubOverLimitOK = true // the clear flipped the row

	restoredCalls := 0
	svc.SetNotifyHubRestored(func(_ context.Context, _ string) {
		restoredCalls++
	})

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if restoredCalls != 1 {
		t.Errorf("notifyHubRestored calls = %d, want 1", restoredCalls)
	}
}

func TestChangeHubPlanSkipsHubRestoredWhenNoTransition(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, []model.Plan{hubTeamPlan()})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 3
	ms.memoryCountByHub = 100
	ms.clearHubOverLimitOK = false // nothing to clear

	restoredCalls := 0
	svc.SetNotifyHubRestored(func(_ context.Context, _ string) {
		restoredCalls++
	})

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if restoredCalls != 0 {
		t.Errorf("notifyHubRestored should not fire without a non-null→null transition; got %d calls", restoredCalls)
	}
}

func TestChangeHubPlanAlwaysClearsForUnlimitedPlan(t *testing.T) {
	t.Parallel()

	unlimited := hubTeamPlan()
	unlimited.MemoryLimit = -1

	svc, ms := buildService(t, []model.Plan{unlimited})
	ms.hubs["hub-1"] = &model.Hub{ID: "hub-1", HubType: "team", OwnerID: "u1"}
	ms.hubMemberCount = 3
	ms.memoryCountByHub = 999999999 // far above any other plan's limit
	ms.clearHubOverLimitOK = true

	restoredCalls := 0
	svc.SetNotifyHubRestored(func(_ context.Context, _ string) {
		restoredCalls++
	})

	if err := svc.ChangeHubPlan(context.Background(), "hub-1", "hub_team", "test"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if restoredCalls != 1 {
		t.Errorf("unlimited plan should unconditionally attempt clear; notifyHubRestored = %d", restoredCalls)
	}
}

// --- UpdateHubSeatCount paths ---

func TestUpdateHubSeatCountNoopWhenNoSubscription(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, nil)
	if err := svc.UpdateHubSeatCount(context.Background(), "hub-1"); err != nil {
		t.Fatalf("expected nil err when no sub exists, got %v", err)
	}
}

func TestUpdateHubSeatCountUpdatesWhenCountChanged(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	ms.activeHubSubs["hub-1"] = &model.HubSubscription{
		ID: "sub-1", HubID: "hub-1", SeatCount: 3, Status: "active", BillingUserID: "u1",
	}
	ms.hubMemberCount = 9

	if err := svc.UpdateHubSeatCount(context.Background(), "hub-1"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !ms.updateHubSubCalled {
		t.Error("expected subscription update when count changed")
	}
	if ms.activeHubSubs["hub-1"].SeatCount != 9 {
		t.Errorf("seat_count = %d, want 9", ms.activeHubSubs["hub-1"].SeatCount)
	}
}

func TestUpdateHubSeatCountAppliesFloor(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	ms.activeHubSubs["hub-1"] = &model.HubSubscription{
		ID: "sub-1", HubID: "hub-1", SeatCount: 3, Status: "active", BillingUserID: "u1",
	}
	// One-member hub: seat_count should stay at 3 (floor), which
	// equals the current value → no update call.
	ms.hubMemberCount = 1

	if err := svc.UpdateHubSeatCount(context.Background(), "hub-1"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if ms.updateHubSubCalled {
		t.Error("no-op case should not call UpdateHubSubscription")
	}
}

func TestUpdateHubSeatCountSurfacesUpdateError(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	ms.activeHubSubs["hub-1"] = &model.HubSubscription{
		ID: "sub-1", HubID: "hub-1", SeatCount: 3, Status: "active", BillingUserID: "u1",
	}
	ms.hubMemberCount = 9
	ms.updateHubSubErr = errors.New("lock timeout")

	err := svc.UpdateHubSeatCount(context.Background(), "hub-1")
	if err == nil || !contains(err.Error(), "lock timeout") {
		t.Fatalf("want update error, got %v", err)
	}
}

// --- TransferHubBilling paths ---

func TestTransferHubBillingNoopWhenNoSubscription(t *testing.T) {
	t.Parallel()

	svc, _ := buildService(t, nil)
	if err := svc.TransferHubBilling(context.Background(), "hub-1", "new-owner"); err != nil {
		t.Fatalf("expected nil err when no sub, got %v", err)
	}
}

func TestTransferHubBillingUpdatesBillingUserID(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	ms.activeHubSubs["hub-1"] = &model.HubSubscription{
		ID: "sub-1", HubID: "hub-1", SeatCount: 3, BillingUserID: "old-owner", Status: "active",
	}

	if err := svc.TransferHubBilling(context.Background(), "hub-1", "new-owner"); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if ms.activeHubSubs["hub-1"].BillingUserID != "new-owner" {
		t.Errorf("billing_user_id = %q, want new-owner", ms.activeHubSubs["hub-1"].BillingUserID)
	}
}

// --- GetHubSubscription + ListBillingHubs pass-throughs ---

func TestGetHubSubscriptionPassesThrough(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	want := &model.HubSubscription{ID: "sub-1", HubID: "hub-1"}
	ms.activeHubSubs["hub-1"] = want

	got, err := svc.GetHubSubscription(context.Background(), "hub-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != want {
		t.Error("GetHubSubscription did not return the store's value unchanged")
	}
}

func TestListBillingHubsPassesThrough(t *testing.T) {
	t.Parallel()

	svc, ms := buildService(t, nil)
	ms.hubSubs = []model.HubSubscription{{ID: "a"}, {ID: "b"}}

	out, err := svc.ListBillingHubs(context.Background(), "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("len = %d, want 2", len(out))
	}
	if !ms.listHubSubsByBillingUserCalled {
		t.Error("store method not called")
	}
}

// --- Provider + AdminProvider ---

func TestAdminProviderReturnsNoOpResults(t *testing.T) {
	t.Parallel()

	p := &AdminProvider{}
	if id, err := p.CreateCustomer(context.Background(), &model.User{ID: "u1"}); err != nil || id != "" {
		t.Errorf("CreateCustomer: got (%q, %v), want ('', nil)", id, err)
	}
	if sub, err := p.CreateSubscription(context.Background(), "cus-1", "pro"); err != nil || sub != nil {
		t.Errorf("CreateSubscription: got (%+v, %v), want (nil, nil)", sub, err)
	}
	if err := p.CancelSubscription(context.Background(), "sub-1"); err != nil {
		t.Errorf("CancelSubscription: %v", err)
	}
	if sub, err := p.ChangeSubscription(context.Background(), "sub-1", "pro"); err != nil || sub != nil {
		t.Errorf("ChangeSubscription: got (%+v, %v), want (nil, nil)", sub, err)
	}
	if ev, err := p.HandleWebhook(context.Background(), []byte("{}"), "sig"); err != nil || ev != nil {
		t.Errorf("HandleWebhook: got (%+v, %v), want (nil, nil)", ev, err)
	}
}

func TestProviderNameExternalFallback(t *testing.T) {
	t.Parallel()

	// Any non-AdminProvider implementation reports as "external".
	// Keeps the billing_subscriptions.provider column stable for
	// downstream analytics — we only distinguish the admin-managed
	// case from "talks to Stripe/Polar/etc." externally.
	var p Provider = fakeExternalProvider{}
	if name := providerName(p); name != "external" {
		t.Errorf("providerName(external) = %q, want 'external'", name)
	}
}

type fakeExternalProvider struct{}

func (fakeExternalProvider) CreateCustomer(_ context.Context, _ *model.User) (string, error) {
	return "cus_fake", nil
}
func (fakeExternalProvider) CreateSubscription(_ context.Context, _, _ string) (*Subscription, error) {
	return nil, nil
}
func (fakeExternalProvider) CancelSubscription(_ context.Context, _ string) error { return nil }
func (fakeExternalProvider) ChangeSubscription(_ context.Context, _, _ string) (*Subscription, error) {
	return nil, nil
}
func (fakeExternalProvider) HandleWebhook(_ context.Context, _ []byte, _ string) (*WebhookEvent, error) {
	return nil, nil
}

// contains is a tiny helper so tests don't need to import strings
// just for one-shot Contains checks.
func contains(haystack, needle string) bool {
	return indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
