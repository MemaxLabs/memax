package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// CampaignEnqueueFn is the callback wired at server startup that
// inserts a River job for a campaign send. When scheduledAt is non-nil
// the job is scheduled; otherwise it runs as soon as the worker picks
// it up.
type CampaignEnqueueFn func(ctx context.Context, campaignID, initiatedBy string, scheduledAt *time.Time) error

// AdminCampaignsHandler owns the /v1/admin/campaigns* surface.
type AdminCampaignsHandler struct {
	store                store.Store
	enqueueCampaign      CampaignEnqueueFn
	templates            *email.TemplateManager
	enqueueRenderedEmail func(to, subject, html, text string, tags map[string]string) error
}

// NewAdminCampaignsHandler constructs the handler. Enqueue is optional
// and may be wired later via SetEnqueue; without it, Send and Schedule
// return 503.
func NewAdminCampaignsHandler(s store.Store) *AdminCampaignsHandler {
	return &AdminCampaignsHandler{store: s}
}

// SetEnqueue wires the campaign send enqueue callback.
func (h *AdminCampaignsHandler) SetEnqueue(fn CampaignEnqueueFn) {
	h.enqueueCampaign = fn
}

// SetTemplateManager wires the template renderer used by TestSend to
// wrap the campaign's current draft body in the brand layout. Without
// it, TestSend returns 503.
func (h *AdminCampaignsHandler) SetTemplateManager(tm *email.TemplateManager) {
	h.templates = tm
}

// SetEnqueueRenderedEmail wires the raw email enqueue used by TestSend.
// Without it, TestSend returns 503.
func (h *AdminCampaignsHandler) SetEnqueueRenderedEmail(fn func(to, subject, html, text string, tags map[string]string) error) {
	h.enqueueRenderedEmail = fn
}

type campaignCreateRequest struct {
	Name             string                  `json:"name"`
	Kind             string                  `json:"kind"`
	Channel          string                  `json:"channel"`
	AudienceID       string                  `json:"audience_id,omitempty"`
	AudienceSnapshot *model.AudienceSnapshot `json:"audience_snapshot,omitempty"`
	Content          json.RawMessage         `json:"content"`
}

type campaignUpdateRequest struct {
	Name             *string                 `json:"name,omitempty"`
	AudienceID       *string                 `json:"audience_id,omitempty"`
	AudienceSnapshot *model.AudienceSnapshot `json:"audience_snapshot,omitempty"`
	Content          json.RawMessage         `json:"content,omitempty"`
}

type campaignScheduleRequest struct {
	ScheduledAt string `json:"scheduled_at"`
}

type campaignDetailResponse struct {
	Campaign *model.Campaign         `json:"campaign"`
	Audit    []model.CommsAuditEntry `json:"audit"`
}

// List returns campaigns filtered by optional status, newest-first.
// GET /v1/admin/campaigns
func (h *AdminCampaignsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := model.CampaignListOpts{
		Status: q.Get("status"),
		Cursor: q.Get("cursor"),
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	campaigns, nextCursor, err := h.store.ListCampaigns(r.Context(), opts)
	if err != nil {
		slog.Error("list campaigns failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not list campaigns.")
		return
	}
	if campaigns == nil {
		campaigns = []model.Campaign{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"campaigns":   campaigns,
		"next_cursor": nextCursor,
	}})
}

// Get returns a single campaign plus its audit timeline.
// GET /v1/admin/campaigns/{id}
func (h *AdminCampaignsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := h.store.GetCampaign(r.Context(), id)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if err != nil {
		slog.Error("get campaign failed", "error", err, "campaign_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign.")
		return
	}
	audit, err := h.store.ListCommsAudit(r.Context(), model.CommsAuditResourceCampaign, id)
	if err != nil {
		slog.Warn("list campaign audit failed", "error", err, "campaign_id", id)
		audit = []model.CommsAuditEntry{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: campaignDetailResponse{
		Campaign: c,
		Audit:    audit,
	}})
}

