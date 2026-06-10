package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// AgentsHandler manages connected agent entities.
type AgentsHandler struct {
	store     store.Store
	publisher events.Publisher // user-axis SSE events; nil = no-op (helpers guard)
}

// NewAgentsHandler constructs the agent handler. publisher may be nil
// in test contexts — the events helpers guard nil so mutations still
// succeed with publishing silently disabled.
func NewAgentsHandler(s store.Store, publisher events.Publisher) *AgentsHandler {
	return &AgentsHandler{store: s, publisher: publisher}
}

// List returns all connected agents with computed stats (key count, config count, memory count, last active).
// GET /v1/agents
func (h *AgentsHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	agents, err := h.store.ListConnectedAgentsWithStats(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if agents == nil {
		agents = []model.ConnectedAgentWithStats{}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: agents})
}

// Update patches an agent's display_name and/or icon.
// PATCH /v1/agents/{slug}
func (h *AgentsHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	slug := r.PathValue("slug")

	if slug == "" {
		writeError(w, http.StatusBadRequest, "missing_slug", "Agent slug is required")
		return
	}

	// Memax is the always-on platform actor — its identity (display
	// name, icon) is owned by the frontend AGENT_IDENTITIES catalog,
	// not the connected_agents table. Reject rename/icon mutations
	// loudly instead of silently no-opping (UpdateConnectedAgent's
	// SQL would match no rows since memax is never inserted).
	if slug == "memax" {
		writeError(w, http.StatusBadRequest, "builtin_agent_immutable",
			"Memax is a built-in agent and can't be renamed or restyled")
		return
	}

	existing, err := h.store.GetConnectedAgent(ownerID, slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}

	var req struct {
		DisplayName *string `json:"display_name"`
		Icon        *string `json:"icon"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if req.DisplayName != nil {
		existing.DisplayName = *req.DisplayName
	}
	if req.Icon != nil {
		existing.Icon = *req.Icon
	}

	if err := h.store.UpdateConnectedAgent(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	track(ownerID, "api.agents.update", map[string]any{"agent": slug})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: existing})
}

// Disconnect removes a connected agent: revokes all keys, tombstones +
// deletes configs, removes agent row. Memories are preserved (they belong
// to the user, not the agent).
//
// Returns a structured `AgentDisconnectResult` in the ApiResponse envelope.
// The handler always returns 200 — `not_found` and `cascade_failed` are
// reported as skip reasons in the `skipped` array, not as 4xx/5xx. This
// matches the memory batch-delete + config batch-delete pattern and lets
// the client distinguish idempotent success (`not_found`, target reached)
// from retryable failure (`cascade_failed`, rollback needed).
//
// Skip reasons:
//   - not_found: `GetConnectedAgent` returned an error. Unknown slug or
//     another user's agent (WHERE clause makes them indistinguishable).
//     Client treats this as silent success — the user's target state is
//     reached.
//   - cascade_failed: the atomic delete tx returned an error mid-flight.
//     Server still has the agent row; client must roll back the optimistic
//     removal and surface retry copy.
//
// DELETE /v1/agents/{slug}
func (h *AgentsHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	slug := r.PathValue("slug")

	if slug == "" {
		writeError(w, http.StatusBadRequest, "missing_slug", "Agent slug is required")
		return
	}

	// Memax is the always-on platform actor — disconnect makes no
	// sense (nothing to revoke, no row to delete). Reject 400 instead
	// of letting the cascade no-op silently, which would return
	// "disconnected: true · revoked 0 · tombstoned 0" and lie to the
	// client.
	if slug == "memax" {
		writeError(w, http.StatusBadRequest, "builtin_agent_immutable",
			"Memax is a built-in agent and can't be disconnected")
		return
	}

	emptySkipped := []model.SkippedMemory{}

	_, err := h.store.GetConnectedAgent(ownerID, slug)
	if err != nil {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.AgentDisconnectResult{
			Disconnected: false,
			Skipped: []model.SkippedMemory{
				{ID: slug, Reason: model.AgentDisconnectSkipNotFound},
			},
		}})
		return
	}

	// Pre-query the cascade counts so the client can show
	// "disconnected claude-code — revoked 3 keys, forgot 12 configs"
	// without waiting for a refetch. Count errors are non-fatal —
	// degrade to zero rather than aborting the whole disconnect.
	keysRevoked, err := h.store.CountAgentApiKeys(ownerID, slug)
	if err != nil {
		slog.Warn("count agent api keys failed", "agent", slug, "owner", ownerID, "err", err)
		keysRevoked = 0
	}
	configsTombstoned, err := h.store.CountAgentConfigs(ownerID, slug)
	if err != nil {
		slog.Warn("count agent configs failed", "agent", slug, "owner", ownerID, "err", err)
		configsTombstoned = 0
	}

	if err := h.store.DeleteConnectedAgent(ownerID, slug); err != nil {
		slog.Error("disconnect agent failed", "agent", slug, "owner", ownerID, "err", err)
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.AgentDisconnectResult{
			Disconnected: false,
			Skipped: []model.SkippedMemory{
				{ID: slug, Reason: model.AgentDisconnectSkipCascadeFailed},
			},
		}})
		return
	}

	track(ownerID, "api.agents.disconnect", map[string]any{
		"agent":              slug,
		"keys_revoked":       keysRevoked,
		"configs_tombstoned": configsTombstoned,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.AgentDisconnectResult{
		Disconnected:      true,
		KeysRevoked:       keysRevoked,
		ConfigsTombstoned: configsTombstoned,
		Skipped:           emptySkipped,
	}})

	// Emit user-axis SSE so other tabs/devices drop the agent row
	// from Settings → Your Agents and invalidate the cascaded keys
	// list without a reload. A single agent.changed invalidates
	// connected-agents + api-keys + agent-configs via the client
	// route, so we don't emit N per-key key.changed events here.
	events.PublishAgentChanged(r.Context(), h.publisher, ownerID, slug, "disconnected")
}

// EnsureConnectedAgent is a helper for auto-populating the
// connected_agents table. Called from nine paths (MCP first-request,
// OAuth, API key create, config upsert, memory push with
// source_agent, etc.). Fire-and-forget — errors are logged, not
// propagated.
//
// Plan 18 §4.6 onboarding tick used to fire here via a package-level
// hook. Now the materializer at internal/onboarding/materialize.go
// computes connect_agent completion lazily from connected_agents on
// notification read, so the row insert above + the agent.changed
// event downstream are all the sync this path needs.
func EnsureConnectedAgent(s store.Store, ownerID, agentName string) {
	if agentName == "" {
		return
	}
	if err := s.UpsertConnectedAgent(&model.ConnectedAgent{
		OwnerID:   ownerID,
		AgentName: agentName,
	}); err != nil {
		slog.Warn("failed to upsert connected agent", "agent", agentName, "owner", ownerID, "err", err)
	}
}
