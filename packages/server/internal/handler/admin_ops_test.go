package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// These tests exercise the handler glue in isolation: a fake ops
// store + fake river actioner are sufficient to check HTTP contract,
// error translation, and the SSE envelope. Integration tests that
// hit a real River fleet live in internal/store/postgres_ops_test.go.

type fakeOpsStore struct {
	pulse      *model.OpsPulse
	pulseErr   error
	jobs       *model.JobListResponse
	jobsErr    error
	job        *model.JobDetail
	jobErr     error
	stuck      []model.StuckMemory
	stuckErr   error
	failed     []model.FailedIngestion
	failedErr  error
	sources    []model.IngestionBySource
	sourcesErr error
	audits     []model.AdminAuditEntry
}

func (f *fakeOpsStore) GetOpsPulse(context.Context, int) (*model.OpsPulse, error) {
	return f.pulse, f.pulseErr
}
func (f *fakeOpsStore) ListOpsJobs(context.Context, model.JobListOpts) (*model.JobListResponse, error) {
	return f.jobs, f.jobsErr
}
func (f *fakeOpsStore) GetOpsJob(context.Context, int64) (*model.JobDetail, error) {
	return f.job, f.jobErr
}
func (f *fakeOpsStore) ListOpsStuckMemories(context.Context, int, int) ([]model.StuckMemory, error) {
	return f.stuck, f.stuckErr
}
func (f *fakeOpsStore) ListOpsFailedIngestions(context.Context, int, int) ([]model.FailedIngestion, error) {
	return f.failed, f.failedErr
}
func (f *fakeOpsStore) ListOpsIngestionBySource(context.Context, int) ([]model.IngestionBySource, error) {
	return f.sources, f.sourcesErr
}
func (f *fakeOpsStore) AppendAdminAudit(_ context.Context, entry *model.AdminAuditEntry) error {
	f.audits = append(f.audits, *entry)
	return nil
}

type fakeJobActioner struct {
	retried   []int64
	cancelled []int64
	retryErr  error
}

func (f *fakeJobActioner) JobRetry(_ context.Context, id int64) error {
	if f.retryErr != nil {
		return f.retryErr
	}
	f.retried = append(f.retried, id)
	return nil
}
func (f *fakeJobActioner) JobCancel(_ context.Context, id int64) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

func newOpsHandlerForTest(t *testing.T, ops store.AdminOpsStore, actioner jobActioner) *AdminOpsHandler {
	t.Helper()
	s := store.NewInMemoryStore()
	return NewAdminOpsHandler(s, ops, actioner, nil)
}