// Create inserts a new draft campaign.
// POST /v1/admin/campaigns
func (h *AdminCampaignsHandler) Create(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req campaignCreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Channel == "" {
		// kind=email_announcement implies channel=email; otherwise default to in-app.
		if req.Kind == model.CampaignKindEmailAnnouncement {
			req.Channel = model.CampaignChannelEmail
		} else {
			req.Channel = model.CampaignChannelInapp
		}
	}
	if msg := validateCampaignKindAndContent(req.Kind, req.Content); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_content", msg)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_campaign", "Name is required.")
		return
	}
	if !model.ValidCampaignChannel(req.Channel) {
		writeError(w, http.StatusBadRequest, "unsupported_channel", "Channel must be inapp or email.")
		return
	}
	if msg := validateChannelKindCompat(req.Channel, req.Kind); msg != "" {
		writeError(w, http.StatusBadRequest, "channel_kind_mismatch", msg)
		return
	}

	snapshot, msg := h.resolveAudienceForRequest(r.Context(), req.AudienceID, req.AudienceSnapshot)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_audience", msg)
		return
	}

	c := &model.Campaign{
		ID:               uuid.New().String(),
		Name:             req.Name,
		Channel:          req.Channel,
		Kind:             req.Kind,
		Status:           model.CampaignStatusDraft,
		AudienceID:       optionalString(req.AudienceID),
		AudienceSnapshot: snapshot,
		Content:          req.Content,
		CreatedBy:        actorID,
	}
	if err := h.store.CreateCampaign(r.Context(), c); err != nil {
		slog.Error("create campaign failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not create campaign.")
		return
	}
	writeCampaignAudit(r.Context(), h.store, c.ID, model.CommsAuditActionCreated, actorID, map[string]any{
		"name": c.Name,
		"kind": c.Kind,
	})
	fresh, err := h.store.GetCampaign(r.Context(), c.ID)
	if err == nil {
		c = fresh
	}
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: c})
}

// UpdateDraft mutates editable fields on a draft campaign.
// PATCH /v1/admin/campaigns/{id}
func (h *AdminCampaignsHandler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req campaignUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	patch := store.CampaignDraftPatch{UpdatedBy: actorID}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		patch.Name = &trimmed
	}

	// Audience resolution at patch time: audience_id is authoritative.
	// When the client switches to a different saved audience we must
	// re-snapshot so the send target matches the displayed audience.
	// When the client switches to inline we must clear audience_id so
	// the summary doesn't reference a stale row.
	if req.AudienceID != nil && *req.AudienceID != "" {
		snap, msg := h.resolveAudienceForRequest(r.Context(), *req.AudienceID, nil)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "invalid_audience", msg)
			return
		}
		patch.AudienceID = req.AudienceID
		patch.AudienceSnapshot = snap
	} else if req.AudienceSnapshot != nil {
		if !model.ValidAudienceRuleType(req.AudienceSnapshot.RuleType) {
			writeError(w, http.StatusBadRequest, "invalid_audience",
				"Audience rule_type must be all, users, or hub.")
			return
		}
		empty := ""
		patch.AudienceID = &empty
		patch.AudienceSnapshot = req.AudienceSnapshot
	} else if req.AudienceID != nil {
		// Explicit clear: empty string with no replacement snapshot.
		empty := ""
		patch.AudienceID = &empty
	}

	if len(req.Content) > 0 {
		// Validate against the existing campaign's kind.
		current, err := h.store.GetCampaign(r.Context(), id)
		if errors.Is(err, store.ErrCampaignNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign.")
			return
		}
		if msg := validateCampaignKindAndContent(current.Kind, req.Content); msg != "" {
			writeError(w, http.StatusBadRequest, "invalid_content", msg)
			return
		}
		patch.Content = req.Content
	}

	err = h.store.UpdateCampaignDraft(r.Context(), id, patch)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if errors.Is(err, store.ErrCampaignStateMismatch) {
		writeError(w, http.StatusConflict, "not_draft", "Only draft campaigns can be edited.")
		return
	}
	if err != nil {
		slog.Error("update campaign draft failed", "error", err, "campaign_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not update campaign.")
		return
	}
	writeCampaignAudit(r.Context(), h.store, id, model.CommsAuditActionUpdated, actorID, nil)
	fresh, _ := h.store.GetCampaign(r.Context(), id)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: fresh})
}

