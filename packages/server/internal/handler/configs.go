package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type agentConfigKey struct {
	agent    string
	filePath string
	scope    string
}

// ConfigsHandler handles agent config sync endpoints.
type ConfigsHandler struct {
	store   store.Store
	llm     *anthropic.Client
	events  events.Publisher
	enqueue func(configID, ownerID string) // optional: enqueue extraction job after upsert
}

const agentConfigTombstoneRetention = 30 * 24 * time.Hour

func NewConfigsHandler(s store.Store, llm *anthropic.Client) *ConfigsHandler {
	return &ConfigsHandler{store: s, llm: llm}
}

// WithEvents installs an events.Publisher so config mutations emit
// a user-axis `agent.changed` event and Settings → Your Agents
// converges across devices. Optional — the handler remains fully
// functional without one; publish helpers no-op on nil.
func (h *ConfigsHandler) WithEvents(p events.Publisher) *ConfigsHandler {
	h.events = p
	return h
}

func (h *ConfigsHandler) purgeExpiredTombstoneContent() {
	if err := h.store.PurgeExpiredAgentConfigTombstoneContent(time.Now()); err != nil {
		slog.Warn("purge expired config tombstone content failed", "error", err)
	}
}

// SetEnqueue sets the function that triggers background extraction after config upsert.
func (h *ConfigsHandler) SetEnqueue(fn func(configID, ownerID string)) {
	h.enqueue = fn
}

// Upsert creates or updates an agent config.
// PUT /v1/configs
// maxAgentConfigBytes caps synced config file size — configs are stored
// inline in Postgres, not object storage. Mirrors MAX_AGENT_CONFIG_BYTES
// in the CLI's agent-configs-discovery.ts.
const maxAgentConfigBytes = 512 << 10

func (h *ConfigsHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req struct {
		Agent     string `json:"agent"`
		FilePath  string `json:"file_path"`
		Scope     string `json:"scope"`
		Content   string `json:"content"`
		DeviceID  string `json:"device_id"`
		LocalPath string `json:"local_path"`
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

	if req.Agent == "" {
		writeError(w, http.StatusBadRequest, "missing_agent", "Agent identifier is required")
		return
	}
	if req.FilePath == "" {
		writeError(w, http.StatusBadRequest, "missing_file_path", "File path is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "Content is required")
		return
	}
	if len(req.Content) > maxAgentConfigBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "config_too_large",
			"Config file exceeds the 512KB sync limit — trim it or exclude it from sync")
		return
	}
	if req.Scope == "" {
		req.Scope = "global"
	}
	_ = h.store.DeleteAgentConfigTombstone(req.Agent, req.FilePath, req.Scope, ownerID)

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Content)))

	// Check if unchanged (idempotent)
	existing, err := h.store.GetAgentConfigByPath(req.Agent, req.FilePath, req.Scope, ownerID)
	if err == nil && existing.ContentHash == hash {
		h.persistSyncState(ownerID, req.DeviceID, req.Agent, req.FilePath, req.Scope, req.LocalPath, existing.Version, existing.ContentHash)
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: existing})
		return
	}

	now := time.Now()
	config := &model.AgentConfig{
		ID:          generateID(),
		OwnerID:     ownerID,
		Agent:       req.Agent,
		FilePath:    req.FilePath,
		Scope:       req.Scope,
		Content:     req.Content,
		ContentHash: hash,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.store.UpsertAgentConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Auto-populate connected_agents registry
	EnsureConnectedAgent(h.store, ownerID, req.Agent)
	events.PublishAgentChanged(r.Context(), h.events, ownerID, req.Agent, "upserted")

	// Re-read to get correct version after upsert
	updated, err := h.store.GetAgentConfigByPath(req.Agent, req.FilePath, req.Scope, ownerID)
	if err != nil {
		// Upsert succeeded, return what we have
		h.persistSyncState(ownerID, req.DeviceID, req.Agent, req.FilePath, req.Scope, req.LocalPath, config.Version, config.ContentHash)
		if h.enqueue != nil {
			h.enqueue(config.ID, ownerID)
		}
		h.syncPersonaFromConfig(ownerID, config)
		writeJSON(w, http.StatusCreated, model.ApiResponse{Data: config})
		return
	}

	// Trigger extraction in background
	h.persistSyncState(ownerID, req.DeviceID, req.Agent, req.FilePath, req.Scope, req.LocalPath, updated.Version, updated.ContentHash)
	if h.enqueue != nil {
		h.enqueue(updated.ID, ownerID)
	}
	h.syncPersonaFromConfig(ownerID, updated)
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: updated})
}

