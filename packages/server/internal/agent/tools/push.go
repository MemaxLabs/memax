//memax:lint-skip:tool-store
//
// Rationale for the skip directive: the
// scripts/check-tool-store-calls lint enforces the
// "OwnerID-without-HubIDs is fail-open" pattern that protects
// READ tools from falling back to owner-only store reads when
// scope.HubIDs is empty. push_memory is a WRITE tool: the sink
// is a *model.Memory with a SINGLE HubID (not a HubIDs list);
// the fail-open mode the lint guards doesn't apply. The
// security checks the lint cares about ARE present:
//
//   - Resolve is called before the service.CreateAndProcess
//     call.
//   - Empty scope.HubIDs is rejected: pickPushHub returns
//     errPushNoHub when no writable single-hub fallback
//     exists and no caller-supplied hub_id was provided.
//   - Caller-supplied hub_id is validated via TWO checks:
//     scope.IsHubAllowed (in scope at all) and
//     scope.IsHubWritable (Phase 3.7 step 1 — user has a
//     non-viewer role). Distinct error messages for each.
//
// Phase 3.6b3d — push_memory tool wrapper.
//
// The chat agent's first mutating tool. Plan 24 §"Tool registry"
// pins push_memory as ApprovalRequired: every invocation must
// be preceded by a successful request_approval grant
// (agentpolicy.ApprovalBeforeTool gates the call), and the
// approval must include the canonical tool_input
// (WithInputBoundApprovals) so a "yes" doesn't authorize a
// different push.
//
// **Why a tool wrapper, not direct store access.** The chat
// agent's session has a SessionDescriptor that the resolver
// turns into a SessionScope (the live, intersected hub set
// the agent is allowed to touch). The push tool MUST validate
// the requested hub_id against that scope at call time —
// blindly trusting the agent's args would break the
// owner-scope contract. Centralizing the check + the memory
// build + the dispatch into a single tool wrapper keeps the
// boundary explicit.
//
// **Provenance.** The host always sets:
//
//   - source = "chat_agent" (the intake surface)
//   - source_agent = "chat-agent" (a stable actor SLUG, not a
//     per-session id; display joins and agent aggregation
//     query on this string and expect a stable value)
//   - InitiationType = MemoryInitiationAgentProactive (the
//     agent decided to push, on the user's approval — vs
//     MemoryInitiationHumanDirect for a direct REST push)
//   - CreatedByType = MemoryCreatedByAgent
//   - AttributionSource = MemoryAttributionSourceAuth
//
// Per-session identity (chat session id, message id) is NOT
// stuffed into source_agent — codex's Phase 3.6b3d v2 review
// caught the over-loading risk. If we need session-level
// audit later, add a separate provenance field; don't
// repurpose source_agent.

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/sanitize"
)

// PushOutcome carries the post-write disposition. Phase 3.6b3d
// v2 codex review caught that returning a flat "saved" when
// background processing failed to enqueue was misleading —
// the memory was durable but never chunked/embedded, so
// recall couldn't find it. The two-state outcome lets the
// agent surface "saved, but search indexing is delayed" to
// the user.
type PushOutcome struct {
	Status string // "saved" | "saved_unprocessed"
}

const (
	// PushStatusSaved means the memory committed AND
	// background processing was successfully enqueued.
	// Recall should find it within a few seconds (chunking
	// + embedding latency).
	PushStatusSaved = "saved"

	// PushStatusSavedUnprocessed means the memory committed
	// but the background-processing enqueue failed (e.g.,
	// River insert error). The row is durable; a manual
	// reprocess or a future periodic sweeper picks it up.
	// Codex's Phase 3.6b3d v2 review flagged that returning
	// just "saved" hid this state.
	PushStatusSavedUnprocessed = "saved_unprocessed"
)

// PushService is the production-side dependency the push tool
// uses to commit a new memory + enqueue background processing.
// Implementations live in workerapp (CreateAndProcess wraps a
// store.CreateMemory call + a queue.MemoryProcessArgs insert
// via the worker's River client).
//
// The interface is intentionally small — the tool builds the
// fully-formed *model.Memory and a model.PushRequest envelope;
// the service runs the write-permission gate, persists, and
// enqueues. This keeps store / queue / role-checking
// dependencies out of the tool package.
//
// Returning a PushOutcome alongside error lets the service
// signal partial-success states (saved_unprocessed) without
// using the err channel.
type PushService interface {
	CreateAndProcess(ctx context.Context, m *model.Memory, req model.PushRequest) (PushOutcome, error)
}

