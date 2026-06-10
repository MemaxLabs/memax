//memax:lint-skip:tool-store
//
// Rationale for the skip directive: the
// scripts/check-tool-store-calls lint enforces the
// "OwnerID-without-HubIDs is fail-open" pattern that protects
// READ tools from falling back to owner-only store reads when
// scope.HubIDs is empty. propose_topic_merge is a TARGETED-WRITE
// tool: the user supplies hub_id + target_topic_id +
// source_topic_ids, the wrapper scope-checks hub_id with TWO
// separate checks BEFORE the service call, and the service
// layer does its own (id, hub_id) lookup that rejects
// out-of-hub topics. The fail-open mode the lint guards
// (OwnerID set without a HubIDs slice → store reads owner-only)
// doesn't apply — we operate on EXACTLY one hub, not an
// allow-set, and the request struct uses a single HubID field
// rather than a HubIDs slice. The security checks the lint
// cares about ARE present:
//
//   - Resolve is called before the service.ProposeTopicMerge call.
//   - Empty scope.HubIDs is rejected: the wrapper returns
//     IsError when len(scope.HubIDs) == 0.
//   - The hub_id arg is validated via scope.IsHubAllowed
//     (in scope at all) AND scope.IsHubWritable (Phase 3.7
//     step 1 — user has a non-viewer role on the hub).
//   - The target_topic_id and each source_topic_id are
//     resolved via store.GetTopic(id, hub_id) inside the
//     service — cross-hub topics fail this lookup.
//
// Same posture as forget.go for the lint-skip side, but
// forget DOESN'T use IsHubWritable because forget has an
// owner-bypass exception (viewer can forget memory they
// authored). propose_topic_merge has no equivalent bypass
// — it's a hub-write operation either way.

// Phase 3.6b3f — propose_topic_merge tool wrapper.
//
// The chat agent's third (and last) approval-gated mutating
// tool. Architecturally distinct from push/forget: this tool
// does NOT mutate the topic graph. It writes a pending
// `review_topic_merge` notification — same shape the dream
// engine produces for topic-merge suggestions — that lands in
// the user's notifications inbox. The actual MergeTopics call
// happens later when the user resolves the notification, from
// the notifications-resolve API. So the approval flow has TWO
// stages: chat-side approval gates the PROPOSAL (the agent
// wants to add a suggestion to your inbox); inbox resolution
// gates the MERGE (the user picks "apply" or "dismiss").
//
// **Why notification-native, not direct mutation.** Plan 24
// §"Tool table" pins this surface: "Writes to topic-merge
// review notifications, not directly to topics." Three
// reasons:
//
//   1. The dream engine already produces the same notification
//      kind. A chat-proposed merge that dedupes against an
//      existing dream-proposed merge surfaces ONE inbox row
//      (the global UNIQUE constraint on
//      (source_kind, source_id) collapses duplicates). The
//      user's inbox stays clean.
//   2. Chat approvals confirm intent ("yes, surface this to
//      me"); merge-resolution confirms action ("yes, do it").
//      Conflating them removes the safety the user should
//      have when they later actually apply the merge.
//   3. The merge mutation has its own permission model + audit
//      surface in the notifications-resolve handler. Bypassing
//      it from chat would fork the audit trail.
//
// **Stable source key for dedup.** source_id is built from a
// SORTED list of source-topic-ids, so:
//   - Repeated chat proposals of the same merge upsert one row.
//   - A chat proposal of a merge dreams already proposed
//     resolves to the same partial-unique-index slot — the
//     CreateNotificationIfAbsent path returns
//     status="already_pending" and we suppress the
//     `notification.created` event, matching the dream
//     engine's semantics.
//
// **Authorization.** Owner and admin can always propose.
// Contributors can propose ONLY when the hub has
// `allow_contributor_topics = true` (the same flag that gates
// contributor-driven topic creation/edit on REST). Viewers
// cannot propose. Non-members cannot propose. Codex's Phase
// 3.6b3e v3 review pinned the exact contributor branch test
// matrix to lock in this rule.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ProposeTopicMergeService is the production-side dependency
// the tool calls into. workerapp wires the implementation.
//
// Returns a structured outcome on success and one of the typed
// sentinels below on a clean failure (not-found, no-permission,
// invalid-args). Other errors propagate up as-is for the SDK
// to surface as tool errors.
type ProposeTopicMergeService interface {
	ProposeTopicMerge(ctx context.Context, in ProposeTopicMergeRequest) (ProposeTopicMergeOutcome, error)
}

