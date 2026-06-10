// Phase 2b agentic-contradict path. plan 24 §"Lucid Dream
// (agentic upgrade)". Routes a single contradiction-pair candidate
// through agent.Run when the per-hub flag is on AND the trigger
// evaluator fired on either memory. The phase code stays
// path-agnostic — the agentic and single-call routes both produce
// the same (contradicts, reason) verdict shape.
package dreams

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams/triggers"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// agenticContradictionResult is the structured verdict returned
// from the agent path. The phase code reads these fields the same
// way it reads the single-call return values: Contradicts is the
// gate, Reason is the human-readable rationale, Usage tracks the
// total token cost across the agent's turns.
//
// SessionID + ModelCalls + ToolCalls + Final are the durable
// audit signals stamped onto the resulting dream_action so
// calibration queries can attribute cost and reproduce the
// agent's reasoning trail.
type agenticContradictionResult struct {
	Contradicts bool
	Reason      string
	Usage       sdkmodel.Usage
	SessionID   string
	ModelCalls  int
	ToolCalls   []agent.ToolCallRecord
	Final       string
}

// budgetConsumable is the narrow shape `dreamRunBudget` needs to
// project a phase-specific result onto its accumulator. Every
// agentic phase (contradict, organize, …) implements it on its
// result type so the budget stays decoupled from any one shape.
//
// Returning concrete fields (not the entire SDK RunResult)
// keeps the surface tight and lets each phase store its own
// extra audit fields without polluting the budget contract.
type budgetConsumable interface {
	budgetModelCalls() int
	budgetToolCalls() int
}

func (r *agenticContradictionResult) budgetModelCalls() int { return r.ModelCalls }
func (r *agenticContradictionResult) budgetToolCalls() int  { return len(r.ToolCalls) }

// toAgentRunResultForBudget projects ANY budgetConsumable onto
// the agent.RunResult shape `dreamRunBudget.consume` expects.
// Returns nil for a nil pointer so the budget caller can be
// nil-safe at call sites without redundant guards.
func toAgentRunResultForBudget(r budgetConsumable) *agent.RunResult {
	if r == nil || isNilInterface(r) {
		return nil
	}
	return &agent.RunResult{
		ModelCalls: r.budgetModelCalls(),
		// ToolCalls field on agent.RunResult is a slice; the
		// budget only inspects len() so we can hand it a
		// length-only slice without allocating real records.
		ToolCalls: make([]agent.ToolCallRecord, r.budgetToolCalls()),
	}
}

// isNilInterface guards against the famous Go gotcha where a
// non-nil interface wrapping a nil pointer reads as non-nil at
// the call site but panics on first method call. Caller passes
// `(*agenticContradictionResult)(nil)` → interface is non-nil →
// without this check we'd dereference and crash.
func isNilInterface(c budgetConsumable) bool {
	switch v := c.(type) {
	case *agenticContradictionResult:
		return v == nil
	case *agenticOrganizeResult:
		return v == nil
	}
	return false
}

func (r *agenticOrganizeResult) budgetModelCalls() int { return r.ModelCalls }
func (r *agenticOrganizeResult) budgetToolCalls() int  { return len(r.ToolCalls) }

// shouldRouteContradictionAgent reports whether the contradict
// phase should route this pair through agent.Run rather than the
// existing single-call path. Three independent gates — all must
// hold, ANY false short-circuits to single-call:
//
//  1. AgentRuntime is configured at the engine layer (app-level
//     opt-in: Anthropic key + recall service available).
//  2. Per-hub flag `dreams_use_agent_runtime` is true.
//  3. Trigger evaluator fired on at least one memory of the pair.
//     We only count KindContradiction or KindUserFollowup as
//     contradict-relevant fires; KindURLDrift and KindTopicOverload
//     fire on different signals that don't bear on contradiction
//     resolution and should stay on the single-call path even when
//     they fire alongside a contradiction candidate.
func (e *Engine) shouldRouteContradictionAgent(
	settings map[string]any,
	decisions map[string]triggers.Decision,
	pair *model.SimilarMemoryPair,
) bool {
	if !e.agentRuntime.IsConfigured() {
		return false
	}
	if enabled, _ := settings["dreams_use_agent_runtime"].(bool); !enabled {
		return false
	}
	if decisions == nil || pair == nil {
		return false
	}
	for _, id := range []string{pair.MemoryA.ID, pair.MemoryB.ID} {
		d := decisions[id]
		if !d.Fired {
			continue
		}
		switch d.Kind {
		case triggers.KindContradiction, triggers.KindUserFollowup:
			return true
		}
	}
	return false
}