// List returns all agent configs for the authenticated user.
// GET /v1/configs
func (h *ConfigsHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	configs, err := h.store.ListAgentConfigs(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Filter by query params
	agent := r.URL.Query().Get("agent")
	scope := r.URL.Query().Get("scope")

	if agent != "" || scope != "" {
		var filtered []model.AgentConfig
		for _, c := range configs {
			if agent != "" && c.Agent != agent {
				continue
			}
			if scope != "" && c.Scope != scope {
				continue
			}
			filtered = append(filtered, c)
		}
		configs = filtered
	}

	if configs == nil {
		configs = []model.AgentConfig{}
	}

	extractionCounts, err := h.store.CountExtractedMemories(ownerID)
	if err != nil {
		slog.Warn("failed to count extracted memories", "error", err)
		extractionCounts = map[string]int{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"configs":           configs,
		"extraction_counts": extractionCounts,
	}})
}

// Get returns a single agent config by ID.
// GET /v1/configs/{id}
func (h *ConfigsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	config, err := h.store.GetAgentConfig(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Config not found")
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: config})
}

// Delete removes an agent config by ID.
// DELETE /v1/configs/{id}
func (h *ConfigsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")

	config, err := h.store.GetAgentConfig(id, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Config not found")
		return
	}
	expiresAt := time.Now().Add(agentConfigTombstoneRetention)
	tombstone := &model.AgentConfigTombstone{
		OwnerID:            ownerID,
		Agent:              config.Agent,
		FilePath:           config.FilePath,
		Scope:              config.Scope,
		Version:            config.Version + 1,
		DeletedAt:          time.Now(),
		DeletedContent:     config.Content,
		DeletedContentHash: config.ContentHash,
		ContentExpiresAt:   &expiresAt,
	}
	if err := h.store.CreateAgentConfigTombstone(tombstone); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	h.removePersonaForConfig(ownerID, config)
	if err := h.store.DeleteAgentConfig(id, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	// Emit `agent.changed` so Settings → Your Agents' config card
	// count drops in real time on other devices. State stays
	// "upserted" because the agent itself isn't removed — only one
	// of its config files — and coining a new state just to say
	// "something about this agent changed" fragments the route.
	events.PublishAgentChanged(r.Context(), h.events, ownerID, config.Agent, "upserted")

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]bool{"deleted": true},
	})
}

// BatchDelete removes multiple agent configs in a single request and returns
// a structured result mirroring /v1/memories/batch-delete. Each id follows
// the same tombstone-then-delete path as Delete, but in its own per-id
// mini-transaction so partial success is the default: a single store failure
// on one id does not abort the rest of the batch.
//
// Skip reasons:
//   - not_found: the id does not resolve via GetAgentConfig(id, ownerID).
//     Covers unknown ids, already-deleted rows (race with another device),
//     and rows owned by a different user — the SQL WHERE clause in the
//     store makes miss and not-owned indistinguishable without a pre-lookup,
//     and for the delete-hardening UX the collapse is desired: both surface
//     as idempotent success on the client.
//   - delete_failed: the tombstone insert or the DELETE itself returned an
//     error. Logged server-side so infra failures are distinguished from
//     "already gone." Clients treat this as retryable.
//
// Always returns 200 with BatchDeleteResult in the ApiResponse envelope;
// 4xx/5xx are reserved for full-request failures (malformed body, auth).
// POST /v1/configs/batch-delete
func (h *ConfigsHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req struct {
		IDs []string `json:"ids"`
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

	result := model.BatchDeleteResult{
		Deleted: 0,
		Skipped: []model.SkippedMemory{},
	}

	// Collect unique agent slugs touched by successful deletes so the
	// post-loop emit fires ONE `agent.changed` event per affected
	// agent, not N events per config. Matches PR H.4 spec: CLI users
	// bulk-removing 50 configs spread across 5 agents get 5
	// invalidations, not 50.
	touchedAgents := map[string]struct{}{}

	for _, id := range req.IDs {
		if id == "" {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchDeleteSkipNotFound,
			})
			continue
		}
		config, err := h.store.GetAgentConfig(id, ownerID)
		if err != nil {
			// Unknown id, already deleted, or owned by another user —
			// GetAgentConfig's WHERE clause makes these indistinguishable,
			// and collapsing them to not_found is correct for the
			// idempotent-success UX the client layer wants.
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchDeleteSkipNotFound,
			})
			continue
		}
		expiresAt := time.Now().Add(agentConfigTombstoneRetention)
		tombstone := &model.AgentConfigTombstone{
			OwnerID:            ownerID,
			Agent:              config.Agent,
			FilePath:           config.FilePath,
			Scope:              config.Scope,
			Version:            config.Version + 1,
			DeletedAt:          time.Now(),
			DeletedContent:     config.Content,
			DeletedContentHash: config.ContentHash,
			ContentExpiresAt:   &expiresAt,
		}
		if err := h.store.CreateAgentConfigTombstone(tombstone); err != nil {
			slog.Error("batch-delete: tombstone create failed", "id", id, "owner", ownerID, "err", err)
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchDeleteSkipDeleteFailed,
			})
			continue
		}
		if err := h.store.DeleteAgentConfig(id, ownerID); err != nil {
			slog.Error("batch-delete: delete failed", "id", id, "owner", ownerID, "err", err)
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchDeleteSkipDeleteFailed,
			})
			continue
		}
		result.Deleted++
		touchedAgents[config.Agent] = struct{}{}
	}
	for agent := range touchedAgents {
		events.PublishAgentChanged(r.Context(), h.events, ownerID, agent, "upserted")
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: result})
}

