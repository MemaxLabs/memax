// Phase 3.4b2.b — chat SSE stream handler.
//
// GET /v1/chat/sessions/{id}/messages/{msg_id}/stream serves the
// SSE stream a client connects to after POST /messages returns
// 202 with a `resume_url`. The handler is replay-aware: a client
// supplies `Last-Event-ID` (per HTML5 SSE) and we replay from
// chat_message_events.seq > Last-Event-ID, then tail live events
// as the worker writes them.
//
// **Wire event translation.** The replay buffer stores SDK-shaped
// events (`assistant`, `tool_use`, `result`); the wire surface
// the client sees is plan 24's chat-public event vocabulary
// (`model.delta`, `tool.call`, `message.completed`). Codex's
// Phase 3.4b2.a v2 review pinned this boundary explicitly:
// passing SDK event types straight through would tie our public
// wire format to the SDK's contract, which is the wrong layering.
//
// **At-least-once with client-side dedupe.** Clients dedupe on
// (msg_id, seq) — every SSE frame we emit carries `id: <seq>` so
// the standard EventSource resume mechanism reads `Last-Event-ID`
// and skips already-seen events on reconnect.
//
// **24h replay window.** chat_message_events rows are kept for
// 24h after creation (purge sweeper lands in Phase 3.5). A client
// that connects past the window for a terminal message gets a
// `410 chat_replay_expired` body with `{message_id, status,
// content_url}` so it can re-fetch the final persisted content
// via GET /v1/chat/sessions/{id}/messages/{msg_id} instead of
// silently seeing a partial stream.

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

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// chatStreamPollInterval is the cadence at which the handler
// polls chat_message_events for new rows while the message is
// still in-flight. 250ms balances responsiveness (the user
// perceives < 500ms latency for text deltas) against DB load
// at 100s of concurrent streams. Phase 3.4b3+ will add a Redis
// broker subscription so the poll becomes the fallback path for
// missed pub-sub frames, not the primary one.
const chatStreamPollInterval = 250 * time.Millisecond

// chatStreamHeartbeatInterval is the cadence for `: heartbeat`
// SSE comments when nothing else is being written. Keeps the
// connection alive through proxies that close idle streams.
// 25s is below the typical 30s idle-timeout floor (nginx default,
// Cloudflare, Vercel).
const chatStreamHeartbeatInterval = 25 * time.Second

// chatStreamMaxDuration caps how long a single SSE connection
// stays open. The chat profile allows ≤120s of agent work plus
// finalize round-trips; a generous 5-minute ceiling absorbs the
// worst case while preventing one stuck connection from holding
// a goroutine forever. After max-duration, the handler closes
// cleanly and the client's EventSource auto-reconnects with the
// last seq.
const chatStreamMaxDuration = 5 * time.Minute

// chatStreamReplayWindow mirrors plan 24's "24h replay buffer"
// retention. Phase 3.5's purge sweeper enforces it at the row
// level; the handler enforces it at connect-time (a client
// connecting past the window for a terminal message gets
// chat_replay_expired immediately rather than reading rows the
// sweeper hasn't gotten to yet).
const chatStreamReplayWindow = 24 * time.Hour

