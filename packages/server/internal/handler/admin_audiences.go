package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AdminAudiencesHandler serves /v1/admin/audiences* endpoints.
type AdminAudiencesHandler struct {
	store store.Store
}

// NewAdminAudiencesHandler constructs the audiences handler.
func NewAdminAudiencesHandler(s store.Store) *AdminAudiencesHandler {
	return &AdminAudiencesHandler{store: s}
}

type audienceRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	RuleType    string             `json:"rule_type"`
	Rule        model.AudienceRule `json:"rule"`
}

type audienceEstimateRequest struct {
	AudienceID string                  `json:"audience_id,omitempty"`
	Snapshot   *model.AudienceSnapshot `json:"snapshot,omitempty"`
}

// List returns all saved audiences newest-first.
// GET /v1/admin/audiences
func (h *AdminAudiencesHandler) List(w http.ResponseWriter, r *http.Request) {
	audiences, err := h.store.ListAudiences(r.Context())
	if err != nil {
		slog.Error("list audiences failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not list audiences.")
		return
	}
	if audiences == nil {
		audiences = []model.Audience{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"audiences": audiences,
	}})
}

// Get returns one audience by id.
// GET /v1/admin/audiences/{id}
func (h *AdminAudiencesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := h.store.GetAudience(r.Context(), id)
	if errors.Is(err, store.ErrAudienceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Audience not found.")
		return
	}
	if err != nil {
		slog.Error("get audience failed", "error", err, "audience_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load audience.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: a})
}

// Create inserts a new saved audience.
// POST /v1/admin/audiences
func (h *AdminAudiencesHandler) Create(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	req, ok := h.decodeAudienceRequest(w, r)
	if !ok {
		return
	}
	if msg := validateAudienceRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_audience", msg)
		return
	}

	a := &model.Audience{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		RuleType:    req.RuleType,
		Rule:        req.Rule,
		CreatedBy:   actorID,
	}
	if err := h.store.CreateAudience(r.Context(), a); err != nil {
		slog.Error("create audience failed", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not create audience.")
		return
	}
	h.writeAudit(r.Context(), a.ID, model.CommsAuditActionCreated, actorID, map[string]any{
		"name":      a.Name,
		"rule_type": a.RuleType,
	})

	// Reload to get server-populated timestamps.
	fresh, err := h.store.GetAudience(r.Context(), a.ID)
	if err == nil {
		a = fresh
	}
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: a})
}

// Update mutates name/description/rule on a saved audience.
// PATCH /v1/admin/audiences/{id}
func (h *AdminAudiencesHandler) Update(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	req, ok := h.decodeAudienceRequest(w, r)
	if !ok {
		return
	}
	if msg := validateAudienceRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_audience", msg)
		return
	}

	current, err := h.store.GetAudience(r.Context(), id)
	if errors.Is(err, store.ErrAudienceNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Audience not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", "Could not load audience.")
		return
	}
	current.Name = req.Name
	current.Description = req.Description
	current.RuleType = req.RuleType
	current.Rule = req.Rule
	if err := h.store.UpdateAudience(r.Context(), current); err != nil {
		slog.Error("update audience failed", "error", err, "audience_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not update audience.")
		return
	}
	h.writeAudit(r.Context(), id, model.CommsAuditActionUpdated, actorID, map[string]any{
		"name": current.Name,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: current})
}

// Delete removes a saved audience.
// DELETE /v1/admin/audiences/{id}
func (h *AdminAudiencesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actorID := GetUserID(r)
	id := r.PathValue("id")
	if err := h.store.DeleteAudience(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrAudienceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Audience not found.")
			return
		}
		slog.Error("delete audience failed", "error", err, "audience_id", id)
		writeError(w, http.StatusInternalServerError, "store_error", "Could not delete audience.")
		return
	}
	h.writeAudit(r.Context(), id, model.CommsAuditActionDeleted, actorID, nil)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "deleted", "id": id}})
}

// Estimate resolves a rule (by id or inline) into a recipient count
// plus a small sample of user IDs. Exposed as POST to accept inline
// rules without URL encoding.
// POST /v1/admin/audiences/estimate
func (h *AdminAudiencesHandler) Estimate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return
	}
	var req audienceEstimateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	snapshot := req.Snapshot
	if req.AudienceID != "" {
		a, err := h.store.GetAudience(r.Context(), req.AudienceID)
		if errors.Is(err, store.ErrAudienceNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Audience not found.")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", "Could not load audience.")
			return
		}
		audienceID := a.ID
		snapshot = &model.AudienceSnapshot{
			AudienceID: &audienceID,
			RuleType:   a.RuleType,
			Rule:       a.Rule,
		}
	}
	if snapshot == nil {
		writeError(w, http.StatusBadRequest, "missing_rule", "Provide audience_id or snapshot.")
		return
	}
	if !model.ValidAudienceRuleType(snapshot.RuleType) {
		writeError(w, http.StatusBadRequest, "invalid_rule_type", "Rule type must be all, users, or hub.")
		return
	}

	estimate, err := estimateAudience(r.Context(), h.store, snapshot)
	if err != nil {
		slog.Error("estimate audience failed", "error", err)
		writeError(w, http.StatusBadRequest, "estimate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: estimate})
}

func (h *AdminAudiencesHandler) decodeAudienceRequest(w http.ResponseWriter, r *http.Request) (audienceRequest, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", "Could not read request body.")
		return audienceRequest{}, false
	}
	var req audienceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return audienceRequest{}, false
	}
	req.Name = strings.TrimSpace(req.Name)
	return req, true
}

func (h *AdminAudiencesHandler) writeAudit(ctx context.Context, resourceID, action, actorID string, metadata map[string]any) {
	var md json.RawMessage
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			md = b
		}
	}
	entry := &model.CommsAuditEntry{
		ID:           uuid.New().String(),
		ResourceType: model.CommsAuditResourceAudience,
		ResourceID:   resourceID,
		Action:       action,
		Metadata:     md,
	}
	if actorID != "" {
		entry.ActorID = &actorID
	}
	// Intentionally fire-and-forget: a missing audit entry should not
	// block the business mutation. We log on failure and move on.
	if err := h.store.AppendCommsAudit(ctx, entry); err != nil {
		slog.Warn("append comms audit failed", "error", err, "action", action, "audience_id", resourceID)
	}
}

func validateAudienceRequest(req audienceRequest) string {
	if req.Name == "" {
		return "Name is required."
	}
	if !model.ValidAudienceRuleType(req.RuleType) {
		return "Rule type must be all, users, or hub."
	}
	switch req.RuleType {
	case model.AudienceRuleUsers:
		if len(req.Rule.UserIDs) == 0 && len(req.Rule.Emails) == 0 {
			return "Users rule needs at least one user_id or email."
		}
	case model.AudienceRuleHub:
		if req.Rule.HubID == "" {
			return "Hub rule requires hub_id."
		}
	}
	return ""
}
