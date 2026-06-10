package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// usageStore embeds InMemoryStore and supplies just the methods
// GetUserUsage actually hits (GetUser, CountMemoriesByOwner,
// SumStorageBytesByOwner). InMemoryStore's GetUser is a stub that
// always returns "not found", so we override it here.
type usageStore struct {
	*store.InMemoryStore
	user         *model.User
	memoryCount  int
	storageBytes int64
}

func (u *usageStore) GetUser(id string) (*model.User, error) {
	if u.user != nil && u.user.ID == id {
		return u.user, nil
	}
	return nil, fmt.Errorf("not found")
}
func (u *usageStore) CountMemoriesByOwner(_ context.Context, _ string) (int, error) {
	return u.memoryCount, nil
}
func (u *usageStore) SumStorageBytesByOwner(_ context.Context, _ string) (int64, error) {
	return u.storageBytes, nil
}

// stubUsageReader satisfies adminUsageReader for tests.
type stubUsageReader struct {
	push, recall, ask int
	storageBytes      int64
}

func (s stubUsageReader) GetCommittedUsage(_ context.Context, _ string) (int, int, int) {
	return s.push, s.recall, s.ask
}
func (s stubUsageReader) GetCommittedStorageBytes(_ context.Context, _ string) int64 {
	return s.storageBytes
}

// stubPlanResolver lets the usage endpoint run without a full
// plans.Registry. Returns whatever limits the test wants.
type stubPlanResolver struct{ limits model.UserLimits }

func (s stubPlanResolver) GetUserLimits(_ context.Context, _, _ string) model.UserLimits {
	return s.limits
}
func (s stubPlanResolver) Reload(_ context.Context) error { return nil }

// stubPlanChanger is a no-op adminPlanChanger — GetUserUsage doesn't
// call into it but AdminUsersHandler requires a non-nil implementation.
type stubPlanChanger struct{}

func (stubPlanChanger) ChangePlan(_ context.Context, _, _, _ string) error    { return nil }
func (stubPlanChanger) ChangeHubPlan(_ context.Context, _, _, _ string) error { return nil }
func (stubPlanChanger) GetHubSubscription(_ context.Context, _ string) (*model.HubSubscription, error) {
	return nil, nil
}
func (stubPlanChanger) ListBillingHubs(_ context.Context, _ string) ([]model.HubSubscription, error) {
	return nil, nil
}

func TestAdminUsers_GetUserUsage_ReturnsMonthlyAndLifetimePairs(t *testing.T) {
	s := &usageStore{
		InMemoryStore: store.NewInMemoryStore(),
		user:          &model.User{ID: "u1", Email: "a@test.local", PersonalPlanID: "personal_pro"},
		memoryCount:   287,
		storageBytes:  0, // zero triggers the Postgres-SUM fallback in the handler
	}

	h := NewAdminUsersHandler(s, stubPlanChanger{}, stubPlanResolver{
		limits: model.UserLimits{
			PlanID:            "personal_pro",
			PlanDisplayName:   "Pro",
			MemoryLimit:       5000,
			PushLimit:         1000,
			RecallLimit:       -1, // unlimited — endpoint must preserve the -1 marker
			AskLimit:          100,
			StorageBytesLimit: 5 * 1024 * 1024 * 1024,
		},
	})
	h.SetUsageReader(stubUsageReader{push: 42, recall: 120, ask: 8, storageBytes: 1234567})

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/u1/usage", nil)
	req.SetPathValue("id", "u1")
	rec := httptest.NewRecorder()
	h.GetUserUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := resp.Data.(map[string]any)
	if data["plan_id"] != "personal_pro" {
		t.Errorf("plan_id = %v, want personal_pro", data["plan_id"])
	}
	monthly, _ := data["monthly"].(map[string]any)
	push, _ := monthly["push"].(map[string]any)
	if push["current"] != float64(42) || push["limit"] != float64(1000) {
		t.Errorf("push pair = %v, want {42, 1000}", push)
	}
	recall, _ := monthly["recall"].(map[string]any)
	if recall["limit"] != float64(-1) {
		t.Errorf("recall.limit = %v, want -1 (unlimited sentinel preserved)", recall["limit"])
	}
	lifetime, _ := data["lifetime"].(map[string]any)
	storage, _ := lifetime["storage_bytes"].(map[string]any)
	if storage["current"] != float64(1234567) {
		t.Errorf("storage_bytes.current = %v, want 1234567", storage["current"])
	}
}

func TestAdminUsers_GetUserUsage_NilUsageReaderReturnsZeros(t *testing.T) {
	// Dev / degraded-mode parity: if the meter isn't wired, every
	// counter reads 0 and the endpoint still returns 200 so the admin
	// UI can render "usage unavailable" rather than break entirely.
	s := &usageStore{
		InMemoryStore: store.NewInMemoryStore(),
		user:          &model.User{ID: "u2", PersonalPlanID: "personal_free"},
	}
	h := NewAdminUsersHandler(s, stubPlanChanger{}, stubPlanResolver{
		limits: model.UserLimits{PlanID: "personal_free"},
	})
	// NOTE: no SetUsageReader — h.usage stays nil.

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/u2/usage", nil)
	req.SetPathValue("id", "u2")
	rec := httptest.NewRecorder()
	h.GetUserUsage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in degraded mode, got %d", rec.Code)
	}
}

func TestAdminUsers_GetUserUsage_UserNotFound_404(t *testing.T) {
	s := &usageStore{InMemoryStore: store.NewInMemoryStore()}
	h := NewAdminUsersHandler(s, stubPlanChanger{}, stubPlanResolver{})
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/nope/usage", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	h.GetUserUsage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAdminUsers_GetUserUsage_MissingID_400(t *testing.T) {
	s := &usageStore{InMemoryStore: store.NewInMemoryStore()}
	h := NewAdminUsersHandler(s, stubPlanChanger{}, stubPlanResolver{})
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users//usage", nil)
	req.SetPathValue("id", "") // Go 1.22 routing wouldn't match this, but handler-level guard exists
	rec := httptest.NewRecorder()
	h.GetUserUsage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty id, got %d", rec.Code)
	}
}