// Stream handles GET /v1/chat/sessions/{id}/messages/{msg_id}/stream.
//
// Lifecycle:
//
//  1. Verify owner + load the assistant message.
//  2. If terminal AND past the replay window → 410.
//  3. Set SSE response headers; obtain http.Flusher.
//  4. Loop:
//     - Read chat_message_events with seq > lastSeq.
//     - Translate each SDK event to a public wire event; write SSE.
//     - If message is terminal AND we drained all events → write
//     `message.completed` synthetic event (carries the final
//     content from chat_messages.content) and close.
//     - Else sleep chatStreamPollInterval and repeat.
//
// **Heartbeat:** every chatStreamHeartbeatInterval with no
// outgoing event we write `: heartbeat\n\n` to keep proxies
// from closing the idle connection.
//
// **Owner-scope:** every store call passes ownerID; cross-owner
// requests get 404 (no existence leak).
func (h *ChatHandler) Stream(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	sessionID := r.PathValue("id")
	messageID := r.PathValue("msg_id")

	// 1. Load message + ownership check. Cross-owner → 404.
	msg, err := h.store.GetChatMessage(r.Context(), messageID, ownerID)
	if errors.Is(err, store.ErrChatMessageNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Path-id consistency: a stream URL with a different session
	// in the path is rejected so a curious client can't read
	// across session boundaries via a known message id.
	if sessionID != "" && msg.SessionID != sessionID {
		writeError(w, http.StatusNotFound, "not_found", "Chat message not found")
		return
	}

	// 2. 24h replay-window check. Only meaningful when the
	// message is terminal — an in-flight message can't be
	// "expired"; the worker is still writing fresh events.
	if isChatMessageTerminalStatus(msg.Status) && msg.FinishedAt != nil {
		if time.Since(*msg.FinishedAt) > chatStreamReplayWindow {
			writeJSON(w, http.StatusGone, model.ApiResponse{
				Error: &model.Error{
					Code:    "chat_replay_expired",
					Message: "the 24h SSE replay window has passed; re-fetch the final message via GET /v1/chat/sessions/{id}/messages/{msg_id}",
					Details: map[string]any{
						"message_id":  msg.ID,
						"status":      msg.Status,
						"content_url": "/v1/chat/sessions/" + msg.SessionID + "/messages/" + msg.ID,
					},
				},
			})
			return
		}
	}

	// 3. Resume: read Last-Event-ID. Empty / malformed → start
	// from 0 (the worker's first event has seq=1).
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))

	// 4. SSE headers + flusher.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "sse_unsupported",
			"streaming is not supported on this connection")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering disables nginx response buffering — without
	// it, frames pile up in nginx's send buffer and the user sees
	// the entire reply land at once instead of streaming.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithTimeout(r.Context(), chatStreamMaxDuration)
	defer cancel()

	h.streamMessageEvents(ctx, w, flusher, ownerID, msg, lastSeq)
}

