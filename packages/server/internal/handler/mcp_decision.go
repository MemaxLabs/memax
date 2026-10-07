package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
)

// toolRequestDecision implements the memax_request_decision MCP tool
// (plan 25 P2). Shares createDecisionGate with the REST endpoint so
// remote MCP, local MCP (via SDK → REST), and any future surface have
// identical semantics.
//
// In spaces on the V2 record it doesn't run: internal/mcpv2 asks a
// decision gate (G-, v2.decision_gates) there instead (epic 1.11).
func (h *MCPHandler) toolRequestDecision(r *http.Request, args json.RawMessage, ownerID string) *mcp.CallToolResult {
	var a struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Context  string   `json:"context"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Question == "" || len(a.Options) < 2 {
		return mcpError("memax_request_decision requires 'question' and 2-4 'options'.")
	}

	hubID := resolvedHubID(r)
	if hubID == "" {
		return mcpError("No target hub resolved for this API key.")
	}
	if denied := mcpRequirePermission(r, PermMemoryWrite, hubID); denied != nil {
		return denied
	}
	sourceAgent := resolveAuthSourceAgent(r)

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		return mcpError("Could not load the target hub.")
	}

	slot, err := createDecisionGate(h.store, h.events, hub, a.Question, a.Context, sourceAgent, a.Options)
	if errors.Is(err, errGateLimit) {
		return mcpError("The board already has 3 open decisions waiting on the user. Don't add more — continue with other work and recall later.")
	}
	if err != nil {
		return mcpError(fmt.Sprintf("Could not create decision gate: %v", err))
	}

	if h.logEvent != nil {
		h.logEvent(ownerID, "push", hubID, "mcp/request_decision", sourceAgent, map[string]any{
			"slot_key": slot.SlotKey,
		})
	}
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	return mcpText(fmt.Sprintf(
		"Decision card created (id: %s). The user has been pinged; their choice will be saved to memory. Recall with keywords from your question later to read it. Continue with other work now.",
		slot.SlotKey))
}
