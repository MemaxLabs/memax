package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	memaxagent "github.com/MemaxLabs/memax-go-agent-sdk"
	sdkhook "github.com/MemaxLabs/memax-go-agent-sdk/hook"
	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdksession "github.com/MemaxLabs/memax-go-agent-sdk/session"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
)

// Errors returned by Run for caller-visible programmer or wiring
// problems. The model.Client's runtime errors (provider 429,
// timeout, budget exhausted) come back inside RunResult.Err with a
// typed Status so the caller can branch without string matching;
// THESE errors are pre-flight, the kind that indicate a bug in the
// dream/chat handler that built the input, not a transient runtime
// failure.
var (
	ErrEmptyPrompt   = errors.New("agent: RunInput.Prompt is required")
	ErrInvalidPrf    = errors.New("agent: RunInput.Profile is invalid")
	ErrMissingModel  = errors.New("agent: RunInput.Model is required")
	ErrInvalidScope  = errors.New("agent: RunInput.Session is invalid")
	ErrEmptyToolList = errors.New("agent: RunInput.Tools must contain at least one tool")
)

// RunInput is the per-invocation request. Both the dream engine and
// the chat handler construct one of these per agent invocation —
// dreams once per memory-considered, chat once per user turn.
//
// Run takes RunInput by value (not by pointer) intentionally: the
// runtime never mutates the input, and the call-site readability of
// `agent.Run(ctx, agent.RunInput{...})` is better than passing a
// constructed pointer through. The struct stays small enough that
// the copy cost is irrelevant next to the LLM round-trip.
type RunInput struct {
	// Prompt is the initial user/system message the agent reasons
	// over. Non-empty. For dream runs this is the trigger framing
	// ("memory M fired contradiction trigger; here's the evidence");
	// for chat runs it's the user's typed message.
	Prompt string

	// Profile selects the budget envelope and (downstream) the
	// model id. Must be a valid production profile; the test-only
	// envelope is rejected here so a profile typo can't silently
	// escape into prod.
	Profile Profile

	// Session is the descriptor every tool wrapper resolves
	// against. Carries OwnerID + scope shape + pinned hubs +
	// optional WriteHubID. The runtime validates it once at
	// Run-entry; tools resolve at execute-time for live membership.
	Session SessionDescriptor

	// Tools is the per-session tool list. Each tool wrapper has
	// already been bound to the session descriptor at construction
	// (see internal/agent/tools/recall.go for the pattern), so the
	// runtime does NOT re-bind here. Empty list rejected at
	// validate-time — an agent with no tools is a single-call LLM,
	// which is a different code path entirely.
	Tools []sdktool.Tool

	// Model is the SDK provider client. Constructed by the caller
	// via internal/agent/providers/anthropic.go (or a fake in
	// tests). Required — Run does NOT default-construct from env;
	// the dream/chat handlers own provider lifetime so they can
	// share one client across many Runs without reauthing each
	// time.
	Model sdkmodel.Client

	// SystemPrompt overrides the default system prompt for this
	// run. Empty falls through to the SDK's default. Each surface
	// (dream, chat) maintains its own prompt template above this
	// layer; the runtime stays prompt-agnostic so a future
	// "chat with code-mode persona" doesn't fork the runtime.
	SystemPrompt string

	// SessionID is optional. Empty → SDK generates a fresh one.
	// Non-empty → SDK looks it up in the session store. Chat will
	// pass a stable id so subsequent turns continue the same
	// transcript; dreams pass empty (each run is its own session).
	SessionID string

	// Sessions is the optional session store. nil → SDK uses an
	// in-memory store (ephemeral; fine for dream runs that
	// terminate within one Run call). Chat passes a Postgres-backed
	// store in Phase 3 so transcripts survive across requests.
	Sessions sdksession.Store

	// OnEvent is the optional per-event observer. When set, Run
	// calls it for EVERY event the SDK emits before the drain
	// folds the event into the rolled-up RunResult. Designed for
	// chat SSE (Phase 3.4b2): the chat worker writes each event
	// to chat_message_events as a row in the replay buffer so a
	// disconnected client can resume from Last-Event-ID.
	//
	// **Synchronous and ordered.** Run waits for OnEvent to
	// return before reading the next event. This serializes the
	// per-event sink (DB write) with the agent loop's natural
	// pace; in particular, a slow sink will exert backpressure
	// on the SDK's event channel, which is the right behavior
	// for "don't get ahead of the durable log."
	//
	// **Errors are observability-only.** The drain does not
	// abort if OnEvent returns an error; the dream/chat callers
	// own the durable side-effect, and a transient sink failure
	// (DB hiccup) shouldn't trash the rest of the run. The
	// callback should log non-fatal errors itself.
	//
	// nil callback is a no-op — keeps existing dream callers
	// from having to opt out.
	OnEvent func(ctx context.Context, ev memaxagent.Event) error

	// Hooks is the optional SDK hook runner — used to install
	// tool-policy gates like agentpolicy.ApprovalBeforeTool that
	// deny mutating tools until the model has obtained an
	// approval grant via request_approval. Phase 3.6b3 chat
	// runs construct one of these per turn with the approval-
	// required tool list; dream runs leave it nil.
	//
	// Construction: hook.NewRunner(policy.Options()...). The
	// runner is per-Run because some policies are stateful
	// (ApprovalBeforeTool tracks granted approvals across
	// tool_use events within the same run); reusing a runner
	// across Runs would leak grants across messages.
	Hooks *sdkhook.Runner
}

