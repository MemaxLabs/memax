// Phase 2b agentic-organize path. plan 24 §"Lucid Dream
// (agentic upgrade)". Routes a single unassigned-memory through
// agent.Run when the trigger evaluator fired KindTopicOverload
// or KindUserFollowup on it AND the per-hub flag is on AND the
// engine has an AgentRuntime configured AND the cycle's run
// budget hasn't reached its hard cap. Non-fired memories continue
// through the existing batched single-call path.
//
// The agent's verdict is one of three structured assignments:
//
//   - **ASSIGN_EXISTING** — pin the memory to an existing topic.
//   - **NEW_TOPIC** — create a new topic and pin the memory there.
//   - **SKIP** — leave the memory unassigned (fall through to
//     the next dream cycle's organize phase).
//
// The agent emits the verdict as a JSON object on a line prefixed
// `VERDICT_JSON:` so the engine can parse it deterministically.
// JSON beats pipe-separated fields for the new-topic case where
// name/icon/description need to be carried unambiguously.
package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// organizeAgentVerdict is the JSON shape the agent emits on the
// VERDICT_JSON: line. Mirrors the existing organizeAssignment
// struct from llmOrganize so the engine's apply path can stay
// shared between the agentic and single-call routes.
//
// Action is the discriminator: "assign_existing" / "new_topic" /
// "skip" (lowercase — JSON convention; the prompt instructs the
// agent accordingly).
type organizeAgentVerdict struct {
	Action      string  `json:"action"`
	TopicID     string  `json:"topic_id,omitempty"`
	NewName     string  `json:"new_name,omitempty"`
	NewIcon     string  `json:"new_icon,omitempty"`
	NewDesc     string  `json:"new_description,omitempty"`
	NewParentID string  `json:"new_parent_id,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

const (
	organizeAgentActionAssignExisting = "assign_existing"
	organizeAgentActionNewTopic       = "new_topic"
	organizeAgentActionSkip           = "skip"
)

// shouldRouteOrganizeAgent reports whether a single unassigned
// memory should route through agent.Run. Same gate shape as
// shouldRouteContradictionAgent — three independent gates plus
// the cycle budget. Trigger relevance for organize is
// KindTopicOverload (organizational drift) or KindUserFollowup
// (user explicitly flagged); KindContradiction and KindURLDrift
// don't bear on topic assignment so they stay on the batch path.
func (e *Engine) shouldRouteOrganizeAgent(
	settings map[string]any,
	decisions map[string]triggers.Decision,
	mem *model.Memory,
) bool {
	if !e.agentRuntime.IsConfigured() {
		return false
	}
	if enabled, _ := settings["dreams_use_agent_runtime"].(bool); !enabled {
		return false
	}
	if decisions == nil || mem == nil {
		return false
	}
	d := decisions[mem.ID]
	if !d.Fired {
		return false
	}
	switch d.Kind {
	case triggers.KindTopicOverload, triggers.KindUserFollowup:
		return true
	}
	return false
}

// nullableParentID returns a *string for Topic.ParentID. Empty
// input → nil so Postgres stores SQL NULL (the agent's "no
// parent" verdict). Non-empty → pointer to value.
func nullableParentID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// agenticOrganizeOne routes one unassigned memory through
// agent.Run. Returns (applied, action, ok):
//
//   - applied: true if the agent produced an assignment AND the
//     engine successfully created/found the topic AND wrote the
//     dream_action. Caller increments organized counter.
//   - action: the persisted DreamAction (zero value if !applied).
//   - ok: true unless an error blocked even partial bookkeeping;
//     false means the caller should `continue` past this memory.
//     The cycle budget is consumed regardless — agent runs cost
//     real model calls whether they verdict-applied or not.
func (e *Engine) agenticOrganizeOne(
	ctx context.Context,
	hub *model.Hub,
	runID string,
	mem *model.Memory,
	allTopics []model.Topic,
	topicCounts map[string]int,
	decisions map[string]triggers.Decision,
	newTopicsByName map[string]string,
	metrics *model.DreamPhaseMetrics,
	runBudget *dreamRunBudget,
	actorID string,
) (bool, model.DreamAction, bool) {
	metrics.Attempted++
	res, err := e.runAgenticOrganize(ctx, hub, mem, allTopics, topicCounts, decisions)
	runBudget.consume(toAgentRunResultForBudget(res))
	if res != nil {
		metrics.LLMCalls += res.ModelCalls
		metrics.TokensIn += int64(res.Usage.InputTokens)
		metrics.TokensOut += int64(res.Usage.OutputTokens)
	}
	if err != nil {
		metrics.LLMErrors++
		slog.WarnContext(ctx, "dream: agentic organize failed",
			"hub_id", hub.ID, "memory_id", mem.ID, "error", err,
		)
		return false, model.DreamAction{}, true
	}
	// Successful agent terminal: count one Processed memory,
	// matching the batch path's "completed batch increments
	// Processed += len(batch)" semantics. Without this,
	// per-phase telemetry (Attempted vs Processed gap) would
	// silently undercount whenever fired memories route through
	// the agent path. Increment BEFORE verdict handling so a
	// missing-verdict-but-success terminal still counts.
	metrics.Processed++
	if res == nil || res.Verdict == nil {
		// No parsable verdict — treat as skip (memory stays
		// unassigned for next cycle).
		metrics.Skipped++
		return false, model.DreamAction{}, true
	}
	v := res.Verdict
	switch v.Action {
	case organizeAgentActionSkip:
		metrics.Skipped++
		return false, model.DreamAction{}, true
	case organizeAgentActionAssignExisting:
		if v.TopicID == "" {
			slog.WarnContext(ctx, "dream: agentic organize assign_existing missing topic_id",
				"hub_id", hub.ID, "memory_id", mem.ID,
			)
			metrics.Skipped++
			return false, model.DreamAction{}, true
		}
		if _, terr := e.store.GetTopic(v.TopicID, hub.ID); terr != nil {
			slog.WarnContext(ctx, "dream: agentic organize suggested non-existent topic",
				"hub_id", hub.ID, "memory_id", mem.ID, "topic_id", v.TopicID, "err", terr,
			)
			metrics.Skipped++
			return false, model.DreamAction{}, true
		}
		return e.applyOrganizeAssignment(ctx, hub, runID, mem, v.TopicID, v.Reason, v.Confidence, res, metrics, actorID)
	case organizeAgentActionNewTopic:
		if v.NewName == "" {
			slog.WarnContext(ctx, "dream: agentic organize new_topic missing name",
				"hub_id", hub.ID, "memory_id", mem.ID,
			)
			metrics.Skipped++
			return false, model.DreamAction{}, true
		}
		// Reuse a topic this cycle already created with the same
		// name. Mirrors the batch-path's newTopicsByName cache.
		topicID, ok := newTopicsByName[v.NewName]
		if !ok {
			now := time.Now()
			icon := v.NewIcon
			if icon == "" {
				icon = "folder"
			}
			desc := v.NewDesc
			if desc == "" {
				desc = fmt.Sprintf("Memories related to %s", v.NewName)
			}
			topic := &model.Topic{
				ID:          generateID(),
				OwnerID:     hub.OwnerID,
				HubID:       hub.ID,
				Name:        v.NewName,
				Description: desc,
				Icon:        icon,
				ParentID:    nullableParentID(v.NewParentID),
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if cerr := e.store.CreateTopic(topic); cerr != nil {
				slog.WarnContext(ctx, "dream: agentic organize failed to create topic",
					"hub_id", hub.ID, "memory_id", mem.ID, "name", v.NewName, "err", cerr,
				)
				metrics.Errors++
				return false, model.DreamAction{}, true
			}
			topicID = topic.ID
			newTopicsByName[v.NewName] = topicID
			slog.InfoContext(ctx, "dream: agentic organize created new topic",
				"hub_id", hub.ID, "memory_id", mem.ID, "topic_id", topicID, "name", v.NewName,
			)
		}
		return e.applyOrganizeAssignment(ctx, hub, runID, mem, topicID, v.Reason, v.Confidence, res, metrics, actorID)
	default:
		slog.WarnContext(ctx, "dream: agentic organize emitted unknown action",
			"hub_id", hub.ID, "memory_id", mem.ID, "action", v.Action,
		)
		metrics.Skipped++
		return false, model.DreamAction{}, true
	}
}

// applyOrganizeAssignment writes the per-memory store mutation
// (memory_topics row) + the dream_action audit row. Shared by
// both verdict branches (assign_existing and new_topic) so the
// store-call shape stays in one place.
func (e *Engine) applyOrganizeAssignment(
	ctx context.Context,
	hub *model.Hub,
	runID string,
	mem *model.Memory,
	topicID string,
	reason string,
	confidence float64,
	res *agenticOrganizeResult,
	metrics *model.DreamPhaseMetrics,
	_ string,
) (bool, model.DreamAction, bool) {
	// Confidence cap: never overwrite a user-set assignment.
	conf := model.ConfidenceAutoLow
	if confidence > 0 {
		conf = confidence
		if conf > model.ConfidenceAutoHigh {
			conf = model.ConfidenceAutoHigh
		}
	}
	if err := e.store.AssignMemoryToTopic(mem.ID, topicID, hub.ID, conf); err != nil {
		slog.WarnContext(ctx, "dream: agentic organize assign failed",
			"hub_id", hub.ID, "memory_id", mem.ID, "topic_id", topicID, "err", err,
		)
		metrics.Errors++
		return false, model.DreamAction{}, true
	}

	action := model.DreamAction{
		ID:              generateID(),
		RunID:           runID,
		ActionType:      "organize",
		SourceMemoryIDs: []string{mem.ID},
		ResultMemoryID:  topicID,
		ToTopicID:       topicID,
		Reason:          reason,
		CreatedAt:       time.Now(),
		// Phase 2b audit — same shape contradict uses. Path tag
		// + session id + call counts let calibration queries
		// distinguish agent-derived organize actions from the
		// single-call batch path's actions.
		AgentPath:       model.DreamActionAgentPathLucidActive,
		AgentSessionID:  res.SessionID,
		AgentModelCalls: res.ModelCalls,
		AgentToolCalls:  len(res.ToolCalls),
	}
	if err := e.store.CreateDreamAction(&action); err != nil {
		slog.WarnContext(ctx, "dream: agentic organize action insert failed",
			"hub_id", hub.ID, "memory_id", mem.ID, "err", err,
		)
		metrics.Errors++
		return false, model.DreamAction{}, true
	}
	metrics.Actions++
	return true, action, true
}

// agenticOrganizeResult carries the agent-run terminal state plus
// the parsed structured verdict. Same shape pattern as
// agenticContradictionResult — result is non-nil even on
// non-success terminals so the budget consumes real ModelCalls
// regardless of verdict success.
type agenticOrganizeResult struct {
	Verdict    *organizeAgentVerdict
	Final      string
	SessionID  string
	ModelCalls int
	ToolCalls  []agent.ToolCallRecord
	Usage      sdkmodelUsageProxy
}

// sdkmodelUsageProxy is a lightweight mirror of sdkmodel.Usage's
// fields the engine actually reads. Avoids forcing every consumer
// of agenticOrganizeResult to import the SDK's package directly.
type sdkmodelUsageProxy struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// runAgenticOrganize wires up agent.Run with the recall_memories
// tool, the organize-specific system prompt, and the per-memory
// user prompt. Returns a populated result on every code path
// (success OR non-success) so the budget consumer can account
// for real model calls regardless of verdict.
func (e *Engine) runAgenticOrganize(
	ctx context.Context,
	hub *model.Hub,
	mem *model.Memory,
	allTopics []model.Topic,
	topicCounts map[string]int,
	decisions map[string]triggers.Decision,
) (*agenticOrganizeResult, error) {
	if !e.agentRuntime.IsConfigured() {
		return nil, fmt.Errorf("agent runtime not configured")
	}

	session := agent.SessionDescriptor{
		OwnerID: hub.OwnerID,
		Type:    agent.ScopeTypeSingleHub,
		HubIDs:  []string{hub.ID},
	}
	recallTool, err := tools.NewRecallTool(tools.RecallToolConfig{
		Service:  e.agentRuntime.RecallService,
		Resolver: e.agentRuntime.Resolver,
		Session:  session,
	})
	if err != nil {
		return nil, fmt.Errorf("build recall tool: %w", err)
	}

	prompt := buildOrganizePrompt(mem, allTopics, topicCounts, decisions[mem.ID])
	res, err := agent.Run(ctx, agent.RunInput{
		Prompt:       prompt,
		Profile:      agent.ProfileDreamActive,
		Tools:        []sdktool.Tool{recallTool},
		Model:        e.agentRuntime.Model,
		Session:      session,
		SystemPrompt: organizeAgentSystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	out := &agenticOrganizeResult{
		Final:      res.Final,
		SessionID:  res.SessionID,
		ModelCalls: res.ModelCalls,
		ToolCalls:  append([]agent.ToolCallRecord(nil), res.ToolCalls...),
		Usage: sdkmodelUsageProxy{
			InputTokens:  res.Usage.InputTokens,
			OutputTokens: res.Usage.OutputTokens,
			TotalTokens:  res.Usage.TotalTokens,
		},
	}
	if res.Status != agent.RunStatusSuccess {
		// Non-success: the result is populated for budget +
		// metrics, but no parsable verdict.
		return out, fmt.Errorf("agent run terminated %s: %v", res.Status, res.Err)
	}
	v := parseOrganizeVerdict(res.Final)
	out.Verdict = v
	if v != nil {
		slog.InfoContext(ctx, "dream: agentic organize verdict",
			"hub_id", hub.ID,
			"memory_id", mem.ID,
			"action", v.Action,
			"topic_id", v.TopicID,
			"new_name", v.NewName,
			"tool_calls", len(res.ToolCalls),
			"tokens_in", res.Usage.InputTokens,
			"tokens_out", res.Usage.OutputTokens,
		)
	}
	return out, nil
}

// organizeAgentSystemPrompt sets the agent's organize-specific
// persona + verdict-line contract. JSON shape because the
// new_topic case carries fields (name + icon + description +
// optional parent) that don't fit a single delimiter.
//
// The const name uses an "Agent" infix to avoid colliding with
// `organizeSystemPrompt` from organize_llm.go (the single-call
// path's prompt).
const organizeAgentSystemPrompt = `You are the dream-engine topic organizer for a personal-memory system. The user's hub has accumulated a memory that hasn't been assigned to a topic yet. Your job is to decide one of:
  - assign it to an existing topic (return its topic_id);
  - create a new topic and put the memory in it (return name + icon + short description);
  - skip the assignment for now (the engine will retry next cycle).

Use the recall_memories tool when context from neighboring memories would change your decision. Don't recall just to be thorough — only when context would matter (e.g. to confirm the right existing topic vs. creating a duplicate).

End your reply with EXACTLY ONE line in this format, as the LAST line of your output:

VERDICT_JSON: {"action":"assign_existing","topic_id":"<uuid>","reason":"<short>","confidence":0.7}
VERDICT_JSON: {"action":"new_topic","new_name":"<title-case name>","new_icon":"<lucide icon name>","new_description":"<one sentence>","reason":"<short>","confidence":0.6}
VERDICT_JSON: {"action":"skip","reason":"<short>"}

Confidence is 0..1 and SHOULD be set on assign/new_topic verdicts. The verdict line is parsed mechanically — invalid JSON or a missing line falls back to skip.`

// buildOrganizePrompt frames the per-memory task. Existing topics
// are listed (id + name + member-count) so the agent can pick one
// or decide to create new. Trigger context (topic-overload signals
// or user-followup marker text) is included verbatim so the agent
// understands WHY it's being asked to think harder than the batch
// path does.
func buildOrganizePrompt(
	mem *model.Memory,
	allTopics []model.Topic,
	topicCounts map[string]int,
	decision triggers.Decision,
) string {
	var b strings.Builder
	b.WriteString("Memory:\n")
	b.WriteString(memorySnippetForAgent(mem))
	b.WriteString("\n\nExisting topics in this hub:\n")
	if len(allTopics) == 0 {
		b.WriteString("(none — every assignment will create a new topic.)\n")
	}
	for _, t := range allTopics {
		fmt.Fprintf(&b, "- id=%s name=%q members=%d", t.ID, t.Name, topicCounts[t.ID])
		if t.Description != "" {
			fmt.Fprintf(&b, " description=%q", compactPreview(t.Description, 120))
		}
		b.WriteString("\n")
	}

	if decision.Fired {
		b.WriteString("\nTrigger context:\n")
		fmt.Fprintf(&b, "- kind=%s\n", decision.Kind)
		if decision.Signals.UserFollowupMarker != "" {
			fmt.Fprintf(&b, "- user_followup_marker=%q (the user explicitly flagged this memory for follow-up)\n",
				decision.Signals.UserFollowupMarker)
		}
		if decision.Signals.TopicSiblingCount > 0 {
			fmt.Fprintf(&b, "- topic_sibling_count=%d (this memory's neighbors are concentrated; consider whether the existing topic is overloaded and a more specific topic would help)\n",
				decision.Signals.TopicSiblingCount)
		}
	}
	b.WriteString("\nDecide on the assignment and reply with the VERDICT_JSON line as instructed.")
	return b.String()
}

// parseOrganizeVerdict walks the agent's reply backwards looking
// for the VERDICT_JSON: prefix and parses the trailing JSON.
// Returns nil on parse failure — caller treats nil as skip.
//
// Last-line-only is the contract; trailing prose after the verdict
// line is a contract violation that returns nil. Same fail-safe
// shape contradict uses.
func parseOrganizeVerdict(final string) *organizeAgentVerdict {
	for _, line := range reverseLines(final) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const prefix = "VERDICT_JSON:"
		idx := strings.Index(strings.ToUpper(line), prefix)
		if idx < 0 {
			// First non-empty line isn't a verdict line —
			// contract violation. Return nil so caller
			// treats it as skip.
			return nil
		}
		jsonStr := strings.TrimSpace(line[idx+len(prefix):])
		var v organizeAgentVerdict
		if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
			return nil
		}
		// Normalize action to lowercase for the switch consumer.
		v.Action = strings.ToLower(v.Action)
		switch v.Action {
		case organizeAgentActionAssignExisting,
			organizeAgentActionNewTopic,
			organizeAgentActionSkip:
			return &v
		}
		return nil
	}
	return nil
}
