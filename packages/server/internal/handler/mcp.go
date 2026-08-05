package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// MCPHandler implements the MCP Streamable HTTP transport.
// POST /mcp handles all JSON-RPC messages (initialize, tools/list, tools/call).
// LogEventFn is the subset of meter.Meter used by MCP tools to write
// analytics rows into usage_events. Modelled as a callback rather than
// a direct meter package dependency so MCPHandler stays loosely coupled
// — the meter is wired at route-registration time via SetLogEvent.
//
// Called from toolPush / toolRecall / toolCapture to record the
// operation + summary metadata so the agent card's "Recalled X" /
// "Saved Y" line can populate. The HTTP meter middleware never runs
// for MCP requests because ClassifyOperation only recognises REST
// paths (/v1/memories, /v1/recall, /v1/ask) — MCP's JSON-RPC path is
// /mcp, so it falls through with no logging. This callback is the
// explicit escape hatch.
//
// Nil is a valid state: when no meter is wired (tests, memory-mode),
// the tool handlers skip logging silently.
type LogEventFn func(userID, op, hubID, source, agentName string, metadata map[string]any)

type MCPHandler struct {
	store    store.Store
	recall   *RecallHandler
	memories *MemoriesHandler
	events   events.Publisher
	mode     mcpMode
	sessions sync.Map // sessionID -> *mcpSession
	logEvent LogEventFn
}

type mcpMode string

const (
	mcpModeAgent   mcpMode = "agent"
	mcpModeChatGPT mcpMode = "chatgpt"
)

type mcpSession struct {
	ownerID string
}

func NewMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher) *MCPHandler {
	return newMCPHandler(s, recall, memories, publisher, mcpModeAgent)
}

func NewChatGPTMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher) *MCPHandler {
	return newMCPHandler(s, recall, memories, publisher, mcpModeChatGPT)
}

func newMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher, mode mcpMode) *MCPHandler {
	return &MCPHandler{store: s, recall: recall, memories: memories, events: publisher, mode: mode}
}

// SetLogEvent wires the meter's LogEvent so MCP tool handlers can
// write usage_events rows directly. See LogEventFn doc for the why.
// Safe to call with nil — the tool handlers null-check before calling.
func (h *MCPHandler) SetLogEvent(fn LogEventFn) {
	h.logEvent = fn
}

func (h *MCPHandler) publishMemoryChanged(ctx context.Context, memory *model.Memory, actorID string) {
	if memory == nil {
		return
	}
	privateOnly := false
	if memory.Boundary == "private" {
		if hub, err := h.store.GetHub(memory.HubID); err == nil && hub.HubType == "personal" {
			privateOnly = true
		}
	}
	events.PublishMemoryChangedWithPrivacy(ctx, h.events, memory, actorID, privateOnly)
}