// ProposeTopicMergeRequest carries everything the service needs
// to validate scope + role + topic existence and to upsert the
// notification.
type ProposeTopicMergeRequest struct {
	OwnerID        string
	HubID          string
	TargetTopicID  string
	SourceTopicIDs []string // de-duped + sorted by the wrapper before dispatch
	Reason         string
}

// ProposeTopicMergeOutcome carries the post-upsert disposition.
// Status = "proposed" when the row was freshly inserted;
// "already_pending" when an existing pending row matches the
// stable source key. Either way, NotificationID identifies the
// row the user will see in their inbox.
type ProposeTopicMergeOutcome struct {
	Status         string
	NotificationID string
	TargetTitle    string
	SourceTitles   []string
}

// Wire status values for ProposeTopicMergeResult.Status.
//
// "proposed"            — fresh row written; the user will see a
//                         new notification in their inbox.
// "already_pending"     — an existing PENDING row keyed on the
//                         same (source_kind, source_id) was
//                         found; the proposal is already in the
//                         user's inbox waiting on them.
// "already_merged"      — the user previously RESOLVED this
//                         exact merge by applying it (the topic
//                         graph already reflects it).
// "already_kept_separate"
//                       — the user previously RESOLVED this
//                         merge by keeping the topics separate.
//                         Respect the prior decision; the agent
//                         can offer to revisit with different
//                         framing.
// "already_dismissed"   — the user previously dismissed this
//                         proposal. Covers both
//                         (status=resolved + resolution=dismissed)
//                         AND the rare receipt-style
//                         status=dismissed path.
// "already_expired"     — the prior proposal hit its expires_at
//                         without resolution.
// "already_resolved"    — fall-through for an existing resolved
//                         row whose resolution doesn't match any
//                         of the topic-merge resolutions above.
//                         Defensive: if the resolution enum
//                         grows without an update here, the
//                         agent still gets a meaningful answer
//                         instead of "already_pending" (which
//                         would lie).
//
// Codex Phase 3.6b3f v2 [Medium] flagged that the v1 mapping
// flattened all resolved rows into "already_resolved"
// regardless of resolution, but the user-facing semantics of
// merged / kept_separate / dismissed are different — the agent
// needs to phrase its response differently for each.
const (
	ProposeStatusProposed            = "proposed"
	ProposeStatusAlreadyPending      = "already_pending"
	ProposeStatusAlreadyMerged       = "already_merged"
	ProposeStatusAlreadyKeptSeparate = "already_kept_separate"
	ProposeStatusAlreadyDismissed    = "already_dismissed"
	ProposeStatusAlreadyExpired      = "already_expired"
	ProposeStatusAlreadyResolved     = "already_resolved"
)

// ErrProposeTopicMergeNotFound — target or one of the sources
// either doesn't exist or doesn't belong to the requested hub.
// Same fail-closed shape forget_memory uses (cross-hub probes
// fold into not-found so the agent can't enumerate).
var ErrProposeTopicMergeNotFound = errors.New("topic not found in this hub")

// ErrProposeTopicMergeNoPermission — caller lacks the role +
// hub-policy combination required to propose merges in this
// hub. Distinct from NotFound so the agent can give a useful
// error.
var ErrProposeTopicMergeNoPermission = errors.New("propose_topic_merge requires owner/admin role, or contributor with allow_contributor_topics")

