package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/onboarding"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

var errNotificationActionForbidden = errors.New("notification action forbidden")

// NotificationsHandler serves the /v1/notifications surface defined in
// docs/plans/17-inbox-notification-framework.md §4.4. The handler is
// the policy boundary for the decision-vs-receipt split (§6.4): the
// store accepts any kind, but /resolve refuses receipts and /dismiss
// refuses decisions so the lifecycle state machine stays clean.
// notifBillingService is the billing service interface for notification-path membership mutations.
type notifBillingService interface {
	UpdateHubSeatCount(ctx context.Context, hubID string) error
	TransferHubBilling(ctx context.Context, hubID, newOwnerID string) error
}

// notifOwnershipResolver checks ownership cap for transfer acceptance.
type notifOwnershipResolver interface {
	ResolveOwnershipEntitlements(ctx context.Context, userID string) (model.OwnershipEntitlements, error)
}

type NotificationsHandler struct {
	store              store.Store
	events             events.Publisher
	invalidateUserPlan func(ctx context.Context, userID string) error
	billing            notifBillingService
	ownership          notifOwnershipResolver // nil = no cap check
}

func NewNotificationsHandler(s store.Store, publisher events.Publisher) *NotificationsHandler {
	return &NotificationsHandler{store: s, events: publisher}
}

// SetInvalidateUserPlan wires plan cache invalidation for notification invite acceptance.
func (h *NotificationsHandler) SetInvalidateUserPlan(fn func(ctx context.Context, userID string) error) {
	h.invalidateUserPlan = fn
}

// SetBilling wires the billing service for seat count and billing transfer.
func (h *NotificationsHandler) SetBilling(b notifBillingService) {
	h.billing = b
}

// SetOwnershipResolver wires the plan resolver for ownership cap checks
// during transfer acceptance.
func (h *NotificationsHandler) SetOwnershipResolver(r notifOwnershipResolver) {
	h.ownership = r
}

// notificationDecisionKinds is the set of kinds that leave pending
// only via /resolve. Every kind NOT in this set is a receipt and
// leaves pending via /dismiss (or the nightly expiry sweep). Mirrors
// the per-kind allow-list in reviewKindAllowList + plan §6.4.
var notificationDecisionKinds = map[string]bool{
	model.NotificationKindReviewContradiction:    true,
	model.NotificationKindReviewTopicMerge:       true,
	model.NotificationKindReviewTopicRestructure: true,
	model.NotificationKindHubInvite:              true,
	model.NotificationKindHubOwnershipTransfer:   true,
	// Plan 18 — super-notif checklist exits pending via /resolve.
	// User-driven path: action=dismiss → resolution=dismissed (see
	// notificationResolveAllowList). Server-driven auto-completion
	// (every required item complete) bypasses the allow-list entirely
	// and writes ResolutionAppliedAuto directly from CompleteItem.
	model.NotificationKindChecklist: true,
	// Plan 25 P2 — decision gates. The real choice happens on the
	// board card (which records the option + writes the decision
	// memory); the notification accepts only dismiss, plus "resolved"
	// applied server-side when the board slot resolves.
	model.NotificationKindDecisionGate: true,
}

// notificationResolveAllowList maps each decision kind to the resolve
// actions it accepts and the resolution value persisted on success.
// Kept in lockstep with the Phase 1 reviewKindAllowList and the shared
// actionToResolutionByKind map. When a new decision kind lands, all
// three sources must be updated in the same PR.
var notificationResolveAllowList = map[string]map[string]string{
	model.NotificationKindDecisionGate: {
		"dismiss": "dismissed",
	},
	model.NotificationKindReviewContradiction: {
		"keep_a":    "kept_a",
		"keep_b":    "kept_b",
		"keep_both": "kept_both",
		"dismiss":   "dismissed",
	},
	model.NotificationKindReviewTopicMerge: {
		"merge":         "merged",
		"keep_separate": "kept_separate",
		"dismiss":       "dismissed",
	},
	model.NotificationKindReviewTopicRestructure: {
		"apply":   "applied",
		"keep":    "kept",
		"dismiss": "dismissed",
	},
	model.NotificationKindHubInvite: {
		"accept":  "accepted",
		"decline": "declined",
	},
	model.NotificationKindHubOwnershipTransfer: {
		"accept":  "accepted",
		"decline": "declined",
	},
	// Plan 18 — checklist accepts only `dismiss` from a client. The
	// `complete_all` action documented in the RFC is server-only:
	// CompleteItem writes ResolutionAppliedAuto directly via the store
	// when every required item completes, so there's no client-callable
	// path that produces applied_auto. Keeping complete_all out of this
	// map means a stray client POST /resolve {action:"complete_all"}
	// hits the `invalid_action_for_notification_kind` 400 branch
	// rather than authoring its own celebration.
	model.NotificationKindChecklist: {
		"dismiss": "dismissed",
	},
}

// notificationListResponse is the wire shape for GET /v1/notifications.
// next_cursor is empty when there is no next page.
type notificationListResponse struct {
	Notifications []model.Notification `json:"notifications"`
	NextCursor    string               `json:"next_cursor,omitempty"`
	HasMore       bool                 `json:"has_more"`
}

// List — GET /v1/notifications
//
// Query params (plan §4.4):
//
//	hub          — single hub narrow (defaults to union)
//	status       — pending | resolved | dismissed | expired (default pending)
//	kind         — repeatable, narrows to one or more kinds
//	resolution   — repeatable, only meaningful with status=resolved
//	unseen_only  — bool, filters seen_at IS NULL
//	since        — RFC3339 lower bound on created_at
//	limit        — 1..500, default 50
//	cursor       — opaque RFC3339 cursor on created_at from the previous page
func (h *NotificationsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	q := r.URL.Query()

	opts := store.NotificationListOpts{
		UserID: userID,
		HubIDs: GetAccessibleHubIDs(r),
		HubID:  q.Get("hub"),
	}
	if statusStr := q.Get("status"); statusStr != "" {
		opts.Status = model.NotificationStatus(statusStr)
	} else {
		opts.Status = model.NotificationStatusPending
	}
	if kinds := q["kind"]; len(kinds) > 0 {
		opts.Kinds = kinds
	}
	if resolutions := q["resolution"]; len(resolutions) > 0 {
		for _, res := range resolutions {
			opts.Resolutions = append(opts.Resolutions, model.NotificationResolution(res))
		}
	}
	if q.Get("unseen_only") == "true" {
		opts.UnseenOnly = true
	}
	if since := q.Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_since", "since must be RFC3339")
			return
		}
		opts.Since = t
	}
	if limit := q.Get("limit"); limit != "" {
		var n int
		if _, err := fmtSscanInt(limit, &n); err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		opts.Limit = n
	}
	opts.Cursor = q.Get("cursor")

	notifs, nextCursor, err := h.store.ListNotificationsForUser(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if notifs == nil {
		notifs = []model.Notification{}
	}

	// Read-time enrichment. The producer snapshots memory titles into
	// the notification payload at creation time, but a memory's title
	// can evolve afterwards (LLM rename, manual edit, summary
	// backfill) — so rows written during quick capture can surface
	// as "(untitled memory)" even though the memory has a real title
	// by the time the user reads the inbox row. Refresh the titles in
	// place against the current Memory rows, and swap dropped /
	// deleted memories for an explicit "(removed memory)" marker.
	h.enrichNotificationPayloads(notifs, userID, opts.HubIDs)

	// Status filter contract: GET /v1/notifications?status=X must
	// only return rows whose status matches X. The onboarding
	// materializer's auto-resolve path can flip a row from `pending`
	// to `resolved` DURING enrichment (when all required items are
	// now done). Re-apply the status filter post-enrichment so a
	// `?status=pending` request doesn't briefly leak a freshly-
	// resolved row into the pending list — the same row will surface
	// in `?status=resolved` requests + the SSE
	// notification.resolved fan-out invalidates the cache.
	if opts.Status != "" {
		filtered := notifs[:0]
		for _, n := range notifs {
			if n.Status == opts.Status {
				filtered = append(filtered, n)
			}
		}
		notifs = filtered
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: notificationListResponse{
		Notifications: notifs,
		NextCursor:    nextCursor,
		HasMore:       nextCursor != "",
	}})
}