// --- JSON-RPC types ---

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // number or string; nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP tool/content types ---

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations map[string]any  `json:"annotations,omitempty"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// --- HTTP handler ---

func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		// SSE stream — not supported, return 405
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	case http.MethodDelete:
		// Session termination
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID != "" {
			h.sessions.Delete(sessionID)
		}
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *MCPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, -32700, "Could not read request body")
		return
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "Invalid JSON")
		return
	}

	// Get owner from auth middleware
	ownerID := GetUserID(r)

	switch req.Method {
	case "initialize":
		h.handleInitialize(w, r, req, ownerID)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		h.handleToolsList(w, req)
	case "tools/call":
		h.handleToolsCall(w, r, req, ownerID)
	case "ping":
		writeRPCResult(w, req.ID, map[string]any{})
	default:
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (h *MCPHandler) handleInitialize(w http.ResponseWriter, r *http.Request, req jsonrpcRequest, ownerID string) {
	// Generate session ID
	b := make([]byte, 16)
	rand.Read(b)
	sessionID := hex.EncodeToString(b)

	h.sessions.Store(sessionID, &mcpSession{ownerID: ownerID})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", sessionID)
	json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    h.serverName(),
				"version": "1.0.0",
			},
			"instructions": h.instructions(),
		},
	})
}

func (h *MCPHandler) handleToolsList(w http.ResponseWriter, req jsonrpcRequest) {
	if h.mode == mcpModeChatGPT {
		writeRPCResult(w, req.ID, map[string]any{"tools": h.chatGPTTools()})
		return
	}

	writeRPCResult(w, req.ID, map[string]any{"tools": h.agentTools()})
}

func (h *MCPHandler) serverName() string {
	if h.mode == mcpModeChatGPT {
		return "memax-chatgpt"
	}
	return "memax"
}

func (h *MCPHandler) instructions() string {
	if h.mode == mcpModeChatGPT {
		return "Memax gives ChatGPT access to the user's personal and shared team knowledge. Use search_memories for retrieval, cite returned memory IDs and source metadata, use get_memory only when the full memory is needed, and only call save_memory or forget_memory when the user explicitly asks to change their Memax knowledge base."
	}
	return "memax is a personal knowledge brain — shared memory for humans and AI. Use memax_recall to ask questions and surface memories, memax_push to remember new knowledge, memax_get to read a full memory, memax_list to browse memories, memax_hubs to inspect available hubs, and memax_request_decision to put a decision you should not make alone in front of the user."
}

func (h *MCPHandler) agentTools() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "memax_recall",
			Description: "Search every hub the current token can access with a natural language query. Returns relevant memories ranked by relevance. Use this when you need background information about the project, team processes, or past decisions.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Natural language search query"},"limit":{"type":"number","description":"Max results to return (default 10)"},"topic_id":{"type":"string","description":"Restrict results to memories in this topic (UUID)."},"hub_id":{"type":"string","description":"Hub ID, slug, or 'personal'. Boosts ranking for this hub."},"project_context":{"type":"object","description":"Current project context for relevance boosting. Keys: repo (git remote URL), project (short name)."}},"required":["query"]}`),
		},
		{
			Name:        "memax_push",
			Description: "Save a piece of knowledge to the user's Memax knowledge base. Memax classifies it automatically for retrieval.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","description":"The knowledge content to save"},"title":{"type":"string","description":"Optional title (auto-generated if omitted)"},"hint":{"type":"string","description":"Context hint to help AI process this memory (e.g. 'This is my resume', 'Meeting notes from product review'). Improves summarization and retrieval."},"tags":{"type":"array","items":{"type":"string"},"description":"Tags for the memory"},"source_agent":{"type":"string","description":"Agent identity (e.g. 'claude-code', 'cursor'). Auto-set by CLI."},"initiation_type":{"type":"string","description":"How this save was initiated: human_direct, human_requested_agent, agent_proactive, agent_automatic, import, or unknown."},"project_context":{"type":"object","description":"Project context (auto-detected by CLI). Keys: repo (git remote URL), project (short name), branch."},"hub_id":{"type":"string","description":"Target hub ID. Required when pushing into a team hub."},"hub_reason":{"type":"string","description":"Why this belongs in the shared hub. Required when hub_id targets a team hub."}},"required":["content"]}`),
		},
		{
			Name:        "memax_get",
			Description: "Get the full content of a specific memory by ID. Use this after memax_recall to read the complete content of a relevant memory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The memory ID (from memax_recall or memax_list results)"}},"required":["id"]}`),
		},
		{
			Name:        "memax_list",
			Description: "List memories with pagination. Use cursor from a previous response for the next page. Returns total count.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"number","description":"Max results (default 20, max 50)"},"cursor":{"type":"string","description":"Pagination cursor from previous response"},"sort":{"type":"string","description":"Sort: newest (default) or relevant"},"hub_id":{"type":"string","description":"Optional hub ID, slug, or \"personal\" to filter results."},"topic_id":{"type":"string","description":"Optional topic ID (UUID) to filter results."}}}`),
		},
		{
			Name:        "memax_hubs",
			Description: "List hubs the current user can access, including hub IDs, slugs, roles, and memory counts.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "memax_hub_members",
			Description: "List members for a hub. Requires hub_id (hub ID, slug, or \"personal\").",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"hub_id":{"type":"string","description":"Hub ID, slug, or \"personal\"."}},"required":["hub_id"]}`),
		},
		{
			Name:        "memax_forget",
			Description: "Delete a memory from the user's Memax knowledge base by ID. Use this to remove outdated, incorrect, or duplicate memories.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The memory ID to delete (from memax_recall or memax_list results)"}},"required":["id"]}`),
		},
		{
			Name:        "memax_capture",
			Description: "Capture key decisions, learnings, and context from this session. Call at the end of a significant work session to save what was accomplished and what should be remembered. Each fact is extracted and stored as a separate searchable memory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"Brief summary of what was accomplished"},"decisions":{"type":"array","items":{"type":"string"},"description":"Key decisions made"},"learnings":{"type":"array","items":{"type":"string"},"description":"Things learned"}},"required":["summary"]}`),
		},
		{
			Name:        "memax_request_decision",
			Description: "Ask the user to make a decision you should not make alone (architecture choices, irreversible actions, tradeoffs with no clear winner). Creates a decision card on the user's memax board and pings them. The user's choice is written back into memory — call memax_recall later with keywords from your question to read their decision. Do NOT block waiting; continue with other work or end your turn.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string","description":"The decision question, one sentence, user-facing"},"options":{"type":"array","items":{"type":"string"},"description":"2-4 mutually exclusive choices, short labels"},"context":{"type":"string","description":"Why this decision matters — tradeoffs, constraints, your recommendation if any"}},"required":["question","options"]}`),
		},
		{
			Name:        "memax_topics",
			Description: "Browse the user's knowledge topics. Without topic_id: returns full topic tree with memory counts. With topic_id: returns memories in that topic.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"topic_id":{"type":"string","description":"Topic ID to browse. Omit for full tree."},"hub_id":{"type":"string","description":"Hub ID to scope topics."}}}`),
		},
	}

	return tools
}

