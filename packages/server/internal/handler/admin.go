package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminHandler serves all /v1/admin/* endpoints.
type AdminHandler struct {
	store                store.Store
	enqueueEmail         func(template, to string, vars map[string]string) error
	enqueueRenderedEmail func(to, subject, html, text string, tags map[string]string) error
	appBaseURL           string
	waveCap              int // WAITLIST_WAVE_CAP — global capacity limit per wave
	templateManager      *email.TemplateManager
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(s store.Store) *AdminHandler {
	return &AdminHandler{store: s}
}

// SetEnqueueEmail wires the email enqueue callback. Called after queue client is ready.
func (h *AdminHandler) SetEnqueueEmail(fn func(template, to string, vars map[string]string) error) {
	h.enqueueEmail = fn
}

// SetEnqueueRenderedEmail wires the raw email enqueue callback for admin-triggered sends.
func (h *AdminHandler) SetEnqueueRenderedEmail(fn func(to, subject, html, text string, tags map[string]string) error) {
	h.enqueueRenderedEmail = fn
}

// SetAppBaseURL sets the base URL for building registration links.
func (h *AdminHandler) SetAppBaseURL(url string) {
	h.appBaseURL = url
}

// SetWaveCap sets the global capacity cap per wave.
func (h *AdminHandler) SetWaveCap(cap int) {
	h.waveCap = cap
}

func (h *AdminHandler) SetTemplateManager(manager *email.TemplateManager) {
	h.templateManager = manager
}

// ListWaitlist returns paginated, filtered waitlist entries.
// GET /v1/admin/waitlist
func (h *AdminHandler) ListWaitlist(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	opts := model.WaitlistListOpts{
		Status:  q.Get("status"),
		UseCase: q.Get("use_case"),
		Query:   q.Get("q"),
		Sort:    q.Get("sort"),
		Order:   q.Get("order"),
		Limit:   limit,
		Cursor:  q.Get("cursor"),
	}

	entries, nextCursor, totalCount, err := h.store.ListWaitlistEntries(r.Context(), opts)
	if err != nil {
		slog.Error("failed to list waitlist", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to list waitlist entries.")
		return
	}

	if entries == nil {
		entries = []model.WaitlistEntry{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"entries":     entries,
		"next_cursor": nextCursor,
		"has_more":    nextCursor != "",
		"total_count": totalCount,
	}})
}

// UpdateWaitlist updates a single waitlist entry (approve, reject, notes).
// PATCH /v1/admin/waitlist/{id}
func (h *AdminHandler) UpdateWaitlist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	adminUserID := GetUserID(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}

	var req struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
		Wave   *int   `json:"wave"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	// Validate status transition
	if req.Status != "" {
		switch req.Status {
		case model.WaitlistStatusApproved, model.WaitlistStatusRejected, model.WaitlistStatusPending:
			// valid — pending allows undo-reject
		default:
			writeError(w, http.StatusBadRequest, "invalid_status",
				"Status must be 'approved', 'rejected', or 'pending'.")
			return
		}
	}

	entry, err := h.store.UpdateWaitlistEntry(r.Context(), id, req.Status, req.Notes, req.Wave, adminUserID)
	if err != nil {
		slog.Error("failed to update waitlist entry", "error", err, "id", id)
		writeError(w, http.StatusNotFound, "not_found", "Waitlist entry not found.")
		return
	}

	// If approved, generate an invite token
	if req.Status == model.WaitlistStatusApproved {
		if err := h.createInviteForEntry(r, entry); err != nil {
			slog.Error("failed to create invite for approved entry", "error", err, "id", id)
			// Entry was already approved — don't fail the whole request.
			// Admin can re-invite later.
		}
	}

	trackRequest(r, "api.admin.waitlist.update", map[string]any{
		"entry_id": id, "new_status": req.Status,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: entry})
}

// BatchApprove approves multiple waitlist entries at once.
// POST /v1/admin/waitlist/batch-approve
func (h *AdminHandler) BatchApprove(w http.ResponseWriter, r *http.Request) {
	adminUserID := GetUserID(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}

	var req struct {
		IDs   []string `json:"ids"`
		Wave  int      `json:"wave"`
		Notes string   `json:"notes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "missing_ids", "At least one ID is required.")
		return
	}
	if len(req.IDs) > 200 {
		writeError(w, http.StatusBadRequest, "too_many_ids", "Maximum 200 entries per batch.")
		return
	}

	count, err := h.store.BatchApproveWaitlist(r.Context(), req.IDs, req.Wave, adminUserID, req.Notes)
	if err != nil {
		slog.Error("failed to batch approve", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to batch approve.")
		return
	}

	// Create invites for each approved entry
	inviteCount := 0
	var failed []string
	for _, id := range req.IDs {
		entry, err := h.store.GetWaitlistEntry(r.Context(), id)
		if err != nil || entry.Status != model.WaitlistStatusApproved {
			continue
		}
		if err := h.createInviteForEntry(r, entry); err != nil {
			slog.Error("failed to create invite", "error", err, "entry_id", id)
			failed = append(failed, id)
			continue
		}
		inviteCount++
	}

	trackRequest(r, "api.admin.waitlist.batch_approve", map[string]any{
		"approved_count": count, "invite_count": inviteCount, "wave": req.Wave,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"approved_count": count,
		"invite_count":   inviteCount,
		"failed":         failed,
	}})
}

// WaitlistStats returns aggregate waitlist metrics.
// GET /v1/admin/waitlist/stats
func (h *AdminHandler) WaitlistStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetWaitlistStats(r.Context())
	if err != nil {
		slog.Error("failed to get waitlist stats", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to get waitlist stats.")
		return
	}
	stats.CapacityCap = h.waveCap
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: stats})
}

// InviteByEmail creates a waitlist entry + invite for an email that may not be on the waitlist yet.
// POST /v1/admin/waitlist/invite
func (h *AdminHandler) InviteByEmail(w http.ResponseWriter, r *http.Request) {
	adminUserID := GetUserID(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}

	var req struct {
		Email string `json:"email"`
		Wave  *int   `json:"wave"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "missing_email", "Email is required.")
		return
	}

	// Check if already on waitlist
	entry, entryErr := h.store.GetWaitlistEntryByEmail(r.Context(), req.Email)
	if entryErr != nil {
		// Not found — create a new entry
		entry = &model.WaitlistEntry{
			Email:   req.Email,
			UseCase: "",
			AITools: []string{},
			Role:    "",
		}
		if err := h.store.CreateWaitlistEntry(r.Context(), entry); err != nil {
			slog.Error("failed to create waitlist entry for invite", "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "Failed to create waitlist entry.")
			return
		}
	}

	// Prevent re-inviting already-registered users
	if entry.Status == model.WaitlistStatusRegistered {
		writeError(w, http.StatusConflict, "already_registered", "This user has already registered.")
		return
	}

	// Check for active invite — store returns (nil, nil) for "none", (nil, err) for DB failure
	existing, activeErr := h.store.GetActiveInviteForEntry(r.Context(), entry.ID)
	if activeErr != nil {
		slog.Error("failed to check active invite", "error", activeErr, "entry_id", entry.ID)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to check existing invites.")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "invite_active", "An active invite already exists for this email.")
		return
	}

	// Approve the entry (only if pending or rejected — don't downgrade approved)
	if entry.Status == model.WaitlistStatusPending || entry.Status == model.WaitlistStatusRejected {
		updated, updateErr := h.store.UpdateWaitlistEntry(r.Context(), entry.ID, model.WaitlistStatusApproved, req.Notes, req.Wave, adminUserID)
		if updateErr != nil {
			slog.Error("failed to approve entry for invite", "error", updateErr, "entry_id", entry.ID)
			writeError(w, http.StatusInternalServerError, "store_error", "Failed to approve entry.")
			return
		}
		entry = updated
	}

	// Generate invite
	if err := h.createInviteForEntry(r, entry); err != nil {
		slog.Error("failed to create invite", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to create invite.")
		return
	}

	trackRequest(r, "api.admin.waitlist.invite_by_email", map[string]any{
		"email": entry.Email,
	})

	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: map[string]string{
		"status": "invited",
		"email":  entry.Email,
	}})
}

// RevokeInvite revokes an active invite by invite ID.
// DELETE /v1/admin/waitlist/invites/{invite_id}
func (h *AdminHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID := r.PathValue("invite_id")

	if err := h.store.RevokeWaitlistInvite(r.Context(), inviteID); err != nil {
		slog.Error("failed to revoke invite", "error", err, "invite_id", inviteID)
		writeError(w, http.StatusNotFound, "not_found", "Invite not found or already used/expired.")
		return
	}

	trackRequest(r, "api.admin.waitlist.revoke_invite", map[string]any{
		"invite_id": inviteID,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status": "revoked",
	}})
}

// RevokeInviteByEntry revokes the active invite for a waitlist entry.
// DELETE /v1/admin/waitlist/{id}/invite
func (h *AdminHandler) RevokeInviteByEntry(w http.ResponseWriter, r *http.Request) {
	entryID := r.PathValue("id")

	invite, err := h.store.GetActiveInviteForEntry(r.Context(), entryID)
	if err != nil || invite == nil {
		writeError(w, http.StatusNotFound, "not_found", "No active invite for this entry.")
		return
	}

	if err := h.store.RevokeWaitlistInvite(r.Context(), invite.ID); err != nil {
		slog.Error("failed to revoke invite by entry", "error", err, "entry_id", entryID)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to revoke invite.")
		return
	}

	trackRequest(r, "api.admin.waitlist.revoke_invite_by_entry", map[string]any{
		"entry_id": entryID, "invite_id": invite.ID,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{
		"status": "revoked",
	}})
}

// createInviteForEntry generates an invite token and stores it.
func (h *AdminHandler) createInviteForEntry(r *http.Request, entry *model.WaitlistEntry) error {
	token, err := generateInviteToken()
	if err != nil {
		return err
	}

	invite := &model.WaitlistInvite{
		WaitlistID: entry.ID,
		Email:      entry.Email,
		Token:      token,
		ExpiresAt:  time.Now().Add(7 * 24 * time.Hour), // 7-day expiry
	}

	if err := h.store.CreateWaitlistInvite(r.Context(), invite); err != nil {
		return err
	}

	// Enqueue invite email (skip if APP_BASE_URL is not set — broken links are worse than no email)
	if h.enqueueEmail != nil && h.appBaseURL != "" {
		registerURL := h.appBaseURL + "/register?invite=" + invite.Token
		if err := h.enqueueEmail("waitlist_invite", entry.Email, map[string]string{
			"RegisterURL": registerURL,
			"ExpiresAt":   invite.ExpiresAt.Format("January 2, 2006"),
		}); err != nil {
			slog.Warn("failed to enqueue waitlist invite email", "error", err, "entry_id", entry.ID)
		}
	} else if h.appBaseURL == "" {
		slog.Warn("skipping invite email — APP_BASE_URL not set", "entry_id", entry.ID)
	}

	slog.Info("invite created",
		"waitlist_id", entry.ID,
		"invite_id", invite.ID,
		"expires_at", invite.ExpiresAt)

	return nil
}

// generateInviteToken produces a cryptographically random, URL-safe token.
func generateInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
