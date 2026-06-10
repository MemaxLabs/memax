package handler

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminNotificationsHandler serves /v1/admin/notifications/* endpoints.
type AdminNotificationsHandler struct {
	store            store.Store
	events           events.Publisher
	enqueueBroadcast func(kind string, payload json.RawMessage, batchID, adminID string) error // nil = broadcast unavailable
}

// NewAdminNotificationsHandler creates a new handler.
func NewAdminNotificationsHandler(s store.Store) *AdminNotificationsHandler {
	return &AdminNotificationsHandler{store: s}
}

// SetEvents wires the SSE publisher.
func (h *AdminNotificationsHandler) SetEvents(p events.Publisher) {
	h.events = p
}

// SetEnqueueBroadcast wires the callback that inserts a broadcast job.
// The callback must return an error if queue insertion fails so the
// handler can propagate the failure to the admin rather than returning
// 202 Accepted for a job that was never enqueued.
func (h *AdminNotificationsHandler) SetEnqueueBroadcast(fn func(kind string, payload json.RawMessage, batchID, adminID string) error) {
	h.enqueueBroadcast = fn
}

// --- Request / response types ---

type adminNotifTargeting struct {
	Mode          string   `json:"mode"`            // "users", "hub", "all"
	UserIDs       []string `json:"user_ids"`        // mode=users
	Emails        []string `json:"emails"`          // mode=users
	HubID         string   `json:"hub_id"`          // mode=hub
	HubMemberRole string   `json:"hub_member_role"` // mode=hub (optional)
}

type adminSendNotifRequest struct {
	Kind      string              `json:"kind"`
	Targeting adminNotifTargeting `json:"targeting"`
	Payload   json.RawMessage     `json:"payload"`
}

type adminSendNotifResult struct {
	BatchID      string   `json:"batch_id"`
	SentCount    int      `json:"sent_count"`
	FailedEmails []string `json:"failed_emails"`
}

type adminNotifBatch struct {
	BatchID        string    `json:"batch_id"`
	Kind           string    `json:"kind"`
	PayloadPreview string    `json:"payload_preview"`
	RecipientCount int       `json:"recipient_count"`
	CreatedAt      time.Time `json:"created_at"`
}

// Send creates admin notifications for the specified recipients.
// POST /v1/admin/notifications/send
func (h *AdminNotificationsHandler) Send(w http.ResponseWriter, r *http.Request) {
	adminID := GetUserID(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req adminSendNotifRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	// Validate kind
	sourceKind := ""
	switch req.Kind {
	case model.NotificationKindSystemNotice:
		sourceKind = model.NotificationSourceSystemNotice
	case model.NotificationKindGiftInviteLink:
		sourceKind = model.NotificationSourceGift
	default:
		writeError(w, http.StatusBadRequest, "invalid_kind",
			"Kind must be system_notice or gift_invite_link.")
		return
	}

	// Validate payload against the kind-specific contract the inbox renderer
	// expects. Reject payloads that would render blank or crash the client.
	if err := validateAdminNotifPayload(req.Kind, req.Payload); err != "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", err)
		return
	}

	// Validate targeting mode
	switch req.Targeting.Mode {
	case "users", "hub", "all":
		// ok
	default:
		writeError(w, http.StatusBadRequest, "invalid_targeting",
			"Targeting mode must be users, hub, or all.")
		return
	}

	batchID := uuid.New().String()

	// Resolve target user IDs
	var targetUserIDs []string
	var failedEmails []string

	switch req.Targeting.Mode {
	case "users":
		targetUserIDs, failedEmails = h.resolveUserTargets(req.Targeting.UserIDs, req.Targeting.Emails)
		if len(targetUserIDs) == 0 {
			writeError(w, http.StatusBadRequest, "no_recipients",
				"No valid recipients found. Check user IDs and emails.")
			return
		}

	case "hub":
		if req.Targeting.HubID == "" {
			writeError(w, http.StatusBadRequest, "missing_hub_id",
				"Hub targeting requires hub_id.")
			return
		}
		members, err := h.store.ListHubMembers(req.Targeting.HubID)
		if err != nil {
			slog.Error("failed to list hub members", "error", err, "hub_id", req.Targeting.HubID)
			writeError(w, http.StatusInternalServerError, "store_error", "Failed to list hub members.")
			return
		}
		for _, m := range members {
			if req.Targeting.HubMemberRole != "" && m.Role != req.Targeting.HubMemberRole {
				continue
			}
			targetUserIDs = append(targetUserIDs, m.UserID)
		}
		if len(targetUserIDs) == 0 {
			writeError(w, http.StatusBadRequest, "no_recipients",
				"No hub members match the targeting criteria.")
			return
		}

	case "all":
		if h.enqueueBroadcast == nil {
			writeError(w, http.StatusServiceUnavailable, "broadcast_unavailable",
				"Broadcast is not available (queue not configured).")
			return
		}
		if err := h.enqueueBroadcast(req.Kind, req.Payload, batchID, adminID); err != nil {
			slog.Error("failed to enqueue broadcast notification",
				"error", err, "batch_id", batchID, "kind", req.Kind)
			writeError(w, http.StatusInternalServerError, "enqueue_failed",
				"Failed to enqueue broadcast job. No notifications were sent.")
			return
		}
		slog.Info("admin broadcast notification enqueued",
			"batch_id", batchID, "kind", req.Kind, "admin_id", adminID)
		trackRequest(r, "api.admin.notifications.broadcast", map[string]any{
			"batch_id": batchID, "kind": req.Kind,
		})
		writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: adminSendNotifResult{
			BatchID:      batchID,
			SentCount:    0, // async — count unknown until worker finishes
			FailedEmails: []string{},
		}})
		return
	}

	// Direct fanout for users/hub modes (inline, ≤ reasonable count)
	sentCount := 0
	for _, userID := range targetUserIDs {
		n := &model.Notification{
			ID:              uuid.New().String(),
			Audience:        model.AudienceUser,
			RecipientUserID: userID,
			Kind:            req.Kind,
			Status:          model.NotificationStatusPending,
			SourceKind:      sourceKind,
			SourceID:        batchID + ":" + userID,
			Payload:         req.Payload,
			CreatedAt:       time.Now(),
		}
		if err := h.store.CreateNotification(n); err != nil {
			slog.Warn("failed to create admin notification",
				"error", err, "user_id", userID, "batch_id", batchID)
			continue
		}
		events.PublishNotificationCreated(r.Context(), h.events, n)
		sentCount++
	}

	slog.Info("admin notifications sent",
		"batch_id", batchID, "kind", req.Kind, "sent", sentCount, "admin_id", adminID)
	trackRequest(r, "api.admin.notifications.send", map[string]any{
		"batch_id": batchID, "kind": req.Kind, "sent": sentCount,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: adminSendNotifResult{
		BatchID:      batchID,
		SentCount:    sentCount,
		FailedEmails: failedEmails,
	}})
}

// resolveUserTargets converts user_ids + emails into deduplicated,
// validated user IDs. User IDs are verified via GetUser — invalid IDs
// are silently dropped so the caller sees an accurate sent_count.
func (h *AdminNotificationsHandler) resolveUserTargets(userIDs, emails []string) (resolved []string, failed []string) {
	seen := make(map[string]bool, len(userIDs)+len(emails))

	for _, uid := range userIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" || seen[uid] {
			continue
		}
		// Validate the user actually exists before accepting the ID.
		if _, err := h.store.GetUser(uid); err != nil {
			continue // invalid/unknown ID — drop silently
		}
		seen[uid] = true
		resolved = append(resolved, uid)
	}

	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		u, err := h.store.GetUserByCanonicalEmail(email)
		if err != nil || u == nil {
			failed = append(failed, email)
			continue
		}
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		resolved = append(resolved, u.ID)
	}

	if failed == nil {
		failed = []string{}
	}
	return resolved, failed
}

// ListSent returns admin-sent notification batches.
// GET /v1/admin/notifications/sent
func (h *AdminNotificationsHandler) ListSent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	// Query notifications with admin source kinds, grouped by batch prefix.
	sourceKinds := []string{
		model.NotificationSourceSystemNotice,
		model.NotificationSourceGift,
	}
	kindFilter := q.Get("kind")

	ctx := r.Context()

	// Use a raw query to aggregate by batch.
	// source_id format: "{batchID}:{userID}" — batch_id is the part before ':'
	rows, err := h.store.QueryAdminNotificationBatches(ctx, store.AdminNotifBatchOpts{
		SourceKinds: sourceKinds,
		KindFilter:  kindFilter,
		Limit:       limit + 1,
		Cursor:      q.Get("cursor"),
	})
	if err != nil {
		slog.Error("failed to list admin notification batches", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error",
			"Failed to list sent notifications.")
		return
	}

	nextCursor := ""
	if len(rows) > limit {
		nextCursor = rows[limit-1].CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
		rows = rows[:limit]
	}

	// Build response
	batches := make([]adminNotifBatch, 0, len(rows))
	for _, row := range rows {
		batches = append(batches, adminNotifBatch{
			BatchID:        row.BatchID,
			Kind:           row.Kind,
			PayloadPreview: row.PayloadPreview,
			RecipientCount: row.RecipientCount,
			CreatedAt:      row.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"batches":     batches,
		"next_cursor": nextCursor,
	}})
}

// validateAdminNotifPayload checks that the payload contains the fields
// the inbox renderer needs for safe rendering. Returns an error message
// string (empty = valid). This prevents admins from sending payloads
// that would render blank or crash the client.
func validateAdminNotifPayload(kind string, raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '{' {
		return "Payload must be a JSON object."
	}

	switch kind {
	case model.NotificationKindSystemNotice:
		var p model.SystemNoticePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return "Invalid system_notice payload: " + err.Error()
		}
		if p.Title == "" && p.Body == "" {
			return "system_notice requires at least a title or body."
		}
		return ""

	case model.NotificationKindGiftInviteLink:
		var p model.GiftInviteLinkPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return "Invalid gift_invite_link payload: " + err.Error()
		}
		if p.Token == "" {
			return "gift_invite_link requires a token."
		}
		if p.Sender.ID == "" && p.Sender.DisplayName == "" {
			return "gift_invite_link requires sender with at least id or display name."
		}
		if p.ExpiresAt.IsZero() {
			return "gift_invite_link requires expires_at."
		}
		return ""

	default:
		return "Unknown notification kind."
	}
}

// --- Plan 18 §7 super-notif funnel ---
//
// Operator-only admin surface — never exposed via the public SDK.
// Web side calls these through @/lib/admin-client/ per the admin
// boundary rule in CLAUDE.md.

type adminSuperFunnelParentBucket struct {
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	Count      int    `json:"count"`
}

type adminSuperFunnelItemRow struct {
	CohortDay  string `json:"cohort_day"` // YYYY-MM-DD
	ItemID     string `json:"item_id"`
	Recipients int    `json:"recipients"`
	Viewed     int    `json:"viewed"`
	Completed  int    `json:"completed"`
}

type adminSuperFunnelResponse struct {
	Kind       string                         `json:"kind"`
	SourceKind string                         `json:"source_kind,omitempty"`
	WindowDays int                            `json:"window_days"`
	Parent     []adminSuperFunnelParentBucket `json:"parent"`
	Items      []adminSuperFunnelItemRow      `json:"items"`
}

// SuperFunnel — GET /v1/admin/notifications/super
//
// Query params:
//   - kind (required)        — "checklist" or "digest"
//   - source_kind (optional) — narrows the kind (e.g. "onboarding")
//   - window_days (optional) — defaults to 30
//
// Returns the two funnel slices from plan 18 §7.3: a parent bucket
// histogram (status × resolution) and a per-(cohort_day, item_id)
// row list. CSV export is a separate query param (?format=csv).
func (h *AdminNotificationsHandler) SuperFunnel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind == "" {
		writeError(w, http.StatusBadRequest, "missing_kind",
			"kind query parameter is required (checklist or digest)")
		return
	}
	if kind != model.NotificationKindChecklist && kind != model.NotificationKindDigest {
		writeError(w, http.StatusBadRequest, "invalid_kind",
			"kind must be 'checklist' or 'digest'")
		return
	}
	sourceKind := q.Get("source_kind")

	windowDays := 30
	if raw := q.Get("window_days"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 365 {
			windowDays = v
		}
	}
	since := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)

	ctx := r.Context()
	parentRows, err := h.store.QuerySuperNotifParentFunnel(ctx, kind, sourceKind, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	itemRows, err := h.store.QuerySuperNotifItemFunnel(ctx, kind, sourceKind, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	resp := adminSuperFunnelResponse{
		Kind:       kind,
		SourceKind: sourceKind,
		WindowDays: windowDays,
		Parent:     make([]adminSuperFunnelParentBucket, 0, len(parentRows)),
		Items:      make([]adminSuperFunnelItemRow, 0, len(itemRows)),
	}
	for _, p := range parentRows {
		resp.Parent = append(resp.Parent, adminSuperFunnelParentBucket{
			Status:     p.Status,
			Resolution: p.Resolution,
			Count:      p.Count,
		})
	}
	for _, it := range itemRows {
		resp.Items = append(resp.Items, adminSuperFunnelItemRow{
			CohortDay:  it.CohortDay.UTC().Format("2006-01-02"),
			ItemID:     it.ItemID,
			Recipients: it.Recipient,
			Viewed:     it.Viewed,
			Completed:  it.Completed,
		})
	}

	if q.Get("format") == "csv" {
		writeSuperFunnelCSV(w, resp)
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

// writeSuperFunnelCSV streams the item-level funnel as a flat CSV.
// Parent funnel is excluded — operators using CSV typically want the
// time-series data; the parent histogram is small enough that the
// admin UI renders it directly from the JSON response.
func writeSuperFunnelCSV(w http.ResponseWriter, resp adminSuperFunnelResponse) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition",
		"attachment; filename=\"super-notif-funnel.csv\"")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"cohort_day", "item_id", "recipients", "viewed", "completed"})
	for _, row := range resp.Items {
		_ = cw.Write([]string{
			row.CohortDay,
			row.ItemID,
			strconv.Itoa(row.Recipients),
			strconv.Itoa(row.Viewed),
			strconv.Itoa(row.Completed),
		})
	}
}