// enrichNotificationPayloads walks a notification list and refreshes
// stale snapshot fields (memory titles, topic names, memory counts)
// against the current store state. The notification rows themselves
// stay untouched in the database — only the serialized payloads
// emitted to the client are enriched, so this is a stateless
// read-time pass and cannot corrupt history. Every REST / MCP / SDK
// consumer reads the same fresh view through this one hop.
//
// Kinds covered:
//   - review_contradiction: memory_a / memory_b title
//   - review_topic_merge:   target_topic + source_topics name + memory_count
//   - review_topic_restructure: parent_topic + child_topic name + count
//
// When a referenced row is gone from the store the enrichment marks
// the ref with "(removed memory)" / "(removed topic)" so the user can
// still tell something was there without the row silently vanishing.
func (h *NotificationsHandler) enrichNotificationPayloads(notifs []model.Notification, userID string, hubIDs []string) {
	h.enrichContradictionPayloads(notifs, userID, hubIDs)
	h.enrichTopicPayloads(notifs, userID, hubIDs)
	h.enrichOnboardingChecklists(notifs, userID)
}

// enrichOnboardingChecklists is the lazy-compute SOT for the four
// checklist items whose underlying condition is queryable
// (memory_count, configs_sync = agent connected, hub_event = team
// hub joined/created). For each pending onboarding checklist, the
// materializer asks "what should this row look like based on the
// memories / connected_agents / team_hubs tables right now?" and
// returns a list of intended ticks (item completions + progress
// patches). The handler then APPLIES those ticks via the same
// store primitives the HTTP /complete + /progress endpoints use,
// so the durable payload, the response payload, AND the SSE event
// stream all stay aligned. Without persisting via the store path
// (i.e., if the handler only mutated `n.Payload` in-memory), the
// /complete endpoint would still see the stale stored payload and
// reject `first_dream` with `item_locked` even when the materializer
// observed five_memories was done — codex-review High finding,
// 2026-05-20.
//
// Welcome / first_ask / first_dream remain payload-cached and are
// written via POST /complete from explicit UI gestures + the slim
// ask recorder hook. The materializer ignores those triggers.
//
// Best-effort: store errors are logged but never propagated. A
// failed materialization leaves the response carrying the stale
// stored state — the next read retries.
func (h *NotificationsHandler) enrichOnboardingChecklists(notifs []model.Notification, userID string) {
	if h.store == nil || strings.TrimSpace(userID) == "" {
		return
	}
	for i := range notifs {
		n := &notifs[i]
		if n.Kind != model.NotificationKindChecklist {
			continue
		}
		if n.SourceKind != model.NotificationSourceOnboarding {
			continue
		}
		if n.Status != model.NotificationStatusPending {
			continue
		}
		var payload model.ChecklistPayload
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			continue
		}
		ctx := context.Background()
		ticks := onboarding.MaterializeChecklist(ctx, h.store, userID, payload)
		if len(ticks) == 0 {
			continue
		}
		anyComplete := false
		var allRequiredDone bool
		for _, tick := range ticks {
			if tick.ShouldComplete {
				res, cerr := h.store.CompleteNotificationItem(
					ctx, n.ID, userID, nil, tick.ItemID,
				)
				if cerr != nil {
					// Locked / not-pending / not-found are expected in
					// race conditions (concurrent /complete) — log at
					// warn but don't fail the read.
					slog.Warn("onboarding materialize: complete item",
						"notification_id", n.ID, "item_id", tick.ItemID,
						"user_id", userID, "err", cerr)
					continue
				}
				if res == nil {
					continue
				}
				anyComplete = true
				if res.AllRequiredDone {
					allRequiredDone = true
				}
				// Fire notification.updated SSE so other tabs/devices
				// pick up the materializer-driven tick the same way
				// they would an HTTP /complete.
				if updated, gerr := h.store.GetNotification(ctx, n.ID, userID, nil); gerr == nil && updated != nil {
					events.PublishNotificationItemUpdated(
						ctx, h.events, updated, itemMutationSnapshot(res),
					)
				}
			} else if tick.ProgressCurrent != nil && tick.ProgressTarget != nil {
				res, perr := h.store.UpdateChecklistItemProgress(
					ctx, n.ID, userID, nil, tick.ItemID,
					*tick.ProgressCurrent, *tick.ProgressTarget,
				)
				if perr != nil {
					slog.Warn("onboarding materialize: progress update",
						"notification_id", n.ID, "item_id", tick.ItemID,
						"user_id", userID, "err", perr)
					continue
				}
				if res == nil {
					continue
				}
				if updated, gerr := h.store.GetNotification(ctx, n.ID, userID, nil); gerr == nil && updated != nil {
					events.PublishNotificationItemUpdated(
						ctx, h.events, updated, itemMutationSnapshot(res),
					)
				}
			}
		}
		if allRequiredDone {
			flipped, post, terr := h.store.TryAutoResolveChecklist(
				ctx, n.ID, userID, nil,
			)
			if terr != nil {
				slog.Warn("onboarding materialize: auto-resolve",
					"notification_id", n.ID, "user_id", userID, "err", terr)
			} else if flipped && post != nil {
				// Emit notification.resolved SSE and update the response
				// row so this GET returns the freshly-resolved state
				// (status=resolved + resolution=applied_auto), not the
				// stale pending snapshot it loaded from the list query.
				events.PublishNotificationResolved(ctx, h.events, post)
				*n = *post
				continue // *n already has the latest payload from post
			}
		}
		// Re-fetch the row so the response carries the post-tick
		// payload (completed_at + progress fields the store writes
		// flipped in the loop above).
		if anyComplete || len(ticks) > 0 {
			if updated, gerr := h.store.GetNotification(ctx, n.ID, userID, nil); gerr == nil && updated != nil {
				*n = *updated
			}
		}
	}
}

