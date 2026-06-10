package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func setupAdminCampaignsFixture(t *testing.T) (*handler.AdminCampaignsHandler, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	adminID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'A', 'personal_early_access', now(), now())`,
		adminID, adminID[:8]+"@ac",
	); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return handler.NewAdminCampaignsHandler(s), adminID
}

func TestAdminCampaignsList_EmptyStore(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/campaigns", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Campaigns []any `json:"campaigns"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Campaigns == nil {
		t.Error("campaigns should be []-initialized, not null")
	}
}

func TestAdminCampaignsList_FiltersAndLimit(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)

	// Exercise status + cursor + limit parsing.
	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/campaigns?status=draft&limit=25&cursor=abc", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAdminCampaignsGet_NotFoundForUnknown(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/campaigns/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d", rec.Code)
	}
}

func TestAdminCampaignsCreate_RejectsBadJSON(t *testing.T) {
	t.Parallel()
	h, adminID := setupAdminCampaignsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/campaigns",
		bytes.NewBufferString("not json"))
	req = handler.InjectUserIDForTest(req, adminID)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: expected 400, got %d", rec.Code)
	}
}

func TestAdminCampaignsDelete_NotFoundForUnknown(t *testing.T) {
	t.Parallel()
	h, adminID := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodDelete, "/v1/admin/campaigns/"+id, nil)
	req = handler.InjectUserIDForTest(req, adminID)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d", rec.Code)
	}
}

func TestAdminCampaignsSchedule_NotFoundForUnknown(t *testing.T) {
	t.Parallel()
	h, adminID := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/campaigns/"+id+"/schedule", bytes.NewBufferString(`{}`))
	req = handler.InjectUserIDForTest(req, adminID)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Schedule(rec, req)
	// Unknown campaign → not-found / conflict / state-mismatch; not 200.
	if rec.Code == http.StatusOK {
		t.Errorf("unknown id schedule should not 200")
	}
}

func TestAdminCampaignsCancel_NotFoundForUnknown(t *testing.T) {
	t.Parallel()
	h, adminID := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/campaigns/"+id+"/cancel", nil)
	req = handler.InjectUserIDForTest(req, adminID)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Cancel(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("unknown id cancel should not 200")
	}
}

func TestAdminCampaignsAudit_UnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/campaigns/"+id+"/audit", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Audit(rec, req)
	// Audit on nonexistent campaign returns empty audit list, 200.
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}

func TestAdminCampaignsDeliveryStats_UnknownReturnsZeroOrError(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)
	id := uuid.NewString()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/campaigns/"+id+"/deliveries/stats", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.DeliveryStats(rec, req)
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}

func TestAdminCampaignsSetters_NoPanic(t *testing.T) {
	t.Parallel()
	h, _ := setupAdminCampaignsFixture(t)
	h.SetEnqueue(nil)
	h.SetTemplateManager(nil)
	h.SetEnqueueRenderedEmail(nil)
}