// streamMessageEvents is the per-connection loop. Pulled out
// from Stream() so it can be unit-tested without spinning up a
// real HTTP request.
//
// **Terminal handling.** The worker writes a final row to
// chat_message_events at the end of every turn (one of
// `message.completed` / `message.partial_failed` /
// `message.failed` / `message.canceled`). The handler doesn't
// synthesize a terminal frame; it just drains rows. When the
// parent chat_messages.status is terminal AND the buffer is
// empty for `seq > sinceSeq`, we know all events landed and can
// close. Codex's Phase 3.4b2.b review pinned this shape over
// the original synthetic approach — synthetic-terminal seqs
// were unstable across reconnects.
func (h *ChatHandler) streamMessageEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, ownerID string, msg *model.ChatMessage, sinceSeq int) {
	heartbeatTimer := time.NewTimer(chatStreamHeartbeatInterval)
	defer heartbeatTimer.Stop()

	pollTicker := time.NewTicker(chatStreamPollInterval)
	defer pollTicker.Stop()

	// Phase 3.5b — subscribe to per-message wake-up signals.
	// Returns nil when no signaler is wired (no Redis); the
	// loop's `wakes` channel below is then the nil channel,
	// which `select` ignores — the poll ticker remains the
	// only progression signal. With a signaler, wakes fires on
	// every event commit, dropping live latency from 250ms
	// (poll interval) to <10ms (Redis pub-sub).
	sub := h.signaler.Subscribe(ctx, msg.ID)
	if sub != nil {
		defer sub.Close()
	}
	wakes := sub.Wakes() // nil if sub is nil — safe in select

	resetHeartbeat := func() {
		if !heartbeatTimer.Stop() {
			select {
			case <-heartbeatTimer.C:
			default:
			}
		}
		heartbeatTimer.Reset(chatStreamHeartbeatInterval)
	}

	// Flush a snapshot of events already in the buffer FIRST,
	// then drop into the live-tail loop. This is the resume
	// path: the client's Last-Event-ID tells us where to start.
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		events, listErr := h.store.ListChatMessageEvents(ctx, msg.ID, ownerID, sinceSeq)
		if listErr != nil {
			slog.WarnContext(ctx, "chat stream: ListChatMessageEvents failed",
				"message_id", msg.ID, "since_seq", sinceSeq, "err", listErr)
			// Don't kill the stream on a transient query error;
			// the next poll may succeed. The heartbeat keeps the
			// connection open meanwhile.
		} else {
			for _, ev := range events {
				if !writeChatStreamEvent(w, flusher, ev) {
					// Client disconnected. No point continuing.
					return
				}
				sinceSeq = ev.Seq
				resetHeartbeat()
			}
		}

		// After draining, check whether the parent message is
		// terminal AND the buffer is fully drained (last poll
		// returned 0 events successfully). The terminal row is
		// written inside FinalizeChatMessage's transaction
		// (Phase 3.4b2.b v2 fix), so terminal-status implies
		// the row is committed and discoverable on the next
		// list. If listErr fired we DON'T exit — the buffer may
		// still have events we haven't read; the next poll
		// retries and lets the loop re-enter the drain branch.
		// Codex caught this gap on v2.
		fresh, err := h.store.GetChatMessage(ctx, msg.ID, ownerID)
		if err != nil {
			// Typically pgx context cancellation when the client
			// disconnects. Log + exit cleanly.
			slog.WarnContext(ctx, "chat stream: refresh GetChatMessage failed",
				"message_id", msg.ID, "err", err)
			return
		}
		if isChatMessageTerminalStatus(fresh.Status) && listErr == nil && len(events) == 0 {
			return
		}

		// Live-tail: wait for the next poll tick, the next
		// signaler wake-up, or the heartbeat. Wakes fires on
		// every chat_message_events commit (or terminal write)
		// when Redis is wired; the poll is the fallback that
		// catches missed signals (Redis disconnect, fail-over
		// dropping a frame).
		//
		// The wakes channel is closed by the Subscription's
		// pump goroutine when Redis disconnects — receiving on
		// a closed channel returns the zero value forever, so
		// the `_, ok` pattern + setting wakes=nil drops the
		// case out of the select. From there the poll loop
		// continues unchanged (poll cadence delivery + the
		// pre-loop `events` query catches up). Codex flagged
		// the missing `ok` check as a tight-loop risk on
		// Phase 3.5b v1.
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wakes:
			if !ok {
				wakes = nil // disable this case; fall back to poll-only
			}
			// fall through — the next iteration re-queries
		case <-pollTicker.C:
			// fall through — fallback path when no Redis
		case <-heartbeatTimer.C:
			if !writeChatStreamHeartbeat(w, flusher) {
				return
			}
			resetHeartbeat()
		}
	}
}

// writeChatStreamEvent translates one chat_message_events row
// into the public SSE wire event. Returns false if the underlying
// write failed (client disconnected).
//
// **Public wire event vocabulary** (plan 24 §"Send-message + resume"):
//
//   - model.delta       — assistant text streamed live
//   - tool.call         — model invoked a tool (recall_memories etc.)
//   - tool.result       — tool returned a result back to the model
//   - approval.required — mutating tool blocked on user approval (Phase 3.6)
//   - error             — terminal error (mid-stream errors only;
//     terminal "this message failed" goes via the
//     synthetic message.failed below)
//
// SDK event types we DO NOT pass through — internal accounting
// details that don't belong on the wire:
//
//   - session_started, model_request, usage, context_applied
//
// The terminal `message.completed` event is synthesized by
// writeChatStreamTerminal below — it carries the final content
// from chat_messages.content rather than the SDK's terminal
// EventResult event (so we get the post-FinalizeChatMessage
// content, not whatever the model emitted).
func writeChatStreamEvent(w http.ResponseWriter, flusher http.Flusher, ev model.ChatMessageEvent) bool {
	wire, ok := translateChatStreamEvent(ev)
	if !ok {
		// Internal-only event — skip it but advance the seq so
		// the client's Last-Event-ID still moves forward.
		return true
	}
	return writeSSEEventWithID(w, flusher, ev.Seq, wire.event, wire.payload)
}

