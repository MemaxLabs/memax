//memax:lint-skip:tool-store
//
// Rationale for the skip directive: the
// scripts/check-tool-store-calls lint enforces the
// "OwnerID-without-HubIDs is fail-open" pattern that protects
// READ tools from falling back to owner-only store reads when
// scope.HubIDs is empty. forget_memory is a TARGETED-WRITE
// tool: the user supplies a memory_id, the resolver yields a
// SessionScope, and the service layer scope-checks the
// memory's hub against scope.HubIDs explicitly before any
// delete runs. The fail-open mode the lint guards doesn't
// apply. The security checks the lint cares about ARE
// present:
//
//   - Resolve is called before the service.ForgetMemory call.
//   - Empty scope.HubIDs is rejected: the service layer
//     returns ErrMemoryNotFound when the memory's hub isn't
//     in the scope's allowed set.
//   - The memory_id arg is validated by the service layer's
//     hub check (we don't trust the agent's claim).

// Phase 3.6b3e — forget_memory tool wrapper.
//
// The chat agent's second mutating tool. Plan 24 §"Tool registry"
// pins forget_memory as ApprovalRequired with PermMemoryDelete
// as the gating permission. Same approval flow as push_memory:
// the agent calls request_approval first; the user clicks
// approve; agentpolicy.ApprovalBeforeTool admits the
// subsequent forget_memory call bound to the canonical args
// (WithInputBoundApprovals).
//
// **Why a target-aware shape, not push-style hub picking.**
// push_memory lets the agent pick the destination hub; the
// scope check there is "is the chosen hub in scope?" For
// forget_memory the agent supplies a memory_id; the memory's
// hub is determined by the row, NOT chosen by the agent. The
// scope check is "is the memory's hub in scope?" — same
// principle, different shape. Codex's Phase 3.6b3d v4 review
// pinned this distinction; the per-tool required-permission
// map (push:memory:write, forget:memory:delete) handles the
// authorization difference, but the runtime path needed its
// own implementation that resolves the hub from the memory.
//
// **Delete semantics.** Hard delete — store.DeleteMemory /
// DeleteHubMemory issue `DELETE FROM memories` and the row is
// gone. Audit lives in chat_tool_approvals (args + decided_at +
// decided_by persist after the row is removed). The
// hub.memories.changed SSE event payload carries
// state="deleted" as a wire discriminator so the frontend can
// remove the entry from caches; that is an event-payload
// state, not a memory-row state. The chat agent's
// forget_memory is identical to the REST DELETE
// /v1/memories/{id} surface in semantics; the differences are
// (a) approval gate, (b) per-call scope + role check applied
// inside the service layer.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ForgottenTurnTracker remembers which memory IDs the agent has
// successfully forgotten during the current turn. It is the
// turn-local idempotency layer that turns the model-retries-after-
// success failure mode (described in the package comment below) into
// a clean second-call success.
//
// One tracker per turn. Concurrency: tool calls inside a single turn
// can run sequentially or, in theory, in parallel from a
// streaming-tool-use future; the sync.Mutex keeps both shapes safe
// without forcing callers to think about it. Zero value is usable —
// nil-receiver methods short-circuit to "not yet forgotten" so the
// wrapper still works when no tracker is plumbed (e.g., legacy
// tests).
type ForgottenTurnTracker struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

// Forgotten reports whether memoryID was successfully forgotten
// earlier in this turn. Nil-safe.
func (t *ForgottenTurnTracker) Forgotten(memoryID string) bool {
	if t == nil || memoryID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.ids[memoryID]
	return ok
}

// MarkForgotten records a successful forget. Nil-safe; empty IDs are
// ignored.
func (t *ForgottenTurnTracker) MarkForgotten(memoryID string) {
	if t == nil || memoryID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ids == nil {
		t.ids = make(map[string]struct{}, 1)
	}
	t.ids[memoryID] = struct{}{}
}

// ForgetService is the production-side dependency the forget
// tool uses to scope-check + delete a memory. Implementations
// live in workerapp.
//
// The service receives the per-turn SessionScope (resolved by
// the tool wrapper) so it can reject memory_ids whose hub is
// outside the agent's reach. This narrowing is a SECURITY
// boundary: the agent cannot forget a memory it cannot see.
//
// Returns ErrForgetMemoryNotFound when the memory doesn't
// exist OR is outside the session's scope (we don't
// distinguish — same fail-closed shape the REST + MCP delete
// paths use to avoid leaking memory-existence across hubs).
type ForgetService interface {
	ForgetMemory(ctx context.Context, in ForgetMemoryRequest) (ForgetOutcome, error)
}