// itemMutationSnapshot converts a store ItemMutationResult to the
// SSE event snapshot shape. Local helper because the recorder owns
// its own copy in internal/onboarding; this is the handler side.
func itemMutationSnapshot(res *store.ItemMutationResult) events.NotificationItemSnapshot {
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

// enrichContradictionPayloads refreshes memory_a / memory_b titles for
// every review_contradiction row against the current Memory table.
func (h *NotificationsHandler) enrichContradictionPayloads(notifs []model.Notification, userID string, hubIDs []string) {
	memoryIDs := make(map[string]struct{})
	for i := range notifs {
		if notifs[i].Kind != model.NotificationKindReviewContradiction {
			continue
		}
		var payload model.ReviewContradictionPayload
		if err := json.Unmarshal(notifs[i].Payload, &payload); err != nil {
			continue
		}
		if payload.MemoryA.ID != "" {
			memoryIDs[payload.MemoryA.ID] = struct{}{}
		}
		if payload.MemoryB.ID != "" {
			memoryIDs[payload.MemoryB.ID] = struct{}{}
		}
	}
	if len(memoryIDs) == 0 {
		return
	}
	ids := make([]string, 0, len(memoryIDs))
	for id := range memoryIDs {
		ids = append(ids, id)
	}
	memories, err := h.store.GetAccessibleMemories(ids, userID, hubIDs)
	if err != nil {
		// Swallow: stale titles are strictly better than a failed
		// inbox fetch. The client fallback (reviewMemoryDisplayTitle)
		// still renders "(untitled memory)" for any missing refs.
		return
	}
	for i := range notifs {
		if notifs[i].Kind != model.NotificationKindReviewContradiction {
			continue
		}
		var payload model.ReviewContradictionPayload
		if err := json.Unmarshal(notifs[i].Payload, &payload); err != nil {
			continue
		}
		payload.MemoryA.Title = currentMemoryRefTitle(memories, payload.MemoryA.ID, payload.MemoryA.Title)
		payload.MemoryB.Title = currentMemoryRefTitle(memories, payload.MemoryB.ID, payload.MemoryB.Title)
		refreshed, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		notifs[i].Payload = refreshed
	}
}

// enrichTopicPayloads refreshes topic Name + MemoryCount for every
// review_topic_merge / review_topic_restructure row against the
// current Topics + memory counts. Notifications are grouped by
// HubID first so we only hit ListTopics / CountTopicMemories once
// per hub rather than once per row. Topics the producer wrote with
// stale names (or that have since been renamed / deleted) are all
// rebuilt from this single read.
func (h *NotificationsHandler) enrichTopicPayloads(notifs []model.Notification, userID string, hubIDs []string) {
	// Collect the set of hubs whose topics we need fresh data for.
	hubsNeedingTopics := make(map[string]struct{})
	accessibleHubIDs := make(map[string]struct{}, len(hubIDs))
	for _, hubID := range hubIDs {
		if hubID == "" {
			continue
		}
		accessibleHubIDs[hubID] = struct{}{}
	}
	for i := range notifs {
		kind := notifs[i].Kind
		if kind != model.NotificationKindReviewTopicMerge &&
			kind != model.NotificationKindReviewTopicRestructure {
			continue
		}
		if notifs[i].HubID == "" {
			continue
		}
		if len(accessibleHubIDs) > 0 {
			if _, ok := accessibleHubIDs[notifs[i].HubID]; !ok {
				continue
			}
		}
		hubsNeedingTopics[notifs[i].HubID] = struct{}{}
	}
	if len(hubsNeedingTopics) == 0 {
		return
	}

	// Per hub, build a fresh topicByID map and a memory-count map.
	// An error for one hub's lookup just skips enrichment for that
	// hub — the row still renders via the payload's creation-time
	// snapshot plus the client "(untitled / removed)" fallbacks.
	topicsByHub := make(map[string]map[string]*model.Topic, len(hubsNeedingTopics))
	countsByHub := make(map[string]map[string]int, len(hubsNeedingTopics))
	for hubID := range hubsNeedingTopics {
		topics, err := h.store.ListTopics(hubID)
		if err != nil {
			continue
		}
		byID := make(map[string]*model.Topic, len(topics))
		for i := range topics {
			byID[topics[i].ID] = &topics[i]
		}
		topicsByHub[hubID] = byID
		counts, _, err := h.store.CountTopicMemories(store.VisibilityScope{
			OwnerID: userID,
			HubIDs:  hubIDs,
		}, hubID)
		if err != nil {
			countsByHub[hubID] = map[string]int{}
			continue
		}
		countsByHub[hubID] = counts
	}

	for i := range notifs {
		kind := notifs[i].Kind
		hubID := notifs[i].HubID
		topicByID, ok := topicsByHub[hubID]
		if !ok {
			continue
		}
		counts := countsByHub[hubID]

		switch kind {
		case model.NotificationKindReviewTopicMerge:
			var payload model.ReviewTopicMergePayload
			if err := json.Unmarshal(notifs[i].Payload, &payload); err != nil {
				continue
			}
			payload.TargetTopic = currentTopicRef(topicByID, counts, payload.TargetTopic)
			for j := range payload.SourceTopics {
				payload.SourceTopics[j] = currentTopicRef(topicByID, counts, payload.SourceTopics[j])
			}
			refreshed, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			notifs[i].Payload = refreshed

		case model.NotificationKindReviewTopicRestructure:
			var payload model.ReviewTopicRestructurePayload
			if err := json.Unmarshal(notifs[i].Payload, &payload); err != nil {
				continue
			}
			payload.ParentTopic = currentTopicRef(topicByID, counts, payload.ParentTopic)
			payload.ChildTopic = currentTopicRef(topicByID, counts, payload.ChildTopic)
			refreshed, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			notifs[i].Payload = refreshed
		}
	}
}

// currentTopicRef picks the freshest label + memory count for a
// topic reference. Precedence: current Topic.Name from the store →
// the payload's creation-time snapshot → "(removed topic)" marker
// for rows the caller can no longer see. Memory counts always reflect
// the live store because a stale count is more misleading than a
// stale name (a user merging two topics needs to know how many
// memories will actually move).
func currentTopicRef(topicByID map[string]*model.Topic, counts map[string]int, ref model.ReviewTopicRef) model.ReviewTopicRef {
	if ref.ID == "" {
		return ref
	}
	topic, ok := topicByID[ref.ID]
	if !ok || topic == nil {
		return model.ReviewTopicRef{
			ID:          ref.ID,
			Name:        "(removed topic)",
			MemoryCount: 0,
		}
	}
	name := strings.TrimSpace(topic.Name)
	if name == "" {
		name = strings.TrimSpace(ref.Name)
	}
	if name == "" {
		name = "(untitled topic)"
	}
	return model.ReviewTopicRef{
		ID:          ref.ID,
		Name:        name,
		MemoryCount: counts[ref.ID],
	}
}

// currentMemoryRefTitle picks the freshest human-readable label for a
// memory reference. Mirrors reviewMemoryRefTitle in
// packages/server/internal/dreams/contradictions.go but reads from a
// pre-fetched map so the enrichment pass stays O(1) per row.
//
// Precedence: current Memory.Title → current Memory.Summary → compact
// Memory.Content preview → the title the payload had at creation time
// → empty string (which the client then renders as
// "(untitled memory)"). Memories the caller can no longer see return
// an explicit "(removed memory)" marker so the user understands why
// the row is still in their inbox.
func currentMemoryRefTitle(memories map[string]*model.Memory, id string, fallback string) string {
	if id == "" {
		return fallback
	}
	m, ok := memories[id]
	if !ok || m == nil {
		return "(removed memory)"
	}
	if t := strings.TrimSpace(m.Title); t != "" {
		return t
	}
	if s := strings.TrimSpace(m.Summary); s != "" {
		return compactMemoryPreview(s, 80)
	}
	if c := strings.TrimSpace(m.Content); c != "" {
		return compactMemoryPreview(c, 80)
	}
	return fallback
}

// compactMemoryPreview collapses whitespace and truncates to max
// characters, appending an ellipsis if truncation occurred. Duplicated
// from dreams.compactPreview to keep the handler package self-contained.
func compactMemoryPreview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Summary — GET /v1/notifications/summary
func (h *NotificationsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	summary, err := h.store.GetNotificationSummary(r.Context(), store.NotificationSummaryOpts{
		UserID: userID,
		HubIDs: GetAccessibleHubIDs(r),
		HubID:  r.URL.Query().Get("hub"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: summary})
}

// MarkSeen — POST /v1/notifications/{id}/seen (idempotent)
func (h *NotificationsHandler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	notifID := r.PathValue("id")
	hubIDs := GetAccessibleHubIDs(r)

	if err := h.store.MarkNotificationSeen(r.Context(), notifID, userID, hubIDs); err != nil {
		if errors.Is(err, store.ErrNotificationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Re-fetch so the publish carries current routing info.
	n, err := h.store.GetNotification(r.Context(), notifID, userID, hubIDs)
	if err == nil {
		events.PublishNotificationUpdated(r.Context(), h.events, n, events.NotificationChangeSeen)
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "seen"}})
}

// Dismiss — POST /v1/notifications/{id}/dismiss (receipt-only per §6.4)
func (h *NotificationsHandler) Dismiss(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	notifID := r.PathValue("id")
	hubIDs := GetAccessibleHubIDs(r)

	n, err := h.store.GetNotification(r.Context(), notifID, userID, hubIDs)
	if err != nil {
		if errors.Is(err, store.ErrNotificationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if notificationDecisionKinds[n.Kind] {
		writeError(w, http.StatusBadRequest, "dismiss_not_applicable_to_decision",
			"Decision notifications leave pending via /resolve; use that endpoint with action=\"dismiss\" instead")
		return
	}
	if err := h.store.DismissNotification(r.Context(), notifID, userID, hubIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Re-fetch post-dismiss so the event carries the updated row.
	updated, err := h.store.GetNotification(r.Context(), notifID, userID, hubIDs)
	if err == nil {
		events.PublishNotificationUpdated(r.Context(), h.events, updated, events.NotificationChangeDismissed)
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "dismissed"}})
}

// Resolve — POST /v1/notifications/{id}/resolve (decision-only per §6.4)
//
// Body: { "action": <per-kind action> }
//
// Dispatches the destructive domain action (archive on contradiction,
// MergeTopics on topic_merge, ApplyTopicRestructure on
// topic_restructure, AcceptHubInviteByID/DeclineHubInviteByID on
// hub_invite) before writing the resolution. Mirrors the /v1/reviews/
// resolve allow-list exactly; any divergence would be caught by a
// drift-proofing test at the Phase 3b wiring site.
func (h *NotificationsHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	notifID := r.PathValue("id")
	hubIDs := GetAccessibleHubIDs(r)

	var req struct {
		Action string `json:"action"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	n, err := h.store.GetNotification(r.Context(), notifID, userID, hubIDs)
	if err != nil {
		if errors.Is(err, store.ErrNotificationNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if !notificationDecisionKinds[n.Kind] {
		writeError(w, http.StatusBadRequest, "resolve_not_applicable_to_receipt",
			"Receipt notifications do not have resolve semantics; use /seen or /dismiss")
		return
	}
	allowed, ok := notificationResolveAllowList[n.Kind]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported_review_kind", "Notification kind has no resolve allow-list")
		return
	}
	resolution, ok := allowed[req.Action]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_action_for_notification_kind",
			"Action \""+req.Action+"\" is not valid for notification kind \""+n.Kind+"\"")
		return
	}
	ctx := r.Context()
	if err := h.authorizeResolveAction(ctx, userID, hubIDs, isHubScopeBounded(r), n, req.Action); err != nil {
		if errors.Is(err, errNotificationActionForbidden) {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to perform this inbox action")
			return
		}
		if code, msg, status, ok := mapStoreErrToAPI(err); ok {
			writeError(w, status, code, msg)
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Dispatch destructive side effects first. Any failure aborts
	// before the resolve write so a refused merge / apply / accept
	// doesn't leave a half-resolved row.
	switch n.Kind {
	case model.NotificationKindReviewContradiction:
		if err := h.dispatchContradictionAction(ctx, n, req.Action); err != nil {
			if code, msg, status, ok := mapStoreErrToAPI(err); ok {
				writeError(w, status, code, msg)
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	case model.NotificationKindReviewTopicMerge:
		if req.Action == "merge" {
			if err := h.dispatchTopicMerge(ctx, n); err != nil {
				if status, code, msg, ok := mapTopicMergeError(err); ok {
					writeError(w, status, code, msg)
					return
				}
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
		}
	case model.NotificationKindReviewTopicRestructure:
		if req.Action == "apply" {
			if err := h.dispatchTopicRestructure(ctx, n); err != nil {
				if status, code, msg, ok := mapTopicRestructureError(err); ok {
					writeError(w, status, code, msg)
					return
				}
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
		}
	case model.NotificationKindHubInvite:
		if err := h.dispatchHubInviteAction(ctx, n, userID, req.Action); err != nil {
			if code, msg, status, ok := mapStoreErrToAPI(err); ok {
				writeError(w, status, code, msg)
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	case model.NotificationKindHubOwnershipTransfer:
		if err := h.dispatchHubOwnershipTransferAction(ctx, n, userID, req.Action); err != nil {
			if code, msg, status, ok := mapStoreErrToAPI(err); ok {
				writeError(w, status, code, msg)
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
	}

	if err := h.store.ResolveNotification(ctx, notifID, userID, hubIDs, model.NotificationResolution(resolution)); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	updated, err := h.store.GetNotification(ctx, notifID, userID, hubIDs)
	if err == nil {
		events.PublishNotificationResolved(ctx, h.events, updated)
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status":     "resolved",
		"action":     req.Action,
		"resolution": resolution,
	}})
}

// itemUpdateResponse mirrors the inline snapshot the SSE event carries.
// Clients reading the HTTP response use the same shape to patch their
// local cache so /view + /complete behave identically to receiving an
// item_updated SSE event.
type itemUpdateResponse struct {
	ItemID         string                       `json:"item_id"`
	ViewedAt       *time.Time                   `json:"viewed_at,omitempty"`
	CompletedAt    *time.Time                   `json:"completed_at,omitempty"`
	Progress       *model.ItemProgress          `json:"progress,omitempty"`
	AutoResolved   bool                         `json:"auto_resolved,omitempty"`
	AutoResolvedAs model.NotificationResolution `json:"auto_resolved_as,omitempty"`
}

// ViewItem — POST /v1/notifications/{id}/items/{item_id}/view (plan 18
// §4.3). Stamps viewed_at on the item if currently nil. Idempotent.
// Works for both `checklist` and `digest` rows. Emits a
// notification.updated event with change=item_updated and the inline
// snapshot per §4.5.
func (h *NotificationsHandler) ViewItem(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	notifID := r.PathValue("id")
	itemID := r.PathValue("item_id")
	if notifID == "" || itemID == "" {
		writeError(w, http.StatusBadRequest, "missing_path", "notification id and item_id are required")
		return
	}
	hubIDs := GetAccessibleHubIDs(r)
	ctx := r.Context()

	res, err := h.store.ViewNotificationItem(ctx, notifID, userID, hubIDs, itemID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotificationNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Notification not found")
		case errors.Is(err, store.ErrNotificationNotPending):
			writeError(w, http.StatusBadRequest, "notification_not_pending",
				"Notification has already been resolved, dismissed, or expired")
		case errors.Is(err, store.ErrChecklistItemNotFound):
			writeError(w, http.StatusNotFound, "item_not_found", "Notification item not found")
		case errors.Is(err, store.ErrInvalidNotificationKindForOp):
			writeError(w, http.StatusBadRequest, "kind_not_supported", "Notification kind does not have viewable items")
		default:
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}

	// Re-fetch the parent row so the SSE envelope carries the live
	// routing fields, then publish item_updated with the inline snapshot.
	if updated, gerr := h.store.GetNotification(ctx, notifID, userID, hubIDs); gerr == nil {
		events.PublishNotificationItemUpdated(ctx, h.events, updated, itemSnapshotFromMutation(res))
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: itemUpdateResponse{
		ItemID:      res.ItemID,
		ViewedAt:    res.ViewedAt,
		CompletedAt: res.CompletedAt,
		Progress:    res.Progress,
	}})
}

// CompleteItem — POST /v1/notifications/{id}/items/{item_id}/complete
// (plan 18 §4.3). Stamps completed_at + viewed_at on a checklist item.
// Refuses non-checklist kinds with 400 kind_not_supported, locked
// items with 400 item_locked, and terminal rows with 400
// notification_not_pending. Idempotent.
//
// When the call satisfies every required_ids item, the handler invokes
// store.TryAutoResolveChecklist, which atomically re-checks the live
// payload + status='pending' under FOR UPDATE before flipping the row
// to resolution=applied_auto. Only when that primitive reports
// flipped=true does the handler emit notification.resolved and
// advertise auto_resolved=true in the HTTP response — so a concurrent
// /resolve {dismiss} winning the race produces neither lie.
func (h *NotificationsHandler) CompleteItem(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	notifID := r.PathValue("id")
	itemID := r.PathValue("item_id")
	if notifID == "" || itemID == "" {
		writeError(w, http.StatusBadRequest, "missing_path", "notification id and item_id are required")
		return
	}
	hubIDs := GetAccessibleHubIDs(r)
	ctx := r.Context()

	res, err := h.store.CompleteNotificationItem(ctx, notifID, userID, hubIDs, itemID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotificationNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Notification not found")
		case errors.Is(err, store.ErrNotificationNotPending):
			writeError(w, http.StatusBadRequest, "notification_not_pending",
				"Notification has already been resolved, dismissed, or expired")
		case errors.Is(err, store.ErrChecklistItemNotFound):
			writeError(w, http.StatusNotFound, "item_not_found", "Notification item not found")
		case errors.Is(err, store.ErrChecklistItemLocked):
			writeError(w, http.StatusBadRequest, "item_locked", "Item locked — finish its prerequisites first")
		case errors.Is(err, store.ErrInvalidNotificationKindForOp):
			writeError(w, http.StatusBadRequest, "kind_not_supported", "Only checklist notifications support /complete")
		case errors.Is(err, store.ErrChecklistItemPayloadInvalid):
			writeError(w, http.StatusBadRequest, "invalid_checklist_payload", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}

	// Publish item_updated first so clients can paint the item check
	// before the (possible) auto-resolve overlay lands.
	if updated, gerr := h.store.GetNotification(ctx, notifID, userID, hubIDs); gerr == nil {
		events.PublishNotificationItemUpdated(ctx, h.events, updated, itemSnapshotFromMutation(res))
	}

	resp := itemUpdateResponse{
		ItemID:      res.ItemID,
		ViewedAt:    res.ViewedAt,
		CompletedAt: res.CompletedAt,
		Progress:    res.Progress,
	}

	// Auto-resolve gate — plan 18 §3.2.
	//
	// TryAutoResolveChecklist atomically re-locks the row, verifies
	// every required item is still complete on the live payload, and
	// returns flipped=true only when THIS call actually performed the
	// status flip. A concurrent /resolve {dismiss} that landed between
	// our /complete tx and this call returns flipped=false with the
	// dismissed row, so we don't lie about auto_resolved or
	// double-fire the SSE. Idempotent /complete re-fires after a
	// successful auto-resolve also return flipped=false (status is
	// already non-pending) — the retry path collapses cleanly.
	if res.AllRequiredDone {
		flipped, post, rerr := h.store.TryAutoResolveChecklist(ctx, notifID, userID, hubIDs)
		if rerr != nil {
			// Best-effort: the item completion already succeeded and is
			// durable. The next /complete fire (or P2 Recorder retry)
			// will see AllRequiredDone=true on the idempotent path and
			// re-attempt the auto-resolve. Don't 500 the request.
			slog.Warn("checklist auto-resolve failed",
				"notification_id", notifID, "item_id", itemID, "err", rerr)
		} else if flipped && post != nil {
			events.PublishNotificationResolved(ctx, h.events, post)
			resp.AutoResolved = true
			resp.AutoResolvedAs = model.ResolutionAppliedAuto
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// itemSnapshotFromMutation converts the store-side ItemMutationResult
// into the SSE-side snapshot envelope. The two shapes diverge only at
// the progress sub-type (model.ItemProgress vs events.NotifItemProgress)
// to keep the events package free of an upstream-model import.
func itemSnapshotFromMutation(res *store.ItemMutationResult) events.NotificationItemSnapshot {
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

func (h *NotificationsHandler) authorizeResolveAction(ctx context.Context, userID string, hubIDs []string, strict bool, n *model.Notification, action string) error {
	switch n.Kind {
	case model.NotificationKindReviewContradiction:
		if action == "keep_both" || action == "dismiss" {
			return nil
		}
		hub, role, err := h.resolveNotificationHubRole(n.HubID, userID)
		if err != nil {
			return err
		}
		memoryID, err := contradictionArchivedMemoryID(n, action)
		if err != nil {
			return err
		}
		// Scope-aware load — a scope-bounded principal must not be
		// able to act on a contradiction-archived memory outside
		// their granted hubs even if the notification routed to
		// them.
		memory, err := loadMemoryWithScope(ctx, h.store, memoryID, userID, hubIDs, strict)
		if err != nil {
			return err
		}
		if !canDeleteMemory(role, hub, memory.OwnerID == userID) {
			return errNotificationActionForbidden
		}
	case model.NotificationKindReviewTopicMerge:
		if action != "merge" {
			return nil
		}
		hub, role, err := h.resolveNotificationHubRole(n.HubID, userID)
		if err != nil {
			return err
		}
		if !canWriteTopics(role, hub) {
			return errNotificationActionForbidden
		}
	case model.NotificationKindReviewTopicRestructure:
		if action != "apply" {
			return nil
		}
		hub, role, err := h.resolveNotificationHubRole(n.HubID, userID)
		if err != nil {
			return err
		}
		if !canWriteTopics(role, hub) {
			return errNotificationActionForbidden
		}
	}
	return nil
}

func (h *NotificationsHandler) resolveNotificationHubRole(hubID string, userID string) (*model.Hub, string, error) {
	if hubID == "" {
		return nil, "", errors.New("notification is missing hub id")
	}
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		return nil, "", err
	}
	role, err := h.store.GetHubMemberRole(hubID, userID)
	if err != nil || role == "" {
		return nil, "", errNotificationActionForbidden
	}
	return hub, role, nil
}

func contradictionArchivedMemoryID(n *model.Notification, action string) (string, error) {
	if len(n.Payload) == 0 {
		return "", errors.New("contradiction notification is missing payload")
	}
	var payload model.ReviewContradictionPayload
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return "", fmt.Errorf("decode contradiction payload: %w", err)
	}
	switch action {
	case "keep_a":
		if payload.MemoryB.ID == "" {
			return "", errors.New("contradiction payload missing memory_b id")
		}
		return payload.MemoryB.ID, nil
	case "keep_b":
		if payload.MemoryA.ID == "" {
			return "", errors.New("contradiction payload missing memory_a id")
		}
		return payload.MemoryA.ID, nil
	default:
		return "", nil
	}
}

// BulkSeen — POST /v1/notifications/seen
// Body: { hub?, kinds[] }
func (h *NotificationsHandler) BulkSeen(w http.ResponseWriter, r *http.Request) {
	h.handleBulk(w, r, bulkOpSeen)
}

// BulkDismiss — POST /v1/notifications/dismiss
func (h *NotificationsHandler) BulkDismiss(w http.ResponseWriter, r *http.Request) {
	h.handleBulk(w, r, bulkOpDismiss)
}

type bulkOp int

const (
	bulkOpSeen bulkOp = iota
	bulkOpDismiss
)

func (h *NotificationsHandler) handleBulk(w http.ResponseWriter, r *http.Request, op bulkOp) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	var req struct {
		Hub   string   `json:"hub,omitempty"`
		Kinds []string `json:"kinds"`
		Since string   `json:"since,omitempty"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
			return
		}
	}
	// Decision-kind refusal — plan §4.4 bulk rule.
	for _, k := range req.Kinds {
		if notificationDecisionKinds[k] {
			writeError(w, http.StatusBadRequest, "bulk_not_allowed_for_decision_kind",
				"Kind \""+k+"\" is a decision kind and cannot be bulk-mutated")
			return
		}
	}

	opts := store.BulkNotificationMutationOpts{
		UserID: userID,
		HubIDs: GetAccessibleHubIDs(r),
		HubID:  req.Hub,
		Kinds:  req.Kinds,
	}
	if req.Since != "" {
		t, err := time.Parse(time.RFC3339Nano, req.Since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_since", "since must be RFC3339")
			return
		}
		opts.Since = t
	}

	var touched []model.Notification
	var change events.NotificationChange
	switch op {
	case bulkOpSeen:
		touched, err = h.store.BulkMarkNotificationsSeen(r.Context(), opts)
		change = events.NotificationChangeSeen
	case bulkOpDismiss:
		touched, err = h.store.BulkDismissNotifications(r.Context(), opts)
		change = events.NotificationChangeDismissed
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// One notification.updated per affected row so other tabs / the
	// badge / the inbox list converge without polling.
	for i := range touched {
		events.PublishNotificationUpdated(r.Context(), h.events, &touched[i], change)
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"affected": len(touched),
	}})
}

// dispatchContradictionAction runs the destructive side effect for a
// contradiction resolve action before the resolution is persisted.
// keep_a archives memory_b, keep_b archives memory_a; keep_both and
// dismiss are no-ops against memories and always return nil.
//
// Error contract (fixes a Phase 3b review finding): every failure
// mode propagates to the caller so the handler aborts BEFORE writing
// the resolution. A corrupted payload or a failed archive would
// otherwise leave the notification marked resolved/kept_a|kept_b
// even though neither memory was actually archived — exactly the
// state the handler comment promises we never ship.
func (h *NotificationsHandler) dispatchContradictionAction(_ context.Context, n *model.Notification, action string) error {
	// No-op actions run no side effect and cannot fail.
	if action == "keep_both" || action == "dismiss" {
		return nil
	}
	if n.HubID == "" {
		return errors.New("contradiction notification is missing hub id")
	}
	if len(n.Payload) == 0 {
		return errors.New("contradiction notification is missing payload")
	}
	var payload model.ReviewContradictionPayload
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return fmt.Errorf("decode contradiction payload: %w", err)
	}
	switch action {
	case "keep_a":
		if payload.MemoryB.ID == "" {
			return errors.New("contradiction payload missing memory_b id")
		}
		if err := h.store.ArchiveMemoryInHub(payload.MemoryB.ID, n.HubID); err != nil {
			return fmt.Errorf("archive memory_b: %w", err)
		}
	case "keep_b":
		if payload.MemoryA.ID == "" {
			return errors.New("contradiction payload missing memory_a id")
		}
		if err := h.store.ArchiveMemoryInHub(payload.MemoryA.ID, n.HubID); err != nil {
			return fmt.Errorf("archive memory_a: %w", err)
		}
	}
	return nil
}