// translateChatStreamEvent maps a stored SDK event_type to the
// public wire event the SSE client renders. Returns ok=false for
// internal-only event types that should be skipped.
type chatStreamWireEvent struct {
	event   string
	payload json.RawMessage
}

func translateChatStreamEvent(ev model.ChatMessageEvent) (chatStreamWireEvent, bool) {
	switch ev.EventType {
	case "assistant":
		// SDK emits ev.Message.PlainText() in our payload
		// under "text". The wire event is model.delta.
		return chatStreamWireEvent{event: "model.delta", payload: ev.Payload}, true
	case "provider_artifact", "thinking_delta":
		// Readable model reasoning. thinking_delta rows are coalesced
		// partials ({text, partial:true} — full text so far, client
		// REPLACES); provider_artifact is the completed block
		// ({text, duration_ms}).
		return chatStreamWireEvent{event: "model.thinking", payload: ev.Payload}, true
	case "tool_use":
		return chatStreamWireEvent{event: "tool.call", payload: ev.Payload}, true
	case "tool_result":
		return chatStreamWireEvent{event: "tool.result", payload: ev.Payload}, true
	case "approval_requested":
		return chatStreamWireEvent{event: "approval.required", payload: ev.Payload}, true
	case "error":
		return chatStreamWireEvent{event: "error", payload: ev.Payload}, true
	}
	// Worker-emitted post-finalize terminal rows are stored with
	// their wire-event names already (`message.completed`, etc.)
	// — passthrough. Per Phase 3.4b2.b's persisted-terminal
	// design, these rows have a deterministic seq and are part
	// of the regular replay buffer.
	if strings.HasPrefix(ev.EventType, "message.") {
		return chatStreamWireEvent{event: ev.EventType, payload: ev.Payload}, true
	}
	// session_started, model_request, usage, context_applied,
	// and any future SDK event types we haven't classified —
	// internal accounting; skipped.
	return chatStreamWireEvent{}, false
}

// writeChatStreamHeartbeat emits a comment frame (`: heartbeat\n\n`)
// to keep proxies from closing the idle connection. Per the SSE
// spec, lines starting with `:` are comments — the client's
// EventSource ignores them.
func writeChatStreamHeartbeat(w http.ResponseWriter, flusher http.Flusher) bool {
	if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// writeSSEEventWithID writes one SSE frame with the standard
// triple: id, event, data. The `id` line is what enables
// Last-Event-ID resume on the client side — the EventSource
// records the last id and sends it back on reconnect.
func writeSSEEventWithID(w http.ResponseWriter, flusher http.Flusher, seq int, eventType string, payload json.RawMessage) bool {
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, eventType, payloadWithSeq(payload, seq)); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func payloadWithSeq(payload json.RawMessage, seq int) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		return payload
	}
	if _, ok := obj["seq"]; ok {
		return payload
	}
	seqJSON, err := json.Marshal(seq)
	if err != nil {
		return payload
	}
	obj["seq"] = seqJSON
	next, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return next
}

// parseLastEventID reads the SSE `Last-Event-ID` header. Per the
// HTML5 SSE spec the header is ALWAYS a string; we treat the
// chat surface's seq numbers as decimal integers so a malformed
// or missing header degrades to "start from beginning" (seq 0).
func parseLastEventID(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isChatMessageTerminalStatus is the handler-side counterpart of
// the store's terminal-status set. Duplicating the predicate
// here (vs. importing) keeps the package boundary clean — the
// handler shouldn't have to reach into the store to ask "is
// this a terminal status?", because the model package already
// owns the lifecycle constants.
func isChatMessageTerminalStatus(status string) bool {
	switch status {
	case model.ChatMessageStatusCompleted,
		model.ChatMessageStatusPartialFailed,
		model.ChatMessageStatusFailed,
		model.ChatMessageStatusCanceled:
		return true
	}
	return false
}