// ErrProposeTopicMergeInvalidArgs — caller-provided argument
// shape is wrong (target appears in sources, empty source list
// after dedup, target == any source). Distinct from a
// schema-decode failure so the agent gets a meaningful error
// without re-running the tool.
var ErrProposeTopicMergeInvalidArgs = errors.New("invalid topic merge arguments")

// ProposeTopicMergeToolConfig wires the per-turn tool to the
// runtime's service + resolver + session descriptor.
type ProposeTopicMergeToolConfig struct {
	Service  ProposeTopicMergeService
	Resolver *agent.ScopeResolver
	Session  agent.SessionDescriptor
}

// ProposeTopicMergeArgs is the wire shape the agent sends.
type ProposeTopicMergeArgs struct {
	HubID          string   `json:"hub_id"`
	TargetTopicID  string   `json:"target_topic_id"`
	SourceTopicIDs []string `json:"source_topic_ids"`
	Reason         string   `json:"reason,omitempty"`
}

// ProposeTopicMergeResult is the wire shape returned to the agent.
type ProposeTopicMergeResult struct {
	NotificationID string   `json:"notification_id"`
	Status         string   `json:"status"`
	HubID          string   `json:"hub_id"`
	TargetTopicID  string   `json:"target_topic_id"`
	TargetTitle    string   `json:"target_title,omitempty"`
	SourceTopicIDs []string `json:"source_topic_ids"`
	SourceTitles   []string `json:"source_titles,omitempty"`
}

var (
	errProposeEmptyHub        = errors.New("hub_id is required")
	errProposeEmptyTarget     = errors.New("target_topic_id is required")
	errProposeEmptySources    = errors.New("source_topic_ids must contain at least one id")
	errProposeTargetInSources = errors.New("target_topic_id must not appear in source_topic_ids")
)

