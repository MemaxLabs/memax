package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
)

const (
	ListMemoriesName = "list_memories"
	GetMemoryName    = "get_memory"
	ListHubsName     = "list_hubs"
	GetHubName       = "get_hub"
	ListTopicsName   = "list_topics"
)

const (
	defaultListMemoriesLimit = 12
	maxListMemoriesLimit     = 50
)

type BrowseToolConfig struct {
	Service  *AgentBrowseService
	Resolver *agent.ScopeResolver
	Session  agent.SessionDescriptor
}

func validateBrowseToolConfig(cfg BrowseToolConfig) error {
	if cfg.Service == nil {
		return errors.New("tools: BrowseToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return errors.New("tools: BrowseToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return fmt.Errorf("tools: BrowseToolConfig.Session: %w", err)
	}
	return nil
}

type listMemoriesHandler struct {
	service  *AgentBrowseService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

type listMemoriesArgs struct {
	HubID   string `json:"hub_id,omitempty"`
	TopicID string `json:"topic_id,omitempty"`
	Sort    string `json:"sort,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Since   string `json:"since,omitempty"`
}

func NewListMemoriesTool(cfg BrowseToolConfig) (sdktool.Tool, error) {
	if err := validateBrowseToolConfig(cfg); err != nil {
		return nil, err
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:            ListMemoriesName,
			Description:     "List recent memories in the current chat session scope. Use this when the user asks what is in memory, asks for recent saved items, or needs memory IDs before get_memory or forget_memory.",
			SearchHint:      "list memories recent saved notes ids",
			InputSchema:     listMemoriesSchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
			MaxResultBytes:  64 * 1024,
		},
		Handler: (&listMemoriesHandler{service: cfg.Service, resolver: cfg.Resolver, session: cfg.Session}).execute,
	}, nil
}

func (h *listMemoriesHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[listMemoriesArgs](call.Use)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("decode args: %v", err))
	}
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		return browseErrResult(call, errEmptyOwnerInScope.Error())
	}
	if len(scope.HubIDs) == 0 {
		return browseErrResult(call, errEmptyScope.Error())
	}

	hubID := strings.TrimSpace(args.HubID)
	if hubID != "" && !scope.IsHubAllowed(hubID) {
		return browseErrResult(call, errHubNotInScope.Error())
	}
	limit := args.Limit
	if limit == 0 {
		limit = defaultListMemoriesLimit
	}
	if limit < 0 || limit > maxListMemoriesLimit {
		return browseErrResult(call, "limit must be > 0 and <= 50")
	}
	sortBy := strings.TrimSpace(args.Sort)
	if sortBy != "" && sortBy != "newest" && sortBy != "relevant" {
		return browseErrResult(call, `sort must be "newest" or "relevant"`)
	}

	var since time.Time
	if s := strings.TrimSpace(args.Since); s != "" {
		// Accept RFC3339 / ISO-8601 with timezone. The system prompt
		// tells the agent to use this format; if the model sends a
		// vague natural-language date we reject rather than guess,
		// so the user can correct.
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return browseErrResult(call, `since must be an ISO-8601 timestamp (e.g. "2026-05-01T00:00:00Z")`)
		}
		since = t.UTC()
	}

	req := AgentListMemoriesRequest{
		OwnerID: scope.OwnerID,
		HubIDs:  scope.HubIDs,
		HubID:   hubID,
		TopicID: strings.TrimSpace(args.TopicID),
		Sort:    sortBy,
		Limit:   limit,
		Cursor:  strings.TrimSpace(args.Cursor),
		Since:   since,
	}
	resp, err := h.service.AgentListMemories(ctx, req)
	if err != nil {
		return browseErrResult(call, err.Error())
	}
	return browseJSONResult(call, resp)
}

type getMemoryHandler struct {
	service  *AgentBrowseService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

type getMemoryArgs struct {
	MemoryID string `json:"memory_id"`
}

func NewGetMemoryTool(cfg BrowseToolConfig) (sdktool.Tool, error) {
	if err := validateBrowseToolConfig(cfg); err != nil {
		return nil, err
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:            GetMemoryName,
			Description:     "Read the full content of one memory by memory_id, constrained to the current chat session scope.",
			SearchHint:      "get read memory detail content full",
			InputSchema:     getMemorySchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
			MaxResultBytes:  96 * 1024,
		},
		Handler: (&getMemoryHandler{service: cfg.Service, resolver: cfg.Resolver, session: cfg.Session}).execute,
	}, nil
}

func (h *getMemoryHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[getMemoryArgs](call.Use)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("decode args: %v", err))
	}
	memoryID := strings.TrimSpace(args.MemoryID)
	if memoryID == "" {
		return browseErrResult(call, "memory_id is required")
	}
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		return browseErrResult(call, errEmptyOwnerInScope.Error())
	}
	if len(scope.HubIDs) == 0 {
		return browseErrResult(call, errEmptyScope.Error())
	}
	req := AgentGetMemoryRequest{
		OwnerID:  scope.OwnerID,
		HubIDs:   scope.HubIDs,
		MemoryID: memoryID,
	}
	resp, err := h.service.AgentGetMemory(ctx, req)
	if err != nil {
		return browseErrResult(call, err.Error())
	}
	return browseJSONResult(call, resp)
}

type listHubsHandler struct {
	service  *AgentBrowseService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

func NewListHubsTool(cfg BrowseToolConfig) (sdktool.Tool, error) {
	if err := validateBrowseToolConfig(cfg); err != nil {
		return nil, err
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:            ListHubsName,
			Description:     "List hubs available to the current chat session, including IDs the agent can use for hub-scoped tools.",
			SearchHint:      "list hubs workspaces teams ids",
			InputSchema:     map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MaxResultBytes:  16 * 1024,
		},
		Handler: (&listHubsHandler{service: cfg.Service, resolver: cfg.Resolver, session: cfg.Session}).execute,
	}, nil
}

func (h *listHubsHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		return browseErrResult(call, errEmptyOwnerInScope.Error())
	}
	if len(scope.HubIDs) == 0 {
		return browseErrResult(call, errEmptyScope.Error())
	}
	req := AgentListHubsRequest{
		OwnerID: scope.OwnerID,
		HubIDs:  scope.HubIDs,
	}
	resp, err := h.service.AgentListHubs(ctx, req)
	if err != nil {
		return browseErrResult(call, err.Error())
	}
	return browseJSONResult(call, resp)
}

type getHubHandler struct {
	service  *AgentBrowseService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

type getHubArgs struct {
	HubID string `json:"hub_id"`
}

func NewGetHubTool(cfg BrowseToolConfig) (sdktool.Tool, error) {
	if err := validateBrowseToolConfig(cfg); err != nil {
		return nil, err
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:            GetHubName,
			Description:     "Read metadata for one hub in the current chat session scope.",
			SearchHint:      "get hub workspace team metadata",
			InputSchema:     getHubSchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
			MaxResultBytes:  8 * 1024,
		},
		Handler: (&getHubHandler{service: cfg.Service, resolver: cfg.Resolver, session: cfg.Session}).execute,
	}, nil
}

func (h *getHubHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[getHubArgs](call.Use)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("decode args: %v", err))
	}
	hubID := strings.TrimSpace(args.HubID)
	if hubID == "" {
		return browseErrResult(call, "hub_id is required")
	}
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		return browseErrResult(call, errEmptyOwnerInScope.Error())
	}
	if len(scope.HubIDs) == 0 {
		return browseErrResult(call, errEmptyScope.Error())
	}
	if !scope.IsHubAllowed(hubID) {
		return browseErrResult(call, errHubNotInScope.Error())
	}
	req := AgentGetHubRequest{
		OwnerID: scope.OwnerID,
		HubIDs:  scope.HubIDs,
		HubID:   hubID,
	}
	resp, err := h.service.AgentGetHub(ctx, req)
	if err != nil {
		return browseErrResult(call, err.Error())
	}
	return browseJSONResult(call, resp)
}

type listTopicsHandler struct {
	service  *AgentBrowseService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

type listTopicsArgs struct {
	HubID string `json:"hub_id"`
}

func NewListTopicsTool(cfg BrowseToolConfig) (sdktool.Tool, error) {
	if err := validateBrowseToolConfig(cfg); err != nil {
		return nil, err
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:            ListTopicsName,
			Description:     "List topics in one hub in the current chat session scope, with memory counts.",
			SearchHint:      "list topics categories organize hub",
			InputSchema:     listTopicsSchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
			MaxResultBytes:  32 * 1024,
		},
		Handler: (&listTopicsHandler{service: cfg.Service, resolver: cfg.Resolver, session: cfg.Session}).execute,
	}, nil
}

func (h *listTopicsHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[listTopicsArgs](call.Use)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("decode args: %v", err))
	}
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		return browseErrResult(call, errEmptyOwnerInScope.Error())
	}
	if len(scope.HubIDs) == 0 {
		return browseErrResult(call, errEmptyScope.Error())
	}
	hubID := strings.TrimSpace(args.HubID)
	if hubID == "" {
		if len(scope.HubIDs) != 1 {
			return browseErrResult(call, "hub_id is required when the session has multiple hubs")
		}
		hubID = scope.HubIDs[0]
	}
	if !scope.IsHubAllowed(hubID) {
		return browseErrResult(call, errHubNotInScope.Error())
	}
	req := AgentListTopicsRequest{
		OwnerID: scope.OwnerID,
		HubIDs:  scope.HubIDs,
		HubID:   hubID,
	}
	resp, err := h.service.AgentListTopics(ctx, req)
	if err != nil {
		return browseErrResult(call, err.Error())
	}
	return browseJSONResult(call, resp)
}

func browseJSONResult(call sdktool.Call, v any) (sdkmodel.ToolResult, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return browseErrResult(call, fmt.Sprintf("encode result: %v", err))
	}
	return sdkmodel.ToolResult{
		ToolUseID: call.Use.ID,
		Name:      call.Use.Name,
		Content:   string(body),
	}, nil
}

func browseErrResult(call sdktool.Call, msg string) (sdkmodel.ToolResult, error) {
	return sdkmodel.ToolResult{
		ToolUseID: call.Use.ID,
		Name:      call.Use.Name,
		Content:   msg,
		IsError:   true,
	}, nil
}

var listMemoriesSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]any{
		"hub_id": map[string]any{
			"type":        "string",
			"description": "Optional. Narrow to one hub in the session scope.",
		},
		"topic_id": map[string]any{
			"type":        "string",
			"description": "Optional. Narrow to memories assigned to this topic.",
		},
		"sort": map[string]any{
			"type":        "string",
			"description": `Optional. "newest" (default) or "relevant" (most accessed).`,
			"enum":        []any{"newest", "relevant"},
		},
		"limit": map[string]any{
			"type":        "integer",
			"description": "Optional. Max results (1-50). Default 12.",
			"minimum":     1,
			"maximum":     maxListMemoriesLimit,
		},
		"cursor": map[string]any{
			"type":        "string",
			"description": "Optional pagination cursor from a previous list_memories result.",
		},
		"since": map[string]any{
			"type":        "string",
			"description": `Optional ISO-8601 UTC timestamp (e.g. "2026-05-01T00:00:00Z"). When set, returns only memories whose updated_at is at or after this instant. Use this when the user asks "what's new since X" or "anything updated this week" — compute the cutoff from the Current time in the system prompt.`,
		},
	},
}

var getMemorySchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"memory_id"},
	"properties": map[string]any{
		"memory_id": map[string]any{
			"type":        "string",
			"description": "Memory UUID from list_memories or recall_memories.",
			"minLength":   1,
		},
	},
}

var getHubSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"hub_id"},
	"properties": map[string]any{
		"hub_id": map[string]any{
			"type":        "string",
			"description": "Hub UUID from list_hubs.",
			"minLength":   1,
		},
	},
}

var listTopicsSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]any{
		"hub_id": map[string]any{
			"type":        "string",
			"description": "Hub UUID. Required when the session has multiple hubs; optional for single-hub sessions.",
		},
	},
}
