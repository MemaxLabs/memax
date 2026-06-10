package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func setupTopicsHandlerFixture(t *testing.T) (*handler.TopicsHandler, string, string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'T', 'personal_early_access', now(), now())`,
		ownerID, ownerID[:8]+"@tp",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "P", Slug: "tp-" + uuid.NewString()[:6],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}

	return handler.NewTopicsHandler(s, nil), ownerID, hubID
}

func TestTopicsList_EmptyHub(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupTopicsHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/topics?hub_id="+hubID, nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.TopicListResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.UnassignedCount != 0 {
		t.Errorf("empty hub: unassigned = %d, want 0", resp.Data.UnassignedCount)
	}
}

func TestTopicsList_MissingHubRejected(t *testing.T) {
	t.Parallel()
	h, ownerID, _ := setupTopicsHandlerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/topics", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing hub: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTopicsCreate_HappyPath(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupTopicsHandlerFixture(t)

	body := map[string]any{
		"hub_id":   hubID,
		"name":     "Research",
		"icon":     "🔬",
		"position": 0,
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/topics", bytes.NewBuffer(buf))
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.Topic `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Name != "Research" {
		t.Errorf("Name = %q", resp.Data.Name)
	}
	if resp.Data.ID == "" {
		t.Error("ID should be generated")
	}
}

func TestTopicsCreate_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupTopicsHandlerFixture(t)

	body := map[string]any{"hub_id": hubID, "name": ""}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/topics", bytes.NewBuffer(buf))
	req = handler.InjectUserIDForTest(req, ownerID)
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Errorf("empty name should be rejected, got %d", rec.Code)
	}
}

func TestTopicsGetDelete_RoundTrip(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupTopicsHandlerFixture(t)
	s, _ := testdb.Acquire(t)
	_ = s // we'll use the one created inside setupTopicsHandlerFixture via handler

	// Create a topic first via the handler, then GET it, then DELETE.
	body, _ := json.Marshal(map[string]any{"hub_id": hubID, "name": "Temp", "icon": "📝"})
	createReq := httptest.NewRequest(http.MethodPost, "/v1/topics", bytes.NewBuffer(body))
	createReq = handler.InjectUserIDForTest(createReq, ownerID)
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusOK && createRec.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Data model.Topic `json:"data"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	topicID := created.Data.ID
	if topicID == "" {
		t.Fatal("create returned no id")
	}

	// GET
	getReq := httptest.NewRequest(http.MethodGet, "/v1/topics/"+topicID, nil)
	getReq = handler.InjectUserIDForTest(getReq, ownerID)
	getReq = handler.InjectHubIDForTest(getReq, hubID)
	getReq.SetPathValue("id", topicID)
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Errorf("Get: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// DELETE
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/topics/"+topicID, nil)
	delReq = handler.InjectUserIDForTest(delReq, ownerID)
	delReq = handler.InjectHubIDForTest(delReq, hubID)
	delReq.SetPathValue("id", topicID)
	delRec := httptest.NewRecorder()
	h.Delete(delRec, delReq)
	if delRec.Code != http.StatusOK && delRec.Code != http.StatusNoContent {
		t.Errorf("Delete: expected 200/204, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// Follow-up GET should 404.
	getReq2 := httptest.NewRequest(http.MethodGet, "/v1/topics/"+topicID, nil)
	getReq2 = handler.InjectUserIDForTest(getReq2, ownerID)
	getReq2 = handler.InjectHubIDForTest(getReq2, hubID)
	getReq2.SetPathValue("id", topicID)
	getRec2 := httptest.NewRecorder()
	h.Get(getRec2, getReq2)
	if getRec2.Code != http.StatusNotFound {
		t.Errorf("after delete, Get should 404, got %d", getRec2.Code)
	}
}

func TestTopicsRecordVisit_UpsertsRow(t *testing.T) {
	t.Parallel()
	h, ownerID, hubID := setupTopicsHandlerFixture(t)

	// Seed a topic first.
	s, _ := testdb.Acquire(t)
	_ = s
	body, _ := json.Marshal(map[string]any{"hub_id": hubID, "name": "Watch me"})
	createReq := httptest.NewRequest(http.MethodPost, "/v1/topics", bytes.NewBuffer(body))
	createReq = handler.InjectUserIDForTest(createReq, ownerID)
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	var created struct {
		Data model.Topic `json:"data"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	visitReq := httptest.NewRequest(http.MethodPost,
		"/v1/topics/"+created.Data.ID+"/visit", nil)
	visitReq = handler.InjectUserIDForTest(visitReq, ownerID)
	visitReq = handler.InjectHubIDForTest(visitReq, hubID)
	visitReq.SetPathValue("id", created.Data.ID)
	visitRec := httptest.NewRecorder()
	h.RecordVisit(visitRec, visitReq)
	if visitRec.Code != http.StatusOK && visitRec.Code != http.StatusNoContent {
		t.Errorf("RecordVisit: expected 200/204, got %d: %s",
			visitRec.Code, visitRec.Body.String())
	}

	// time param is effectively a wall-clock touch; second call is
	// idempotent.
	time.Sleep(5 * time.Millisecond)
	visitRec2 := httptest.NewRecorder()
	h.RecordVisit(visitRec2, visitReq)
	if visitRec2.Code != http.StatusOK && visitRec2.Code != http.StatusNoContent {
		t.Errorf("second RecordVisit: got %d", visitRec2.Code)
	}
}
