// Package tools holds the agent runtime's host-owned tool wrappers.
// Each wrapper:
//
//   - Accepts JSON args validated by the SDK against a JSON schema.
//   - Resolves the session's authoritative scope via ScopeResolver.
//   - Rejects an empty scope (the user has no authorized hub
//     targets — never falls through to owner-only reads).
//   - Validates a caller-supplied hub_id against scope.HubIDs.
//   - Constructs an explicit store.VisibilityScope and passes it
//     into the existing memax store, so owner-isolation enforcement
//     is identical to the rest of the API.
//
// The CI lint at scripts/check-tool-store-calls.go enforces this
// pattern at build time — no tool wrapper may call a hub-scoped
// store method without first passing through VisibilityScope
// construction.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
)

// recall_memories tool name. Exported so the catalog and CI lint
// can reference it.
const RecallMemoriesName = "recall_memories"

// Default limits for recall_memories. The model's natural tendency
// is to ask for "as many as possible"; the limit hard-caps that
// regardless of what's in args. Configurable for tests.
const (
	defaultRecallLimit = 8
	maxRecallLimit     = 20
)

// RecallArgs is the JSON-decoded shape of a recall_memories tool
// call. We expose it as a type so tests can construct args directly
// without having to round-trip through json.Marshal.
//
// HubID is optional. When omitted, the tool fans out across the
// session's full scope (the load-bearing UX for cross-hub research:
// "where did we decide X?" doesn't need to specify a hub). When
// supplied, the tool narrows to that single hub — useful for
// follow-ups like "summarize what's in my work hub specifically."
//
// Per the empty-HubIDs-is-meaningful rule (see SessionScope docs),
// when scope.HubIDs is empty the tool returns an authz error
// rather than falling through to an owner-only VisibilityScope.
type RecallArgs struct {
	Query string `json:"query"`
	HubID string `json:"hub_id,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// RecallResult is one match returned by the tool. The shape is
// flat so the model can read it without descending into nested
// objects; the SDK's max-result-bytes cap on ToolResult bounds
// the total size.
type RecallResult struct {
	MemoryID string  `json:"memory_id"`
	HubID    string  `json:"hub_id"`
	Title    string  `json:"title,omitempty"`
	Snippet  string  `json:"snippet"`
	Score    float64 `json:"score,omitempty"`
}

// RecallService is the focused dependency the tool wrapper needs.
// Defined here as a narrow interface so:
//
//   - The wrapper test can use a small fake (no real embedder, no
//     real Postgres; the security pattern under test is the scope
//     enforcement, not the search algorithm itself).
//   - Production wiring composes the embedder + reranker + a
//     strict-hub search path in a single Service implementation,
//     keeping the tool wrapper agnostic to those moving parts.
//   - A future swap of vector-store or reranker doesn't ripple
//     through the tool wrapper — only the Service implementation.
//
// **STRICT HUB SEMANTICS REQUIRED.** Service implementations MUST
// apply `m.hub_id = ANY(req.HubIDs)` exactly — they MUST NOT use
// store.VisibilityScope or any other primitive that broadens to
// `owner_id OR hub_id`. The agent tool's "narrow to hub_id=X"
// contract requires that user-owned memories outside X do NOT
// leak into results. The tool wrapper has already built the
// authoritative HubIDs list (validated against scope) — the
// service's job is to query exactly that set.
type RecallService interface {
	// AgentRecall returns the memories most relevant to req.Query
	// within req.HubIDs. The Service is responsible for embedding,
	// search, and (eventually) reranking; the tool wrapper supplies
	// the security-validated owner+hub set.
	AgentRecall(ctx context.Context, req AgentRecallRequest) (AgentRecallResponse, error)
}

// AgentRecallRequest is the input the tool wrapper hands to the
// Service. Fields are pre-validated by the wrapper — the Service
// trusts them.
//
// We deliberately do NOT use store.VisibilityScope here.
// VisibilityScope's filter is `owner_id = $user OR hub_id = ANY(hubs)`,
// which is correct for the user-facing /v1/recall endpoint (a user
// recalling their own content shouldn't have to re-scope by hub),
// but WRONG for the agent path: when the model says "search
// hub_id=h-work specifically," user-owned content in h-personal
// must NOT surface. The strict hub-only filter the service applies
// matches the wrapper's narrowing semantics exactly.
type AgentRecallRequest struct {
	// Query is the natural-language search text. Non-empty.
	Query string

	// OwnerID is the user the agent is acting on behalf of. Carried
	// for audit/telemetry; not used by the SQL filter (the strict
	// hub set already contains the right access boundary because
	// every memory is hub-scoped — there are no orphan memories).
	OwnerID string

	// HubIDs is the strict hub allowlist. Service MUST query with
	// `m.hub_id = ANY(HubIDs)` only — no owner-OR-hub fallback.
	// Always non-empty (the wrapper rejects empty before calling).
	HubIDs []string

	// Limit caps results. 1..maxRecallLimit (validated by wrapper).
	Limit int
}

// AgentRecallResponse mirrors RecallResult under a wrapper for
// future fields (e.g. timing, debug info) without breaking the
// tool's wire contract.
type AgentRecallResponse struct {
	Memories []RecallResult
}

// Errors returned to the model. The SDK propagates ToolResult.IsError
// up to the agent loop, which lets the model retry with corrected
// args; we use these specific reasons so the model gets actionable
// feedback ("you used a hub_id you don't have access to" is
// recoverable; "scope is empty" is not).
var (
	errEmptyQuery        = errors.New("query is required")
	errHubNotInScope     = errors.New("hub_id is not in this session's scope")
	errEmptyScope        = errors.New("session has no authorized hub targets")
	errLimitOutOfRange   = errors.New("limit must be > 0 and <= 20")
	errEmptyOwnerInScope = errors.New("internal: scope has no owner")
)

// RecallToolConfig assembles the dependencies a recall_memories
// tool needs. Each session creates one tool instance with a
// session-bound resolver — the tool itself is stateless beyond
// the resolver/service references it captures.
type RecallToolConfig struct {
	Service  RecallService
	Resolver *agent.ScopeResolver
	// Session is the descriptor the wrapper resolves against on
	// every Execute call. Bound at tool-build time; never mutated.
	Session agent.SessionDescriptor
}

// NewRecallTool returns the SDK tool.Tool for recall_memories.
// Returns an error if config is incomplete; callers fail-fast at
// session-create time rather than discovering the bad wiring on
// the agent's first turn.
func NewRecallTool(cfg RecallToolConfig) (sdktool.Tool, error) {
	if cfg.Service == nil {
		return nil, errors.New("agent/tools: RecallToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("agent/tools: RecallToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return nil, fmt.Errorf("agent/tools: invalid session descriptor: %w", err)
	}

	return sdktool.Definition{
		ToolSpec: model.ToolSpec{
			Name:        RecallMemoriesName,
			Description: recallToolDescription,
			InputSchema: recallToolSchema,
			ReadOnly:    true,
			// ConcurrencySafe: the SDK may dispatch parallel reads in
			// one turn (the agent searches two hubs simultaneously,
			// say). Recall is read-only against the store; safe.
			ConcurrencySafe: true,
			// Cap the result size delivered into the model context.
			// At maxRecallLimit=20 results × ~2KB/result observed
			// average, 64KB is comfortable headroom; large memories
			// are still truncated by the service's per-snippet cap.
			// The cap protects against runaway context bloat from
			// pathological hub content, not the typical case.
			MaxResultBytes: 64 * 1024,
		},
		Handler: (&recallHandler{
			service:  cfg.Service,
			resolver: cfg.Resolver,
			session:  cfg.Session,
		}).execute,
	}, nil
}

// recallHandler closes over the dependencies; its execute method
// is the SDK Handler the tool delegates to. Keeping the handler
// as a struct (vs a closure literal) makes it cheap to test the
// individual checks via direct method calls without going through
// the SDK's tool-call shell.
type recallHandler struct {
	service  RecallService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

func (h *recallHandler) execute(ctx context.Context, call sdktool.Call) (model.ToolResult, error) {
	args, err := sdktool.DecodeInput[RecallArgs](call.Use)
	if err != nil {
		return errResult(call, fmt.Sprintf("decode args: %v", err))
	}

	if strings.TrimSpace(args.Query) == "" {
		return errResult(call, errEmptyQuery.Error())
	}
	limit := args.Limit
	if limit == 0 {
		limit = defaultRecallLimit
	}
	if limit < 0 || limit > maxRecallLimit {
		return errResult(call, errLimitOutOfRange.Error())
	}

	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		// Resolve already wraps with package context; surface as a
		// non-retryable error to the model (transient DB failures
		// look like a bug from the model's POV).
		return errResult(call, fmt.Sprintf("scope resolve: %v", err))
	}

	// Defense in depth — Resolve guarantees a non-empty OwnerID via
	// SessionDescriptor.Validate, but a programmer error in the
	// resolver layer would otherwise leak owner-only reads. Belt +
	// suspenders.
	if scope.OwnerID == "" {
		return errResult(call, errEmptyOwnerInScope.Error())
	}

	// CRITICAL — never pass empty HubIDs into VisibilityScope. The
	// store interprets an empty hub list as "owner_id-only reads,"
	// which is the wrong fail-open for a session whose user lost
	// access to every hub in the session's scope. Reject explicitly
	// so the model sees an authz error.
	if len(scope.HubIDs) == 0 {
		return errResult(call, errEmptyScope.Error())
	}

	// Hub narrowing: caller may scope this single recall to one of
	// the session's allowed hubs ("look only in my work hub").
	// Validate against scope.HubIDs (the resolved current set,
	// NOT the descriptor's pinned list — caller could have lost
	// access since session-create).
	targetHubs := scope.HubIDs
	if args.HubID != "" {
		if !scope.IsHubAllowed(args.HubID) {
			return errResult(call, errHubNotInScope.Error())
		}
		targetHubs = []string{args.HubID}
	}

	// Build the strict-hub request. The service applies
	// `hub_id = ANY(HubIDs)` exactly — no owner-OR-hub broadening.
	// The wrapper's "narrow to hub_id=X" contract requires this:
	// user-owned memories in OTHER hubs must NOT leak into a
	// recall scoped to X.
	resp, err := h.service.AgentRecall(ctx, AgentRecallRequest{
		Query:   args.Query,
		OwnerID: scope.OwnerID,
		HubIDs:  targetHubs,
		Limit:   limit,
	})
	if err != nil {
		return errResult(call, fmt.Sprintf("recall: %v", err))
	}

	// Normalize nil-slice → empty-slice so json.Marshal emits "[]"
	// instead of "null". Some model parsers handle null + array
	// inconsistently; "[]" is the safer wire shape for "no matches."
	memories := resp.Memories
	if memories == nil {
		memories = []RecallResult{}
	}
	body, err := json.Marshal(memories)
	if err != nil {
		return errResult(call, fmt.Sprintf("encode result: %v", err))
	}

	return model.ToolResult{
		ToolUseID: call.Use.ID,
		Name:      call.Use.Name,
		Content:   string(body),
	}, nil
}

// errResult returns a tool error in the SDK's expected shape:
// IsError=true, Content holds a model-readable explanation.
func errResult(call sdktool.Call, msg string) (model.ToolResult, error) {
	return model.ToolResult{
		ToolUseID: call.Use.ID,
		Name:      call.Use.Name,
		Content:   msg,
		IsError:   true,
	}, nil
}

const recallToolDescription = `Search memories that are relevant to a natural-language query.

Use this when the user asks about past decisions, conversations, or context they've shared with memax. Returns up to 20 matches sorted by relevance.

Scope: the search runs across the session's allowed hubs by default. Pass hub_id to narrow to a single hub (e.g., when the user says "in my work hub specifically").`

// recallToolSchema is the JSON schema the model sees. Strict
// enough that the SDK rejects malformed inputs at the boundary
// rather than letting them reach our handler.
var recallToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"query": map[string]any{
			"type":        "string",
			"description": "Natural-language search query.",
			"minLength":   1,
		},
		"hub_id": map[string]any{
			"type":        "string",
			"description": "Optional. Narrow the search to a single hub. Must be a hub the session has access to. Omit to search across the full session scope.",
		},
		"limit": map[string]any{
			"type":        "integer",
			"description": "Optional. Max results (1-20). Default 8.",
			"minimum":     1,
			"maximum":     maxRecallLimit,
		},
	},
	// JSON-schema validators reject `[]string`; the schema must serialize
	// as a JSON array of strings, which Go's encoding produces from
	// `[]any` (or `[]string`, but the SDK compiles via a validator that
	// inspects the in-memory shape directly and rejects []string).
	"required": []any{"query"},
}
