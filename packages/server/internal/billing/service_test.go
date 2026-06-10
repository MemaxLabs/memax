package billing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func setupTestService(t *testing.T) (*Service, *store.InMemoryStore) {
	t.Helper()
	s := store.NewInMemoryStore()
	registry := plans.NewRegistry(s)
	// Registry will have no plans (InMemoryStore returns nil for ListPlans),
	// so we test the error path and the admin provider path.
	svc := NewService(s, registry)
	return svc, s
}

func TestNewService_DefaultsToAdminProvider(t *testing.T) {
	svc, _ := setupTestService(t)
	if _, ok := svc.provider.(*AdminProvider); !ok {
		t.Fatalf("expected AdminProvider, got %T", svc.provider)
	}
}

func TestSetProvider_ReplacesProvider(t *testing.T) {
	svc, _ := setupTestService(t)
	custom := &AdminProvider{} // still AdminProvider but a different instance
	svc.SetProvider(custom)
	if svc.provider != custom {
		t.Fatal("SetProvider did not replace the provider")
	}
}

func TestSetProvider_NilIsIgnored(t *testing.T) {
	svc, _ := setupTestService(t)
	original := svc.provider
	svc.SetProvider(nil)
	if svc.provider != original {
		t.Fatal("SetProvider(nil) should not replace the provider")
	}
}

func TestChangePlan_RejectsNonexistentPlan(t *testing.T) {
	svc, _ := setupTestService(t)
	err := svc.ChangePlan(context.Background(), "user-1", "nonexistent", "test")
	if err == nil {
		t.Fatal("expected error for nonexistent plan")
	}
}

func TestChangePlan_LogsAuditEvent(t *testing.T) {
	// Use a mock store that tracks calls
	ms := &mockStore{InMemoryStore: store.NewInMemoryStore()}
	ms.planToReturn = &model.Plan{ID: "pro", Active: true, DisplayName: "Pro"}

	registry := plans.NewRegistry(ms)
	// Load so the registry caches the plan from ListPlans
	if err := registry.Load(context.Background()); err != nil {
		t.Fatalf("registry load: %v", err)
	}
	svc := NewService(ms, registry)

	err := svc.ChangePlan(context.Background(), "user-1", "pro", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ms.updateUserPlanCalled {
		t.Error("expected UpdateUserPlan to be called")
	}
	if ms.lastUserPlan != "pro" {
		t.Errorf("expected plan 'pro', got %q", ms.lastUserPlan)
	}
	if !ms.insertUsageEventCalled {
		t.Error("expected InsertUsageEvent to be called")
	}
	if ms.lastUsageEvent == nil || ms.lastUsageEvent.Operation != "plan_change" {
		t.Error("expected plan_change usage event")
	}
}

func TestResetUsageCounters_ZerosPostgres(t *testing.T) {
	ms := &mockStore{InMemoryStore: store.NewInMemoryStore()}
	registry := plans.NewRegistry(ms)
	svc := NewService(ms, registry)

	err := svc.ResetUsageCounters(context.Background(), "user-1", "admin-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ms.resetUsageCountersCalled {
		t.Error("expected ResetUsageCounters to be called (not SetUsageCounters)")
	}
	if ms.setUsageCountersCalled {
		t.Error("ResetUsageCounters should NOT call SetUsageCounters (GREATEST would prevent zeroing)")
	}
}

func TestResetUsageCounters_CallsRedisReset(t *testing.T) {
	ms := &mockStore{InMemoryStore: store.NewInMemoryStore()}
	registry := plans.NewRegistry(ms)
	svc := NewService(ms, registry)

	resetCalled := false
	svc.SetResetCounters(func(_ context.Context, userID string) error {
		resetCalled = true
		if userID != "user-1" {
			return fmt.Errorf("wrong user ID: %s", userID)
		}
		return nil
	})

	err := svc.ResetUsageCounters(context.Background(), "user-1", "admin-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resetCalled {
		t.Error("expected Redis reset callback to be called")
	}
}

func TestProviderName(t *testing.T) {
	if name := providerName(&AdminProvider{}); name != "admin" {
		t.Errorf("expected 'admin', got %q", name)
	}
}

// mockStore wraps InMemoryStore to track method calls for testing.
type mockStore struct {
	*store.InMemoryStore

	// GetPlan override
	planToReturn *model.Plan

	// UpdateUserPlan tracking
	updateUserPlanCalled bool
	lastUserPlan         string

	// InsertUsageEvent tracking
	insertUsageEventCalled bool
	lastUsageEvent         *model.UsageEvent

	// UpsertBillingSubscription tracking
	upsertBillingSubCalled bool

	// SetUsageCounters tracking
	setUsageCountersCalled bool

	// ResetUsageCounters tracking
	resetUsageCountersCalled bool
}

func (m *mockStore) ListPlans(_ context.Context) ([]model.Plan, error) {
	if m.planToReturn != nil {
		return []model.Plan{*m.planToReturn}, nil
	}
	return nil, nil
}

func (m *mockStore) GetPlan(_ context.Context, planID string) (*model.Plan, error) {
	if m.planToReturn != nil && m.planToReturn.ID == planID {
		return m.planToReturn, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockStore) UpdateUserPlan(_ context.Context, _ string, planID string) error {
	m.updateUserPlanCalled = true
	m.lastUserPlan = planID
	return nil
}

func (m *mockStore) InsertUsageEvent(_ context.Context, event *model.UsageEvent) error {
	m.insertUsageEventCalled = true
	m.lastUsageEvent = event
	return nil
}

func (m *mockStore) UpsertBillingSubscription(_ context.Context, _ *model.BillingSubscription) error {
	m.upsertBillingSubCalled = true
	return nil
}

func (m *mockStore) SetUsageCounters(_ context.Context, _ string, _, _ time.Time, _, _, _ int) error {
	m.setUsageCountersCalled = true
	return nil
}

func (m *mockStore) ResetUsageCounters(_ context.Context, _ string, _, _ time.Time) error {
	m.resetUsageCountersCalled = true
	return nil
}