// Sync receives a local manifest and returns a sync plan.
// POST /v1/configs/sync
func (h *ConfigsHandler) Sync(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	h.purgeExpiredTombstoneContent()

	var req struct {
		DeviceID string `json:"device_id"`
		Configs  []struct {
			Agent       string `json:"agent"`
			FilePath    string `json:"file_path"`
			Scope       string `json:"scope"`
			ContentHash string `json:"content_hash"`
			UpdatedAt   string `json:"updated_at"`
			LocalPath   string `json:"local_path"`
		} `json:"configs"`
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
	if req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "missing_device_id", "Device identifier is required")
		return
	}

	// Get all cloud configs for this user
	cloudConfigs, err := h.store.ListAgentConfigs(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	cloudMap := make(map[agentConfigKey]*model.AgentConfig)
	for i := range cloudConfigs {
		c := &cloudConfigs[i]
		cloudMap[agentConfigKey{c.Agent, c.FilePath, c.Scope}] = c
	}

	deviceStates, err := h.store.ListAgentConfigSyncStates(ownerID, req.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	stateMap := make(map[agentConfigKey]*model.AgentConfigSyncState)
	for i := range deviceStates {
		state := &deviceStates[i]
		stateMap[agentConfigKey{state.Agent, state.FilePath, state.Scope}] = state
	}

	tombstones, err := h.store.ListAgentConfigTombstones(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	tombstoneMap := make(map[agentConfigKey]*model.AgentConfigTombstone)
	for i := range tombstones {
		tombstone := &tombstones[i]
		if tombstone.Version > 0 {
			tombstoneMap[agentConfigKey{tombstone.Agent, tombstone.FilePath, tombstone.Scope}] = tombstone
		}
	}

	// Track which cloud configs were matched by local manifest
	matched := make(map[agentConfigKey]bool)

	type syncAction struct {
		Action         string `json:"action"`
		Agent          string `json:"agent"`
		FilePath       string `json:"file_path"`
		Scope          string `json:"scope"`
		Reason         string `json:"reason,omitempty"`
		ConfigID       string `json:"config_id,omitempty"`
		CloudHash      string `json:"cloud_hash,omitempty"`
		CloudUpdatedAt string `json:"cloud_updated_at,omitempty"`
		Version        int    `json:"version,omitempty"`
	}

	var actions []syncAction

	for _, local := range req.Configs {
		scope := local.Scope
		if scope == "" {
			scope = "global"
		}
		key := agentConfigKey{local.Agent, local.FilePath, scope}
		matched[key] = true

		cloud, hasCloud := cloudMap[key]
		tombstone, hasTombstone := tombstoneMap[key]
		if hasTombstone && (!hasCloud || tombstone.Version > cloud.Version) {
			state, hasState := stateMap[key]
			if !hasState {
				actions = append(actions, syncAction{
					Action:         "conflict",
					Agent:          local.Agent,
					FilePath:       local.FilePath,
					Scope:          scope,
					CloudUpdatedAt: tombstone.DeletedAt.Format(time.RFC3339),
					Version:        tombstone.Version,
					Reason:         "cloud_deleted_unknown_base_state",
				})
				continue
			}

			switch {
			case state.LastSeenVersion < tombstone.Version:
				if !state.Suppressed && state.LastSeenHash == local.ContentHash {
					actions = append(actions, syncAction{
						Action:         "delete_local",
						Agent:          local.Agent,
						FilePath:       local.FilePath,
						Scope:          scope,
						CloudUpdatedAt: tombstone.DeletedAt.Format(time.RFC3339),
						Version:        tombstone.Version,
						Reason:         "deleted_everywhere",
					})
				} else {
					actions = append(actions, syncAction{
						Action:         "conflict",
						Agent:          local.Agent,
						FilePath:       local.FilePath,
						Scope:          scope,
						CloudUpdatedAt: tombstone.DeletedAt.Format(time.RFC3339),
						Version:        tombstone.Version,
						Reason:         "local_changed_while_cloud_deleted",
					})
				}
			case state.LastSeenVersion == tombstone.Version:
				actions = append(actions, syncAction{
					Action:   "push",
					Agent:    local.Agent,
					FilePath: local.FilePath,
					Scope:    scope,
					Reason:   "local_recreated_after_delete",
					Version:  tombstone.Version,
				})
			default:
				actions = append(actions, syncAction{
					Action:         "conflict",
					Agent:          local.Agent,
					FilePath:       local.FilePath,
					Scope:          scope,
					CloudUpdatedAt: tombstone.DeletedAt.Format(time.RFC3339),
					Version:        tombstone.Version,
					Reason:         "state_ahead_of_delete",
				})
			}
			continue
		}

		if !hasCloud {
			// Cloud doesn't have it — push
			actions = append(actions, syncAction{
				Action:   "push",
				Agent:    local.Agent,
				FilePath: local.FilePath,
				Scope:    scope,
				Reason:   "local_only",
			})
			continue
		}

		if local.ContentHash == cloud.ContentHash {
			actions = append(actions, syncAction{
				Action:   "unchanged",
				Agent:    local.Agent,
				FilePath: local.FilePath,
				Scope:    scope,
				ConfigID: cloud.ID,
				Version:  cloud.Version,
			})
			continue
		}

		state, hasState := stateMap[key]
		if !hasState {
			actions = append(actions, syncAction{
				Action:         "conflict",
				Agent:          local.Agent,
				FilePath:       local.FilePath,
				Scope:          scope,
				ConfigID:       cloud.ID,
				CloudHash:      cloud.ContentHash,
				CloudUpdatedAt: cloud.UpdatedAt.Format(time.RFC3339),
				Version:        cloud.Version,
				Reason:         "unknown_base_state",
			})
			continue
		}

		switch {
		case state.LastSeenVersion == cloud.Version:
			if state.LastSeenHash == local.ContentHash {
				actions = append(actions, syncAction{
					Action:         "pull",
					Agent:          local.Agent,
					FilePath:       local.FilePath,
					Scope:          scope,
					ConfigID:       cloud.ID,
					CloudHash:      cloud.ContentHash,
					CloudUpdatedAt: cloud.UpdatedAt.Format(time.RFC3339),
					Version:        cloud.Version,
					Reason:         "cloud_changed_since_last_sync",
				})
			} else {
				actions = append(actions, syncAction{
					Action:   "push",
					Agent:    local.Agent,
					FilePath: local.FilePath,
					Scope:    scope,
					Reason:   "local_changed_since_last_sync",
					Version:  cloud.Version,
				})
			}
		case state.LastSeenVersion < cloud.Version:
			if state.LastSeenHash == local.ContentHash {
				actions = append(actions, syncAction{
					Action:         "pull",
					Agent:          local.Agent,
					FilePath:       local.FilePath,
					Scope:          scope,
					ConfigID:       cloud.ID,
					CloudHash:      cloud.ContentHash,
					CloudUpdatedAt: cloud.UpdatedAt.Format(time.RFC3339),
					Version:        cloud.Version,
					Reason:         "cloud_newer",
				})
			} else {
				actions = append(actions, syncAction{
					Action:         "conflict",
					Agent:          local.Agent,
					FilePath:       local.FilePath,
					Scope:          scope,
					ConfigID:       cloud.ID,
					CloudHash:      cloud.ContentHash,
					CloudUpdatedAt: cloud.UpdatedAt.Format(time.RFC3339),
					Version:        cloud.Version,
					Reason:         "diverged_since_last_sync",
				})
			}
		default:
			actions = append(actions, syncAction{
				Action:         "conflict",
				Agent:          local.Agent,
				FilePath:       local.FilePath,
				Scope:          scope,
				ConfigID:       cloud.ID,
				CloudHash:      cloud.ContentHash,
				CloudUpdatedAt: cloud.UpdatedAt.Format(time.RFC3339),
				Version:        cloud.Version,
				Reason:         "state_ahead_of_cloud",
			})
		}
	}

	// Cloud configs not in local manifest → pull
	for key, cloud := range cloudMap {
		if !matched[key] {
			if state, ok := stateMap[key]; ok && state.Suppressed {
				continue
			}
			actions = append(actions, syncAction{
				Action:   "pull",
				Agent:    cloud.Agent,
				FilePath: cloud.FilePath,
				Scope:    cloud.Scope,
				Reason:   "cloud_only",
				ConfigID: cloud.ID,
				Version:  cloud.Version,
			})
		}
	}

	for key, tombstone := range tombstoneMap {
		if matched[key] {
			continue
		}
		state, ok := stateMap[key]
		if !ok {
			continue
		}
		if state.Suppressed {
			continue
		}
		if state.LastSeenVersion < tombstone.Version {
			actions = append(actions, syncAction{
				Action:         "delete_local",
				Agent:          key.agent,
				FilePath:       key.filePath,
				Scope:          key.scope,
				CloudUpdatedAt: tombstone.DeletedAt.Format(time.RFC3339),
				Version:        tombstone.Version,
				Reason:         "deleted_everywhere",
			})
		}
	}

	if actions == nil {
		actions = []syncAction{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"actions": actions,
	}})

	// Plan 18 onboarding tick used to fire here via the recorder.
	// Now the materializer at internal/onboarding/materialize.go
	// computes connect_agent completion lazily from the
	// connected_agents table on notification read; the agent.changed
	// SSE event (PR C) invalidates ["notifications"] so the next
	// read picks up the new agent without any explicit tick here.
}

// ListDeleted returns recoverable deleted configs for the authenticated user.
// GET /v1/configs/deleted
func (h *ConfigsHandler) ListDeleted(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	h.purgeExpiredTombstoneContent()

	tombstones, err := h.store.ListAgentConfigTombstones(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	now := time.Now()
	items := make([]model.RecoverableAgentConfigTombstone, 0, len(tombstones))
	for _, tombstone := range tombstones {
		if tombstone.DeletedContent == "" {
			continue
		}
		if tombstone.ContentExpiresAt != nil && !tombstone.ContentExpiresAt.After(now) {
			continue
		}
		items = append(items, model.RecoverableAgentConfigTombstone{
			Agent:              tombstone.Agent,
			FilePath:           tombstone.FilePath,
			Scope:              tombstone.Scope,
			Version:            tombstone.Version,
			DeletedAt:          tombstone.DeletedAt,
			DeletedContentHash: tombstone.DeletedContentHash,
			ContentExpiresAt:   tombstone.ContentExpiresAt,
		})
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"configs": items,
	}})
}

