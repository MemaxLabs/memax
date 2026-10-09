package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// MCPHandler serves the remote MCP server on /mcp (the agent profile) and
// /mcp/chatgpt (the ChatGPT profile) on the official go-sdk
// (modelcontextprotocol/go-sdk), for both protocol eras:
//
//   - 2026-07-28 (stateless): every request carries its version and
//     capabilities in _meta; server/discover replaces initialize, and a
//     tool that needs the person's answer returns resultType
//     "input_required" (multi round-trip requests). Served by a stateless
//     handler, so any machine can answer any request.
//   - 2025-11-25, 2025-06-18, 2025-03-26 and 2024-11-05: the initialize
//     handshake. A client that advertises elicitation gets a session
//     (Mcp-Session-Id), because elicitation/create is a request from the
//     server and needs a session to answer on. Every other legacy request
//     is served statelessly, as V1 served them all. See mcp_transport.go.
//
// The tools are the catalogue in mcp_tools.json. Each call runs the V1
// implementation (mcp_tools_v1.go) unless the space it touches has
// switched to the V2 record, in which case the injected MCPV2 serves it
// (internal/mcpv2, through the ledger).
type MCPHandler struct {
	store         store.Store
	recall        *RecallHandler
	memories      *MemoriesHandler
	events        events.Publisher
	mode          mcpMode
	logEvent      LogEventFn
	beginOp       func(r *http.Request, userID, op, billingHubID string) (func(bool), *meterctx.OpDenial)
	denialMessage func(*meterctx.OpDenial) string
	v2            MCPV2

	server    *mcp.Server
	stateless *mcp.StreamableHTTPHandler
	stateful  *mcp.StreamableHTTPHandler
	// instance is the machine this process runs on (FLY_MACHINE_ID), the
	// prefix of every session ID it mints, so a request for a session
	// another machine holds is replayed there (mcp_transport.go).
	instance string
	log      *slog.Logger
}

// LogEventFn is the subset of meter.Meter used by MCP tools to write
// analytics rows into usage_events. Modelled as a callback rather than a
// direct meter package dependency so MCPHandler stays loosely coupled —
// the meter is wired at route-registration time via SetLogEvent.
//
// Called from the push / recall / capture tools to record the operation
// and summary metadata so the agent card's "Recalled X" / "Saved Y" line
// can populate. The HTTP meter middleware never runs for MCP requests
// because ClassifyOperation only recognises REST paths (/v1/memories,
// /v1/recall, /v1/ask) — MCP's JSON-RPC path is /mcp, so it falls through
// with no logging. This callback is the explicit escape hatch.
//
// Nil is a valid state: when no meter is wired (tests, memory-mode), the
// tool handlers skip logging silently.
type LogEventFn func(userID, op, hubID, source, agentName string, metadata map[string]any)

type mcpMode string

const (
	mcpModeAgent   mcpMode = "agent"
	mcpModeChatGPT mcpMode = "chatgpt"
)

// MCPServerVersion is the version the remote MCP server reports.
const MCPServerVersion = "2.0.0"

// mcpSessionTimeout closes a legacy session after this long without a
// request. Clients re-initialize when told the session is gone.
const mcpSessionTimeout = 30 * time.Minute

// mcpMaxBody bounds an MCP request body. A V1 push may carry a document of
// a few MB, JSON-escaped.
const mcpMaxBody = 10 << 20

func NewMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher) *MCPHandler {
	return newMCPHandler(s, recall, memories, publisher, mcpModeAgent)
}

func NewChatGPTMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher) *MCPHandler {
	return newMCPHandler(s, recall, memories, publisher, mcpModeChatGPT)
}