func (h *NotificationsHandler) dispatchTopicMerge(ctx context.Context, n *model.Notification) error {
	if n.HubID == "" || len(n.Payload) == 0 {
		return errors.New("topic_merge notification missing hub or payload")
	}
	var payload model.ReviewTopicMergePayload
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return err
	}
	sourceIDs := make([]string, 0, len(payload.SourceTopics))
	for _, src := range payload.SourceTopics {
		sourceIDs = append(sourceIDs, src.ID)
	}
	_, err := h.store.MergeTopics(ctx, n.HubID, payload.TargetTopic.ID, sourceIDs)
	return err
}

func (h *NotificationsHandler) dispatchTopicRestructure(ctx context.Context, n *model.Notification) error {
	if n.HubID == "" || len(n.Payload) == 0 {
		return errors.New("topic_restructure notification missing hub or payload")
	}
	var payload model.ReviewTopicRestructurePayload
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return err
	}
	return h.store.ApplyTopicRestructure(ctx, n.HubID, payload.ChildTopic.ID, payload.ParentTopic.ID)
}

func (h *NotificationsHandler) dispatchHubInviteAction(ctx context.Context, n *model.Notification, userID string, action string) error {
	if n.SourceID == "" {
		return errors.New("hub_invite notification missing source id")
	}
	switch action {
	case "accept":
		// Resolve member cap. Cap is enforced atomically inside AcceptHubInviteByID.
		memberCap := -1
		if inv, err := h.store.GetHubInvite(n.SourceID); err == nil && inv != nil {
			cap, capErr := h.store.GetHubMemberCap(ctx, inv.HubID)
			if capErr != nil {
				return fmt.Errorf("check hub member cap: %w", capErr)
			}
			memberCap = cap
		}

		invite, err := h.store.AcceptHubInviteByID(ctx, n.SourceID, userID, memberCap)
		if err != nil {
			return err
		}
		// Invalidate the accepting user's effective plan cache so hub elevation
		// takes effect immediately.
		if h.invalidateUserPlan != nil {
			if invErr := h.invalidateUserPlan(ctx, userID); invErr != nil {
				slog.Warn("plan cache invalidation failed after notification invite accept",
					"error", invErr, "user_id", userID)
			}
		}
		// Update seat count for billing
		if h.billing != nil {
			if seatErr := h.billing.UpdateHubSeatCount(ctx, invite.HubID); seatErr != nil {
				slog.Warn("seat count update failed after notification invite accept",
					"error", seatErr, "hub_id", invite.HubID)
			}
		}
		onHubInviteAccepted(ctx, h.store, h.events, invite, userID, false)
		events.PublishHubMembersChanged(ctx, h.events, invite.HubID, userID)
		// User-axis emit mirrors hubs.go:AcceptInvite — the new
		// member's hub list can't be reached by the hub-axis event
		// until they reconnect with updated HubRoles.
		events.PublishHubListChanged(ctx, h.events, userID, invite.HubID, "membership_joined")
		return nil
	case "decline":
		// Snapshot the invite BEFORE the decline flips its state, so
		// onHubInviteDeclined can fan outcome receipts to both
		// inviter and invitee from a consistent view of the row.
		invite, err := h.store.GetHubInvite(n.SourceID)
		if err != nil {
			return store.ErrHubInviteNotFound
		}
		if err := h.store.DeclineHubInviteByID(ctx, n.SourceID, userID); err != nil {
			return err
		}
		onHubInviteDeclined(ctx, h.store, h.events, invite)
		return nil
	}
	return nil
}

