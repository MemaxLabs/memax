package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

// AdminV2UIHandler serves the V2 UI flag for operators (internal/v2ui):
// whether a person sees the V2 Ledger UI, why, and the operator's own
// choice for them, which they turn on, off or back to the rules from the
// admin user page. Admin-only (AdminMiddleware), and never in the public
// SDK: the web calls it through @/lib/admin-client.
type AdminV2UIHandler struct {
	ui *v2ui.Resolver
}

// NewAdminV2UIHandler returns the handler, or nil without a database (the
// routes are then not registered).
func NewAdminV2UIHandler(r *v2ui.Resolver) *AdminV2UIHandler {
	if r == nil {
		return nil
	}
	return &AdminV2UIHandler{ui: r}
}

// AdminV2UI is a person's V2 UI flag as the admin panel shows it.
type AdminV2UI struct {
	UserID string `json:"user_id"`
	v2ui.Decision
	// Since is V2_UI_SINCE (accounts created at or after it get V2), or
	// null when unset.
	Since *string `json:"since"`
}

func (h *AdminV2UIHandler) view(id uuid.UUID, d v2ui.Decision) AdminV2UI {
	out := AdminV2UI{UserID: id.String(), Decision: d}
	if since := h.ui.Since(); !since.IsZero() {
		s := since.UTC().Format(time.RFC3339)
		out.Since = &s
	}
	return out
}

func adminV2UIPerson(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "User not found.")
		return uuid.Nil, false
	}
	return id, true
}

// Get answers a person's V2 UI flag.
// GET /v1/admin/users/{id}/v2-ui
func (h *AdminV2UIHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := adminV2UIPerson(w, r)
	if !ok {
		return
	}
	d, err := h.ui.For(r.Context(), id)
	if errors.Is(err, v2ui.ErrNoPerson) {
		writeError(w, http.StatusNotFound, "not_found", "User not found.")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "admin: V2 UI flag", "user_id", id.String(), "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Couldn't read the V2 UI flag. Try again, and check the API logs if it persists.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: h.view(id, d)})
}

// Set turns the V2 UI on or off for a person, or back to the rules
// ("default"), and answers the flag as it now stands. The change is
// audited (admin_audit, action v2_ui) with the operator; the person's
// browser picks it up at its next page load.
// PUT /v1/admin/users/{id}/v2-ui  {"setting": "on" | "off" | "default"}
func (h *AdminV2UIHandler) Set(w http.ResponseWriter, r *http.Request) {
	id, ok := adminV2UIPerson(w, r)
	if !ok {
		return
	}
	var req struct {
		Setting string `json:"setting"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", `Send {"setting": "on" | "off" | "default"}.`)
		return
	}
	setting, err := v2ui.ParseSetting(req.Setting)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_setting", `Choose "on", "off" or "default".`)
		return
	}
	operator, _ := uuid.Parse(GetUserID(r))
	d, err := h.ui.Set(r.Context(), id, setting, operator, v2ui.ViaAdmin)
	if errors.Is(err, v2ui.ErrNoPerson) {
		writeError(w, http.StatusNotFound, "not_found", "User not found.")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "admin: set the V2 UI flag", "user_id", id.String(), "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Couldn't change the V2 UI flag. Try again, and check the API logs if it persists.")
		return
	}
	slog.InfoContext(r.Context(), "admin: V2 UI flag set", "user_id", id.String(), "setting", string(setting),
		"ui", string(d.UI), "operator", operator.String())
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: h.view(id, d)})
}
