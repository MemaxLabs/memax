package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// WaitlistHandler serves public waitlist endpoints.
type WaitlistHandler struct {
	store        store.Store
	enqueueEmail func(template, to string, vars map[string]string) error
}

// NewWaitlistHandler creates a new WaitlistHandler.
func NewWaitlistHandler(s store.Store) *WaitlistHandler {
	return &WaitlistHandler{store: s}
}

// SetEnqueueEmail wires the email enqueue callback. Called after queue client is ready.
func (h *WaitlistHandler) SetEnqueueEmail(fn func(template, to string, vars map[string]string) error) {
	h.enqueueEmail = fn
}

// Submit handles public waitlist signups.
// POST /v1/waitlist
func (h *WaitlistHandler) Submit(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}

	var req struct {
		Email   string   `json:"email"`
		UseCase string   `json:"use_case"`
		AITools []string `json:"ai_tools"`
		Role    string   `json:"role"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	// Validate required fields
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_email", "A valid email address is required.")
		return
	}

	// Check if already on waitlist — return 200 (not error) to prevent enumeration
	existing, _ := h.store.GetWaitlistEntryByEmail(r.Context(), email)
	if existing != nil {
		// Identical response to a new signup — no information leak
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
			"id":         existing.ID,
			"email":      maskEmail(existing.Email),
			"status":     "pending",
			"created_at": existing.CreatedAt,
		}})
		return
	}

	entry := &model.WaitlistEntry{
		Email:   email,
		UseCase: req.UseCase,
		AITools: req.AITools,
		Role:    req.Role,
	}
	if entry.AITools == nil {
		entry.AITools = []string{}
	}

	if err := h.store.CreateWaitlistEntry(r.Context(), entry); err != nil {
		slog.Error("failed to create waitlist entry", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to join waitlist.")
		return
	}

	// Enqueue confirmation email (best-effort — signup succeeds regardless)
	if h.enqueueEmail != nil {
		if err := h.enqueueEmail("waitlist_confirmation", email, nil); err != nil {
			slog.Warn("failed to enqueue waitlist confirmation email", "error", err, "email", maskEmail(email))
		}
	}

	slog.Info("waitlist signup", "email", maskEmail(email))

	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: map[string]any{
		"id":         entry.ID,
		"email":      maskEmail(entry.Email),
		"status":     entry.Status,
		"created_at": entry.CreatedAt,
	}})
}

// ValidateInvite validates a waitlist invite token.
// GET /v1/waitlist/invites/{token}
func (h *WaitlistHandler) ValidateInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing_token", "Invite token is required.")
		return
	}

	invite, err := h.store.GetWaitlistInviteByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Invalid invite link.")
		return
	}

	if invite.Status != model.WaitlistInviteStatusActive {
		writeError(w, http.StatusGone, "invite_expired", "This invite has already been used or expired.")
		return
	}

	// Enforce expiry even if the background expiry job hasn't run yet
	if time.Now().After(invite.ExpiresAt) {
		// Opportunistic cleanup — mark all past-due invites as expired
		go func() { _, _ = h.store.ExpireWaitlistInvites(context.Background()) }()
		writeError(w, http.StatusGone, "invite_expired", "This invite has expired.")
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.WaitlistInvitePublic{
		Email:     maskEmail(invite.Email),
		Status:    invite.Status,
		ExpiresAt: invite.ExpiresAt,
	}})
}

// maskEmail replaces the local part with *** for logging and responses.
// "user@gmail.com" → "***@gmail.com"
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return "***" + email[at:]
}