func (h *MCPHandler) chatGPTTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "search_memories",
			Description: "Search Memax memories with a natural language query across every hub the current user can access. Use this to find source memories before answering.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Natural language search query."},"limit":{"type":"number","description":"Maximum number of results to return. Default 5."},"topic_id":{"type":"string","description":"Restrict results to memories in this topic (UUID)."},"project_context":{"type":"object","description":"Optional project context for relevance boosting. Keys may include repo and project."}},"required":["query"]}`),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Searching memories", "openai/toolInvocation/invoked": "Searched memories"},
		},
		{
			Name:        "get_memory",
			Description: "Get the full content and metadata for one Memax memory by ID. Use this after search_memories when the full memory is necessary.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The memory ID returned by search_memories."}},"required":["id"]}`),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Opening memory", "openai/toolInvocation/invoked": "Opened memory"},
		},
		{
			Name:        "list_hubs",
			Description: "List personal and shared Memax hubs the current user can access, including hub IDs, slugs, roles, and memory counts.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Listing hubs", "openai/toolInvocation/invoked": "Listed hubs"},
		},
		{
			Name:        "list_hub_members",
			Description: "List members for a hub. Requires hub_id (hub ID, slug, or \"personal\").",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"hub_id":{"type":"string","description":"Hub ID, slug, or \"personal\"."}},"required":["hub_id"]}`),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Listing hub members", "openai/toolInvocation/invoked": "Listed hub members"},
		},
		{
			Name:        "list_topics",
			Description: "Browse Memax topic organization. Without topic_id returns the topic tree. With topic_id returns memories in that topic.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"topic_id":{"type":"string","description":"Topic ID to browse. Omit for the topic tree."},"hub_id":{"type":"string","description":"Hub ID, slug, or personal. Omit to use the active hub."}}}`),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Loading topics", "openai/toolInvocation/invoked": "Loaded topics"},
		},
		{
			Name:        "save_memory",
			Description: "Save user-approved knowledge to Memax. Only use this when the user explicitly asks ChatGPT to remember, save, or add something to Memax.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","description":"The knowledge content to save."},"title":{"type":"string","description":"Optional title. Generated if omitted."},"hint":{"type":"string","description":"Optional processing hint such as meeting notes, decision, or personal preference."},"tags":{"type":"array","items":{"type":"string"},"description":"Optional tags."},"hub_id":{"type":"string","description":"Target hub ID, slug, or personal. Omit to use the active hub."},"hub_reason":{"type":"string","description":"Required when saving into a team hub. Explain why the content belongs there."}},"required":["content"]}`),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Saving memory", "openai/toolInvocation/invoked": "Saved memory"},
		},
		{
			Name:        "forget_memory",
			Description: "Delete a Memax memory by ID. Only use this when the user explicitly asks to delete or forget that memory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The memory ID to delete."}},"required":["id"]}`),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false},
			Meta:        map[string]any{"openai/toolInvocation/invoking": "Forgetting memory", "openai/toolInvocation/invoked": "Forgot memory"},
		},
	}
}

func (h *MCPHandler) handleToolsCall(w http.ResponseWriter, r *http.Request, req jsonrpcRequest, ownerID string) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	track(ownerID, "api.mcp.tool_call", map[string]any{"tool": params.Name})

	switch params.Name {
	case "memax_recall", "search_memories":
		h.toolRecall(w, r, req.ID, params.Arguments, ownerID)
	case "memax_push", "save_memory":
		h.toolPush(w, r, req.ID, params.Arguments, ownerID)
	case "memax_get", "get_memory":
		h.toolGet(w, r, req.ID, params.Arguments, ownerID)
	case "memax_list":
		h.toolSearch(w, r, req.ID, params.Arguments, ownerID)
	case "memax_hubs", "list_hubs":
		h.toolHubs(w, r, req.ID, ownerID)
	case "memax_hub_members", "list_hub_members":
		h.toolHubMembers(w, r, req.ID, params.Arguments, ownerID)
	case "memax_forget", "forget_memory":
		h.toolForget(w, r, req.ID, params.Arguments, ownerID)
	case "memax_capture":
		h.toolCapture(w, r, req.ID, params.Arguments, ownerID)
	case "memax_request_decision":
		h.toolRequestDecision(w, r, req.ID, params.Arguments, ownerID)
	case "memax_topics", "list_topics":
		h.toolTopics(w, r, req.ID, params.Arguments, ownerID)
	default:
		writeRPCResult(w, req.ID, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}},
			IsError: true,
		})
	}
}

// --- Tool implementations ---