// validate returns the first programmer-error problem with the
// input. Run calls this BEFORE doing anything observable so a bug
// in the caller surfaces as a clean error, not a partial side
// effect (e.g. a session row already created).
func (in RunInput) validate() error {
	if strings.TrimSpace(in.Prompt) == "" {
		return ErrEmptyPrompt
	}
	if !in.Profile.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidPrf, in.Profile)
	}
	if in.Model == nil {
		return ErrMissingModel
	}
	if len(in.Tools) == 0 {
		return ErrEmptyToolList
	}
	if err := in.Session.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidScope, err)
	}
	return nil
}

// RunStatus is the high-level terminal state of a run. Callers
// branch on this; error-message string-matching is brittle and
// banned in handlers.
type RunStatus string

const (
	// RunStatusSuccess — run terminated naturally with a final
	// model response. RunResult.Final is set; RunResult.Err is nil.
	RunStatusSuccess RunStatus = "success"

	// RunStatusBudgetExceeded — the budget governor stopped the
	// run (turns/model-calls/tool-calls/duration/tokens). The
	// chat surface renders this as "this turn hit its budget";
	// the dream engine logs it and finalizes the per-memory dream
	// action with whatever partial state the run produced.
	RunStatusBudgetExceeded RunStatus = "budget_exceeded"

	// RunStatusMaxTurns — the SDK's hard turn-loop bound was hit.
	// Distinct from BudgetExceeded so surfaces can render the
	// "I gave up after N turns" UX (vs. budget's "this turn is
	// done"). In practice this fires for misconfigured budgets;
	// the runner sets policy.MaxTurns < options.MaxTurns to make
	// budget the more common terminal reason.
	RunStatusMaxTurns RunStatus = "max_turns"

	// RunStatusError — provider error, tool wrapper bug, context-
	// window blowout, etc. RunResult.Err carries the wrapped
	// error; the surface decides what to show the user.
	RunStatusError RunStatus = "error"
)

// ToolCallRecord is the audit trail for one tool invocation in a
// run. The dream engine drains these into model.DreamAction rows;
// the chat surface persists them into chat_message_events.
//
// Args and Result are the on-wire JSON strings the SDK exchanged
// with the model — keeping them as strings (not parsed maps) keeps
// the audit trail faithful to what the model actually saw, which
// matters for reproducibility and debugging.
//
// Per-call duration is intentionally NOT recorded here. The SDK's
// Event.Time on EventToolUse / EventToolResult is the canonical
// source; populating it from those timestamps requires forking the
// drain into a streaming variant (Phase 3 will own that for chat).
// Until then, dream audit and chat persistence don't need it.
type ToolCallRecord struct {
	ID       string // SDK tool-use id ("toolu_...")
	Name     string // tool name ("recall_memories")
	ArgsJSON string // raw input JSON the model emitted
	Result   string // tool result content (string per SDK contract)
	IsError  bool   // tool reported an error result
}