func TestAdminOps_GetPulse_HappyPath(t *testing.T) {
	fake := &fakeOpsStore{
		pulse: &model.OpsPulse{
			GeneratedAt: time.Now().UTC(),
			Workers:     model.OpsWorkers{Healthy: 2, Leader: "worker-a"},
			Queues:      []model.OpsQueueDepth{{Queue: "default", Running: 3}},
			Throughput:  model.OpsThroughput{WindowMinutes: 60, MemoriesProcessed: 12},
			Ingestion:   model.OpsIngestionCards{InProcessing: 1},
		},
	}
	h := newOpsHandlerForTest(t, fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/pulse", nil)
	rec := httptest.NewRecorder()
	h.GetPulse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data *model.OpsPulse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data == nil || resp.Data.Workers.Healthy != 2 {
		t.Fatalf("unexpected pulse: %#v", resp.Data)
	}
	if len(resp.Data.Queues) != 1 || resp.Data.Queues[0].Running != 3 {
		t.Errorf("queue depth lost in serialization")
	}
}

func TestAdminOps_ListJobs_Defaults(t *testing.T) {
	fake := &fakeOpsStore{jobs: &model.JobListResponse{Jobs: nil}}
	h := newOpsHandlerForTest(t, fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs", nil)
	rec := httptest.NewRecorder()
	h.ListJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Contract: `jobs` is always an array, never null — important for
	// the client so it doesn't have to null-check before .map.
	var wire struct {
		Data struct {
			Jobs []model.JobSummary `json:"jobs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if wire.Data.Jobs == nil {
		t.Errorf("jobs should be [], not null")
	}
}

func TestAdminOps_GetJob_NotFound(t *testing.T) {
	fake := &fakeOpsStore{jobErr: store.ErrJobNotFound}
	h := newOpsHandlerForTest(t, fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/42", nil)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()
	h.GetJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminOps_RetryJob_NoRiver_Returns503(t *testing.T) {
	// When the River client isn't wired (dev or degraded), job
	// actions must fail fast with 503 rather than a generic 500 —
	// the UI can surface "job actions temporarily unavailable"
	// based on the status code.
	h := newOpsHandlerForTest(t, &fakeOpsStore{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/jobs/1/retry", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.RetryJob(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAdminOps_RetryJob_HappyPath(t *testing.T) {
	actioner := &fakeJobActioner{}
	h := newOpsHandlerForTest(t, &fakeOpsStore{}, actioner)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/jobs/99/retry", nil)
	req.SetPathValue("id", "99")
	rec := httptest.NewRecorder()
	h.RetryJob(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(actioner.retried) != 1 || actioner.retried[0] != 99 {
		t.Errorf("expected retry of job 99, got %v", actioner.retried)
	}
}

func TestAdminOps_RetryJob_RiverFailure(t *testing.T) {
	actioner := &fakeJobActioner{retryErr: errors.New("boom")}
	h := newOpsHandlerForTest(t, &fakeOpsStore{}, actioner)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/jobs/1/retry", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.RetryJob(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAdminOps_ParseJobID_Invalid(t *testing.T) {
	h := newOpsHandlerForTest(t, &fakeOpsStore{}, &fakeJobActioner{})
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/not-a-number", nil)
	req.SetPathValue("id", "not-a-number")
	rec := httptest.NewRecorder()
	h.GetJob(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAdminOps_ListJobs_InvalidCursorReturns400(t *testing.T) {
	// Bad cursor is user input — must surface as 400, not 500, so
	// the client knows to fix the request (e.g. reset pagination).
	fake := &fakeOpsStore{jobsErr: store.ErrInvalidJobCursor}
	h := newOpsHandlerForTest(t, fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs?cursor=garbage", nil)
	rec := httptest.NewRecorder()
	h.ListJobs(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAdminOps_RetryJob_WritesAudit(t *testing.T) {
	fake := &fakeOpsStore{}
	actioner := &fakeJobActioner{}
	h := newOpsHandlerForTest(t, fake, actioner)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/jobs/42/retry", nil)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()
	h.RetryJob(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// The audit insert happens on the narrow AdminOpsStore
	// (admin_audit), not comms_audit. Regression guard against the
	// earlier bug where we wrote into comms_audit with a bigint
	// resource_id and the insert failed silently.
	if len(fake.audits) != 1 || fake.audits[0].Action != "retry" || fake.audits[0].ResourceID != "42" {
		t.Errorf("expected one retry audit for job 42, got %+v", fake.audits)
	}
	if fake.audits[0].ResourceType != "ops_job" {
		t.Errorf("resource_type = %q, want ops_job", fake.audits[0].ResourceType)
	}
}

func TestAdminOps_IngestionSnapshot_NullableFields(t *testing.T) {
	// Empty stuck/failed/source slices must serialize as [], not null.
	// The client treats null as "not loaded yet"; arrays are "loaded,
	// empty" — they're different UI states.
	fake := &fakeOpsStore{}
	h := newOpsHandlerForTest(t, fake, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/memory-ingestion", nil)
	rec := httptest.NewRecorder()
	h.IngestionSnapshot(rec, req)
	body := rec.Body.String()
	for _, field := range []string{`"stuck_memories":[]`, `"failed_ingestions":[]`, `"by_source":[]`} {
		if !strings.Contains(body, field) {
			t.Errorf("missing %q in response: %s", field, body)
		}
	}
}
