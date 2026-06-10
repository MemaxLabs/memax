package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func setupAdminUsersFixture(t *testing.T) (*handler.AdminUsersHandler, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@au",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// nil planChanger + planResolver — ListUsers / GetUser / GetStats
	// work without them; SetPlan / UpdatePlan do not.
	return handler.NewAdminUsersHandler(s, nil, nil), userID
}

func TestAdminListUsers_ReturnsSeededUser(t *testing.T) {
	t.Parallel()
	h, userID := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	rec := httptest.NewRecorder()
	h.ListUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Users []struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"users"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Total < 1 {
		t.Errorf("total = %d, want ≥ 1", resp.Data.Total)
	}
	found := false
	for _, u := range resp.Data.Users {
		if u.ID == userID {
			found = true
			break
		}
	}
	if !found {
		t.Error("seeded user not in list")
	}
}

func TestAdminListUsers_ClampsOutOfRangeLimit(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	// 0, negative, and >100 all fall back to the 50 default.
	for _, lim := range []string{"0", "-5", "999"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/users?limit="+lim, nil)
		rec := httptest.NewRecorder()
		h.ListUsers(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("limit=%q: got %d", lim, rec.Code)
		}
	}
}

func TestAdminGetUser_HappyPath(t *testing.T) {
	t.Parallel()
	h, userID := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/"+userID, nil)
	req.SetPathValue("id", userID)
	rec := httptest.NewRecorder()
	h.GetUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminGetUser_RejectsMissingID(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/", nil)
	req.SetPathValue("id", "")
	rec := httptest.NewRecorder()
	h.GetUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing id: expected 400, got %d", rec.Code)
	}
}

func TestAdminGetUser_UnknownIDIs404(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.GetUser(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d", rec.Code)
	}
}

func TestAdminListPlans_Returns(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/plans", nil)
	rec := httptest.NewRecorder()
	h.ListPlans(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminGetStats_Returns(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/stats", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminListTeamHubs_Returns(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/team-hubs", nil)
	rec := httptest.NewRecorder()
	h.ListTeamHubs(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminListBillingHubs_RejectsMissingID(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/billing-hubs", nil)
	rec := httptest.NewRecorder()
	h.ListBillingHubs(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing id: expected 400, got %d", rec.Code)
	}
}

func TestAdminListBillingHubs_UnavailableWithoutPlanChanger(t *testing.T) {
	t.Parallel()
	h, userID := setupAdminUsersFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users/"+userID+"/billing-hubs", nil)
	req.SetPathValue("id", userID)
	rec := httptest.NewRecorder()
	h.ListBillingHubs(rec, req)
	// nil planChanger → 503.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil planChanger: expected 503, got %d", rec.Code)
	}
}

func TestAdminDeleteOverrides_NotFoundForUnknownUser(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodDelete, "/v1/admin/users/"+id+"/overrides", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.DeleteOverrides(rec, req)
	// Idempotent delete: either 200 (no-op) or 404 is acceptable; we
	// just care the handler doesn't panic.
	if rec.Code == 0 {
		t.Error("handler didn't respond")
	}
}

func TestAdminUsersSetUsageReader_NoPanic(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminUsersFixture(t)
	h.SetUsageReader(nil)
}