// agenticDetectContradiction runs the agent.Run path for one
// contradiction-pair candidate. The agent gets:
//
//   - A prompt framing both memories and the trigger context that
//     fired (contradiction count, user-followup marker text), with
//     a strict verdict-line contract the engine parses below.
//   - A scope-bound recall_memories tool so it can fetch related
//     context from the same hub (single-hub session — the dream
//     agent never operates cross-hub).
//   - ProfileDreamActive budget envelope (12 turns, 8 model calls,
//     16 tool calls, 300s — plan 24 §"Provider, model selection,
//     budgets").
//
// Returns a structured result the caller maps onto its existing
// (contradicts, reason, usage) shape. A non-nil error means the
// agent run failed before producing a verdict — the caller skips
// the pair and bumps LLMErrors. A nil error with !result.Contradicts
// means the agent decided "no contradiction" — same as a
// single-call no-vote.
func (e *Engine) agenticDetectContradiction(
	ctx context.Context,
	hub *model.Hub,
	pair *model.SimilarMemoryPair,
	decisions map[string]triggers.Decision,
	actorID string,
) (*agenticContradictionResult, error) {
	if !e.agentRuntime.IsConfigured() {
		// Defensive — shouldRouteContradictionAgent guards this
		// branch in production code, but a future caller might
		// reach here directly from a test. Same fail-closed default.
		return nil, fmt.Errorf("agent runtime not configured")
	}

	// Build the recall tool bound to this hub's scope. The tool's
	// own session-resolver intersects against live membership at
	// every Execute call; if the hub owner has been removed from
	// the hub mid-cycle (rare on personal hubs, possible on team
	// hubs after an admin action), the tool's recall returns no
	// authorized hub targets and the agent gracefully reports the
	// failure into its final reply.
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

	prompt := buildContradictPrompt(pair, decisions)
	res, err := agent.Run(ctx, agent.RunInput{
		Prompt:       prompt,
		Profile:      agent.ProfileDreamActive,
		Tools:        []sdktool.Tool{recallTool},
		Model:        e.agentRuntime.Model,
		Session:      session,
		SystemPrompt: contradictSystemPrompt,
	})
	if err != nil {
		// Pre-flight error (validation, registry build, etc.) —
		// no model calls were made, no result to consume. Caller
		// bumps LLMErrors and skips the pair.
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	// Even on non-success status, project the populated RunResult
	// onto our result type so the budget/metrics layer can consume
	// the real call counts. Codex flagged the previous shape as
	// dropping cost accounting on budget-exhaust / provider-error /
	// max-turns terminals — those still consumed real model calls,
	// they just didn't produce a parsable verdict.
	out := &agenticContradictionResult{
		Usage:      res.Usage,
		SessionID:  res.SessionID,
		ModelCalls: res.ModelCalls,
		ToolCalls:  append([]agent.ToolCallRecord(nil), res.ToolCalls...),
		Final:      res.Final,
	}

	if res.Status != agent.RunStatusSuccess {
		// Budget exhaust, max turns, or a downstream provider
		// error all surface here. Treat as "could not decide" —
		// the caller bumps LLMErrors and the pair gets skipped.
		// We do NOT fall back to the single-call path mid-loop
		// because that would double-count the lucid_active
		// decision under a different model + budget. The caller
		// receives `out` (with real ModelCalls/ToolCalls/Usage)
		// alongside the error so the cycle's budget governor and
		// metrics still account for the work done.
		return out, fmt.Errorf("agent run terminated %s: %v", res.Status, res.Err)
	}

	verdict, reason := parseContradictVerdict(res.Final)
	slog.InfoContext(ctx, "dream: agentic contradiction verdict",
		"hub_id", hub.ID,
		"actor_id", actorID,
		"memory_a", pair.MemoryA.ID,
		"memory_b", pair.MemoryB.ID,
		"verdict", verdictLogValue(verdict),
		"tool_calls", len(res.ToolCalls),
		"tokens_in", res.Usage.InputTokens,
		"tokens_out", res.Usage.OutputTokens,
	)
	out.Contradicts = verdict
	out.Reason = reason
	return out, nil
}

// contradictSystemPrompt sets the agent's persona + verdict-line
// contract. Kept as a const so the engine doesn't allocate it on
// every contradiction pair AND so a future change has one place
// to land.
const contradictSystemPrompt = `You are the dream-engine contradiction reviewer for a personal-memory system. Two memories from the user's knowledge base have been flagged as topically related but possibly contradictory. Your job is to decide whether they contradict each other in fact, recommendation, or stated preference.

Use the recall_memories tool when you need related context from the same hub (e.g. to confirm a versioned decision or check the user's most recent note). Don't recall just to be thorough — only when context would change your verdict.

End your reply with EXACTLY ONE of these verdict lines, on its own line, as the LAST line of your output:

VERDICT: CONTRADICTION — <one-sentence reason>
VERDICT: NO_CONTRADICTION — <one-sentence reason>

The verdict line is parsed mechanically; deviations make the engine fall back to a no-vote.`

// buildContradictPrompt frames the per-pair task. Trigger context
// is included verbatim so the agent knows WHY it was invoked
// (contradiction count vs user-followup) and can weight evidence
// accordingly.
func buildContradictPrompt(pair *model.SimilarMemoryPair, decisions map[string]triggers.Decision) string {
	var b strings.Builder
	b.WriteString("Memory A:\n")
	b.WriteString(memorySnippetForAgent(&pair.MemoryA))
	b.WriteString("\n\nMemory B:\n")
	b.WriteString(memorySnippetForAgent(&pair.MemoryB))
	b.WriteString(fmt.Sprintf("\n\nSimilarity: %.3f\n", pair.Similarity))

	for _, m := range []*model.Memory{&pair.MemoryA, &pair.MemoryB} {
		if d, ok := decisions[m.ID]; ok && d.Fired {
			fmt.Fprintf(&b, "\nTrigger fired on %s (id=%s): kind=%s",
				memoryLabel(m), m.ID, d.Kind)
			if d.Signals.UserFollowupMarker != "" {
				fmt.Fprintf(&b, " marker=%q", d.Signals.UserFollowupMarker)
			}
			if d.Signals.ContradictionCount > 0 {
				fmt.Fprintf(&b, " contradiction_count=%d", d.Signals.ContradictionCount)
			}
		}
	}
	b.WriteString("\n\nDecide whether Memory A and Memory B contradict each other and reply with the verdict line as instructed.")
	return b.String()
}

// memorySnippetForAgent renders one memory in a compact, agent-
// readable form. We pass title + summary + a content preview;
// full content would blow the prompt budget for verbose memories
// and the agent can recall_memories for the full text if needed.
func memorySnippetForAgent(m *model.Memory) string {
	var b strings.Builder
	if t := strings.TrimSpace(m.Title); t != "" {
		fmt.Fprintf(&b, "Title: %s\n", t)
	}
	if s := strings.TrimSpace(m.Summary); s != "" {
		fmt.Fprintf(&b, "Summary: %s\n", s)
	}
	preview := compactPreview(m.Content, 600)
	if preview != "" {
		fmt.Fprintf(&b, "Content preview: %s\n", preview)
	}
	fmt.Fprintf(&b, "ID: %s\n", m.ID)
	return strings.TrimRight(b.String(), "\n")
}

// memoryLabel picks the best human-readable label for trigger-
// context logs. Mirrors reviewMemoryRefTitle but trimmed shorter
// because the trigger context goes in-prompt and budget matters.
func memoryLabel(m *model.Memory) string {
	if m == nil {
		return "memory"
	}
	if t := strings.TrimSpace(m.Title); t != "" {
		return compactPreview(t, 40)
	}
	return "memory"
}

// parseContradictVerdict extracts (contradicts, reason) from the
// agent's final reply. The system prompt instructs the agent to
// emit a strict last line; we look for it case-insensitively to
// tolerate minor formatting drift across model versions.
//
// On parse failure (no recognizable verdict), returns
// (false, "").  The caller treats that as "no vote", which is
// safer than a default-true: a missing verdict shouldn't fan out
// review notifications to recipients on a hub that may not even
// have a contradiction.
func parseContradictVerdict(final string) (bool, string) {
	for _, line := range reverseLines(final) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "VERDICT: CONTRADICTION"):
			return true, extractVerdictReason(line)
		case strings.HasPrefix(upper, "VERDICT: NO_CONTRADICTION"),
			strings.HasPrefix(upper, "VERDICT: NO CONTRADICTION"):
			return false, extractVerdictReason(line)
		}
		// Stop scanning after the first non-empty line that ISN'T
		// a verdict — the contract is "verdict line is the last
		// line", and we don't want to drift back through prose
		// looking for a stray match.
		return false, ""
	}
	return false, ""
}

// reverseLines returns the lines of s in reverse order. Helps
// verdict parsing scan from the end without slicing the whole
// string into a slice when only the last few lines matter.
func reverseLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

// extractVerdictReason pulls the reason text out of a verdict
// line. The contract uses an em-dash separator after the keyword
// ("VERDICT: CONTRADICTION — <reason>") but we tolerate ASCII
// substitutes (`-`, `:`, `–`) for model robustness.
//
// Implementation: locate the verdict KEYWORD (anchoring on the
// keyword rather than on the separator avoids the false-match
// where the leading "VERDICT: " colon would shadow the real
// separator that comes after the keyword), then trim leading
// whitespace + separator chars from the remainder.
func extractVerdictReason(line string) string {
	upper := strings.ToUpper(line)
	for _, kw := range []string{"NO_CONTRADICTION", "NO CONTRADICTION", "CONTRADICTION"} {
		if i := strings.Index(upper, kw); i >= 0 {
			tail := line[i+len(kw):]
			tail = strings.TrimLeft(tail, " \t—-:–")
			return strings.TrimSpace(tail)
		}
	}
	return ""
}

func verdictLogValue(b bool) string {
	if b {
		return "CONTRADICTION"
	}
	return "NO_CONTRADICTION"
}