// NewProposeTopicMergeTool returns the SDK-side tool. Mirrors
// the construction shape of NewPushTool / NewForgetTool so the
// chatToolBuilders registry can stay symmetric.
func NewProposeTopicMergeTool(cfg ProposeTopicMergeToolConfig) (sdktool.Tool, error) {
	if cfg.Service == nil {
		return nil, errors.New("tools: ProposeTopicMergeToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("tools: ProposeTopicMergeToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return nil, fmt.Errorf("tools: ProposeTopicMergeToolConfig.Session: %w", err)
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:        "propose_topic_merge",
			Description: "Propose merging one or more source topics into a target topic. This writes a pending suggestion to the user's notifications inbox; the user resolves the notification to actually apply the merge. Each invocation requires the user's explicit approval via request_approval first; the approval must include the same hub_id, target_topic_id, and source_topic_ids the agent will pass to propose_topic_merge.",
			SearchHint:  "topic merge consolidate combine reorganize",
			InputSchema: map[string]any{
				"type":                 "object",
				"required":             []any{"hub_id", "target_topic_id", "source_topic_ids"},
				"additionalProperties": false,
				"properties": map[string]any{
					"hub_id": map[string]any{
						"type":        "string",
						"description": "UUID of the hub the topics belong to. Must be in the agent's session scope.",
						"minLength":   1,
					},
					"target_topic_id": map[string]any{
						"type":        "string",
						"description": "UUID of the topic that will absorb the source topics' memberships. Must exist in the given hub.",
						"minLength":   1,
					},
					"source_topic_ids": map[string]any{
						"type":        "array",
						"description": "UUIDs of the topics to merge into the target. Must each exist in the given hub and must NOT include target_topic_id.",
						"minItems":    1,
						"items": map[string]any{
							"type":      "string",
							"minLength": 1,
						},
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Optional short reason (carried in the notification payload and the user's approval prompt).",
					},
				},
			},
			MaxResultBytes: 2048,
		},
		Handler: (&proposeTopicMergeHandler{
			service:  cfg.Service,
			resolver: cfg.Resolver,
			session:  cfg.Session,
		}).execute,
	}, nil
}

type proposeTopicMergeHandler struct {
	service  ProposeTopicMergeService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

func (h *proposeTopicMergeHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[ProposeTopicMergeArgs](call.Use)
	if err != nil {
		return errProposeResult(call, fmt.Sprintf("decode args: %v", err))
	}
	hubID := strings.TrimSpace(args.HubID)
	if hubID == "" {
		return errProposeResult(call, errProposeEmptyHub.Error())
	}
	targetID := strings.TrimSpace(args.TargetTopicID)
	if targetID == "" {
		return errProposeResult(call, errProposeEmptyTarget.Error())
	}
	if len(args.SourceTopicIDs) == 0 {
		return errProposeResult(call, errProposeEmptySources.Error())
	}

	// Normalize source ids: trim, drop empties, dedup, sort.
	// Sorting is a security/correctness invariant — the source
	// key fed into the notification's source_id depends on a
	// stable order, so two calls with the same set in different
	// orders dedupe to the same row. Codex Phase 3.6b3e v3
	// flagged this as a required test surface.
	sources := canonicalizeSourceTopicIDs(args.SourceTopicIDs)
	if len(sources) == 0 {
		return errProposeResult(call, errProposeEmptySources.Error())
	}
	for _, s := range sources {
		if s == targetID {
			return errProposeResult(call, errProposeTargetInSources.Error())
		}
	}

	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return errProposeResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if len(scope.HubIDs) == 0 {
		return errProposeResult(call, "session has no accessible hubs; cannot propose topic merges")
	}
	if !scope.IsHubAllowed(hubID) {
		// Cross-scope hub. Same fail-closed shape forget_memory
		// uses for cross-scope memory probes.
		return errProposeResult(call, "hub_id is not in this session's scope")
	}
	// Phase 3.7 step 1: also reject if the hub is in scope
	// but the user is a viewer there. propose_topic_merge is
	// a hub-write operation (it creates a review notification
	// scoped to the hub) so a viewer can't propose. The
	// service still does the per-call role check + the
	// AllowContributorTopics gate; this is the wrapper-level
	// fast-fail so the agent gets a meaningful error without
	// a service round-trip.
	if !scope.IsHubWritable(hubID) {
		return errProposeResult(call, "hub_id is read-only in this session (you don't have write permission to propose topic merges there)")
	}

	outcome, err := h.service.ProposeTopicMerge(ctx, ProposeTopicMergeRequest{
		OwnerID:        scope.OwnerID,
		HubID:          hubID,
		TargetTopicID:  targetID,
		SourceTopicIDs: sources,
		Reason:         strings.TrimSpace(args.Reason),
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrProposeTopicMergeNotFound):
			return errProposeResult(call, "target or source topic not found in this hub")
		case errors.Is(err, ErrProposeTopicMergeNoPermission):
			return errProposeResult(call, "you do not have permission to propose topic merges in this hub")
		case errors.Is(err, ErrProposeTopicMergeInvalidArgs):
			return errProposeResult(call, "invalid topic merge arguments")
		default:
			return errProposeResult(call, fmt.Sprintf("propose_topic_merge: %v", err))
		}
	}

	status := outcome.Status
	if status == "" {
		status = ProposeStatusProposed
	}

	body, _ := json.Marshal(ProposeTopicMergeResult{
		NotificationID: outcome.NotificationID,
		Status:         status,
		HubID:          hubID,
		TargetTopicID:  targetID,
		TargetTitle:    outcome.TargetTitle,
		SourceTopicIDs: sources,
		SourceTitles:   outcome.SourceTitles,
	})
	return sdkmodel.ToolResult{Content: string(body)}, nil
}

// canonicalizeSourceTopicIDs trims whitespace, drops empties,
// dedups, and returns the sorted list. The sort is what keeps
// the dedup source_id stable across different agent argument
// orders.
func canonicalizeSourceTopicIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// errProposeResult shapes an SDK ToolResult marked as an error.
func errProposeResult(call sdktool.Call, msg string) (sdkmodel.ToolResult, error) {
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

// _ = model.ReviewTopicMergePayload keeps the model import
// live; the production service uses it directly.
var _ = model.ReviewTopicMergePayload{}