// dispatchHubOwnershipTransferAction routes notification-native
// accept / decline actions on a hub_ownership_transfer row to the
// existing ownership-transfer primitives. The notification's
// source_id is the transfer id; the caller is the addressed target.
//
// Authorization defense: GetHubOwnershipTransfer + a target-match
// check run before any mutation so a user cannot accept/decline a
// transfer whose notification is visible to them through some other
// path (there shouldn't be one, but the check is cheap).
//
// Side effects on success:
//   - accept: store.AcceptHubOwnershipTransfer flips owner_id +
//     member roles + marks the transfer accepted, then the shared
//     helper emits a hub_ownership_transferred receipt to the old
//     owner. resolveExistingNotification is false because the
//     handler's Resolve flow already writes this specific
//     notification row.
//   - decline: store.CancelHubOwnershipTransfer tombstones the
//     transfer. No additional notification writes — the handler's
//     Resolve flow carries the declined resolution on this row.
func (h *NotificationsHandler) dispatchHubOwnershipTransferAction(ctx context.Context, n *model.Notification, userID string, action string) error {
	if n.SourceID == "" {
		return errors.New("hub_ownership_transfer notification missing source id")
	}
	transfer, err := h.store.GetHubOwnershipTransfer(n.SourceID)
	if err != nil {
		return store.ErrHubTransferNotFound
	}
	if transfer.TargetUserID != userID {
		// Belt-and-suspenders: the notification's visibility clause
		// already pins recipient_user_id to the caller, but a stray
		// row mismatch would be a silent role swap otherwise.
		return store.ErrHubTransferNotFound
	}
	switch action {
	case "accept":
		// Snapshot the current owner before the accept path rewrites
		// roles so the old-owner receipt fans out to the right user.
		hub, err := h.store.GetHub(transfer.HubID)
		if err != nil {
			return err
		}

		// Resolve ownership cap for the target user. The cap is enforced
		// atomically inside AcceptHubOwnershipTransfer with advisory lock.
		maxFreeTeamHubs := -1
		if h.ownership != nil {
			ent, entErr := h.ownership.ResolveOwnershipEntitlements(ctx, userID)
			if entErr != nil {
				return entErr
			}
			maxFreeTeamHubs = ent.MaxOwnedFreeTeamHubs
		}

		oldOwnerID := hub.OwnerID
		if _, err := h.store.AcceptHubOwnershipTransfer(transfer.HubID, transfer.ID, userID, maxFreeTeamHubs); err != nil {
			return err
		}
		// Transfer billing contact to the new owner
		if h.billing != nil {
			if billingErr := h.billing.TransferHubBilling(ctx, transfer.HubID, userID); billingErr != nil {
				slog.Warn("hub billing transfer failed after notification ownership accept",
					"error", billingErr, "hub_id", transfer.HubID, "new_owner", userID)
			}
		}
		onHubOwnershipTransferAccepted(ctx, h.store, h.events, transfer, oldOwnerID, userID, false)
		events.PublishHubMembersChanged(ctx, h.events, transfer.HubID, userID)
		// User-axis fan-out for both parties — role state flipped
		// for each, same as hubs.go:AcceptOwnershipTransfer.
		events.PublishHubListChanged(ctx, h.events, oldOwnerID, transfer.HubID, "metadata_updated")
		events.PublishHubListChanged(ctx, h.events, userID, transfer.HubID, "metadata_updated")
		return nil
	case "decline":
		return h.store.CancelHubOwnershipTransfer(transfer.HubID, transfer.ID)
	}
	return nil
}

