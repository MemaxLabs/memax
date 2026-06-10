package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Logs endpoints — read-only surface over Loki, scoped to one River
// job id. Both endpoints share the same env-isolation story: the
// query client the handler holds baked MEMAX_ENV in at construction,
// so there is no request-level input that can reach another env's
// logs. The UI can display which env it's looking at via the
// `environment` field on every response, sourced from the server.

// timeRangeForJob picks a sensible default [since, until] for a
// given job. We start at created_at − 1 minute (leave some slack for
// slog push batching) and end at max(finalized_at, now). Called with
// JobDetail, so this is cheap and doesn't require another DB round
// trip.
func timeRangeForJob(detail *model.JobDetail) (since, until time.Time) {
	since = detail.CreatedAt.Add(-time.Minute)
	if detail.FinalizedAt != nil {
		until = detail.FinalizedAt.Add(time.Minute)
	} else {
		until = time.Now()
	}
	return since, until
}

// clampTimeRange keeps the window within [1min, 24h] regardless of
// caller input so a runaway query can't exhaust Loki's quota.
func clampTimeRange(since, until time.Time) (time.Time, time.Time) {
	const (
		minWindow = time.Minute
		maxWindow = 24 * time.Hour
	)
	if until.Before(since) {
		until = since.Add(minWindow)
	}
	if until.Sub(since) < minWindow {
		until = since.Add(minWindow)
	}
	if until.Sub(since) > maxWindow {
		since = until.Add(-maxWindow)
	}
	return since, until
}

// GetJobLogs returns one page of logs for a specific River job id.
// GET /v1/admin/ops/jobs/{id}/logs[?since=RFC3339&until=RFC3339&limit=N]
func (h *AdminOpsHandler) GetJobLogs(w http.ResponseWriter, r *http.Request) {
	if h.logs == nil {
		writeError(w, http.StatusServiceUnavailable, "logs_unavailable", "Log querying is not configured on this environment.")
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	// Confirm the job actually exists before spending a Loki query.
	// Also gives us created_at/finalized_at for the default range.
	detail, err := h.ops.GetOpsJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Job not found.")
			return
		}
		slog.ErrorContext(r.Context(), "ops job lookup for logs failed", "job_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load job.")
		return
	}

	since, until := timeRangeForJob(detail)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			since = t
		}
	}
	if raw := r.URL.Query().Get("until"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			until = t
		}
	}
	since, until = clampTimeRange(since, until)

	limit := intQuery(r, "limit", 500)

	queryCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	lines, err := h.logs.QueryJobLogs(queryCtx, id, observability.QueryJobLogsOptions{
		Since: since,
		Until: until,
		Limit: limit,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "ops job logs query failed", "job_id", id, "error", err)
		writeError(w, http.StatusBadGateway, "logs_query_failed", "Could not read logs from the log store.")
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.JobLogsResponse{
		Environment: h.logs.QueryEnv(),
		Since:       since,
		Until:       until,
		Lines:       lines,
	}})
}

// StreamJobLogs pushes new log lines over SSE as they appear in Loki.
// GET /v1/admin/ops/jobs/{id}/logs/stream
//
// Contract:
//   - Initial event "snapshot" with logs from job.CreatedAt → now.
//   - Subsequent "append" events carry only lines newer than the
//     last seen timestamp. Dedupe is by (timestamp, line-hash).
//   - Heartbeat every 25s so middleboxes don't drop the connection.
//   - Closes on client disconnect, 10 min max, or Loki quota error.
func (h *AdminOpsHandler) StreamJobLogs(w http.ResponseWriter, r *http.Request) {
	if h.logStream == nil {
		writeError(w, http.StatusServiceUnavailable, "logs_unavailable", "Log streaming is not configured on this environment.")
		return
	}
	id, ok := parseJobID(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sse_unsupported", "Streaming is not supported on this connection.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	h.logStream.serve(r.Context(), w, flusher, id)
}

// jobLogsStreamer owns the per-connection polling loop that tails
// Loki for new log lines. Separate type so admin_ops.go stays focused
// on the pulse/jobs read path.
type jobLogsStreamer struct {
	logs              JobLogQuerier
	ops               store.AdminOpsStore
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	maxDuration       time.Duration
	// pageLimit bounds each Loki page so even a runaway job producing
	// massive log volumes doesn't choke the stream.
	pageLimit int
}

func newJobLogsStreamer(logs JobLogQuerier, ops store.AdminOpsStore) *jobLogsStreamer {
	return &jobLogsStreamer{
		logs:              logs,
		ops:               ops,
		pollInterval:      3 * time.Second, // matches Loki's ~3–5s ingest latency
		heartbeatInterval: 25 * time.Second,
		maxDuration:       10 * time.Minute,
		pageLimit:         500,
	}
}

func (s *jobLogsStreamer) serve(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, jobID int64) {
	ctx, cancel := context.WithTimeout(ctx, s.maxDuration)
	defer cancel()

	// Look up the job once for its time floor. The stream is scoped
	// to a specific job id; the range grows forward each poll.
	detail, err := s.ops.GetOpsJob(ctx, jobID)
	if err != nil {
		writeSSEEvent(w, flusher, "error", map[string]string{"error": "job_not_found"})
		return
	}
	since, _ := timeRangeForJob(detail)
	lastSeen := since // advances forward as we send lines

	// Initial snapshot: fetch everything up to now, send as one
	// "snapshot" event so the client paints immediately.
	initialCtx, initialCancel := context.WithTimeout(ctx, 8*time.Second)
	initial, err := s.logs.QueryJobLogs(initialCtx, jobID, observability.QueryJobLogsOptions{
		Since: since,
		Until: time.Now(),
		Limit: s.pageLimit,
	})
	initialCancel()
	if err != nil {
		writeSSEEvent(w, flusher, "error", map[string]string{"error": "initial_query_failed", "detail": err.Error()})
		return
	}
	if !writeSSEEvent(w, flusher, "snapshot", model.JobLogsStreamEvent{
		Environment: s.logs.QueryEnv(),
		Lines:       initial,
	}) {
		// Client disconnected before we could paint the first
		// frame. No point running the poll loop.
		return
	}
	if len(initial) > 0 {
		lastSeen = initial[len(initial)-1].Timestamp
	}

	// Dedupe cache of line hashes from the snapshot — prevents
	// sending the same line twice across the snapshot→append boundary.
	seen := make(map[string]struct{}, len(initial))
	for _, line := range initial {
		seen[lineHash(line)] = struct{}{}
	}

	pollTicker := time.NewTicker(s.pollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(s.heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			tickCtx, tickCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
			// Replay a small overlap so out-of-order pushes don't
			// leave gaps; the seen-hash set dedupes the overlap.
			//
			// direction=backward returns newest-first. The page
			// limit then caps the NEWEST N lines in the window
			// rather than the OLDEST — so if a burst of activity
			// produces more than pageLimit dups-plus-news, we
			// still surface the newest news instead of returning
			// pageLimit old dups and skipping the actual new lines.
			// The only scenario we can still drop is "more than
			// pageLimit truly-new lines in a single 3s tick",
			// which is ~167 lines/sec sustained — well outside
			// any realistic memax job, and if we ever do hit it
			// the dropped lines would still be in Loki for manual
			// Grafana inspection.
			queryStart := lastSeen.Add(-2 * time.Second)
			queryEnd := time.Now()
			lines, err := s.logs.QueryJobLogs(tickCtx, jobID, observability.QueryJobLogsOptions{
				Since:     queryStart,
				Until:     queryEnd,
				Limit:     s.pageLimit,
				Direction: "backward",
			})
			tickCancel()
			if err != nil {
				// Stream is best-effort; transient Loki errors
				// shouldn't kill the connection.
				slog.WarnContext(ctx, "ops logs stream tick failed", "job_id", jobID, "error", err)
				continue
			}
			// QueryJobLogs returns newest-first when direction is
			// "backward" — we need chronological for the SSE
			// append payload because the UI paints each line in
			// order. Dedupe + lastSeen bookkeeping work either
			// way, but we sort `fresh` ascending before emission
			// below.
			fresh := make([]observability.LogLine, 0, len(lines))
			for _, line := range lines {
				h := lineHash(line)
				if _, dup := seen[h]; dup {
					continue
				}
				seen[h] = struct{}{}
				fresh = append(fresh, line)
				if line.Timestamp.After(lastSeen) {
					lastSeen = line.Timestamp
				}
			}
			if len(fresh) == 0 {
				continue
			}
			// Normalise to chronological order. Backward-direction
			// queries arrive newest-first; the UI appends lines to
			// its buffer as received, so we must flip here or the
			// panel would paint jumbled timelines.
			sort.Slice(fresh, func(i, j int) bool {
				return fresh[i].Timestamp.Before(fresh[j].Timestamp)
			})
			if !writeSSEEvent(w, flusher, "append", model.JobLogsStreamEvent{
				Environment: s.logs.QueryEnv(),
				Lines:       fresh,
			}) {
				// Client disconnected mid-write — stop polling.
				return
			}
		}
	}
}

// lineHash is a cheap identity for dedupe. timestamp alone is unsafe
// because slog can emit multiple records with the same nanosecond ts
// when the worker logs in tight loops; msg+level+ts gets us there.
func lineHash(line observability.LogLine) string {
	return strconv.FormatInt(line.Timestamp.UnixNano(), 10) + "\x00" + line.Level + "\x00" + line.Message
}

// writeSSEEvent writes a single SSE frame. Returns true on success
// OR on marshal failure (programmer bug, not a transport error —
// skipping one frame but keeping the stream alive is the right
// recovery). Returns false only when the HTTP write itself failed,
// i.e. the client is gone. Callers in a streaming loop should exit
// on false rather than continue polling into a dead connection.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, payload any) bool {
	buf, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("sse marshal failed", "event", eventType, "error", err)
		// A marshal failure is a programmer bug, not a transport
		// error; keep the stream alive so the next frame can try.
		return true
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, buf); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
