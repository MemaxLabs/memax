package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Recorder is the slimmed-down event-driven ticker for the ONE
// onboarding checklist item whose underlying condition is not
// queryable from persistent tables: `first_ask`. Ask is a stateless
// recall RPC — no `asks` row gets written per call — so we can't
// derive completion from the data layer the way the materializer
// (see materialize.go) does for first_memory / five_memories /
// connect_agent / first_hub_invite.
//
// Plan 18 §4.6 originally had Recorder cover all condition-based
// items via per-event hooks. That created a brittle sync chain
// (handler → recorder → CompleteNotificationItem → SSE → client
// refetch) that broke whenever a memory-create path bypassed the
// hook (notably MCP, fixed by audit 2026-05-20). The materializer
// replaced that for queryable items; the recorder now exists only
// for ask, which truly needs an event-driven tick.
//
// Welcome and first_dream are also non-queryable, but they complete
// from explicit UI gestures (modal close, CTA click) routed through
// POST /v1/notifications/.../items/.../complete from the client —
// no recorder needed.
type Recorder interface {
	// RecordAsk fires on a successful /ask. Ticks `first_ask` if
	// the user has a pending onboarding checklist with that item
	// still incomplete. Fire-and-forget: errors are logged but
	// never propagated to the caller.
	RecordAsk(ctx context.Context, userID string)
}

// recorder is the default Recorder implementation. nil store /
// nil publisher are both safe; the helper functions fan out to
// no-op behavior.
type recorder struct {
	store     store.Store
	publisher events.Publisher
}

// NewRecorder constructs a Recorder. Returns nil when store is nil
// so call sites can use the "nil means disabled" pattern.
func NewRecorder(s store.Store, p events.Publisher) Recorder {
	if s == nil {
		return nil
	}
	return &recorder{store: s, publisher: p}
}

func (r *recorder) RecordAsk(ctx context.Context, userID string) {
	if r == nil {
		return
	}
	r.tickByID(ctx, userID, "ask", ItemFirstAsk)
}

// tickByID looks up the user's latest pending onboarding checklist
// and attempts to complete the given itemID. Idempotent — items
// already complete are skipped server-side. Fires the same SSE
// `notification.updated` event the HTTP /complete handler emits so
// other tabs/devices stay in sync. When the completion makes every
// required item done, follows up with TryAutoResolveChecklist to
// flip the row's status to `resolved + applied_auto`.
func (r *recorder) tickByID(ctx context.Context, userID, event, itemID string) {
	if r == nil || r.store == nil || strings.TrimSpace(userID) == "" {
		return
	}
	row, err := r.store.LatestPendingChecklistByRecipient(
		ctx, userID, model.NotificationSourceOnboarding,
	)
	if err != nil {
		if errors.Is(err, store.ErrNotificationNotFound) {
			return
		}
		slog.Warn("onboarding.Recorder: latest pending lookup",
			"event", event, "user_id", userID, "err", err)
		return
	}
	if row == nil {
		return
	}
	var payload model.ChecklistPayload
	if uerr := json.Unmarshal(row.Payload, &payload); uerr != nil {
		slog.Warn("onboarding.Recorder: decode payload",
			"event", event, "notification_id", row.ID, "err", uerr)
		return
	}
	idx := -1
	for i := range payload.Items {
		if payload.Items[i].ID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 || payload.Items[idx].CompletedAt != nil {
		return
	}
	res, cerr := r.store.CompleteNotificationItem(ctx, row.ID, userID, nil, itemID)
	if cerr != nil {
		slog.Debug("onboarding.Recorder: complete item",
			"event", event, "item_id", itemID, "err", cerr)
		return
	}
	if res == nil {
		return
	}
	if updated, gerr := r.store.GetNotification(ctx, row.ID, userID, nil); gerr == nil {
		events.PublishNotificationItemUpdated(
			ctx, r.publisher, updated, recorderItemSnapshot(res),
		)
	}
	if !res.AllRequiredDone {
		return
	}
	flipped, post, terr := r.store.TryAutoResolveChecklist(ctx, row.ID, userID, nil)
	if terr != nil {
		slog.Warn("onboarding.Recorder: auto-resolve",
			"event", event, "notification_id", row.ID, "err", terr)
		return
	}
	if flipped && post != nil {
		events.PublishNotificationResolved(ctx, r.publisher, post)
	}
}

// recorderItemSnapshot mirrors the handler-side helper so the SSE
// envelope shape stays consistent across HTTP- and Recorder-driven
// item updates. Duplicated rather than imported because the handler
// package owns the HTTP-facing helper.
func recorderItemSnapshot(res *store.ItemMutationResult) events.NotificationItemSnapshot {
	if res == nil {
		return events.NotificationItemSnapshot{}
	}
	snap := events.NotificationItemSnapshot{
		ItemID:      res.ItemID,
		ViewedAt:    res.ViewedAt,
		CompletedAt: res.CompletedAt,
	}
	if res.Progress != nil {
		snap.Progress = &events.NotifItemProgress{
			Current: res.Progress.Current,
			Target:  res.Progress.Target,
		}
	}
	return snap
}