// ForgetMemoryRequest carries everything the service needs to
// resolve + authorize + delete the targeted memory.
type ForgetMemoryRequest struct {
	OwnerID       string
	MemoryID      string
	AllowedHubIDs []string // scope-resolved at the tool wrapper
	Reason        string   // optional; carried for audit only
}

// ForgetOutcome carries the post-delete disposition. Status
// is "forgotten" on success. Future non-success states
// (e.g., "already_forgotten" if a concurrent call won the
// race) extend this without breaking agents that only branch
// on "forgotten".
type ForgetOutcome struct {
	Status string
	HubID  string
	Title  string
}

// PushStatus constants are reused conceptually, but to keep
// the wire vocabulary explicit forget_memory has its own:
const (
	ForgetStatusForgotten = "forgotten"
	// ForgetStatusAlreadyForgotten is returned when the same
	// memory_id was successfully forgotten earlier in this turn.
	// Terminal success — agents should NOT retry. Distinct from
	// "forgotten" so the model's narrative ("I just deleted X" vs
	// "X was already gone from my last action") stays honest, and
	// so observability / audit can tell the two apart.
	ForgetStatusAlreadyForgotten = "already_forgotten"
)

// ErrForgetMemoryNotFound is the sentinel the service returns
// when the memory_id is unknown OR outside scope. Same surface
// either way — the SECURITY contract is "agent can't probe
// existence across the scope boundary".
var ErrForgetMemoryNotFound = errors.New("memory not found in this session's scope")

// ErrForgetMemoryNoPermission is the sentinel for "memory
// exists in scope but the user lacks delete permission on its
// hub". Distinct from NotFound so the agent can give a
// meaningful error (vs blanket "not found" which conflates
// missing + forbidden).
var ErrForgetMemoryNoPermission = errors.New("forget requires memory:delete on the memory's hub")

// ForgetToolConfig wires the per-turn forget_memory tool to
// the runtime's ForgetService + the resolver + the session
// descriptor.
type ForgetToolConfig struct {
	Service  ForgetService
	Resolver *agent.ScopeResolver
	Session  agent.SessionDescriptor

	// Tracker is the per-turn already-forgotten map. Optional;
	// when nil the wrapper still works but loses the
	// "model-retries-after-success returns already_forgotten"
	// idempotency described at the top of this file. Production
	// chat turns always plumb a tracker so the retry pattern
	// looks like a success to the model and the loop terminates.
	Tracker *ForgottenTurnTracker
}

// ForgetArgs is the wire shape for forget_memory.
type ForgetArgs struct {
	// MemoryID is the id of the memory to forget. Required.
	MemoryID string `json:"memory_id"`

	// Reason is an optional short string carried in the
	// chat_message_events approval prompt + audit log. Helps
	// the user understand why the agent wants to forget the
	// memory ("user asked to forget the canceled launch
	// notes").
	Reason string `json:"reason,omitempty"`
}

