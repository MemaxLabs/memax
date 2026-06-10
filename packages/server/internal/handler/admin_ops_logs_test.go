package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type fakeLogQuerier struct {
	env        string
	lines      []observability.LogLine
	err        error
	calls      int
	lastOpts   observability.QueryJobLogsOptions
	lastJobID  int64
	onCallHook func()
}

func (f *fakeLogQuerier) QueryEnv() string { return f.env }
func (f *fakeLogQuerier) QueryJobLogs(_ context.Context, jobID int64, opts observability.QueryJobLogsOptions) ([]observability.LogLine, error) {
	f.calls++
	f.lastJobID = jobID
	f.lastOpts = opts
	if f.onCallHook != nil {
		f.onCallHook()
	}
	return f.lines, f.err
}

func newOpsHandlerWithLogs(t *testing.T, ops store.AdminOpsStore, logs JobLogQuerier) *AdminOpsHandler {
	t.Helper()
	s := store.NewInMemoryStore()
	return NewAdminOpsHandler(s, ops, &fakeJobActioner{}, logs)
}

// --- one-shot GetJobLogs ---

func TestGetJobLogs_ReturnsLinesAndEnvironment(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{
		job: &model.JobDetail{
			JobSummary: model.JobSummary{ID: 7, Kind: "memory_process", CreatedAt: now.Add(-5 * time.Minute)},
		},
	}
	logs := &fakeLogQuerier{
		env: "staging",
		lines: []observability.LogLine{
			{Timestamp: now, Level: "INFO", Message: "job started"},
			{Timestamp: now.Add(time.Second), Level: "WARN", Message: "retrying", Fields: map[string]any{"attempt": 2.0}},
		},
	}
	h := newOpsHandlerWithLogs(t, opsStore, logs)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/7/logs", nil)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()
	h.GetJobLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Data struct {
			Environment string                  `json:"environment"`
			Lines       []observability.LogLine `json:"lines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v (body=%q)", err, rec.Body.String())
	}
	if env.Data.Environment != "staging" {
		t.Errorf("expected environment=staging, got %q", env.Data.Environment)
	}
	if len(env.Data.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(env.Data.Lines))
	}
	if env.Data.Lines[0].Message != "job started" {
		t.Errorf("first line wrong: %+v", env.Data.Lines[0])
	}
	if logs.lastJobID != 7 {
		t.Errorf("expected jobID=7, got %d", logs.lastJobID)
	}
}

func TestGetJobLogs_ReturnsNotFoundForUnknownJob(t *testing.T) {
	opsStore := &fakeOpsStore{jobErr: store.ErrJobNotFound}
	logs := &fakeLogQuerier{env: "staging"}
	h := newOpsHandlerWithLogs(t, opsStore, logs)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/999/logs", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()
	h.GetJobLogs(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	// Critical: we must not have spent a Loki query on an unknown job.
	if logs.calls != 0 {
		t.Errorf("expected no log query for unknown job, got %d", logs.calls)
	}
}

func TestGetJobLogs_503WhenLogsUnconfigured(t *testing.T) {
	opsStore := &fakeOpsStore{}
	h := newOpsHandlerWithLogs(t, opsStore, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/1/logs", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.GetJobLogs(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "logs_unavailable") {
		t.Errorf("expected logs_unavailable code: %s", rec.Body.String())
	}
}

func TestGetJobLogs_ClampsWindowAbove24Hours(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{
		job: &model.JobDetail{JobSummary: model.JobSummary{ID: 5, CreatedAt: now.Add(-5 * time.Minute)}},
	}
	logs := &fakeLogQuerier{env: "staging"}
	h := newOpsHandlerWithLogs(t, opsStore, logs)

	// Ask for a 2-month window — clampTimeRange must cap us at 24h.
	url := "/v1/admin/ops/jobs/5/logs?since=" + now.Add(-60*24*time.Hour).Format(time.RFC3339) +
		"&until=" + now.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", "5")
	rec := httptest.NewRecorder()
	h.GetJobLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := logs.lastOpts.Until.Sub(logs.lastOpts.Since)
	if got > 24*time.Hour {
		t.Errorf("window should be clamped to 24h, got %v", got)
	}
	if got < 23*time.Hour {
		t.Errorf("window unexpectedly small: %v", got)
	}
}

func TestGetJobLogs_502OnLokiError(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{job: &model.JobDetail{JobSummary: model.JobSummary{ID: 1, CreatedAt: now}}}
	logs := &fakeLogQuerier{env: "staging", err: context.DeadlineExceeded}
	h := newOpsHandlerWithLogs(t, opsStore, logs)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/1/logs", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.GetJobLogs(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

// --- StreamJobLogs ---

func TestStreamJobLogs_SendsSnapshotAndHeaders(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{
		job: &model.JobDetail{JobSummary: model.JobSummary{ID: 3, CreatedAt: now.Add(-time.Minute)}},
	}
	logs := &fakeLogQuerier{
		env: "staging",
		lines: []observability.LogLine{
			{Timestamp: now, Level: "INFO", Message: "job started"},
		},
	}
	h := newOpsHandlerWithLogs(t, opsStore, logs)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the stream after the first snapshot so the test is bounded.
	// Hook fires on every Loki call; the first call IS the snapshot,
	// cancel after returning so serve() observes ctx.Done on the next tick.
	logs.onCallHook = func() { cancel() }

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/3/logs/stream", nil).WithContext(ctx)
	req.SetPathValue("id", "3")
	rec := httptest.NewRecorder()
	h.StreamJobLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected SSE content-type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("missing snapshot event in body: %q", body)
	}
	if !strings.Contains(body, `"environment":"staging"`) {
		t.Errorf("missing environment in snapshot: %q", body)
	}
	if !strings.Contains(body, "job started") {
		t.Errorf("missing log line in snapshot: %q", body)
	}
}

// Regression for Codex re-review finding: the append ticker must
// query with direction=backward so that a saturated page (all
// pageLimit lines filled) returns the newest-N, not oldest-N. This
// is the only guarantee that ensures we don't lose new lines when
// activity bursts exceed the overlap replay window.
func TestStreamJobLogs_AppendQueriesBackward(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{
		job: &model.JobDetail{JobSummary: model.JobSummary{ID: 3, CreatedAt: now.Add(-time.Minute)}},
	}
	logs := &fakeLogQuerier{env: "staging"}
	// Capture all directions across calls.
	var directions []string
	logs.onCallHook = func() {
		directions = append(directions, logs.lastOpts.Direction)
	}
	h := newOpsHandlerWithLogs(t, opsStore, logs)
	// Collapse the streamer's pollInterval so we don't wait real
	// seconds before the first append query fires.
	h.logStream.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	// Wait for at least one append poll before cancelling.
	appendFired := make(chan struct{}, 1)
	logs.onCallHook = func() {
		directions = append(directions, logs.lastOpts.Direction)
		if len(directions) >= 2 {
			select {
			case appendFired <- struct{}{}:
			default:
			}
		}
	}

	go func() {
		<-appendFired
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/3/logs/stream", nil).WithContext(ctx)
	req.SetPathValue("id", "3")
	rec := httptest.NewRecorder()
	h.StreamJobLogs(rec, req)

	if len(directions) < 2 {
		t.Fatalf("expected at least 2 queries (initial + 1 append), got %d: %v", len(directions), directions)
	}
	// Initial snapshot uses default direction (forward/empty).
	// Every subsequent append query must be backward.
	for i, dir := range directions[1:] {
		if dir != "backward" {
			t.Errorf("append query #%d direction = %q, want %q", i+1, dir, "backward")
		}
	}
}

// Regression for Codex round-3 finding: when the append query uses
// direction=backward, the query client returns newest-first. The
// streamer MUST re-sort to chronological before emitting the SSE
// append payload so the UI paints in order.
func TestStreamJobLogs_AppendLinesAreChronological(t *testing.T) {
	now := time.Now().UTC()
	opsStore := &fakeOpsStore{
		job: &model.JobDetail{JobSummary: model.JobSummary{ID: 9, CreatedAt: now.Add(-time.Minute)}},
	}

	// First call = snapshot (empty). Second call = append, returns
	// three lines newest-first as a real backward query would.
	callIdx := 0
	newest := now.Add(500 * time.Millisecond)
	middle := now.Add(300 * time.Millisecond)
	oldest := now.Add(100 * time.Millisecond)
	logs := &fakeLogQuerier{env: "staging"}
	logs.onCallHook = func() {
		callIdx++
		if callIdx == 1 {
			logs.lines = nil
		} else {
			logs.lines = []observability.LogLine{
				{Timestamp: newest, Level: "INFO", Message: "third"},
				{Timestamp: middle, Level: "INFO", Message: "second"},
				{Timestamp: oldest, Level: "INFO", Message: "first"},
			}
		}
	}
	h := newOpsHandlerWithLogs(t, opsStore, logs)
	h.logStream.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	appended := make(chan struct{}, 1)
	// Once we've seen the append query execute, wait a moment for
	// the SSE frame to be written, then cancel.
	origHook := logs.onCallHook
	logs.onCallHook = func() {
		origHook()
		if callIdx >= 2 {
			select {
			case appended <- struct{}{}:
			default:
			}
		}
	}
	go func() {
		<-appended
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/9/logs/stream", nil).WithContext(ctx)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()
	h.StreamJobLogs(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: append") {
		t.Fatalf("append event not emitted: %s", body)
	}
	// Extract the append frame and parse its data payload.
	appendFrameStart := strings.Index(body, "event: append")
	if appendFrameStart == -1 {
		t.Fatal("append frame not found")
	}
	dataStart := strings.Index(body[appendFrameStart:], "data: ")
	if dataStart == -1 {
		t.Fatal("data line not found in append frame")
	}
	dataStart += appendFrameStart + len("data: ")
	dataEnd := strings.Index(body[dataStart:], "\n\n")
	if dataEnd == -1 {
		t.Fatal("frame terminator not found")
	}
	jsonPayload := body[dataStart : dataStart+dataEnd]

	var payload struct {
		Lines []observability.LogLine `json:"lines"`
	}
	if err := json.Unmarshal([]byte(jsonPayload), &payload); err != nil {
		t.Fatalf("decode: %v (raw=%q)", err, jsonPayload)
	}
	if len(payload.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %+v", len(payload.Lines), payload.Lines)
	}
	// Must be chronological: first < second < third.
	if payload.Lines[0].Message != "first" ||
		payload.Lines[1].Message != "second" ||
		payload.Lines[2].Message != "third" {
		t.Errorf("append payload not chronological: %+v", payload.Lines)
	}
	for i := 1; i < len(payload.Lines); i++ {
		if payload.Lines[i].Timestamp.Before(payload.Lines[i-1].Timestamp) {
			t.Errorf("timestamp order violated at index %d", i)
		}
	}
}

func TestStreamJobLogs_503WhenUnconfigured(t *testing.T) {
	h := newOpsHandlerWithLogs(t, &fakeOpsStore{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/jobs/1/logs/stream", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.StreamJobLogs(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// --- helpers ---

func TestClampTimeRange_NormalizesDegenerate(t *testing.T) {
	now := time.Now()
	// until < since → push until to since + min
	s, u := clampTimeRange(now, now.Add(-time.Hour))
	if u.Before(s) {
		t.Errorf("until < since after clamp: %v < %v", u, s)
	}

	// 0-width window → at least min
	s, u = clampTimeRange(now, now)
	if u.Sub(s) < time.Minute {
		t.Errorf("0-width not clamped to min: %v", u.Sub(s))
	}

	// 48h window → clamped to 24h
	s, u = clampTimeRange(now.Add(-48*time.Hour), now)
	if u.Sub(s) > 24*time.Hour {
		t.Errorf("48h window not clamped: %v", u.Sub(s))
	}
}

func TestLineHash_DedupesIdenticalLines(t *testing.T) {
	ts := time.Now()
	a := observability.LogLine{Timestamp: ts, Level: "INFO", Message: "hello"}
	b := observability.LogLine{Timestamp: ts, Level: "INFO", Message: "hello"}
	c := observability.LogLine{Timestamp: ts, Level: "WARN", Message: "hello"}
	if lineHash(a) != lineHash(b) {
		t.Error("identical lines must hash equal")
	}
	if lineHash(a) == lineHash(c) {
		t.Error("different-level lines must hash differently")
	}
}