func newMCPHandler(s store.Store, recall *RecallHandler, memories *MemoriesHandler, publisher events.Publisher, mode mcpMode) *MCPHandler {
	h := &MCPHandler{
		store: s, recall: recall, memories: memories, events: publisher, mode: mode,
		log: slog.Default().With("component", "mcp", "profile", string(mode)),
	}
	name, instructions := mcpProfileMeta(string(mode))
	tools, err := mcpProfileTools(string(mode))
	if err != nil {
		// The catalogue is embedded and tested; a broken one is a build bug.
		panic(err)
	}
	// go-sdk logs every stateless connect at Info; keep its warnings.
	sdkLog := slog.New(levelFilter{h: slog.Default().Handler(), min: slog.LevelWarn}).With("component", "mcp")
	h.server = mcp.NewServer(&mcp.Implementation{Name: name, Title: "Memax", Version: MCPServerVersion}, &mcp.ServerOptions{
		Instructions: instructions,
		Logger:       sdkLog,
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		GetSessionID: h.newSessionID,
		SetCacheable: mcpCacheable,
	})
	for _, t := range tools {
		h.server.AddTool(t, h.toolHandler(t.Name))
	}
	h.stateless = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		Logger:              sdkLog,
		MaxRequestBodyBytes: mcpMaxBody,
		// Every MCP request carries a bearer token a browser can't attach,
		// so DNS rebinding can't reach anything; and a self-hosted server
		// behind a local reverse proxy must keep working.
		DisableLocalhostProtection: true,
	})
	h.stateful = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return h.server }, &mcp.StreamableHTTPOptions{
		// Server-sent events, so elicitation/create travels on the response
		// stream of the tools/call that asks it.
		JSONResponse:               false,
		Logger:                     sdkLog,
		SessionTimeout:             mcpSessionTimeout,
		MaxRequestBodyBytes:        mcpMaxBody,
		DisableLocalhostProtection: true,
	})
	return h
}

// SetLogEvent wires the meter's LogEvent so MCP tool handlers can
// write usage_events rows directly. See LogEventFn doc for the why.
// Safe to call with nil — the tool handlers null-check before calling.
func (h *MCPHandler) SetLogEvent(fn LogEventFn) {
	h.logEvent = fn
}

// SetOpGuard wires quota enforcement for tool calls (meter.BeginOp +
// meter.OpDenialMessage, injected as functions — meter imports
// handler for context accessors, so handler can never import meter).
// Nil-safe: without a guard (dev/memory mode, tests) tools run
// unmetered, exactly the pre-2026-09 behavior.
func (h *MCPHandler) SetOpGuard(
	begin func(r *http.Request, userID, op, billingHubID string) (func(committed bool), *meterctx.OpDenial),
	message func(*meterctx.OpDenial) string,
) {
	h.beginOp = begin
	h.denialMessage = message
}

// SetV2 lets spaces that switched to the V2 record be served through the
// ledger (internal/mcpv2). Nil keeps every space on V1.
func (h *MCPHandler) SetV2(v2 MCPV2) { h.v2 = v2 }

// SetInstance names the machine this process runs on (FLY_MACHINE_ID). It
// prefixes the session IDs this process mints, so a legacy session's
// requests reach the machine that holds it (mcp_transport.go).
func (h *MCPHandler) SetInstance(id string) { h.instance = id }

// guardOp reserves one metered op for a tool call. A non-nil denial
// result means the quota refused it: the tool must return it at once.
// finish is never nil; call finish(true) after the operation durably
// succeeded, finish(false) on any error path (the deferred-flag idiom at
// the call sites).
func (h *MCPHandler) guardOp(r *http.Request, userID, op, billingHubID string) (finish func(bool), denied *mcp.CallToolResult) {
	if h.beginOp == nil {
		return func(bool) {}, nil
	}
	finish, denial := h.beginOp(r, userID, op, billingHubID)
	if denial != nil {
		text := "Quota exceeded."
		if h.denialMessage != nil {
			text = h.denialMessage(denial)
		}
		return func(bool) {}, mcpError(text)
	}
	return finish, nil
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

// --- Tool dispatch ---

// toolHandler is the go-sdk handler of one tool name in this profile.
func (h *MCPHandler) toolHandler(name string) mcp.ToolHandler {
	canonical := mcpCanonicalName(string(h.mode), name)
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		r, w := mcpHTTPFrom(req)
		if r == nil {
			return nil, fmt.Errorf("memax: tool call without its HTTP request")
		}
		call := &MCPToolCall{
			Tool: canonical, Name: name, Profile: string(h.mode),
			Args: req.Params.Arguments, HTTP: r, Writer: w, MCP: req,
			OwnerID: GetUserID(r), h: h,
		}
		if len(call.Args) == 0 {
			call.Args = json.RawMessage(`{}`)
		}
		return h.callTool(ctx, call), nil
	}
}

