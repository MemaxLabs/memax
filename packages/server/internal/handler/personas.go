package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
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

// profileScopeAgents lists agents with a per-profile config directory on
// disk. Mirrors the profile write paths in the CLI's
// agent-configs-discovery.ts (resolveAgentConfigWritePath) — keep in sync,
// or applies will succeed server-side and never land on any device.
var profileScopeAgents = map[string]bool{"hermes": true}

// validPersonaTargetScope allows "global" for any agent, or a safe
// "profile:<name>" for agents that actually have profile directories.
// Project scopes are rejected — identity doesn't belong to a repository.
func validPersonaTargetScope(agent, scope string) bool {
	if scope == "global" {
		return true
	}
	if name, ok := strings.CutPrefix(scope, "profile:"); ok {
		return profileScopeAgents[agent] && safeProfileName.MatchString(name)
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
	if !validPersonaTargetScope(req.TargetAgent, req.TargetScope) {
		writeError(w, http.StatusBadRequest, "invalid_target_scope",
			`target_scope must be "global", or "profile:<name>" for an agent with profiles (hermes)`)
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

// ListPersonaRevisions returns a persona's version history (metadata only —
// content is omitted; fetch a single revision for the full body).
// GET /v1/personas/{id}/revisions
func (h *ConfigsHandler) ListPersonaRevisions(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	if _, err := h.store.GetPersona(id, ownerID); err != nil {
		writeError(w, http.StatusNotFound, "persona_not_found", "Persona not found")
		return
	}
	revisions, err := h.store.ListPersonaRevisions(id, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if revisions == nil {
		revisions = []model.PersonaRevision{}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"revisions": revisions}})
}

// GetPersonaRevision returns one revision including its full content.
// GET /v1/personas/{id}/revisions/{version}
func (h *ConfigsHandler) GetPersonaRevision(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "invalid_version", "Version must be a positive integer")
		return
	}
	if _, err := h.store.GetPersona(id, ownerID); err != nil {
		writeError(w, http.StatusNotFound, "persona_not_found", "Persona not found")
		return
	}
	rev, err := h.store.GetPersonaRevision(id, version, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "revision_not_found", "Revision not found")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: rev})
}

// RestorePersonaRevision writes an old revision back into the persona's
// SOURCE config slot. This re-derives the persona at a new head version
// (history is append-only — a restore is a new version, never a rewrite)
// and reaches the device through the normal sync path.
// POST /v1/personas/{id}/revisions/{version}/restore
func (h *ConfigsHandler) RestorePersonaRevision(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "invalid_version", "Version must be a positive integer")
		return
	}
	persona, err := h.store.GetPersona(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "persona_not_found", "Persona not found")
		return
	}
	rev, err := h.store.GetPersonaRevision(id, version, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "revision_not_found", "Revision not found")
		return
	}

	now := time.Now()
	config := &model.AgentConfig{
		ID:          generateID(),
		OwnerID:     ownerID,
		Agent:       persona.SourceAgent,
		FilePath:    persona.SourceFilePath,
		Scope:       persona.SourceScope,
		Content:     rev.Content,
		ContentHash: rev.ContentHash,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_ = h.store.DeleteAgentConfigTombstone(persona.SourceAgent, persona.SourceFilePath, persona.SourceScope, ownerID)
	if err := h.store.UpsertAgentConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	events.PublishAgentChanged(r.Context(), h.events, ownerID, persona.SourceAgent, "upserted")

	updated, err := h.store.GetAgentConfigByPath(persona.SourceAgent, persona.SourceFilePath, persona.SourceScope, ownerID)
	if err != nil {
		updated = config
	}
	h.syncPersonaFromConfig(ownerID, updated)

	restored, err := h.store.GetPersona(id, ownerID)
	if err != nil {
		restored = persona
	}
	track(ownerID, "persona_revision_restored", map[string]any{
		"persona_id": id,
		"revision":   version,
	})
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"persona_id":       id,
		"restored_version": version,
		"head_version":     restored.Version,
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