// Restore resurrects a deleted config from a recoverable tombstone.
// POST /v1/configs/restore
func (h *ConfigsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	h.purgeExpiredTombstoneContent()

	var req struct {
		Agent     string `json:"agent"`
		FilePath  string `json:"file_path"`
		Scope     string `json:"scope"`
		DeviceID  string `json:"device_id"`
		LocalPath string `json:"local_path"`
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
	if req.Agent == "" || req.FilePath == "" {
		writeError(w, http.StatusBadRequest, "missing_identity", "Agent and file path are required")
		return
	}
	if req.Scope == "" {
		req.Scope = "global"
	}

	if _, err := h.store.GetAgentConfigByPath(req.Agent, req.FilePath, req.Scope, ownerID); err == nil {
		writeError(w, http.StatusConflict, "already_exists", "Config already exists in cloud")
		return
	}

	tombstone, err := h.store.GetAgentConfigTombstone(req.Agent, req.FilePath, req.Scope, ownerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Deleted config not found")
		return
	}
	if tombstone.DeletedContent == "" || (tombstone.ContentExpiresAt != nil && !tombstone.ContentExpiresAt.After(time.Now())) {
		writeError(w, http.StatusGone, "backup_expired", "Deleted config backup is no longer available")
		return
	}

	now := time.Now()
	config := &model.AgentConfig{
		ID:          generateID(),
		OwnerID:     ownerID,
		Agent:       tombstone.Agent,
		FilePath:    tombstone.FilePath,
		Scope:       tombstone.Scope,
		Content:     tombstone.DeletedContent,
		ContentHash: tombstone.DeletedContentHash,
		Version:     tombstone.Version + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.DeleteAgentConfigTombstone(req.Agent, req.FilePath, req.Scope, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if err := h.store.UpsertAgentConfig(config); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	EnsureConnectedAgent(h.store, ownerID, req.Agent)
	events.PublishAgentChanged(r.Context(), h.events, ownerID, req.Agent, "upserted")
	updated, err := h.store.GetAgentConfigByPath(req.Agent, req.FilePath, req.Scope, ownerID)
	if err != nil {
		writeJSON(w, http.StatusCreated, model.ApiResponse{Data: config})
		return
	}
	h.persistSyncState(ownerID, req.DeviceID, req.Agent, req.FilePath, req.Scope, req.LocalPath, updated.Version, updated.ContentHash)
	if h.enqueue != nil {
		h.enqueue(updated.ID, ownerID)
	}
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: updated})
}

// LocalDelete marks a config as intentionally removed on this device only.
// It does not delete the cloud copy.
// POST /v1/configs/local-delete
func (h *ConfigsHandler) LocalDelete(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req struct {
		DeviceID  string `json:"device_id"`
		Agent     string `json:"agent"`
		FilePath  string `json:"file_path"`
		Scope     string `json:"scope"`
		LocalPath string `json:"local_path"`
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
	if req.DeviceID == "" || req.Agent == "" || req.FilePath == "" {
		writeError(w, http.StatusBadRequest, "missing_fields", "device_id, agent, and file_path are required")
		return
	}
	if req.Scope == "" {
		req.Scope = "global"
	}

	version := 1
	hash := ""
	if cfg, err := h.store.GetAgentConfigByPath(req.Agent, req.FilePath, req.Scope, ownerID); err == nil {
		version = cfg.Version
		hash = cfg.ContentHash
	}
	tombstones, _ := h.store.ListAgentConfigTombstones(ownerID)
	for _, tombstone := range tombstones {
		if tombstone.Agent == req.Agent && tombstone.FilePath == req.FilePath && tombstone.Scope == req.Scope && tombstone.Version > version {
			version = tombstone.Version
			hash = ""
		}
	}
	err = h.store.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         ownerID,
		DeviceID:        req.DeviceID,
		Agent:           req.Agent,
		FilePath:        req.FilePath,
		Scope:           req.Scope,
		LocalPath:       req.LocalPath,
		LastSeenVersion: version,
		LastSeenHash:    hash,
		Suppressed:      true,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"ok": true}})
}