// mapStoreErrToAPI maps common hub-invite / contradiction /
// ownership-transfer sentinel errors to API tuples. Returns ok=false
// when the error is not a recognized sentinel so the caller falls
// through to a 500.
func mapStoreErrToAPI(err error) (code, msg string, status int, ok bool) {
	switch {
	case errors.Is(err, store.ErrHubInviteNotFound):
		return "invite_not_found", "Hub invite not found", http.StatusNotFound, true
	case errors.Is(err, store.ErrHubInviteExpired):
		return "invite_expired", "Hub invite has expired", http.StatusConflict, true
	case errors.Is(err, store.ErrHubInviteUsed):
		return "invite_used", "Hub invite already accepted", http.StatusConflict, true
	case errors.Is(err, store.ErrHubInviteRevoked):
		return "invite_revoked", "Hub invite has been revoked", http.StatusConflict, true
	case errors.Is(err, store.ErrHubInviteNotAddressable):
		return "invite_not_addressable", "Hub invite has no invitee user id", http.StatusConflict, true
	case errors.Is(err, store.ErrHubInviteWrongInvitee):
		return "invite_wrong_invitee", "Hub invite is addressed to a different user", http.StatusForbidden, true
	case errors.Is(err, store.ErrHubAlreadyMember):
		return "already_member", "You are already a member of this hub", http.StatusConflict, true
	case errors.Is(err, store.ErrHubTransferNotFound):
		return "transfer_not_found", "Ownership transfer not found", http.StatusNotFound, true
	case errors.Is(err, store.ErrHubTransferExpired):
		return "transfer_expired", "Ownership transfer has expired", http.StatusConflict, true
	case errors.Is(err, store.ErrHubTransferAccepted):
		return "transfer_accepted", "Ownership transfer already accepted", http.StatusConflict, true
	case errors.Is(err, store.ErrHubTransferCanceled):
		return "transfer_canceled", "Ownership transfer has been canceled", http.StatusConflict, true
	case errors.Is(err, store.ErrHubTransferInvalid):
		return "transfer_invalid", "Ownership transfer is no longer valid", http.StatusConflict, true
	case errors.Is(err, store.ErrMemberCapExceeded):
		return "member_cap_exceeded", "This hub has reached its member limit. Upgrade the hub plan for more seats.", http.StatusForbidden, true
	case errors.Is(err, store.ErrOwnershipCapExceeded):
		return "ownership_cap_exceeded", "You have reached the maximum number of free team hubs for your plan.", http.StatusForbidden, true
	}
	return "", "", 0, false
}

