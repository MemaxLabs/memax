package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// setupHubsHandlerFixture seeds one user with a personal hub + one
// team hub where the user is owner. Returns everything the tests
// below need.
func setupHubsHandlerFixture(t *testing.T) (*handler.HubsHandler, string, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'H', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@hubs",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	personalID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: personalID, Name: "P", Slug: "p-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: userID,
	}); err != nil {
		t.Fatalf("CreateHub (personal): %v", err)
	}
	// CreateHub doesn't auto-add hub_members for personal hubs; the
	// real user-registration flow adds one separately. For ListUserHubs
	// (which joins hub_members) to see the personal hub we add it
	// explicitly here.
	if err := s.AddHubMember(personalID, userID, "owner"); err != nil {
		t.Fatalf("AddHubMember (personal): %v", err)
	}

	teamID := uuid.NewString()
	team := &model.Hub{
		ID: teamID, Name: "Eng", Slug: "eng-" + uuid.NewString()[:6],
		HubType: "team", OwnerID: userID,
		Plan: model.HubFreeTeamPlanID,
	}
	if err := s.CreateTeamHub(ctx, team, userID, 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}

	return handler.NewHubsHandler(s), userID, personalID, teamID
}

func TestHubsList_ReturnsBothHubsForMember(t *testing.T) {
	t.Parallel()
	h, userID, personalID, teamID := setupHubsHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs", nil)
	req = handler.InjectUserIDForTest(req, userID)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []model.HubWithRole `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 hubs, got %d", len(resp.Data))
	}
	ids := map[string]bool{}
	for _, entry := range resp.Data {
		ids[entry.Hub.ID] = true
	}
	if !ids[personalID] || !ids[teamID] {
		t.Errorf("missing hubs: got %+v", ids)
	}
}

func TestHubsList_EmptyForNonMember(t *testing.T) {
	t.Parallel()
	h, _, _, _ := setupHubsHandlerFixture(t)

	other := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/hubs", nil)
	req = handler.InjectUserIDForTest(req, other)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Response is a JSON array; unmarshaled into []HubWithRole it
	// must be empty (normalized from nil) so the web client can
	// render "no hubs" state cleanly.
	var resp struct {
		Data []model.HubWithRole `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Errorf("non-member should see 0 hubs, got %d", len(resp.Data))
	}
}

func TestHubsGet_OwnerCanFetch(t *testing.T) {
	t.Parallel()
	h, userID, _, teamID := setupHubsHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+teamID, nil)
	req = handler.InjectUserIDForTest(req, userID)
	req.SetPathValue("id", teamID)
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Response body should include hub + members block.
	body := rec.Body.String()
	if !containsAll(body, teamID, "Eng") {
		t.Errorf("response missing expected fields: %s", body)
	}
}

func TestHubsGet_NonMemberForbidden(t *testing.T) {
	t.Parallel()
	h, _, _, teamID := setupHubsHandlerFixture(t)

	other := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+teamID, nil)
	req = handler.InjectUserIDForTest(req, other)
	req.SetPathValue("id", teamID)
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-member: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHubsSetters(t *testing.T) {
	t.Parallel()
	h, _, _, _ := setupHubsHandlerFixture(t)
	// Smoke: setters don't panic on nil or zero inputs.
	h.SetInvalidateUserPlan(nil)
	h.SetBilling(nil)
	h.SetOwnershipResolver(nil)
	h.SetEnqueueEmail(nil)
	h.SetAppBaseURL("")
	if h2 := h.WithEvents(nil); h2 == nil {
		t.Error("WithEvents should return *HubsHandler")
	}
}

func containsAll(s string, needles ...string) bool {
	for _, n := range needles {
		if !stringContains(s, n) {
			return false
		}
	}
	return true
}

func stringContains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	n := len(substr)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}