// Ack records that a device now has an exact local copy of the cloud config version.
// POST /v1/configs/ack
func (h *ConfigsHandler) Ack(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req struct {
		DeviceID string `json:"device_id"`
		Configs  []struct {
			Agent       string `json:"agent"`
			FilePath    string `json:"file_path"`
			Scope       string `json:"scope"`
			ContentHash string `json:"content_hash"`
			Version     int    `json:"version"`
			LocalPath   string `json:"local_path"`
			Deleted     bool   `json:"deleted"`
		} `json:"configs"`
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
	if req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "missing_device_id", "Device identifier is required")
		return
	}

	for _, cfg := range req.Configs {
		scope := cfg.Scope
		if scope == "" {
			scope = "global"
		}
		if cfg.Agent == "" || cfg.FilePath == "" || cfg.Version <= 0 {
			continue
		}
		if cfg.Deleted {
			err := h.store.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
				OwnerID:         ownerID,
				DeviceID:        req.DeviceID,
				Agent:           cfg.Agent,
				FilePath:        cfg.FilePath,
				Scope:           scope,
				LocalPath:       cfg.LocalPath,
				LastSeenVersion: cfg.Version,
				LastSeenHash:    "",
				Suppressed:      false,
				UpdatedAt:       time.Now(),
			})
			if err != nil {
				slog.Warn("failed to persist delete ack sync state", "error", err, "agent", cfg.Agent, "file_path", cfg.FilePath, "scope", scope)
			}
			continue
		}
		if cfg.ContentHash == "" {
			continue
		}
		h.persistSyncState(ownerID, req.DeviceID, cfg.Agent, cfg.FilePath, scope, cfg.LocalPath, cfg.Version, cfg.ContentHash)
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{"ok": true}})
}