// mapTopicMergeError translates store.MergeTopics sentinel errors
// into API error tuples. Returns ok=false for unrecognized errors so
// the caller falls through to a 500.
func mapTopicMergeError(err error) (status int, code, msg string, ok bool) {
	switch {
	case errors.Is(err, store.ErrTopicMergeEmptySources):
		return http.StatusBadRequest, "merge_empty_sources", "Topic merge requires at least one source topic", true
	case errors.Is(err, store.ErrTopicMergeTargetIsSource):
		return http.StatusBadRequest, "merge_target_is_source", "Topic merge target cannot also be a source", true
	case errors.Is(err, store.ErrTopicMergeTargetNotInHub):
		return http.StatusConflict, "merge_target_missing", "Topic merge target is no longer in this hub", true
	case errors.Is(err, store.ErrTopicMergeSourceNotInHub):
		return http.StatusConflict, "merge_source_missing", "At least one source topic is no longer in this hub", true
	case errors.Is(err, store.ErrTopicMergeCycle):
		return http.StatusConflict, "merge_cycle", "Topic merge would create a cycle in the topic tree", true
	case errors.Is(err, store.ErrTopicMergeArchived):
		return http.StatusConflict, "merge_topic_archived", "Topic merge involves an archived topic — restore it first", true
	}
	return 0, "", "", false
}