// ForgetResult is the wire shape returned to the agent on
// success.
type ForgetResult struct {
	MemoryID string `json:"memory_id"`
	HubID    string `json:"hub_id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
}

var (
	errForgetEmptyMemoryID = errors.New("memory_id is required")
)

// NewForgetTool returns the SDK-side forget_memory tool.
func NewForgetTool(cfg ForgetToolConfig) (sdktool.Tool, error) {
	if cfg.Service == nil {
		return nil, errors.New("tools: ForgetToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("tools: ForgetToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return nil, fmt.Errorf("tools: ForgetToolConfig.Session: %w", err)
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:        "forget_memory",
			Description: "Permanently delete a memory by id. Each invocation requires the user's explicit approval via request_approval first; the approval must include the same memory_id the agent will pass to forget_memory.",
			SearchHint:  "delete forget remove erase memory",
			InputSchema: map[string]any{
				"type":                 "object",
				"required":             []any{"memory_id"},
				"additionalProperties": false,
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "UUID of the memory to forget. Must be a memory the agent can see (via list_memories / recall_memories results).",
						"minLength":   1,
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Optional short reason (carried in the audit log and the user's approval prompt).",
					},
				},
			},
			MaxResultBytes: 1024,
		},
		Handler: (&forgetHandler{
			service:  cfg.Service,
			resolver: cfg.Resolver,
			session:  cfg.Session,
			tracker:  cfg.Tracker,
		}).execute,
	}, nil
}

type forgetHandler struct {
	service  ForgetService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
	tracker  *ForgottenTurnTracker
}

func (h *forgetHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[ForgetArgs](call.Use)
	if err != nil {
		return errForgetResult(call, fmt.Sprintf("decode args: %v", err))
	}
	memoryID := strings.TrimSpace(args.MemoryID)
	if memoryID == "" {
		return errForgetResult(call, errForgetEmptyMemoryID.Error())
	}

	// Turn-local idempotency short-circuit. If the model already
	// successfully forgot this exact memory_id earlier in the same
	// turn (typical pattern: tool_result didn't propagate cleanly
	// and the model re-emitted the same forget_memory call), skip
	// the service round-trip and return the already_forgotten
	// success sentinel. This breaks the historical loop where the
	// second call hit ErrForgetMemoryNotFound, got narrated as a
	// failure, and triggered yet another retry.
	if h.tracker.Forgotten(memoryID) {
		return okForgetResult(memoryID, "", "", ForgetStatusAlreadyForgotten)
	}

	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return errForgetResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	// Defense-in-depth: an empty scope.HubIDs would let the
	// service layer fall back to owner-only reads if the lint
	// directive at the file head is ever weakened. Catch it
	// here.
	if len(scope.HubIDs) == 0 {
		return errForgetResult(call, "session has no accessible hubs; cannot forget any memory")
	}
	// Note on WritableHubIDs (Phase 3.7 step 1): forget_memory
	// does NOT use scope.IsHubWritable as a wrapper-level
	// fast-fail because forget has an owner-bypass exception
	// — a user who's a VIEWER on hub X but who AUTHORED memory
	// M in X can still forget M (it's their own memory). The
	// wrapper doesn't know who owns M without a store lookup,
	// so the precise authorization happens in the service via
	// canDeleteMemoryRoleNonOwner (non-owner-branch role
	// check) + ContributorDeletePolicy. push_memory and
	// propose_topic_merge use IsHubWritable because they
	// don't have an owner-bypass — they're hub-write
	// operations.

	outcome, err := h.service.ForgetMemory(ctx, ForgetMemoryRequest{
		OwnerID:       scope.OwnerID,
		MemoryID:      memoryID,
		AllowedHubIDs: append([]string(nil), scope.HubIDs...),
		Reason:        strings.TrimSpace(args.Reason),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrForgetMemoryNotFound):
			// NotFound on the FIRST call for this id is a genuine
			// signal (wrong id, or memory belongs to another
			// scope). We never reach this branch on a retried id
			// because the tracker short-circuit above caught it.
			// Preserving the surfaced error here keeps the "no
			// cross-scope existence probing" security shape that
			// the REST + MCP delete paths use.
			return errForgetResult(call, "memory not found in this session's scope")
		case errors.Is(err, ErrForgetMemoryNoPermission):
			return errForgetResult(call, "you do not have permission to forget this memory")
		default:
			return errForgetResult(call, fmt.Sprintf("forget: %v", err))
		}
	}
	status := outcome.Status
	if status == "" {
		// Defensive default — services that forget to set a
		// status fall back to the conservative answer.
		status = ForgetStatusForgotten
	}
	// Record the successful forget into the per-turn tracker so a
	// model-driven retry returns already_forgotten instead of
	// flowing through the service again and tripping NotFound.
	h.tracker.MarkForgotten(memoryID)

	return okForgetResult(memoryID, outcome.HubID, outcome.Title, status)
}

// okForgetResult formats the success-shaped ToolResult. Centralised
// so the in-tracker short-circuit and the post-service success path
// emit identical wire shapes (only the status differs), which keeps
// downstream consumers — the chat UI, audit log, and the agent's
// own narration — simple.
func okForgetResult(memoryID, hubID, title, status string) (sdkmodel.ToolResult, error) {
	body, _ := json.Marshal(ForgetResult{
		MemoryID: memoryID,
		HubID:    hubID,
		Title:    title,
		Status:   status,
	})
	return sdkmodel.ToolResult{Content: string(body)}, nil
}

// errForgetResult shapes an SDK ToolResult marked as an error.
func errForgetResult(call sdktool.Call, msg string) (sdkmodel.ToolResult, error) {
	body, _ := json.Marshal(map[string]any{
		"error":   msg,
		"tool":    call.Use.Name,
		"call_id": call.Use.ID,
	})
	return sdkmodel.ToolResult{
		Content: string(body),
		IsError: true,
	}, nil
}

// _ = model is to keep the model import live in case future
// fields need it; remove when the service starts returning a
// model.Memory in the outcome.
var _ = model.MemoryKindSemantic
