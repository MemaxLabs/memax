package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// MCPV2 serves MCP tool calls in spaces that switched to the V2 record
// (plan 25 §5.12). internal/mcpv2 implements it on the ledger; it can't
// live here because it needs the /v2 principal mapping (v2api imports this
// package). Nil means every space behaves as V1.
type MCPV2 interface {
	// CallTool serves the call when it touches a space on the V2 record and
	// reports handled = false otherwise, so the V1 tool runs exactly as
	// before. For a call that spans spaces (recall without a space), it
	// composes the V2 part with call.RunV1 for the V1 spaces.
	CallTool(ctx context.Context, call *MCPToolCall) (res *mcp.CallToolResult, handled bool)
	// StepUp reports whether a write tool targets a space on the V2 record
	// that the OAuth token's scope can't write to, and the scope to ask for.
	StepUp(r *http.Request, tool string, args json.RawMessage) (scope string, needed bool)
}

// MCPToolCall is one tool call, as both the V1 and the V2 tools see it.
type MCPToolCall struct {
	// Tool is the canonical name (memax_push); Name is the name as called
	// (save_memory on the ChatGPT profile).
	Tool    string
	Name    string
	Profile string
	Args    json.RawMessage
	// HTTP is the request with its auth context (GetUserID, GetGrant, …);
	// Writer its response, for headers.
	HTTP   *http.Request
	Writer http.ResponseWriter
	// MCP is the go-sdk request: the session (for elicitation/create on a
	// legacy session), the client's capabilities and protocol version, and
	// on a multi round-trip retry the answers and the request state.
	MCP     *mcp.CallToolRequest
	OwnerID string

	h *MCPHandler
}

// ResolveHub finds a hub the request can reach by ID, slug or "personal",
// the way the V1 tools do: an empty ref means the request's active hub,
// then the personal hub.
func (c *MCPToolCall) ResolveHub(ref string) (model.HubWithRole, error) {
	return c.h.resolveMCPHub(c.HTTP, c.OwnerID, ref)
}

// AccessibleHubs lists the hubs the request's credential can reach.
func (c *MCPToolCall) AccessibleHubs() ([]model.HubWithRole, error) {
	return c.h.accessibleMCPHubs(c.HTTP, c.OwnerID)
}

// RunV1 runs the V1 implementation of the tool with args (nil: the call's
// own). Reads leave out the hubs in exclude, which are on the V2 record:
// V1 memories there are notes, never served as context.
func (c *MCPToolCall) RunV1(args json.RawMessage, exclude map[string]bool) *mcp.CallToolResult {
	if args == nil {
		args = c.Args
	}
	return c.h.runV1(c, args, exclude)
}

// Store is the V1 store, for V1 data the V2 tools still read (hub
// members, notes).
func (c *MCPToolCall) Store() interface {
	ListHubMembers(hubID string) ([]model.HubMember, error)
} {
	return c.h.store
}

// LogEvent records the tool call for the agent card, when a meter is wired.
func (c *MCPToolCall) LogEvent(op, hubID, source, agentName string, metadata map[string]any) {
	if c.h.logEvent != nil {
		c.h.logEvent(c.OwnerID, op, hubID, source, agentName, metadata)
	}
}