// mapTopicRestructureError translates store.ApplyTopicRestructure
// sentinel errors into API error tuples.
func mapTopicRestructureError(err error) (status int, code, msg string, ok bool) {
	switch {
	case errors.Is(err, store.ErrTopicRestructureSelfParent):
		return http.StatusBadRequest, "restructure_self_parent", "A topic cannot be its own parent", true
	case errors.Is(err, store.ErrTopicRestructureChildNotInHub):
		return http.StatusConflict, "restructure_child_missing", "Topic restructure child is no longer in this hub", true
	case errors.Is(err, store.ErrTopicRestructureParentNotInHub):
		return http.StatusConflict, "restructure_parent_missing", "Topic restructure parent is no longer in this hub", true
	case errors.Is(err, store.ErrTopicRestructureCycle):
		return http.StatusConflict, "restructure_cycle", "Topic restructure would create a cycle in the topic tree", true
	case errors.Is(err, store.ErrTopicRestructureArchived):
		return http.StatusConflict, "restructure_topic_archived", "Topic restructure involves an archived topic — restore it first", true
	}
	return 0, "", "", false
}

// fmtSscanInt is a tiny fmt.Sscanf wrapper used to parse the limit
// query param. Kept as a function so the handler does not need
// another import block for strconv.
func fmtSscanInt(s string, out *int) (int, error) {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not an integer")
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return 0, errors.New("out of range")
		}
	}
	*out = n
	return len(s), nil
}