func (h *MCPHandler) callTool(ctx context.Context, c *MCPToolCall) *mcp.CallToolResult {
	track(c.OwnerID, "api.mcp.tool_call", map[string]any{"tool": c.Name})
	if h.v2 != nil {
		if res, handled := h.v2.CallTool(ctx, c); handled {
			return res
		}
	}
	return h.runV1(c, c.Args, nil)
}

// runV1 runs the V1 implementation of a tool. exclude lists hubs on the V2
// record that V1 reads must leave out (nil: V1 exactly as before).
func (h *MCPHandler) runV1(c *MCPToolCall, args json.RawMessage, exclude map[string]bool) *mcp.CallToolResult {
	w, r, ownerID := c.Writer, c.HTTP, c.OwnerID
	switch c.Tool {
	case toolRecall:
		return h.toolRecall(r, args, ownerID, exclude)
	case toolSearch:
		return h.toolSearchMemories(r, args, ownerID, exclude)
	case toolPush:
		return h.toolPush(w, r, args, ownerID)
	case toolGet:
		return h.toolGet(r, args, exclude)
	case toolList:
		return h.toolList(r, args, ownerID, exclude)
	case toolHubs:
		return h.toolHubs(r, ownerID, exclude)
	case toolHubMembers:
		return h.toolHubMembers(r, args, ownerID)
	case toolForget:
		return h.toolForget(r, args, ownerID, exclude)
	case toolCapture:
		return h.toolCapture(w, r, args, ownerID)
	case toolRequestDecision:
		return h.toolRequestDecision(r, args, ownerID)
	case toolTopics:
		return h.toolTopics(r, args, ownerID)
	}
	return mcpError(fmt.Sprintf("Unknown tool: %s", c.Name))
}

// --- Results ---

// mcpText is a successful tool result with one text block.
func mcpText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// mcpError is a tool error the model reads and can act on.
func mcpError(text string) *mcp.CallToolResult {
	res := mcpText(text)
	res.IsError = true
	return res
}

// mcpStructured is a successful result with structured content and its
// text mirror.
func mcpStructured(text string, structured any) *mcp.CallToolResult {
	res := mcpText(text)
	res.StructuredContent = structured
	return res
}

// mcpCacheable sets the cache hints (ttlMs, cacheScope) on discover and
// list results. The tool list is the same for everyone who connects to a
// profile, so it may be cached for a few minutes by anyone.
func mcpCacheable(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
	c.TTLMs = int((5 * time.Minute).Milliseconds())
	c.CacheScope = "public"
}

// writeRPCError writes a JSON-RPC error outside the go-sdk (a step-up
// challenge, a request too malformed to route).
func writeRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Error   any    `json:"error"`
	}{"2.0", id, struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, message}})
}

// --- Sessions ---

// newSessionID mints a legacy session ID: this machine's ID, a dot, and a
// random part.
func (h *MCPHandler) newSessionID() string {
	if h.instance == "" {
		return rand.Text()
	}
	return h.instance + "." + rand.Text()
}

// levelFilter drops records below min (the go-sdk's per-request Info logs).
type levelFilter struct {
	h   slog.Handler
	min slog.Level
}

func (f levelFilter) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= f.min && f.h.Enabled(ctx, l)
}
func (f levelFilter) Handle(ctx context.Context, r slog.Record) error { return f.h.Handle(ctx, r) }
func (f levelFilter) WithAttrs(a []slog.Attr) slog.Handler {
	return levelFilter{h: f.h.WithAttrs(a), min: f.min}
}
func (f levelFilter) WithGroup(name string) slog.Handler {
	return levelFilter{h: f.h.WithGroup(name), min: f.min}
}

func generateMCPID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func hashContentBytes(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