func (h *MCPHandler) toolRecall(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		Query          string            `json:"query"`
		Limit          int               `json:"limit"`
		TopicID        string            `json:"topic_id"`
		HubID          string            `json:"hub_id"`
		ProjectContext map[string]string `json:"project_context"`
	}
	json.Unmarshal(args, &a)
	if a.Limit <= 0 {
		a.Limit = 5
	}
	if !mcpRequirePermission(w, r, id, PermMemoryRead, "") {
		return
	}

	scope := requestRecallScope(r, nil)
	// Resolve hub_id (supports UUID, slug, or "personal")
	if a.HubID != "" {
		hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
		if err == nil {
			scope.ActiveBoostHubID = hub.Hub.ID
		}
	}
	var filters *model.SearchFilters
	if a.TopicID != "" {
		filters = &model.SearchFilters{TopicID: a.TopicID, Explicit: true}
	}
	results, _, err := h.recall.RunPipeline(r.Context(), a.Query, "mcp", "", a.ProjectContext, a.Limit, ownerID, filters, scope)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Recall failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	// Log the usage event ONCE, above the success/empty branches —
	// "agent recalled X" reflects the user's intent, not whether
	// results were found. A zero-result recall is still real agent
	// activity for the card. Single logging site also avoids future
	// drift where a new branch is added without logging.
	//
	// hub_id mirrors the REST meter middleware's resolveBillingHub
	// for recall: GetHubID(r) is the active read hub (falls back to
	// the personal hub via HubContext), so default-personal MCP
	// recalls attribute correctly. Explicit scoping via args.hub_id
	// populates scope.ActiveBoostHubID and wins over the default —
	// matching the REST path where an X-Hub-ID header would.
	// Summary is run through compactActivitySummary so multiline /
	// long text gets the same 96-rune truncation the REST path
	// applies.
	attributionHubID := scope.ActiveBoostHubID
	if attributionHubID == "" {
		attributionHubID = GetHubID(r)
	}
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "recall", attributionHubID, "mcp", GetAgentName(r),
			map[string]any{"summary": compactActivitySummary(a.Query, 96)},
		)
	}
	// PR H.2's TryPublishAgentActivity was wired into RecallHandler.Recall
	// (the HTTP /v1/recall path) but MCP's toolRecall bypasses that
	// handler and calls h.recall.RunPipeline directly — so agent.changed
	// never fired for MCP recalls. Result: Settings → Your Agents "Last
	// activity" didn't tick live for MCP-driven recalls until the user
	// navigated away and back. Emit here on the MCP success path so
	// MCP and REST have parity. Coalescer still dedup's bursts.
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, GetAgentName(r))

	if len(results) == 0 {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "No results found."}},
		})
		return
	}

	var sb strings.Builder
	for i, n := range results {
		score := int(n.RelevanceScore * 100)
		heading := ""
		if n.HeadingChain != "" {
			heading = fmt.Sprintf(" — %s", n.HeadingChain)
		}
		fmt.Fprintf(&sb, "[%d] %s [%s, %s, %d%%, %s] (id: %s)%s\n",
			i+1, n.Title, n.Kind, n.Stability, score, n.Age, n.ID, heading)
		if n.Summary != "" {
			fmt.Fprintf(&sb, "Summary: %s\n", n.Summary)
		}
		fmt.Fprintf(&sb, "Relevant excerpt:\n%s\n\n", n.ChunkContent)
	}

	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: sb.String()}},
	})
}