// Delete removes a draft or cancelled campaign.
// DELETE /v1/admin/campaigns/{id}
func (h *AdminCampaignsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	// Capture the current state so we can audit the delete before the
	// row is gone.
	_, err := h.store.GetCampaign(r.Context(), id)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign.")
		return
	}
	err = h.store.DeleteCampaign(r.Context(), id, actorID)
	if errors.Is(err, store.ErrCampaignStateMismatch) {
		writeError(w, http.StatusConflict, "cannot_delete",
			"Only draft or cancelled campaigns can be deleted.")
		return
	}
	if err != nil {
		slog.Error("delete campaign failed", "error", err, "campaign_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not delete campaign.")
		return
	}
	writeCampaignAudit(r.Context(), h.store, id, model.CommsAuditActionDeleted, actorID, nil)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "deleted", "id": id}})
}

// Send transitions a draft campaign to sending and enqueues the worker.
// POST /v1/admin/campaigns/{id}/send
func (h *AdminCampaignsHandler) Send(w http.ResponseWriter, r *http.Request) {
	h.transitionToInFlight(w, r, model.CampaignStatusDraft, nil)
}

// Schedule transitions a draft campaign to scheduled and enqueues a
// delayed worker. Body: { "scheduled_at": "<RFC3339>" }.
// POST /v1/admin/campaigns/{id}/schedule
func (h *AdminCampaignsHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req campaignScheduleRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	t, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time", "scheduled_at must be RFC3339.")
		return
	}
	if t.Before(time.Now().Add(-1 * time.Minute)) {
		writeError(w, http.StatusBadRequest, "invalid_time", "scheduled_at must not be in the past.")
		return
	}
	h.transitionToInFlight(w, r, model.CampaignStatusDraft, &t)
}

// Cancel moves draft/scheduled campaigns to cancelled. The River job
// for scheduled sends remains enqueued; the worker no-ops when it
// sees the cancelled status.
// POST /v1/admin/campaigns/{id}/cancel
func (h *AdminCampaignsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	err := h.store.CancelCampaign(r.Context(), id, actorID)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if errors.Is(err, store.ErrCampaignStateMismatch) {
		writeError(w, http.StatusConflict, "cannot_cancel",
			"Campaign is already sending, sent, cancelled, or failed.")
		return
	}
	if err != nil {
		slog.Error("cancel campaign failed", "error", err, "campaign_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not cancel campaign.")
		return
	}
	writeCampaignAudit(r.Context(), h.store, id, model.CommsAuditActionCancelled, actorID, nil)
	fresh, _ := h.store.GetCampaign(r.Context(), id)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: fresh})
}