// PushToolConfig wires the per-turn push_memory tool to the
// runtime's PushService + the resolver + the session
// descriptor. Same shape as RecallToolConfig.
type PushToolConfig struct {
	Service  PushService
	Resolver *agent.ScopeResolver
	Session  agent.SessionDescriptor
}

var (
	errPushEmptyContent = errors.New("content is required")
	errPushNoHub        = errors.New("hub_id is required when the session has multiple hubs and no default write hub")
)

// PushArgs is the wire shape for push_memory.
type PushArgs struct {
	// Content is the memory body. Required; trimmed; empty
	// rejected.
	Content string `json:"content"`

	// Title is an optional short title. When empty the
	// background processor's title resolver fills it; the tool
	// passes the empty title through unchanged so the
	// processor's heuristic owns the contract.
	Title string `json:"title,omitempty"`

	// HubID picks the destination hub. Optional: empty
	// resolves via the session's WriteHubID, then by being
	// the only hub in scope; sessions with multiple hubs and
	// no default write hub MUST set this explicitly.
	HubID string `json:"hub_id,omitempty"`

	// Tags is an optional list of tags applied at create-time.
	// The background processor may add more from the content.
	Tags []string `json:"tags,omitempty"`

	// Hint is an optional retrieval-time hint string the user
	// (or here the agent) writes to bias future recall toward
	// this memory in particular contexts.
	Hint string `json:"hint,omitempty"`
}

// PushResult is the wire shape returned to the agent on
// success. Fields are stable identifiers; the agent may quote
// the memory_id back to the user or chain a recall on the
// new id.
type PushResult struct {
	MemoryID string `json:"memory_id"`
	HubID    string `json:"hub_id"`
	Title    string `json:"title"`
	// Status carries the post-write disposition. Always
	// "saved" today; future states (e.g., "queued" if we add
	// a deferred-write mode) extend this without breaking
	// agents that only branch on "saved".
	Status string `json:"status"`
}

// NewPushTool returns the SDK-side push_memory tool wrapper.
func NewPushTool(cfg PushToolConfig) (sdktool.Tool, error) {
	if cfg.Service == nil {
		return nil, errors.New("tools: PushToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("tools: PushToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return nil, fmt.Errorf("tools: PushToolConfig.Session: %w", err)
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:        "push_memory",
			Description: "Save a new memory in one of the user's hubs. Each invocation requires the user's explicit approval via request_approval first; the approval must include the same tool_input the agent will pass to push_memory.",
			SearchHint:  "save remember capture record memory note",
			InputSchema: map[string]any{
				"type":                 "object",
				"required":             []any{"content"},
				"additionalProperties": false,
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The memory body. Markdown-friendly; trimmed.",
						"minLength":   1,
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Optional short title. Empty defers to the system's title resolver.",
					},
					"hub_id": map[string]any{
						"type":        "string",
						"description": "Destination hub UUID. Required when the session has multiple hubs and no default write hub.",
					},
					"tags": map[string]any{
						"type":        "array",
						"description": "Optional tags applied at create-time.",
						"items":       map[string]any{"type": "string"},
					},
					"hint": map[string]any{
						"type":        "string",
						"description": "Optional retrieval-time hint biasing recall toward this memory.",
					},
				},
			},
			// MaxResultBytes — push results are tiny (memory_id +
			// hub_id + title + status). 1KB is generous.
			MaxResultBytes: 1024,
		},
		Handler: (&pushHandler{
			service:  cfg.Service,
			resolver: cfg.Resolver,
			session:  cfg.Session,
		}).execute,
	}, nil
}