func (h *MCPHandler) toolPush(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		Content        string            `json:"content"`
		Title          string            `json:"title"`
		Hint           string            `json:"hint"`
		Tags           []string          `json:"tags"`
		SourceAgent    string            `json:"source_agent"`
		InitiationType string            `json:"initiation_type"`
		ProjectContext map[string]string `json:"project_context"`
		HubID          string            `json:"hub_id"`
		HubReason      string            `json:"hub_reason"`
	}
	json.Unmarshal(args, &a)

	if a.Content == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Content is required."}},
			IsError: true,
		})
		return
	}

	if a.Title == "" {
		a.Title = ingesttitle.GenerateFromContent(a.Content)
	}
	if a.Tags == nil {
		a.Tags = []string{}
	}
	hubRef := strings.TrimSpace(a.HubID)
	hubWithRole, err := h.resolveMCPHub(r, ownerID, hubRef)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Hub not found or not accessible."}},
			IsError: true,
		})
		return
	}
	hub := &hubWithRole.Hub
	hubID := hub.ID
	if !mcpRequirePermission(w, r, id, PermMemoryWrite, hubID) {
		return
	}
	role := hubWithRole.Role
	if !canWriteMemories(role) && hub.OwnerID != ownerID {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Write access to this hub is required."}},
			IsError: true,
		})
		return
	}
	hubReason := strings.TrimSpace(a.HubReason)
	if hub.HubType == "team" && hubReason == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "hub_reason is required when pushing to a team hub."}},
			IsError: true,
		})
		return
	}

	memoryID := generateMCPID()
	now := time.Now()
	projCtx := a.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}
	provenance, sourceAgent, claimRejected, provErr := resolveMemoryProvenance(h.store, ownerID, model.PushRequest{
		Source:         "mcp",
		SourceAgent:    a.SourceAgent,
		InitiationType: a.InitiationType,
	}, r)
	if provErr != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Push failed: requested agent attribution conflicts with the authenticated agent."}},
			IsError: true,
		})
		return
	}
	if claimRejected {
		setMemaxWarningHeader(w, memaxWarningClaimRejected)
	} else if requestNeedsReconnectWarning(r) {
		setMemaxWarningHeader(w, memaxWarningReconnectNeeded)
	}
	if sourceAgent != "" && provenance.AttributionSource == model.MemoryAttributionSourceAuth {
		go EnsureConnectedAgent(h.store, ownerID, sourceAgent)
	}
	// Auto-heal: claim-path pushes from API-key principals pin the
	// key's agent_name so connected_agents + subsequent auth-path
	// attribution stay correct.
	if conflict, _ := tryAutoHealAPIKeyAgent(r.Context(), h.store, r, sourceAgent, provenance.AttributionSource); conflict {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Push failed: requested agent attribution conflicts with the authenticated agent."}},
			IsError: true,
		})
		return
	}
	userProvidedHint := strings.TrimSpace(a.Hint) != ""
	hint := buildHint(a.Hint, "mcp", "", projCtx)

	// Related-context enrichment: same permission check as REST handler.
	allowRelatedCtx := false
	if !userProvidedHint {
		if authCtx := GetAuthContext(r); authCtx != nil {
			allowRelatedCtx = authCtx.PermissionsByHub[hubID].Has(PermMemoryRead)
		} else {
			allowRelatedCtx = canReadMemories(role)
		}
	}
	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        ownerID,
		Title:                          a.Title,
		Content:                        a.Content,
		ContentType:                    "markdown",
		ContentHash:                    hashContentBytes(a.Content),
		Hint:                           hint,
		Kind:                           model.MemoryKindSemantic,
		Stability:                      model.MemoryStabilityEvolving,
		RetrievalWeight:                1.0,
		Tags:                           a.Tags,
		Boundary:                       "private",
		State:                          "active",
		Source:                         "mcp",
		SourceAgent:                    sourceAgent,
		Provenance:                     provenance,
		ProvenanceCreatedByType:        provenance.CreatedByType,
		ProvenanceCreatedBySlug:        provenance.CreatedBySlug,
		ProvenanceCreatedByDisplayName: provenance.CreatedByDisplayName,
		ProvenanceCreatedVia:           provenance.CreatedVia,
		ProvenanceInitiationType:       provenance.InitiationType,
		ProvenanceAttributionSource:    provenance.AttributionSource,
		ProjectContext:                 projCtx,
		HubReason:                      hubReason,
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}
	// Same sanitization chokepoint as handler.Create — Title, Hint,
	// and Content funnel through sanitizeMemory regardless of intake
	// route (REST vs MCP).
	sanitizeMemory(memory)

	if err := h.store.CreateMemory(memory); err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Push failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	// Trigger the same background processing pipeline as POST /v1/memories
	// (chunking, embedding, categorization, summarization)
	if h.memories != nil {
		h.memories.ProcessMemoryBackground(memoryID, ownerID, model.PushRequest{
			Content:             a.Content,
			Title:               a.Title,
			Hint:                hint,
			Kind:                model.MemoryKindSemantic,
			Stability:           model.MemoryStabilityEvolving,
			Tags:                a.Tags,
			ContentType:         "markdown",
			Source:              "mcp",
			HubReason:           hubReason,
			ProjectContext:      projCtx,
			AllowRelatedContext: allowRelatedCtx,
		})
	}

	slog.Info("mcp push", "id", memoryID, "title", a.Title)
	// Log the usage event directly — the HTTP meter middleware's
	// ClassifyOperation only knows REST paths, so /mcp falls through
	// with no auto-logging. Prefer sourceAgent (effective, post-
	// auto-heal slug from resolveMemoryProvenance) over
	// GetAgentName(r) (pre-heal grant), so the first Hatch-style
	// claim push attributes correctly. Summary reads from memory.Title
	// rather than a.Title so the logged activity matches the sanitized
	// title that actually landed in the DB — mirrors the REST path
	// which meters post-sanitizePushRequest.
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "push", hubID, "mcp", sourceAgent,
			map[string]any{"summary": compactActivitySummary(memory.Title, 96)},
		)
	}
	// Mirror the parity fix applied to toolRecall: MCP pushes don't
	// route through MemoriesHandler's HTTP path, so without this emit
	// the Settings → Your Agents card's "last activity" stays stale
	// for MCP-driven pushes until navigation. Prefer sourceAgent over
	// GetAgentName(r) because the provenance resolver may have
	// auto-healed to a different slug (Hatch-style claim).
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Saved: %s (id: %s)", a.Title, memoryID)}},
	})
}

func (h *MCPHandler) toolGet(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		ID string `json:"id"`
	}
	json.Unmarshal(args, &a)

	hubIDs := GetAccessibleHubIDs(r)
	// Scope-aware load — scope-bounded principals (OAuth grants, hub-
	// allowlisted API keys) get strict hub-only filtering so they
	// cannot fetch a memory outside their granted hubs by ID via MCP.
	memory, err := loadMemoryRespectingScope(r, h.store, a.ID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Memory not found: %s", a.ID)}},
			IsError: true,
		})
		return
	}
	if !mcpRequirePermission(w, r, id, PermMemoryRead, memory.HubID) {
		return
	}

	// memax_get is explicit user intent (an agent asked for the full
	// memory), so it counts as a deliberate view for decay scoring —
	// same contract as the REST POST /v1/memories/{id}/access path.
	// Fire-and-forget; a failed increment must not block the read.
	go h.store.IncrementMemoryAccessed(context.Background(), a.ID, ownerID, hubIDs)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n", memory.Title)
	fmt.Fprintf(&sb, "Kind: %s | Stability: %s | Source: %s | Created: %s\n", memory.Kind, memory.Stability, memory.Source, memory.CreatedAt.Format("2006-01-02"))
	if len(memory.Tags) > 0 {
		fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(memory.Tags, ", "))
	}
	if memory.SourcePath != "" {
		fmt.Fprintf(&sb, "Source: %s\n", memory.SourcePath)
	}
	if memory.Summary != "" {
		fmt.Fprintf(&sb, "\n## Summary\n%s\n", memory.Summary)
	}
	fmt.Fprintf(&sb, "\n## Content\n%s", memory.Content)

	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: sb.String()}},
	})
}

