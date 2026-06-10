package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminOpsHandler serves /v1/admin/ops* endpoints — the operations
// dashboard embedded in the admin panel. All read endpoints project
// River's tables + our memories table into product-framed shapes
// (see store.AdminOpsStore); destructive endpoints (retry, cancel,
// force-active) go through the River client API + the store's own
// mutations, and every destructive call writes an audit row.
//
// The handler deliberately takes a narrow AdminOpsStore instead of
// store.Store so test fixtures don't have to stub a pile of River
// queries. It also takes the broad store.Store for audit + memory
// updates that live on the main interface.
type AdminOpsHandler struct {
	store     store.Store
	ops       store.AdminOpsStore
	river     jobActioner   // typically *queue.Client; interface keeps tests sane
	logs      JobLogQuerier // nil when Loki is not configured
	streamer  *opsStreamer
	logStream *jobLogsStreamer
}

// JobLogQuerier is the Loki-ish surface for reading job logs. Kept
// as an interface so tests can stub responses without touching HTTP.
// Exported because serverapp needs to name it when wiring the
// optional QueryClient (passing a typed-nil *QueryClient would hide
// the nil from our in-handler nil check).
type JobLogQuerier interface {
	QueryJobLogs(ctx context.Context, jobID int64, opts observability.QueryJobLogsOptions) ([]observability.LogLine, error)
	QueryEnv() string
}

// jobActioner is the River-client-ish surface for destructive job
// operations. Kept as an interface so the ops handler can be tested
// without spinning up a real River.
type jobActioner interface {
	JobRetry(ctx context.Context, id int64) error
	JobCancel(ctx context.Context, id int64) error
}

// NewAdminOpsHandler builds the handler. river may be nil in tests
// or in degraded modes; the action endpoints return 503 if it's
// unset, which matches how admin_notifications.go handles missing
// dependencies. Similarly logs may be nil when Loki credentials are
// unset (dev without LOKI_URL) — the logs endpoints surface a clear
// "not configured" message instead of crashing.
func NewAdminOpsHandler(s store.Store, ops store.AdminOpsStore, river jobActioner, logs JobLogQuerier) *AdminOpsHandler {
	h := &AdminOpsHandler{
		store:    s,
		ops:      ops,
		river:    river,
		logs:     logs,
		streamer: newOpsStreamer(ops),
	}
	if logs != nil {
		h.logStream = newJobLogsStreamer(logs, ops)
	}
	return h
}

// --- pulse / ingestion / jobs (read endpoints) ---

