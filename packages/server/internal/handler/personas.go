package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Personas ride on ConfigsHandler: they are derived views over identity-class
// agent configs (SOUL.md etc.) and applying one is just another config
// upsert — same store, same events, same sync machinery.

// Mirrors SAFE_PROFILE_NAME in the CLI's agent-configs-discovery.ts.
var safeProfileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validPersonaTargetScope allows "global" or a safe "profile:<name>".
// Project scopes are rejected — identity doesn't belong to a repository.
func validPersonaTargetScope(scope string) bool {
	if scope == "global" {
		return true
	}
	if name, ok := strings.CutPrefix(scope, "profile:"); ok {
		return safeProfileName.MatchString(name)
	}
	return false
}

// syncPersonaFromConfig upserts the persona row derived from an
// identity-class config. Best-effort: persona rows are derived state, so
// failures log and never fail the config write.
func (h *ConfigsHandler) syncPersonaFromConfig(ownerID string, cfg *model.AgentConfig) {
	if cfg == nil || !model.IsIdentityConfigPath(cfg.FilePath) {
		return
	}
	now := time.Now()
	p := &model.Persona{
		ID:             generateID(),
		OwnerID:        ownerID,
		SourceAgent:    cfg.Agent,
		SourceScope:    cfg.Scope,
		SourceFilePath: cfg.FilePath,
		Name:           model.DerivePersonaName(cfg.Agent, cfg.Scope, cfg.FilePath),
		Content:        cfg.Content,
		ContentHash:    cfg.ContentHash,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := h.store.UpsertPersona(p); err != nil {
		slog.Warn("persona upsert from config failed", "agent", cfg.Agent, "file", cfg.FilePath, "error", err)
	}
}

// removePersonaForConfig drops the derived persona when its source config
// is deleted. Best-effort, same rationale as syncPersonaFromConfig.
func (h *ConfigsHandler) removePersonaForConfig(ownerID string, cfg *model.AgentConfig) {
	if cfg == nil || !model.IsIdentityConfigPath(cfg.FilePath) {
		return
	}
	if err := h.store.DeletePersonaBySource(cfg.Agent, cfg.FilePath, cfg.Scope, ownerID); err != nil {
		slog.Warn("persona cleanup on config delete failed", "agent", cfg.Agent, "file", cfg.FilePath, "error", err)
	}
}

// ListPersonas returns the user's personas.
// GET /v1/personas
func (h *ConfigsHandler) ListPersonas(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	personas, err := h.store.ListPersonas(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if personas == nil {
		personas = []model.Persona{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"personas": personas}})
}

// ApplyPersona writes a persona into a target agent's identity config
// (SOUL.md). The write lands in agent_configs and reaches the device on its
// next `memax agents sync` — one-click switch, zero new sync machinery.
// POST /v1/personas/{id}/apply
func (h *ConfigsHandler) ApplyPersona(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	persona, err := h.store.GetPersona(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "persona_not_found", "Persona not found")
		return
	}

	var req struct {
		TargetAgent string `json:"target_agent"`
		TargetScope string `json:"target_scope"`
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
	if req.TargetAgent == "" {
		writeError(w, http.StatusBadRequest, "missing_target_agent", "target_agent is required")
		return
	}
	if req.TargetScope == "" {
		req.TargetScope = "global"
	}
	if !validPersonaTargetScope(req.TargetScope) {
		writeError(w, http.StatusBadRequest, "invalid_target_scope",
			`target_scope must be "global" or "profile:<name>"`)
		return
	}

	const targetFilePath = "SOUL.md"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(persona.Content)))
	now := time.Now()
	config := &model.AgentConfig{
		ID:          generateID(),
		OwnerID:     ownerID,
		Agent:       req.TargetAgent,
		FilePath:    targetFilePath,
		Scope:       req.TargetScope,
		Content:     persona.Content,
		ContentHash: hash,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// Clear any pending deletion for the slot, then write.
	_ = h.store.DeleteAgentConfigTombstone(req.TargetAgent, targetFilePath, req.TargetScope, ownerID)
	if err := h.store.UpsertAgentConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	EnsureConnectedAgent(h.store, ownerID, req.TargetAgent)
	events.PublishAgentChanged(r.Context(), h.events, ownerID, req.TargetAgent, "upserted")

	updated, err := h.store.GetAgentConfigByPath(req.TargetAgent, targetFilePath, req.TargetScope, ownerID)
	if err != nil {
		updated = config
	}
	// The target slot is itself an identity config now — keep personas in step.
	h.syncPersonaFromConfig(ownerID, updated)

	track(ownerID, "persona_applied", map[string]any{
		"persona_id":   persona.ID,
		"target_agent": req.TargetAgent,
		"target_scope": req.TargetScope,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"persona_id":       persona.ID,
		"target_agent":     req.TargetAgent,
		"target_scope":     req.TargetScope,
		"target_file_path": targetFilePath,
		"config_id":        updated.ID,
		"config_version":   updated.Version,
	}})
}

// DeletePersona removes a persona row. The source config is untouched —
// forgetting the derived object never deletes the user's file.
// DELETE /v1/personas/{id}
func (h *ConfigsHandler) DeletePersona(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	if _, err := h.store.GetPersona(id, ownerID); err != nil {
		writeError(w, http.StatusNotFound, "persona_not_found", "Persona not found")
		return
	}
	if err := h.store.DeletePersona(id, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"deleted": true}})
}