func (h *MCPHandler) toolSearch(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		Limit   int    `json:"limit"`
		Cursor  string `json:"cursor"`
		Sort    string `json:"sort"`
		HubID   string `json:"hub_id"`
		TopicID string `json:"topic_id"`
	}
	json.Unmarshal(args, &a)
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if !mcpRequirePermission(w, r, id, PermMemoryRead, "") {
		return
	}

	hubID := ""
	if a.HubID != "" {
		hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
		if err != nil {
			writeRPCResult(w, id, mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: "Hub not found or not accessible."}},
				IsError: true,
			})
			return
		}
		hubID = hub.Hub.ID
	}
	if a.TopicID != "" && !isValidUUID(a.TopicID) {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "topic_id must be a valid UUID"}},
			IsError: true,
		})
		return
	}

	memories, nextCursor, totalCount, err := h.store.ListMemoriesPaginated(store.ListOptions{
		Scope:   store.VisibilityScope{OwnerID: ownerID, HubIDs: GetAccessibleHubIDs(r)},
		HubID:   hubID,
		TopicID: a.TopicID,
		Limit:   a.Limit,
		Cursor:  a.Cursor,
		Sort:    a.Sort,
	})
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Search failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	if len(memories) == 0 {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("No memories found. (%d total in workspace)", totalCount)}},
		})
		return
	}

	var sb strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&sb, "- %s [%s/%s] — %s (id: %s)\n", m.Title, m.Kind, m.Stability, m.Source, m.ID)
	}
	fmt.Fprintf(&sb, "\nShowing %d of %d total.", len(memories), totalCount)
	if nextCursor != "" {
		fmt.Fprintf(&sb, " More available — pass cursor: \"%s\" for next page.", nextCursor)
	}

	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: sb.String()}},
	})
}

func (h *MCPHandler) toolHubs(w http.ResponseWriter, r *http.Request, id any, ownerID string) {
	if !mcpRequirePermission(w, r, id, PermHubRead, "") {
		return
	}
	hubs, err := h.accessibleMCPHubs(r, ownerID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Hubs failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}
	if len(hubs) == 0 {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "No hubs found."}},
		})
		return
	}

	activeHubID := GetHubID(r)
	var sb strings.Builder
	for _, item := range hubs {
		ref := mcpHubReference(item.Hub)
		active := ""
		if item.Hub.ID == activeHubID {
			active = " active"
		}
		fmt.Fprintf(&sb, "- **%s** (%s, %s%s) ref: %s id: %s memories: %d\n",
			item.Hub.Name, item.Hub.HubType, item.Role, active, ref, item.Hub.ID, item.MemoryCount)
	}
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: strings.TrimSpace(sb.String())}},
	})
}

func (h *MCPHandler) toolHubMembers(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		HubID string `json:"hub_id"`
	}
	json.Unmarshal(args, &a)

	hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Hub members failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}
	if !mcpRequirePermission(w, r, id, PermHubMembersRead, hub.Hub.ID) {
		return
	}

	members, err := h.store.ListHubMembers(hub.Hub.ID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Hub members failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s members\n\n", hub.Hub.Name)
	if len(members) == 0 {
		sb.WriteString("No members found.")
	} else {
		for _, member := range members {
			email := ""
			if member.UserEmail != "" {
				email = fmt.Sprintf(" <%s>", member.UserEmail)
			}
			fmt.Fprintf(&sb, "- **%s**%s [%s] joined: %s\n",
				mcpMemberDisplayName(member), email, member.Role, member.JoinedAt.Format(time.RFC3339))
		}
	}
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: strings.TrimSpace(sb.String())}},
	})
}

func (h *MCPHandler) toolForget(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		ID string `json:"id"`
	}
	json.Unmarshal(args, &a)

	if a.ID == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Memory ID is required."}},
			IsError: true,
		})
		return
	}

	// Scope-aware load — same boundary as toolGet. Without this a
	// scope-bounded principal could delete a memory outside their
	// granted hubs by guessing its ID.
	memory, err := loadMemoryRespectingScope(r, h.store, a.ID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Memory not found: %s", a.ID)}},
			IsError: true,
		})
		return
	}
	if !mcpRequirePermission(w, r, id, PermMemoryDelete, memory.HubID) {
		return
	}

	isMemoryOwner := memory.OwnerID == ownerID
	if !isMemoryOwner {
		role, _ := h.store.GetHubMemberRole(memory.HubID, ownerID)
		hub, hubErr := h.store.GetHub(memory.HubID)
		if hubErr != nil || !canDeleteMemory(role, hub, false) {
			writeRPCResult(w, id, mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: "You do not have permission to delete this memory."}},
				IsError: true,
			})
			return
		}
		if err := h.store.DeleteHubMemory(a.ID, memory.HubID); err != nil {
			writeRPCResult(w, id, mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Delete failed: %s", err.Error())}},
				IsError: true,
			})
			return
		}
	} else {
		if err := h.store.DeleteMemory(a.ID, ownerID); err != nil {
			writeRPCResult(w, id, mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Delete failed: %s", err.Error())}},
				IsError: true,
			})
			return
		}
	}

	slog.Info("mcp forget", "id", a.ID, "title", memory.Title)
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Forgotten: %s (id: %s)", memory.Title, a.ID)}},
	})
}

func (h *MCPHandler) toolCapture(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		Summary   string   `json:"summary"`
		Decisions []string `json:"decisions"`
		Learnings []string `json:"learnings"`
	}
	json.Unmarshal(args, &a)

	if a.Summary == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Summary is required."}},
			IsError: true,
		})
		return
	}

	// Build structured content for the extraction pipeline
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Session Summary\n%s\n", a.Summary)
	if len(a.Decisions) > 0 {
		sb.WriteString("\n## Decisions Made\n")
		for _, d := range a.Decisions {
			fmt.Fprintf(&sb, "- %s\n", d)
		}
	}
	if len(a.Learnings) > 0 {
		sb.WriteString("\n## Learnings\n")
		for _, l := range a.Learnings {
			fmt.Fprintf(&sb, "- %s\n", l)
		}
	}

	content := sb.String()
	memoryID := generateMCPID()
	now := time.Now()
	hubID := resolvedHubID(r)
	if hubID == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "No active hub could be resolved for this request."}},
			IsError: true,
		})
		return
	}
	if !mcpRequirePermission(w, r, id, PermMemoryWrite, hubID) {
		return
	}
	authAgent := resolveAuthSourceAgent(r)
	if authAgent == "" {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Session capture failed: authenticated agent identity is required."}},
			IsError: true,
		})
		return
	}
	provenance, sourceAgent, claimRejected, provErr := resolveMemoryProvenance(h.store, ownerID, model.PushRequest{
		Source:         "mcp",
		InitiationType: model.MemoryInitiationAgentAutomatic,
	}, r)
	if provErr != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: "Session capture failed: authenticated agent attribution was invalid."}},
			IsError: true,
		})
		return
	}
	if claimRejected {
		setMemaxWarningHeader(w, memaxWarningClaimRejected)
	} else if requestNeedsReconnectWarning(r) {
		setMemaxWarningHeader(w, memaxWarningReconnectNeeded)
	}
	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        ownerID,
		Title:                          fmt.Sprintf("Session capture — %s", now.Format("2006-01-02")),
		Content:                        content,
		ContentType:                    "transcript",
		ContentHash:                    hashContentBytes(content),
		Kind:                           model.MemoryKindEpisodic,
		Stability:                      model.MemoryStabilityVolatile,
		RetrievalWeight:                1.0,
		Tags:                           []string{},
		Boundary:                       "private",
		State:                          "processing",
		Source:                         "mcp/capture",
		SourceAgent:                    sourceAgent,
		Provenance:                     provenance,
		ProvenanceCreatedByType:        provenance.CreatedByType,
		ProvenanceCreatedBySlug:        provenance.CreatedBySlug,
		ProvenanceCreatedByDisplayName: provenance.CreatedByDisplayName,
		ProvenanceCreatedVia:           provenance.CreatedVia,
		ProvenanceInitiationType:       provenance.InitiationType,
		ProvenanceAttributionSource:    provenance.AttributionSource,
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}
	// MCP intake skips the REST PushRequest shape, so funnel through
	// the same sanitizer as handler.Create before persist. Without
	// this, <img onerror=...> in title/hint from an MCP-speaking
	// agent would land in the DB unsanitized.
	sanitizeMemory(memory)

	if err := h.store.CreateMemory(memory); err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Capture failed: %s", err.Error())}},
			IsError: true,
		})
		return
	}
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	// Trigger extraction pipeline in background
	if h.memories != nil {
		// Capture has no user-provided hint, so enrichment is always
		// allowed if the credential has read access.
		captureAllowCtx := false
		if authCtx := GetAuthContext(r); authCtx != nil {
			captureAllowCtx = authCtx.PermissionsByHub[hubID].Has(PermMemoryRead)
		} else {
			captureAllowCtx = true // web session — personal hub owner
		}
		h.memories.ProcessMemoryBackground(memoryID, ownerID, model.PushRequest{
			Content:             content,
			Title:               memory.Title,
			Kind:                model.MemoryKindEpisodic,
			Stability:           model.MemoryStabilityVolatile,
			ContentType:         "transcript",
			Source:              "mcp/capture",
			AllowRelatedContext: captureAllowCtx,
		})
	}

	slog.Info("mcp capture", "id", memoryID, "decisions", len(a.Decisions), "learnings", len(a.Learnings))
	track(ownerID, "api.mcp.capture", map[string]any{"decisions": len(a.Decisions), "learnings": len(a.Learnings)})
	// Log usage event for the agent-card activity line. Capture maps
	// to the "push" op because the underlying memory type + cost
	// profile match — it's a write, not a read. Summary normalized
	// to match the REST path's setUsageEventSummary contract.
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "push", hubID, "mcp/capture", sourceAgent,
			map[string]any{"summary": compactActivitySummary(a.Summary, 96)},
		)
	}
	// Same parity fix as toolRecall / toolPush — capture is an MCP
	// tool that writes a usage_event; without this emit the
	// connected-agents card stays stale after capture.
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Session captured (id: %s). Key facts will be extracted and stored as separate memories.", memoryID)}},
	})
}

