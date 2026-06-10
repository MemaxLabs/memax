// Phase 3.6b3b — production ChatApprovalEventEmitter.
//
// Implements approver.ApprovalEventEmitter by writing a
// chat_message_events row of EventType="approval_requested"
// (the SDK's stored vocabulary) and signaling the chatstream
// live-delivery hint so any SSE subscriber wakes immediately.
//
// The SSE translator at handler/chat_stream.go:356 maps the
// stored "approval_requested" type to wire event
// "approval.required" — plan 24's user-facing event name. The
// stored vocabulary stays uniform with SDK-emitted events so
// the chat_message_events buffer is consistent across event
// origins.
//
// **Per-turn lifetime.** One emitter per Work() call, bound
// to (ownerID, messageID, turnSeq) at construction. Codex's
// Phase 3.6b3a review caught the lifetime mismatch in the
// initial design — a Service-level emitter couldn't share the
// per-message seq counter. This file's struct lives at the
// Work() scope alongside chatStreamObserver; both take the
// same *chatTurnSeq so SDK events and host-emitted approval
// rows interleave with distinct seq values.

package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// chatApprovalEventEmitter implements
// approver.ApprovalEventEmitter for the chat worker. Each
// EmitApprovalRequired call:
//
//  1. Marshals the approval payload into the wire shape plan
//     24 §"approval flow" pins
//     ({approval_id, tool, args, requested_at, expires_at,
//     reason, details, risk, summary, metadata}).
//  2. Allocates the next seq from the shared turnSeq counter.
//  3. Appends the row via store.AppendChatMessageEvent
//     (owner-scoped; the (message_id, seq) UNIQUE constraint
//     catches any seq collision, though the atomic counter
//     prevents it).
//  4. Notifies the chatstream signaler so live SSE subscribers
//     wake immediately.
//
// Append failures return an error to the approver, which fails
// closed (CAS-flips the row to timed_out and aborts the
// approval). Signaler failures are logged but don't fail the
// emit — the row is durable, the SSE poll fallback covers a
// missed wake-up.
type chatApprovalEventEmitter struct {
	store     store.Store
	signaler  *chatstream.Signaler // nil-safe
	ownerID   string
	messageID string
	seq       *chatTurnSeq // shared with chatStreamObserver
}

// approvalRequestedPayload is the wire shape persisted in
// chat_message_events.payload. The SSE translator passes this
// through as the wire `approval.required` event payload, so
// clients receive every field for rendering the approval card.
type approvalRequestedPayload struct {
	ApprovalID  string                  `json:"approval_id"`
	Tool        string                  `json:"tool"`
	Args        json.RawMessage         `json:"args"`
	RequestedAt time.Time               `json:"requested_at"`
	ExpiresAt   time.Time               `json:"expires_at"`
	Reason      string                  `json:"reason,omitempty"`
	Details     string                  `json:"details,omitempty"`
	Risk        string                  `json:"risk,omitempty"`
	Summary     *approvalSummaryPayload `json:"summary,omitempty"`
	Metadata    map[string]any          `json:"metadata,omitempty"`
}

// approvalSummaryPayload mirrors approvaltools.Summary on the
// wire. Local copy (not a direct re-use of the SDK type) so
// the wire schema is owned by this package — a future SDK
// refactor that renames a field doesn't silently change the
// wire shape under us.
type approvalSummaryPayload struct {
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Risk        string   `json:"risk,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Changes     int      `json:"changes,omitempty"`
	Added       int      `json:"added,omitempty"`
	Modified    int      `json:"modified,omitempty"`
	Deleted     int      `json:"deleted,omitempty"`
	ByteDelta   int      `json:"byte_delta,omitempty"`
}

// EmitApprovalRequired persists + signals an approval prompt.
func (e *chatApprovalEventEmitter) EmitApprovalRequired(ctx context.Context, evt approver.ApprovalRequiredEvent) error {
	if e.store == nil {
		return fmt.Errorf("chatApprovalEventEmitter: store is required")
	}
	if e.seq == nil {
		return fmt.Errorf("chatApprovalEventEmitter: seq is required")
	}
	if evt.MessageID == "" || evt.OwnerID == "" {
		return fmt.Errorf("chatApprovalEventEmitter: empty MessageID or OwnerID in event")
	}

	payload := approvalRequestedPayload{
		ApprovalID:  evt.ApprovalID,
		Tool:        evt.ToolName,
		Args:        json.RawMessage(evt.Args),
		RequestedAt: evt.RequestedAt,
		ExpiresAt:   evt.ExpiresAt,
		Reason:      evt.Reason,
		Details:     evt.Details,
		Risk:        evt.Risk,
		Metadata:    evt.Metadata,
	}
	if hasSummary(evt.Summary.Title, evt.Summary.Description, evt.Summary.Risk) ||
		len(evt.Summary.Paths) > 0 ||
		evt.Summary.Changes != 0 || evt.Summary.Added != 0 || evt.Summary.Modified != 0 ||
		evt.Summary.Deleted != 0 || evt.Summary.ByteDelta != 0 {
		payload.Summary = &approvalSummaryPayload{
			Title:       evt.Summary.Title,
			Description: evt.Summary.Description,
			Risk:        evt.Summary.Risk,
			Paths:       append([]string(nil), evt.Summary.Paths...),
			Changes:     evt.Summary.Changes,
			Added:       evt.Summary.Added,
			Modified:    evt.Summary.Modified,
			Deleted:     evt.Summary.Deleted,
			ByteDelta:   evt.Summary.ByteDelta,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal approval_requested payload: %w", err)
	}

	// Phase 3.6b3b codex-followup: LockedNext serializes the
	// allocate→append→signal chain so commit order matches
	// allocation order. The atomic-only design prevented
	// duplicate seqs but allowed a slow append for seq N to
	// commit AFTER a fast append for seq N+1, leaving seq N
	// invisible to clients that already advanced
	// Last-Event-ID past it. With the lock, seq N's commit
	// always precedes seq N+1's commit. See chatTurnSeq doc
	// for the full failure mode.
	seq, release := e.seq.LockedNext()
	defer release()
	row := &model.ChatMessageEvent{
		MessageID: evt.MessageID,
		Seq:       seq,
		EventType: "approval_requested",
		Payload:   body,
		CreatedAt: time.Now().UTC(),
	}
	if err := e.store.AppendChatMessageEvent(ctx, e.ownerID, row); err != nil {
		return fmt.Errorf("append approval_requested event: %w", err)
	}

	// Signaler is best-effort. Failures log inside Notify; the
	// SSE 250ms poll covers any missed wake-up.
	e.signaler.Notify(ctx, e.messageID)

	slog.DebugContext(ctx, "approval_requested event written",
		"approval_id", evt.ApprovalID,
		"message_id", evt.MessageID,
		"seq", seq,
		"tool", evt.ToolName,
	)
	return nil
}

func hasSummary(title, description, risk string) bool {
	return title != "" || description != "" || risk != ""
}