// RunResult is the terminal aggregate the dream/chat caller cares
// about. The SDK's event channel is drained internally; surfaces
// that need streaming events (chat SSE) will use a separate
// streaming entry point in Phase 3 — Phase 1 only needs the rolled-
// up result.
type RunResult struct {
	// Status is the high-level terminal state. Always set.
	Status RunStatus

	// SessionID is the SDK session id this run wrote to. Stable
	// across an SDK retry; chat surfaces use this to attach
	// per-turn rows (chat_messages.sdk_session_id).
	SessionID string

	// Final is the model's final natural-language reply. Empty
	// when Status != RunStatusSuccess.
	Final string

	// ToolCalls is the audit trail in invocation order.
	ToolCalls []ToolCallRecord

	// ModelCalls is the count of round-trips to the provider during
	// this run. Each `EventModelRequest` from the SDK increments it.
	// A run with parallel tool fan-out (recall + list in one turn)
	// still counts as ONE model call per turn — `ModelCalls` measures
	// the number of times the orchestration loop invoked the LLM,
	// not how many tool calls happened in total.
	//
	// Why we expose this: the cycle-level governor in
	// internal/dreams (plan 24 §"Dream-run-level shared governor")
	// needs cross-Run aggregate model-call accounting to enforce
	// the per-cycle soft (60) and hard (100) caps. The dream phase
	// also reports it on `dream_actions.agent_model_calls` for cost
	// auditability — without this counter, every agent run would
	// register as a single LLM call regardless of how many turns
	// the agent actually took, and cost calibration would silently
	// underreport ≈8x for an active-trigger run.
	ModelCalls int

	// Usage is the aggregated token count across the run. Provider
	// + model carried from the first non-empty Usage event so the
	// meter knows which model class to bill against.
	Usage sdkmodel.Usage

	// Err is the wrapped error for non-success runs. nil when
	// Status == RunStatusSuccess.
	Err error
}

// Run executes a single agent invocation to terminal and returns
// the rolled-up result. It is the SINGLE entry point both dream
// and chat callers use — no other code in the codebase should call
// memaxagent.Query directly. Centralizing through this function
// means the budget envelope, session-scope contract, audit shape,
// and observability hooks live in exactly one place.
//
// Concurrency: Run is safe to call concurrently. Each invocation
// gets its own SDK session and event channel; nothing in the
// runtime is shared mutable state. Backpressure on the model.Client
// is the caller's responsibility (the dream-run-level governor
// described in plan 24 wraps Run for cross-run coordination).
//
// Error semantics: validate-failures return a non-nil error AND a
// zero RunResult (the run never started). Runtime failures (provider
// timeout, budget exhaust) return a nil error AND a populated
// RunResult with Status reflecting the terminal state — callers MUST
// NOT treat err==nil as "all is well"; they MUST also check Status.
// This split keeps wiring bugs (which crash early) distinct from
// per-run terminal states (which the surface renders).
func Run(ctx context.Context, in RunInput) (RunResult, error) {
	if err := in.validate(); err != nil {
		return RunResult{}, err
	}

	env := envelopeFor(in.Profile)
	policy := policyFor(in.Profile)

	registry := sdktool.NewRegistry()
	for i, t := range in.Tools {
		if t == nil {
			return RunResult{}, fmt.Errorf("agent: RunInput.Tools[%d] is nil", i)
		}
		if err := registry.Register(t); err != nil {
			return RunResult{}, fmt.Errorf("agent: register tool %q: %w", t.Spec().Name, err)
		}
	}

	opts := memaxagent.Options{
		Model:              in.Model,
		Tools:              registry,
		Sessions:           in.Sessions,
		SystemPrompt:       in.SystemPrompt,
		SessionID:          in.SessionID,
		MaxTurns:           env.maxTurns,
		MaxToolConcurrency: env.maxToolConcurrency,
		MaxRunDuration:     env.maxDuration,
		Budget:             policy,
		Hooks:              in.Hooks,
	}

	events, err := memaxagent.Query(ctx, in.Prompt, opts)
	if err != nil {
		return RunResult{
			Status: RunStatusError,
			Err:    fmt.Errorf("agent: start query: %w", err),
		}, nil
	}

	return drainEvents(ctx, events, in.OnEvent), nil
}