func (h *MCPHandler) toolTopics(w http.ResponseWriter, r *http.Request, id any, args json.RawMessage, ownerID string) {
	var a struct {
		TopicID string `json:"topic_id"`
		HubID   string `json:"hub_id"`
	}
	json.Unmarshal(args, &a)

	hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Failed to resolve hub: %s", err.Error())}},
			IsError: true,
		})
		return
	}
	hubID := hub.Hub.ID
	if !mcpRequirePermission(w, r, id, PermTopicRead, hubID) {
		return
	}
	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: []string{hubID}}

	if a.TopicID != "" {
		// Browse specific topic's memories
		memories, _, err := h.store.ListMemoriesByTopic(scope, a.TopicID, 20, "")
		if err != nil {
			writeRPCResult(w, id, mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Failed to list topic memories: %s", err.Error())}},
				IsError: true,
			})
			return
		}

		var sb strings.Builder
		topic, _ := h.store.GetTopic(a.TopicID, hubID)
		if topic != nil {
			sb.WriteString(fmt.Sprintf("## %s (%d memories)\n\n", topic.Name, len(memories)))
		}
		for i, m := range memories {
			sb.WriteString(fmt.Sprintf("%d. **%s** [%s/%s] (id: %s)\n", i+1, m.Title, m.Kind, m.Stability, m.ID))
			if m.Summary != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", m.Summary))
			}
		}
		if len(memories) == 0 {
			sb.WriteString("No memories in this topic yet.\n")
		}
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: sb.String()}},
		})
		return
	}

	topics, err := h.store.ListTopics(hubID)
	if err != nil {
		writeRPCResult(w, id, mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("Failed to list topics: %s", err.Error())}},
			IsError: true,
		})
		return
	}

	counts, _ := h.store.CountMemoriesByTopic(scope, hubID)
	unassigned, _ := h.store.CountUnassignedMemories(scope, hubID)

	var sb strings.Builder
	sb.WriteString("## Topics\n\n")

	if len(topics) == 0 {
		sb.WriteString("No topics yet. Push memories and run a dream cycle to auto-organize.\n")
	} else {
		for _, t := range topics {
			count := counts[t.ID]
			indent := ""
			if t.ParentID != nil {
				indent = "  "
			}
			sb.WriteString(fmt.Sprintf("%s- **%s** (%d memories) [id: %s]\n", indent, t.Name, count, t.ID))
			if t.Description != "" {
				sb.WriteString(fmt.Sprintf("%s  %s\n", indent, t.Description))
			}
		}
	}

	if unassigned > 0 {
		sb.WriteString(fmt.Sprintf("\n📥 **Inbox**: %d unassigned memories\n", unassigned))
	}

	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: sb.String()}},
	})
}

// --- Helpers ---

func (h *MCPHandler) accessibleMCPHubs(r *http.Request, ownerID string) ([]model.HubWithRole, error) {
	hubs, err := h.store.ListUserHubs(ownerID)
	if err != nil {
		return nil, err
	}

	accessibleIDs := GetAccessibleHubIDs(r)
	if len(accessibleIDs) == 0 {
		return hubs, nil
	}
	allowed := make(map[string]bool, len(accessibleIDs))
	for _, hubID := range accessibleIDs {
		allowed[hubID] = true
	}

	filtered := make([]model.HubWithRole, 0, len(hubs))
	for _, hub := range hubs {
		if allowed[hub.Hub.ID] {
			filtered = append(filtered, hub)
		}
	}
	return filtered, nil
}

func (h *MCPHandler) resolveMCPHub(r *http.Request, ownerID string, ref string) (model.HubWithRole, error) {
	hubs, err := h.accessibleMCPHubs(r, ownerID)
	if err != nil {
		return model.HubWithRole{}, err
	}

	normalizedRef := strings.ToLower(strings.TrimSpace(ref))
	if normalizedRef == "" {
		normalizedRef = strings.ToLower(strings.TrimSpace(GetHubID(r)))
	}
	if normalizedRef == "" {
		normalizedRef = "personal"
	}

	for _, hub := range hubs {
		switch {
		case strings.EqualFold(hub.Hub.ID, normalizedRef):
			return hub, nil
		case strings.EqualFold(hub.Hub.Slug, normalizedRef):
			return hub, nil
		case normalizedRef == "personal" && hub.Hub.HubType == "personal":
			return hub, nil
		}
	}
	return model.HubWithRole{}, fmt.Errorf("hub not found or not accessible")
}

func mcpHubReference(hub model.Hub) string {
	if hub.HubType == "personal" {
		return "personal"
	}
	return hub.Slug
}

func mcpMemberDisplayName(member model.HubMember) string {
	if member.UserName != "" {
		return member.UserName
	}
	if member.UserEmail != "" {
		return member.UserEmail
	}
	return member.UserID
}

func mcpRequirePermission(w http.ResponseWriter, r *http.Request, id any, perm Permission, hubID string) bool {
	if hubID != "" {
		if Can(r, perm, hubID) {
			return true
		}
	} else if CanAny(r, perm) {
		return true
	}
	slog.Warn("mcp tool denied by grant permission", "user_id", GetUserID(r), "permission", perm, "hub_id", hubID)
	writeRPCResult(w, id, mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: "This credential is not allowed to perform that Memax action."}},
		IsError: true,
	})
	return false
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func generateMCPID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func hashContentBytes(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
