package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// adminDreamsTestStore overrides the dream-specific store methods
// while delegating everything else to InMemoryStore. Lets the
// handler tests run without DATABASE_URL.
type adminDreamsTestStore struct {
	*store.InMemoryStore
	listRuns     []model.DreamRun
	listCursor   string
	listErr      error
	gotListOpts  store.DreamRunListForUserOpts
	run          *model.DreamRun
	runErr       error
	actions      []model.DreamAction
	actionsErr   error
	notifs       []model.Notification
	notifsErr    error
	gotRunID     string
	gotActionsID string
	gotNotifsRun string
}

func (s *adminDreamsTestStore) ListDreamRunsForUser(_ context.Context, opts store.DreamRunListForUserOpts) ([]model.DreamRun, string, error) {
	s.gotListOpts = opts
	return s.listRuns, s.listCursor, s.listErr
}

func (s *adminDreamsTestStore) GetDreamRunByID(_ context.Context, runID string) (*model.DreamRun, error) {
	s.gotRunID = runID
	return s.run, s.runErr
}

func (s *adminDreamsTestStore) GetDreamActions(runID string) ([]model.DreamAction, error) {
	s.gotActionsID = runID
	return s.actions, s.actionsErr
}

func (s *adminDreamsTestStore) ListNotificationsByDreamRunID(_ context.Context, runID string) ([]model.Notification, error) {
	s.gotNotifsRun = runID
	return s.notifs, s.notifsErr
}

func newAdminDreamsReq(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(context.WithValue(req.Context(), userIDKey, "admin-1"))
}

func TestAdminListDreamRunsForUser_ForwardsOptsAndReturnsRuns(t *testing.T) {
	now := time.Now().UTC()
	s := &adminDreamsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		listRuns: []model.DreamRun{
			{ID: "run-1", OwnerID: "u1", HubID: "h1", Status: "completed", StartedAt: now},
			{ID: "run-2", OwnerID: "u1", HubID: "h2", Status: "partial_failed", StartedAt: now.Add(-time.Hour)},
		},
		listCursor: "2026-04-20T00:00:00Z",
	}
	h := NewAdminDreamsHandler(s)

	req := newAdminDreamsReq(http.MethodGet,
		"/v1/admin/users/u1/dream-runs?hubId=h1&limit=5&cursor=2026-04-20T01:00:00Z")
	req.SetPathValue("id", "u1")
	rec := httptest.NewRecorder()

	h.ListDreamRunsForUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if s.gotListOpts.UserID != "u1" || s.gotListOpts.HubID != "h1" ||
		s.gotListOpts.Limit != 5 || s.gotListOpts.Cursor != "2026-04-20T01:00:00Z" {
		t.Fatalf("opts not forwarded: %+v", s.gotListOpts)
	}

	var resp struct {
		Data AdminDreamRunListResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Runs) != 2 || resp.Data.Runs[0].ID != "run-1" {
		t.Fatalf("unexpected runs: %+v", resp.Data.Runs)
	}
	if resp.Data.NextCursor != "2026-04-20T00:00:00Z" {
		t.Fatalf("cursor not surfaced: %q", resp.Data.NextCursor)
	}
}

func TestAdminListDreamRunsForUser_RejectsInvalidLimit(t *testing.T) {
	h := NewAdminDreamsHandler(&adminDreamsTestStore{InMemoryStore: store.NewInMemoryStore()})
	req := newAdminDreamsReq(http.MethodGet, "/v1/admin/users/u1/dream-runs?limit=abc")
	req.SetPathValue("id", "u1")
	rec := httptest.NewRecorder()
	h.ListDreamRunsForUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAdminGetDreamRunDetail_ComposesRunActionsNotifications(t *testing.T) {
	now := time.Now().UTC()
	runID := "11111111-1111-1111-1111-111111111111"
	s := &adminDreamsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		run: &model.DreamRun{
			ID: runID, OwnerID: "u1", HubID: "h1",
			Status: "completed", StartedAt: now.Add(-time.Minute), FinishedAt: now,
		},
		actions: []model.DreamAction{
			{ID: "a1", RunID: runID, ActionType: "merge", CreatedAt: now},
		},
		notifs: []model.Notification{
			{
				ID: "n1", Audience: model.AudienceUser, RecipientUserID: "u1",
				HubID: "h1", Kind: model.NotificationKindDreamRunCompleted,
				Status: model.NotificationStatusPending, SourceKind: "dream_run",
				SourceID: runID, CreatedAt: now,
			},
		},
	}
	h := NewAdminDreamsHandler(s)

	req := newAdminDreamsReq(http.MethodGet, "/v1/admin/dream-runs/"+runID)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()

	h.GetDreamRunDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if s.gotRunID != runID || s.gotActionsID != runID || s.gotNotifsRun != runID {
		t.Fatalf("store lookups not all forwarded with run id: run=%q actions=%q notifs=%q",
			s.gotRunID, s.gotActionsID, s.gotNotifsRun)
	}

	var resp struct {
		Data AdminDreamRunDetailResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Run.ID != runID || len(resp.Data.Actions) != 1 || len(resp.Data.Notifications) != 1 {
		t.Fatalf("response composition wrong: %+v", resp.Data)
	}
}

func TestAdminGetDreamRunDetail_RunNotFound(t *testing.T) {
	s := &adminDreamsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		runErr:        store.ErrDreamRunNotFound,
	}
	h := NewAdminDreamsHandler(s)
	req := newAdminDreamsReq(http.MethodGet, "/v1/admin/dream-runs/missing")
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	h.GetDreamRunDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestAdminGetDreamRunDetail_SurfacesDBErrorAsFiveHundred covers
// the Medium that Codex flagged: a transient DB/scan error must
// not masquerade as 404. Only ErrDreamRunNotFound maps to 404.
func TestAdminGetDreamRunDetail_SurfacesDBErrorAsFiveHundred(t *testing.T) {
	s := &adminDreamsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		runErr:        &fakeError{"connection refused"},
	}
	h := NewAdminDreamsHandler(s)
	req := newAdminDreamsReq(http.MethodGet, "/v1/admin/dream-runs/missing")
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	h.GetDreamRunDetail(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (non-not-found errors must not 404)", rec.Code)
	}
}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }
