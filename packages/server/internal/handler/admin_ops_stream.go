package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// opsStreamer implements the SSE loop for /v1/admin/ops/stream. It
// doesn't use the Redis-backed event broker (events.Broker) because
// the ops data source is the DB itself — there's nothing to publish.
// A per-connection ticker pulls a fresh pulse from the store and
// sends it, so each admin gets the same view without cross-process
// pub/sub overhead.
//
// Tuning:
//   - pulseInterval: how often to recompute + emit. 2s matches the
//     River Rescuer's own cadence; faster than that adds noise.
//   - heartbeatInterval: keep-alive for middleboxes + client
//     liveness detection. Aligned with events.go (25s).
//   - The handler caps concurrent streams per server — see
//     maxStreams. Admin is low-volume so this is generous.
type opsStreamer struct {
	ops               store.AdminOpsStore
	pulseInterval     time.Duration
	heartbeatInterval time.Duration
	// pulseWindowMinutes passed to GetOpsPulse for every tick so the
	// throughput counters stay consistent across the stream.
	pulseWindowMinutes int
}

func newOpsStreamer(ops store.AdminOpsStore) *opsStreamer {
	return &opsStreamer{
		ops:                ops,
		pulseInterval:      2 * time.Second,
		heartbeatInterval:  25 * time.Second,
		pulseWindowMinutes: 60,
	}
}

// serve writes SSE events until the request context is cancelled.
// The handler has already written status 200 + headers.
func (s *opsStreamer) serve(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	// 1) Send an initial snapshot immediately so the client doesn't
	//    wait pulseInterval for the first paint.
	if pulse, err := s.ops.GetOpsPulse(ctx, s.pulseWindowMinutes); err == nil {
		s.writeEvent(w, flusher, "pulse", model.OpsStreamEvent{
			Type:        "pulse",
			GeneratedAt: pulse.GeneratedAt,
			Pulse:       pulse,
		})
	} else if !isContextDone(ctx, err) {
		slog.Warn("ops stream initial pulse failed", "error", err)
	}

	pulseTicker := time.NewTicker(s.pulseInterval)
	defer pulseTicker.Stop()
	heartbeatTicker := time.NewTicker(s.heartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			// Comment lines keep the connection alive without
			// advancing any application-level event cursor. Must
			// start with ':' per the SSE spec.
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-pulseTicker.C:
			// Use a short per-tick timeout so a slow pulse query
			// doesn't wedge the connection. If it does time out,
			// we just skip this tick — the next one retries.
			tickCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			pulse, err := s.ops.GetOpsPulse(tickCtx, s.pulseWindowMinutes)
			cancel()
			if err != nil {
				if isContextDone(ctx, err) {
					return
				}
				slog.Warn("ops stream pulse failed", "error", err)
				continue
			}
			if !s.writeEvent(w, flusher, "pulse", model.OpsStreamEvent{
				Type:        "pulse",
				GeneratedAt: pulse.GeneratedAt,
				Pulse:       pulse,
			}) {
				return
			}
		}
	}
}

// writeEvent marshals the payload and writes a single SSE frame.
// Returns false on write error so the caller can exit the loop.
func (s *opsStreamer) writeEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, payload any) bool {
	buf, err := json.Marshal(payload)
	if err != nil {
		slog.Error("ops stream marshal failed", "event", eventType, "error", err)
		return true // keep the stream alive; skip this frame
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, buf); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func isContextDone(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	if err == nil {
		return false
	}
	return ctx.Err() != nil
}