// drainEvents reduces the SDK event stream into a RunResult. It is
// internal-only; the streaming surface (chat SSE in Phase 3) will
// fork the channel before this drain runs.
//
// The drain semantics are deliberately tolerant — a malformed event
// (e.g. ToolResult arriving before ToolUse, which shouldn't happen
// per the SDK contract but defensive code pays for itself when an
// SDK upgrade introduces a regression) doesn't abort the drain;
// the result records what we observed and the run finalizes with
// whatever final/error event lands.
func drainEvents(ctx context.Context, events <-chan memaxagent.Event, onEvent func(context.Context, memaxagent.Event) error) RunResult {
	result := RunResult{
		Status: RunStatusError, // worst-case default — overwritten by terminal event
	}
	// Track the in-flight tool calls by id so we can pair tool_use
	// with tool_result. SDK guarantees tool_result lands AFTER its
	// matching tool_use within one session, but multiple calls may
	// be in flight concurrently when a turn dispatches in parallel.
	pending := make(map[string]*ToolCallRecord)
	var (
		sawError    error
		sawResult   bool
		sawTerminal bool
	)

	for ev := range events {
		if result.SessionID == "" && ev.SessionID != "" {
			result.SessionID = ev.SessionID
		}

		// Per-event observer — see RunInput.OnEvent doc. Fired
		// BEFORE the fold so the durable log sees the event in
		// arrival order. Errors are observability-only; a sink
		// that can't keep up still exerts backpressure via the
		// synchronous call but doesn't abort the run.
		if onEvent != nil {
			_ = onEvent(ctx, ev)
		}

		switch ev.Kind {
		case memaxagent.EventModelRequest:
			// One round-trip to the provider. Counts increase per
			// turn; a turn that dispatches parallel tool calls (e.g.
			// `recall_memories` × 2) still emits ONE model_request,
			// then a single model_request next turn after the tool
			// results land. So ModelCalls measures orchestration-
			// loop iterations, not parallel tool-call fan-out.
			result.ModelCalls++
		case memaxagent.EventToolUse:
			if ev.ToolUse == nil {
				continue
			}
			rec := &ToolCallRecord{
				ID:       ev.ToolUse.ID,
				Name:     ev.ToolUse.Name,
				ArgsJSON: string(ev.ToolUse.Input),
			}
			pending[ev.ToolUse.ID] = rec
			// We append to ToolCalls when the result lands so the
			// final slice ordering matches result-arrival order
			// (which is what audit consumers care about — "what did
			// the agent actually receive back, in what order"). This
			// ordering is also more useful for dream-run reasoning
			// than tool-use-emission order.
		case memaxagent.EventToolResult:
			if ev.ToolResult == nil {
				continue
			}
			rec, ok := pending[ev.ToolResult.ToolUseID]
			if !ok {
				// Orphan result (no matching tool_use). Record what
				// we have so audit doesn't lose the call.
				rec = &ToolCallRecord{
					ID:   ev.ToolResult.ToolUseID,
					Name: ev.ToolResult.Name,
				}
			}
			rec.Result = ev.ToolResult.Content
			rec.IsError = ev.ToolResult.IsError
			result.ToolCalls = append(result.ToolCalls, *rec)
			delete(pending, ev.ToolResult.ToolUseID)
		case memaxagent.EventUsage:
			if ev.Usage != nil {
				result.Usage = result.Usage.Add(*ev.Usage)
			}
		case memaxagent.EventResult:
			result.Final = ev.Result
			sawResult = true
			sawTerminal = true
		case memaxagent.EventError:
			if ev.Err != nil {
				sawError = ev.Err
			}
			sawTerminal = true
		}
	}

	switch {
	case sawError != nil:
		result.Status = classifyTerminal(sawError)
		result.Err = sawError
	case sawResult:
		result.Status = RunStatusSuccess
	case sawTerminal:
		// Terminal event with no error and no result is unexpected;
		// surface as error so callers don't read the empty Final
		// as a legitimate empty answer.
		result.Status = RunStatusError
		result.Err = errors.New("agent: run finished without a result or error event")
	default:
		// Channel closed without any terminal event. SDK should
		// always emit one; if we land here, there's a SDK contract
		// regression. Record it explicitly.
		result.Status = RunStatusError
		result.Err = errors.New("agent: event stream closed before terminal event")
	}

	return result
}

// classifyTerminal maps a terminal error to a RunStatus. The SDK
// surfaces the budget governor's reason text in error messages
// (see budget.exceeded() → "budget exceeded: max ..."), and the
// hard turn-loop bound surfaces as "max turns". String-matching is
// the SDK's documented contract for error classification at the
// host layer; a future SDK release that adds typed errors will
// just make this function a one-liner.
func classifyTerminal(err error) RunStatus {
	if err == nil {
		return RunStatusSuccess
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "budget exceeded"):
		return RunStatusBudgetExceeded
	case strings.Contains(msg, "max turns"):
		return RunStatusMaxTurns
	default:
		return RunStatusError
	}
}
