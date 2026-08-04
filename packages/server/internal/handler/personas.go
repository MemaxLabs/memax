package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Personas ride on ConfigsHandler: they are derived views over identity-class
// agent configs (SOUL.md etc.) and applying one is just another config
// upsert — same store, same events, same sync machinery.

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