// DeliveryStats returns per-status counts for a channel=email campaign.
// Safe to call on in-app campaigns too — returns all zeros.
// GET /v1/admin/campaigns/{id}/deliveries/stats
func (h *AdminCampaignsHandler) DeliveryStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetCampaign(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrCampaignNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load campaign.")
		return
	}
	stats, err := h.store.GetCampaignDeliveryStats(r.Context(), id)
	if err != nil {
		slog.Error("load delivery stats failed", "error", err, "campaign_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load delivery stats.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: stats})
}

// TestSend renders the campaign's current draft body through the brand
// layout and enqueues a single email to the supplied address. Intended
// for founder-level "does this actually look right" confirmation before
// broadcasting. Guarded to draft status — sending/sent campaigns have
// frozen content and should not be re-rendered ad-hoc.
//
// POST /v1/admin/campaigns/{id}/test-send
// Body: {"to": "someone@example.com"}
//
// Semantics vs the live send path:
//   - NO delivery row written (not a real campaign recipient)
//   - NO unsubscribe token minted or footer link injected (test-only;
//     the recipient is a trusted admin, not a marketing audience)
//   - Tags mark it as a test so downstream analytics can exclude it
//   - Only channel=email campaigns are supported; calling this on an
//     in-app campaign returns 400
func (h *AdminCampaignsHandler) TestSend(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil || h.enqueueRenderedEmail == nil {
		writeError(w, http.StatusServiceUnavailable, "test_send_unavailable",
			"Test send is not configured on this server.")
		return
	}

	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	to := strings.TrimSpace(req.To)
	if to == "" || !strings.Contains(to, "@") {
		writeError(w, http.StatusBadRequest, "invalid_email", "A valid recipient email is required.")
		return
	}

	campaign, err := h.store.GetCampaign(r.Context(), id)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign.")
		return
	}
	if campaign.Channel != model.CampaignChannelEmail {
		writeError(w, http.StatusBadRequest, "unsupported_channel",
			"Test send is only available for channel=email campaigns.")
		return
	}
	if campaign.Status != model.CampaignStatusDraft {
		writeError(w, http.StatusConflict, "not_draft",
			"Test send is only available while the campaign is in draft.")
		return
	}

	var payload model.EmailAnnouncementContent
	if err := json.Unmarshal(campaign.Content, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_content",
			"Campaign content is not a valid email_announcement payload.")
		return
	}
	if strings.TrimSpace(payload.Subject) == "" ||
		strings.TrimSpace(payload.BodyHTML) == "" ||
		strings.TrimSpace(payload.BodyText) == "" {
		writeError(w, http.StatusBadRequest, "missing_fields",
			"Set subject, body_html, and body_text before test-sending.")
		return
	}

	// Ad-hoc render — no unsubscribe wrapping. A test email reaching the
	// admin's own inbox should not have a live unsubscribe link that could
	// suppress the admin from their own marketing audience if they click.
	rendered, err := h.templates.RenderAdHoc(r.Context(), payload.Subject, payload.BodyHTML, payload.BodyText)
	if err != nil {
		if errors.Is(err, email.ErrOverrideLooksLikeFullDocument) {
			writeError(w, http.StatusBadRequest, "invalid_template_body", err.Error())
			return
		}
		slog.Error("campaign test-send render failed", "campaign_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "render_failed", "Could not render campaign body.")
		return
	}
	tags := map[string]string{
		"campaign_id": campaign.ID,
		"source":      "admin_campaign_test_send",
	}
	if err := h.enqueueRenderedEmail(to, "[TEST] "+rendered.Subject, rendered.HTML, rendered.Text, tags); err != nil {
		// Log the redacted address — operators can tell which admin and
		// campaign failed without the full recipient hitting structured
		// logs. (The queue-worker log + the Resend job args still carry
		// the full `to` because it is the delivery primary key; that's
		// the mail-transport boundary beyond which redaction isn't
		// meaningful. Out of scope for this endpoint.)
		slog.Error("campaign test-send enqueue failed", "campaign_id", id, "to_redacted", redactEmailForLog(to), "error", err)
		writeError(w, http.StatusInternalServerError, "enqueue_failed", "Could not enqueue test email.")
		return
	}
	actorID := GetUserID(r)
	// Persist only a redacted form of the recipient in the audit timeline
	// and analytics — test-send recipients are often personal inboxes,
	// and the full address would be visible to every admin with audit
	// read access forever. The UI surfaces the real address back to the
	// caller in the response below; that's sufficient for the "did my
	// test fire correctly" feedback loop.
	redacted := redactEmailForLog(to)
	writeCampaignAudit(r.Context(), h.store, campaign.ID, model.CommsAuditActionTestSend, actorID, map[string]any{
		"to_redacted": redacted,
	})
	trackRequest(r, "api.admin.campaigns.test_send", map[string]any{
		"campaign_id": campaign.ID,
		"to_redacted": redacted,
	})
	writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: map[string]string{
		"status":      "queued",
		"campaign_id": campaign.ID,
		"to":          to,
		"subject":     "[TEST] " + rendered.Subject,
		"queued_at":   time.Now().UTC().Format(time.RFC3339),
	}})
}

// redactEmailForLog reduces an email address to its domain plus a
// single-rune initial of the local part, e.g. "jane@acme.co" →
// "j***@acme.co", "é@example.com" → "é***@example.com". Operators can
// still spot-check which inbox a test fired to without the full
// address leaking into audit history. Malformed inputs (missing `@` or
// empty local part) collapse to "***" so we never leak raw user input
// into the log on the error path. Rune-safe for non-ASCII locals.
func redactEmailForLog(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "***"
	}
	local := email[:at]
	domain := email[at+1:]
	runes := []rune(local)
	if len(runes) == 0 {
		return "***@" + domain
	}
	return string(runes[0]) + "***@" + domain
}

// Audit returns the audit timeline for one campaign.
// GET /v1/admin/campaigns/{id}/audit
func (h *AdminCampaignsHandler) Audit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	audit, err := h.store.ListCommsAudit(r.Context(), model.CommsAuditResourceCampaign, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not list audit entries.")
		return
	}
	if audit == nil {
		audit = []model.CommsAuditEntry{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"entries": audit}})
}