// Merge uses an LLM to intelligently merge two versions of a config file.
// POST /v1/configs/merge
func (h *ConfigsHandler) Merge(w http.ResponseWriter, r *http.Request) {
	if h.llm == nil {
		writeError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM merge requires ANTHROPIC_API_KEY to be configured")
		return
	}
	ownerID := GetUserID(r)

	var req struct {
		LocalContent string `json:"local_content"`
		CloudContent string `json:"cloud_content"`
		FilePath     string `json:"file_path"`
		Agent        string `json:"agent"`
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

	if req.LocalContent == "" || req.CloudContent == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "Both local_content and cloud_content are required")
		return
	}

	localContent := anthropic.TruncateMiddleToTokens(req.LocalContent, 12000)
	cloudContent := anthropic.TruncateMiddleToTokens(req.CloudContent, 12000)
	if localContent != req.LocalContent || cloudContent != req.CloudContent {
		writeError(w, http.StatusBadRequest, "merge_truncated_input", "Config is too large to merge safely. Please choose local or cloud, or merge manually.")
		return
	}

	prompt := fmt.Sprintf(`You are merging two versions of an AI agent configuration file. Both versions may have unique additions that should be preserved.

File: %s (agent: %s)

Rules:
1. Preserve ALL unique content from both versions — do not drop sections.
2. When both versions have the same section with different content, combine them intelligently.
3. Remove exact duplicates (same text appearing in both).
4. Maintain the original formatting style (markdown headings, bullet points, etc.).
5. If there are true contradictions (e.g., "use X" vs "don't use X"), keep the most recent/specific one and add a comment noting the conflict.
6. Output ONLY the merged file content — no commentary, no explanations, no markdown fences.

=== LOCAL VERSION ===
%s

=== CLOUD VERSION ===
%s

=== MERGED RESULT ===`, req.FilePath, req.Agent, localContent, cloudContent)

	llmCtx := anthropic.WithTracking(r.Context(), anthropic.Tracking{
		DistinctID: ownerID,
		Metadata: map[string]any{
			"owner_id":  ownerID,
			"llm_flow":  "config_sync",
			"agent":     req.Agent,
			"file_path": req.FilePath,
		},
	})
	resp, err := h.llm.Complete(llmCtx, anthropic.CompleteRequest{
		Model:     anthropic.ModelFromEnv("MERGE_MODEL"),
		MaxTokens: 4096,
		Prompt:    prompt,
		Purpose:   "configs.merge",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "merge_failed", fmt.Sprintf("LLM merge failed: %v", err))
		return
	}
	if resp.StopReason == "max_tokens" || resp.OutputTokens >= 4096 || anthropic.HasTruncationMarker(resp.Text) {
		writeError(w, http.StatusBadRequest, "merge_truncated_output", "LLM output was truncated. Please merge locally to avoid data loss.")
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"merged_content": resp.Text,
	}})
}

func (h *ConfigsHandler) persistSyncState(ownerID string, deviceID string, agent string, filePath string, scope string, localPath string, version int, contentHash string) {
	if deviceID == "" || agent == "" || filePath == "" || scope == "" || version <= 0 || contentHash == "" {
		return
	}
	err := h.store.UpsertAgentConfigSyncState(&model.AgentConfigSyncState{
		OwnerID:         ownerID,
		DeviceID:        deviceID,
		Agent:           agent,
		FilePath:        filePath,
		Scope:           scope,
		LocalPath:       localPath,
		LastSeenVersion: version,
		LastSeenHash:    contentHash,
		Suppressed:      false,
		UpdatedAt:       time.Now(),
	})
	if err != nil {
		slog.Warn("failed to persist config sync state", "error", err, "agent", agent, "file_path", filePath, "scope", scope)
	}
}