type pushHandler struct {
	service  PushService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

func (h *pushHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[PushArgs](call.Use)
	if err != nil {
		return errPushResult(call, fmt.Sprintf("decode args: %v", err))
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return errPushResult(call, errPushEmptyContent.Error())
	}

	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return errPushResult(call, fmt.Sprintf("scope resolve: %v", err))
	}

	hubID, err := pickPushHub(scope, args.HubID)
	if err != nil {
		return errPushResult(call, err.Error())
	}

	now := time.Now().UTC()
	tags := append([]string(nil), args.Tags...)
	if tags == nil {
		tags = []string{}
	}

	title := strings.TrimSpace(args.Title)
	hint := strings.TrimSpace(args.Hint)
	// PlainText sanitization for title + hint matches what the
	// REST + MCP push paths apply at the same boundary. Content
	// passes through unchanged because the body is markdown
	// and the chunker downstream owns its own normalization.
	if title != "" {
		title = sanitize.PlainText(title)
	}
	if hint != "" {
		hint = sanitize.PlainText(hint)
	}

	memoryID := uuid.NewString()
	provenance := &model.MemoryProvenance{
		CreatedByType:        model.MemoryCreatedByAgent,
		CreatedBySlug:        "chat-agent",
		CreatedByDisplayName: "Chat Agent",
		CreatedVia:           "chat",
		InitiationType:       model.MemoryInitiationAgentProactive,
		AttributionSource:    model.MemoryAttributionSourceAuth,
	}
	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        scope.OwnerID,
		Title:                          title,
		Content:                        content,
		ContentType:                    "markdown",
		ContentHash:                    hashContentSHA256(content),
		Hint:                           hint,
		Kind:                           model.MemoryKindSemantic,
		Stability:                      model.MemoryStabilityEvolving,
		RetrievalWeight:                1.0,
		Tags:                           tags,
		Boundary:                       "private",
		State:                          "active",
		Source:                         "chat_agent",
		SourceAgent:                    "chat-agent",
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

	req := model.PushRequest{
		Content:        content,
		Title:          title,
		Hint:           hint,
		Tags:           tags,
		Kind:           model.MemoryKindSemantic,
		Stability:      model.MemoryStabilityEvolving,
		ContentType:    "markdown",
		Source:         "chat_agent",
		SourceAgent:    "chat-agent",
		InitiationType: model.MemoryInitiationAgentProactive,
	}

	outcome, err := h.service.CreateAndProcess(ctx, memory, req)
	if err != nil {
		return errPushResult(call, fmt.Sprintf("save: %v", err))
	}
	status := outcome.Status
	if status == "" {
		// Defensive default — services that forget to set a
		// status fall back to the conservative answer rather
		// than implicitly claiming saved+processed.
		status = PushStatusSavedUnprocessed
	}

	body, _ := json.Marshal(PushResult{
		MemoryID: memoryID,
		HubID:    hubID,
		Title:    title,
		Status:   status,
	})
	return sdkmodel.ToolResult{
		Content: string(body),
	}, nil
}

// pickPushHub chooses the destination hub for the new memory.
//
// Priority:
//  1. Explicit args.HubID — must be in scope.HubIDs.
//  2. Session's WriteHubID — must still be in scope (live
//     membership may have removed the user from the pinned
//     write hub since session-create time).
//  3. The single hub in scope (when scope.HubIDs has length 1).
//  4. Otherwise: error — multi-hub session must specify.
//
// **Why not fall back to scope.HubIDs[0] for multi-hub.**
// Picking arbitrarily would make hub_id a silent hidden
// argument; the agent would push to a hub the user didn't
// expect. Forcing the agent to specify keeps the audit
// trail honest.
// Phase 3.7 step 1 added the IsHubWritable check alongside
// IsHubAllowed. push_memory is a MUTATING tool; "in scope"
// (read-eligible) isn't enough — the user must also have a
// non-viewer role on the hub. Distinct error messages let the
// agent give the user a clearer answer:
//
//   - "not in scope": the hub isn't in the session's hub set
//     at all (chat session was created over [A, B] and the
//     agent picked C). User has no relationship to the hub.
//   - "read-only": the hub IS in scope but the user is a
//     viewer there. They can recall from it but not push to
//     it. Agent should suggest a different hub.
//
// The chatPushAdapter's per-call GetHubMemberRole check stays
// as the freshness layer — membership can change between
// scope-resolve and tool execution.
func pickPushHub(scope agent.SessionScope, requestedHubID string) (string, error) {
	requested := strings.TrimSpace(requestedHubID)
	if requested != "" {
		if !scope.IsHubAllowed(requested) {
			return "", fmt.Errorf("hub_id %q is not in this session's scope", requested)
		}
		if !scope.IsHubWritable(requested) {
			return "", fmt.Errorf("hub_id %q is read-only in this session (you don't have write permission)", requested)
		}
		return requested, nil
	}
	if scope.WriteHubID != "" && scope.IsHubWritable(scope.WriteHubID) {
		return scope.WriteHubID, nil
	}
	// When there's exactly one hub in scope, fall back to it
	// only if it's writable. Otherwise the agent must pick
	// explicitly (or the user reconfigures their session).
	if len(scope.HubIDs) == 1 && scope.IsHubWritable(scope.HubIDs[0]) {
		return scope.HubIDs[0], nil
	}
	return "", errPushNoHub
}

// hashContentSHA256 mirrors handler.hashContentBytes so the
// chat agent's pushed memories carry the same content_hash
// scheme as REST/MCP pushes — the dedupe + provenance hash
// chain stays uniform across intake routes.
func hashContentSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// errPushResult shapes an SDK ToolResult marked as an error.
// The agent sees this as a tool failure and can recover (e.g.,
// adjust the args and retry, or surface to the user).
func errPushResult(call sdktool.Call, msg string) (sdkmodel.ToolResult, error) {
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