// transitionToInFlight handles both send-now and scheduled-send. The
// invariant is: after this handler returns successfully, the DB row
// reflects the new status (sending or scheduled) AND a River job
// exists that will execute it. Ordering is CAS → enqueue → rollback
// on failure, so a failed enqueue does not leave a dead campaign in
// flight with no worker behind it.
//
// Editing and deleting are blocked once status != draft, so the
// frozen content/audience the worker will use cannot be mutated out
// from under a pending send.
func (h *AdminCampaignsHandler) transitionToInFlight(w http.ResponseWriter, r *http.Request, expectedStatus string, scheduledAt *time.Time) {
	if h.enqueueCampaign == nil {
		writeError(w, http.StatusServiceUnavailable, "queue_unavailable",
			"Campaign sending requires the worker queue.")
		return
	}
	actorID := GetUserID(r)
	id := r.PathValue("id")

	current, err := h.store.GetCampaign(r.Context(), id)
	if errors.Is(err, store.ErrCampaignNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load campaign.")
		return
	}
	if current.Status != expectedStatus {
		writeError(w, http.StatusConflict, "cannot_send",
			fmt.Sprintf("Campaign is %s; send requires %s.", current.Status, expectedStatus))
		return
	}
	if current.AudienceSnapshot == nil {
		writeError(w, http.StatusBadRequest, "missing_audience",
			"Campaign needs an audience snapshot before it can be sent.")
		return
	}
	if !model.ValidAudienceRuleType(current.AudienceSnapshot.RuleType) {
		writeError(w, http.StatusBadRequest, "invalid_audience",
			"Campaign audience snapshot has an unknown rule type.")
		return
	}

	if scheduledAt != nil {
		// Scheduled send: CAS draft → scheduled first so subsequent
		// edit/delete/send attempts are blocked. If enqueue fails we
		// revert to draft — the River job never got created.
		if err := h.store.ScheduleCampaign(r.Context(), id, *scheduledAt, actorID); err != nil {
			h.writeTransitionError(w, err, "schedule")
			return
		}
		if err := h.enqueueCampaign(r.Context(), id, actorID, scheduledAt); err != nil {
			slog.Error("enqueue scheduled campaign failed",
				"error", err, "campaign_id", id)
			if revertErr := h.store.RevertCampaignToDraft(r.Context(), id, model.CampaignStatusScheduled); revertErr != nil {
				slog.Error("revert scheduled campaign failed",
					"error", revertErr, "campaign_id", id,
					"note", "campaign is stuck in scheduled with no job — requires manual cancel")
			}
			writeError(w, http.StatusInternalServerError, "enqueue_failed",
				"Could not enqueue scheduled campaign.")
			return
		}
		writeCampaignAudit(r.Context(), h.store, id, model.CommsAuditActionScheduled, actorID, map[string]any{
			"scheduled_at": scheduledAt.UTC().Format(time.RFC3339),
		})
	} else {
		// Immediate send: CAS draft → sending with a fresh batch_id
		// before enqueue. The worker reads this campaign and, seeing
		// status=sending with send_started_at already set, proceeds
		// directly to fanout using the handler's batch_id.
		batchID := fmt.Sprintf("campaign:%s:%s", id, uuid.New().String()[:8])
		startedAt := time.Now().UTC()
		if err := h.store.ClaimCampaignForSend(r.Context(), id, model.CampaignStatusDraft, startedAt, batchID); err != nil {
			h.writeTransitionError(w, err, "send")
			return
		}
		if err := h.enqueueCampaign(r.Context(), id, actorID, nil); err != nil {
			slog.Error("enqueue campaign failed",
				"error", err, "campaign_id", id)
			if revertErr := h.store.RevertCampaignToDraft(r.Context(), id, model.CampaignStatusSending); revertErr != nil {
				slog.Error("revert sending campaign failed",
					"error", revertErr, "campaign_id", id,
					"note", "campaign is stuck in sending with no job — admin must cancel or a reaper must reset")
			}
			writeError(w, http.StatusInternalServerError, "enqueue_failed",
				"Could not enqueue campaign send.")
			return
		}
		writeCampaignAudit(r.Context(), h.store, id, model.CommsAuditActionSent, actorID, map[string]any{
			"batch_id": batchID,
		})
	}

	fresh, _ := h.store.GetCampaign(r.Context(), id)
	writeJSON(w, http.StatusAccepted, model.ApiResponse{Data: fresh})
}

func (h *AdminCampaignsHandler) writeTransitionError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrCampaignNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Campaign not found.")
	case errors.Is(err, store.ErrCampaignStateMismatch):
		writeError(w, http.StatusConflict, "bad_state",
			fmt.Sprintf("Campaign is not in a state that allows %s.", action))
	default:
		slog.Error("campaign transition failed", "action", action, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not transition campaign.")
	}
}