// GetPulse returns the landing-view summary.
// GET /v1/admin/ops/pulse
func (h *AdminOpsHandler) GetPulse(w http.ResponseWriter, r *http.Request) {
	windowMin := intQuery(r, "window_minutes", 60)
	pulse, err := h.ops.GetOpsPulse(r.Context(), windowMin)
	if err != nil {
		slog.Error("ops pulse failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load ops pulse.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: pulse})
}

// ListJobs returns a filtered/paginated job list.
// GET /v1/admin/ops/jobs
func (h *AdminOpsHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	opts := model.JobListOpts{
		States:      splitCSV(r.URL.Query().Get("states")),
		Kinds:       splitCSV(r.URL.Query().Get("kinds")),
		KindSearch:  strings.TrimSpace(r.URL.Query().Get("kind_search")),
		Queues:      splitCSV(r.URL.Query().Get("queues")),
		QueueSearch: strings.TrimSpace(r.URL.Query().Get("queue_search")),
		Cursor:      strings.TrimSpace(r.URL.Query().Get("cursor")),
		Limit:       intQuery(r, "limit", 50),
	}
	if ts := r.URL.Query().Get("created_after"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			opts.CreatedAfter = &t
		}
	}
	if ts := r.URL.Query().Get("created_before"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			opts.CreatedBefore = &t
		}
	}

	resp, err := h.ops.ListOpsJobs(r.Context(), opts)
	if err != nil {
		if errors.Is(err, store.ErrInvalidJobCursor) {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "Job list cursor is malformed.")
			return
		}
		slog.Error("ops list jobs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not list jobs.")
		return
	}
	if resp.Jobs == nil {
		resp.Jobs = []model.JobSummary{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// GetJob returns full detail for one job.
// GET /v1/admin/ops/jobs/{id}
func (h *AdminOpsHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	detail, err := h.ops.GetOpsJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Job not found.")
			return
		}
		slog.Error("ops get job failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load job.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: detail})
}

// IngestionSnapshot returns the memory-ingestion detail views.
// GET /v1/admin/ops/memory-ingestion
func (h *AdminOpsHandler) IngestionSnapshot(w http.ResponseWriter, r *http.Request) {
	limit := intQuery(r, "limit", 50)
	sinceMin := intQuery(r, "since_minutes", 60)
	stuckMin := intQuery(r, "stuck_age_seconds", 300)

	stuck, err := h.ops.ListOpsStuckMemories(r.Context(), stuckMin, limit)
	if err != nil {
		slog.Error("ops stuck memories failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load stuck memories.")
		return
	}
	failed, err := h.ops.ListOpsFailedIngestions(r.Context(), sinceMin, limit)
	if err != nil {
		slog.Error("ops failed ingestions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load failed ingestions.")
		return
	}
	sources, err := h.ops.ListOpsIngestionBySource(r.Context(), sinceMin)
	if err != nil {
		slog.Error("ops ingestion by source failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load source breakdown.")
		return
	}

	if stuck == nil {
		stuck = []model.StuckMemory{}
	}
	if failed == nil {
		failed = []model.FailedIngestion{}
	}
	if sources == nil {
		sources = []model.IngestionBySource{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"stuck_memories":    stuck,
		"failed_ingestions": failed,
		"by_source":         sources,
		"window_minutes":    sinceMin,
		"stuck_age_seconds": stuckMin,
	}})
}

// --- destructive actions (audit-logged) ---

// RetryJob asks River to re-run a job immediately.
// POST /v1/admin/ops/jobs/{id}/retry
func (h *AdminOpsHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	if h.river == nil {
		writeError(w, http.StatusServiceUnavailable, "river_unavailable", "Job actions are unavailable.")
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	if err := h.river.JobRetry(r.Context(), id); err != nil {
		slog.Error("ops job retry failed", "job_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "retry_failed", "Could not retry job.")
		return
	}
	h.writeOpsAudit(r.Context(), "job", strconv.FormatInt(id, 10), "retry", GetUserID(r), map[string]any{
		"job_id": id,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"ok": true}})
}

// CancelJob asks River to cancel a job.
// POST /v1/admin/ops/jobs/{id}/cancel
func (h *AdminOpsHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	if h.river == nil {
		writeError(w, http.StatusServiceUnavailable, "river_unavailable", "Job actions are unavailable.")
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	if err := h.river.JobCancel(r.Context(), id); err != nil {
		slog.Error("ops job cancel failed", "job_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "cancel_failed", "Could not cancel job.")
		return
	}
	h.writeOpsAudit(r.Context(), "job", strconv.FormatInt(id, 10), "cancel", GetUserID(r), map[string]any{
		"job_id": id,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"ok": true}})
}

// ForceActiveMemory moves a stuck memory from 'processing' to
// 'active' with an operator-supplied note in the summary. Useful
// when a memory_process job has been lost or retries exhausted
// outside River's knowledge (e.g. worker hard-crashed mid-flight).
// POST /v1/admin/ops/memories/{id}/force-active
func (h *AdminOpsHandler) ForceActiveMemory(w http.ResponseWriter, r *http.Request) {
	memoryID := r.PathValue("id")
	if memoryID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Memory id is required.")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body is fine

	ownerID := GetUserID(r)
	// Look the memory up via the admin-scope helper (no owner filter).
	memory, err := h.store.GetMemoryForAdmin(memoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Memory not found.")
		return
	}
	if memory.State != "processing" {
		writeError(w, http.StatusConflict, "not_processing", fmt.Sprintf("Memory is already in state %q.", memory.State))
		return
	}
	memory.State = "active"
	if body.Note != "" {
		memory.Summary = "Force-activated by admin: " + body.Note
	} else if memory.Summary == "" {
		memory.Summary = "Force-activated by admin (no note provided)."
	}
	if err := h.store.UpdateMemory(memory); err != nil {
		slog.Error("ops force-active failed", "memory_id", memoryID, "error", err)
		writeError(w, http.StatusInternalServerError, "update_failed", "Could not force-activate memory.")
		return
	}
	h.writeOpsAudit(r.Context(), "memory", memoryID, "force_active", ownerID, map[string]any{
		"previous_state": "processing",
		"note":           body.Note,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"ok": true}})
}

// --- SSE stream ---

// Stream emits live pulse updates over Server-Sent Events.
// GET /v1/admin/ops/stream
//
// Contract:
//   - Initial event of type "pulse" with a full snapshot.
//   - Subsequent "pulse" events every ~2s while the connection holds.
//   - "heartbeat" event every 25s so middleboxes don't time out idle
//     connections, and so the client can detect dead connections
//     without waiting for the next pulse tick.
//   - Closes on context cancel (client disconnect or server shutdown).
func (h *AdminOpsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sse_unsupported", "Streaming is not supported on this connection.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable Nginx / Cloudflare buffering so every flush reaches the
	// client immediately.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	h.streamer.serve(r.Context(), w, flusher)
}

// --- helpers ---

func parseJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Job id is required.")
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Job id must be numeric.")
		return 0, false
	}
	return id, true
}

func intQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// writeOpsAudit inserts one admin_audit row for a destructive ops
// action. admin_audit is separate from comms_audit because:
//  1. Its resource_id is text (bigint job IDs don't fit
//     comms_audit.resource_id's uuid cast, which would cause
//     silent insert failures on retry/cancel).
//  2. Its resource_type is free-text, so new admin surfaces can
//     add action rows without migrating an enum.
//
// Non-fatal on failure: a failed audit is logged but doesn't
// roll back the action — same contract as comms_audit.
func (h *AdminOpsHandler) writeOpsAudit(ctx context.Context, resourceType, resourceID, action, actorID string, meta map[string]any) {
	var metadata json.RawMessage
	if len(meta) > 0 {
		if buf, err := json.Marshal(meta); err == nil {
			metadata = buf
		}
	}
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	entry := &model.AdminAuditEntry{
		ID:           uuid.NewString(),
		ResourceType: "ops_" + resourceType, // keeps ops audits filterable separately
		ResourceID:   resourceID,
		Action:       action,
		ActorID:      actor,
		Metadata:     metadata,
	}
	if err := h.ops.AppendAdminAudit(ctx, entry); err != nil {
		slog.Warn("ops audit insert failed", "action", action, "resource", resourceType, "error", err)
	}
}
