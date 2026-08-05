package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
)

// toolRequestDecision implements the memax_request_decision MCP tool
// (plan 25 P2). Shares createDecisionGate with the REST endpoint so
// remote MCP, local MCP (via SDK → REST), and any future surface have
// identical semantics.
func (h *MCPHandler) toolRequestDecision(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Context  string   `json:"context"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Question == "" || len(a.Options) < 2 {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "memax_request_decision requires 'question' and 2-4 'options'."}},
			IsError: true,
		})
		return
	}

	hubID := resolvedHubID(r)
	if hubID == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "No target hub resolved for this API key."}},
			IsError: true,
		})
		return
	}
	if !mcpRequirePermission(w, r, id, PermMemoryWrite, hubID) {
		return
	}
	sourceAgent := resolveAuthSourceAgent(r)

	hub, err := h.store.GetHub(hubID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Could not load the target hub."}},
			IsError: true,
		})
		return
	}

	slot, err := createDecisionGate(h.store, h.events, hub, a.Question, a.Context, sourceAgent, a.Options)
	if errors.Is(err, errGateLimit) {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "The board already has 3 open decisions waiting on the user. Don't add more — continue with other work and recall later."}},
			IsError: true,
		})
		return
	}
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Could not create decision gate: %v", err)}},
			IsError: true,
		})
		return
	}

	if h.logEvent != nil {
		h.logEvent(ownerID, "push", hubID, "mcp/request_decision", sourceAgent, map[string]any{
			"slot_key": slot.SlotKey,
		})
	}
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf(
			"Decision card created (id: %s). The user has been pinged; their choice will be saved to memory. Recall with keywords from your question later to read it. Continue with other work now.",
			slot.SlotKey)}},
	})
}