// resolveAudienceForRequest picks the authoritative snapshot for a
// request. If audienceID is set, we load it and snapshot the rule.
// Otherwise we use the inline snapshot if present.
func (h *AdminCampaignsHandler) resolveAudienceForRequest(ctx context.Context, audienceID string, inline *model.AudienceSnapshot) (*model.AudienceSnapshot, string) {
	if audienceID != "" {
		a, err := h.store.GetAudience(ctx, audienceID)
		if errors.Is(err, store.ErrAudienceNotFound) {
			return nil, "Audience not found."
		}
		if err != nil {
			return nil, "Could not load audience."
		}
		aid := a.ID
		return &model.AudienceSnapshot{
			AudienceID: &aid,
			RuleType:   a.RuleType,
			Rule:       a.Rule,
		}, ""
	}
	if inline == nil {
		return nil, "Provide audience_id or audience_snapshot."
	}
	if !model.ValidAudienceRuleType(inline.RuleType) {
		return nil, "Audience rule_type must be all, users, or hub."
	}
	return inline, ""
}

// validateCampaignKindAndContent reuses the notification payload
// validator for in-app kinds; for email kinds it enforces the
// {subject, body_html, body_text} contract.
func validateCampaignKindAndContent(kind string, content json.RawMessage) string {
	if len(content) == 0 {
		return "Content is required."
	}
	switch kind {
	case model.NotificationKindSystemNotice, model.NotificationKindGiftInviteLink:
		if msg := validateAdminNotifPayload(kind, content); msg != "" {
			return msg
		}
		return ""
	case model.CampaignKindEmailAnnouncement:
		var payload model.EmailAnnouncementContent
		if err := json.Unmarshal(content, &payload); err != nil {
			return "Email campaign content must be JSON with subject, body_html, body_text."
		}
		if strings.TrimSpace(payload.Subject) == "" {
			return "Email subject is required."
		}
		if strings.TrimSpace(payload.BodyHTML) == "" {
			return "Email body_html is required."
		}
		if strings.TrimSpace(payload.BodyText) == "" {
			return "Email body_text is required."
		}
		// Reject full-document HTML at save time so admins get immediate
		// feedback rather than failing opaquely at send time when the
		// render guard trips. Mirrors the template override guard.
		if looksLikeEmailFullDocument(payload.BodyHTML) {
			return "Email body_html must be a body-only fragment — remove <!DOCTYPE>, <html>, <head>, and <body>."
		}
		return ""
	default:
		return "Kind must be system_notice, gift_invite_link, or email_announcement."
	}
}

// looksLikeEmailFullDocument mirrors email.looksLikeFullDocument but
// lives here so the handler doesn't need an extra import dep just for
// this check. If the two ever diverge, consolidate into the email pkg.
func looksLikeEmailFullDocument(html string) bool {
	lower := strings.ToLower(html)
	for _, marker := range []string{"<!doctype", "<html", "<head", "<body"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// validateChannelKindCompat rejects mismatched channel+kind combos
// (e.g., email channel with system_notice kind, or inapp channel with
// email_announcement). Each kind is fundamentally tied to one channel.
func validateChannelKindCompat(channel, kind string) string {
	switch kind {
	case model.NotificationKindSystemNotice, model.NotificationKindGiftInviteLink:
		if channel != model.CampaignChannelInapp {
			return "Kind " + kind + " requires channel=inapp."
		}
	case model.CampaignKindEmailAnnouncement:
		if channel != model.CampaignChannelEmail {
			return "Kind email_announcement requires channel=email."
		}
	}
	return ""
}

func writeCampaignAudit(ctx context.Context, s store.Store, campaignID, action, actorID string, metadata map[string]any) {
	var md json.RawMessage
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			md = b
		}
	}
	entry := &model.CommsAuditEntry{
		ID:           uuid.New().String(),
		ResourceType: model.CommsAuditResourceCampaign,
		ResourceID:   campaignID,
		Action:       action,
		Metadata:     md,
	}
	if actorID != "" {
		entry.ActorID = &actorID
	}
	if err := s.AppendCommsAudit(ctx, entry); err != nil {
		slog.Warn("append comms audit failed",
			"error", err, "action", action, "campaign_id", campaignID)
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}
