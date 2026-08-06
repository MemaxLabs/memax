// Phase 3.4a — chat message run worker.
//
// Drives the agent runtime for one queued assistant message. The
// chat handler enqueues a ChatMessageRunArgs after BeginChatMessageTurn's
// Fresh outcome commits (the user + assistant rows are durably
// persisted, the session is locked); this worker picks up the job,
// loads the assistant + the parent user message + the per-turn scope
// snapshot, runs agent.Run with a recall_memories tool bound to that
// scope, and finalizes the message via FinalizeChatMessage (which
// also releases the session lock).
//
// Lifecycle invariants this worker depends on (Phase 3.3 contract):
//
//   - The assistant row exists with status='queued' and a populated
//     ScopeHubIDsUsed (the per-turn snapshot — caller's live hubs
//     intersected with session scope at lock-acquire time).
//   - The session row exists with status='running' and
//     active_message_id == this assistant's id.
//   - The user message that produced this assistant is the row whose
//     id == assistant.parent_message_id.
//
// On every terminal — success, agent error, max turns, budget
// exceeded, panic — the worker MUST call FinalizeChatMessage so the
// session lock is released. Phase 3.5's lease sweeper picks up
// orphans (worker crash, deploy mid-run); this worker just needs to
// not leave the session locked when it exits cleanly.

package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	memaxagent "github.com/MemaxLabs/memax-go-agent-sdk"
	sdkhook "github.com/MemaxLabs/memax-go-agent-sdk/hook"
	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
	"github.com/MemaxLabs/memax-go-agent-sdk/toolkit/agentpolicy"
	sdkapproval "github.com/MemaxLabs/memax-go-agent-sdk/toolkit/approvaltools"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// ChatAgentRuntime bundles the dependencies the chat worker needs
// to invoke agent.Run. Mirrors dreams.AgentRuntime — both surfaces
// share the same SDK provider client + recall service + scope
// resolver, but each owns its own bundle so a future divergence
// (chat-only tools, dream-only tools) doesn't tangle the two.
//
// **Nil semantics:** when ChatAgentRuntime is nil OR any field is
// nil, ChatMessageRunWorker.Work writes a structured
// `runtime_not_configured` failed status to the assistant row
// (which releases the session lock via FinalizeChatMessage) and
// returns river.JobCancel — the job gets cancelled, not retried.
// Operationally this means: if the worker is deployed without an
// Anthropic key set, the user sees an immediate "this chat
// failed" state with a clear error code, NOT a stuck
// "thinking..." UI. Loud, observable, actionable.
type ChatAgentRuntime struct {
	// Models maps allowlisted chat-session model names to their
	// pre-built SDK provider clients. Each session row carries a
	// `model` value (validated at create-time against the
	// chatModelAllowlist); the worker picks the matching client
	// here. Pre-building at startup keeps the hot path free of
	// per-call client construction. A missing entry falls through
	// to DefaultModel — handled by ModelFor below.
	//
	// Codex's Phase 3.4b1 review caught the gap: the original
	// shape was a single `Model` field, which meant a session
	// created with Haiku still ran on global Sonnet. Per-session
	// model selection is a chat product requirement (free tier
	// runs Haiku, paid tier runs Sonnet); this map closes it.
	Models map[string]sdkmodel.Client

	// DefaultModel is the fallback client used when the session's
	// `model` value isn't in Models (a model name shipped before
	// the worker rolled out). Required when Models is non-empty;
	// IsConfigured() checks both.
	DefaultModel sdkmodel.Client

	// RecallService is the production recall pipeline implementation
	// the chat agent's recall_memories tool calls into. Required.
	RecallService tools.RecallService

	// BrowseService is the strict-hub read surface for list/get
	// tools: list_memories, get_memory, list_hubs, get_hub, and
	// list_topics. Required for the default chat tool profile.
	BrowseService *tools.AgentBrowseService

	// Resolver computes per-call SessionScope from the membership
	// store. Required.
	Resolver *agent.ScopeResolver

	// Approver builds per-turn SDK approvers for the chat
	// agent's approval-gated tools (push_memory, forget_memory,
	// propose_topic_merge — wired in Phase 3.6b3b/c). Optional
	// at the runtime level: when nil, the chat agent simply
	// has no approval-required tools available. The chat
	// session's tool list is filtered against the toolset
	// catalog earlier; tools whose ApprovalRequired=true never
	// reach the worker if Approver is nil (the catalog flips
	// their Available flag based on Approver presence at
	// startup wiring time).
	//
	// Per-turn: in Work(), after BeginChatMessageTurn but
	// before agent.Run, the worker calls
	// runtime.Approver.ForTurn(ownerID, assistantID) to obtain
	// a turnApprover bound to this message; that approver
	// drives the SDK's request_approval tool.
	Approver *approver.Service

	// PushService is the production-side dependency for
	// push_memory. Created at startup and shared across all
	// per-turn invocations. Phase 3.6b3d wires it; nil leaves
	// push_memory unavailable (the catalog still publishes
	// Available=true to clients, but tool construction fails
	// fast at session-start time with a clear error rather
	// than silently appearing).
	PushService tools.PushService

	// ForgetService is the production-side dependency for
	// forget_memory (Phase 3.6b3e). Same shape as PushService
	// — built once at startup, per-turn ForgetMemory calls
	// hard-delete the memory row. Nil makes forget_memory
	// fail at tool-construction with a clear error.
	ForgetService tools.ForgetService

	// ProposeTopicMergeService is the production-side
	// dependency for propose_topic_merge (Phase 3.6b3f).
	// Notification-native: writes a pending review_topic_merge
	// notification to the user's inbox, never mutates the
	// topic graph directly. Nil makes the tool fail at
	// construction with a clear error.
	ProposeTopicMergeService tools.ProposeTopicMergeService

	// FetchURLService is the production-side dependency for
	// fetch_url (Phase 3.6b3g). Wraps an SSRF-safe HTTP
	// client (internal/safefetch). Nil makes the tool fail
	// at construction with a clear error; that's a
	// production misconfiguration since the safefetch
	// transport has no external dependencies and should
	// always be wired.
	FetchURLService tools.FetchURLService
}

// IsConfigured reports whether the runtime has every dependency
// the agent path requires. Workers without it cancel jobs cleanly.
func (r *ChatAgentRuntime) IsConfigured() bool {
	return r != nil && r.DefaultModel != nil && r.RecallService != nil && r.BrowseService != nil && r.Resolver != nil
}

// ModelFor returns the SDK client for the given session model
// name, falling back to DefaultModel when the name isn't in the
// pre-built map. The fallback is loud but non-fatal: Phase 3.4b1
// pre-builds Sonnet 4.6 + Haiku 4.5 (the entire chat allowlist),
// so a fallback path firing in production means a new model name
// shipped without rebuilding the worker — log so SRE notices.
func (r *ChatAgentRuntime) ModelFor(name string) sdkmodel.Client {
	if r == nil {
		return nil
	}
	if c, ok := r.Models[name]; ok && c != nil {
		return c
	}
	if name != "" {
		slog.Warn("chat runtime: no client for session model, falling back to default",
			"session_model", name)
	}
	return r.DefaultModel
}

// ChatMessageRunWorker is the river.Worker that drives one
// assistant turn through the agent runtime to terminal.
type ChatMessageRunWorker struct {
	river.WorkerDefaults[ChatMessageRunArgs]

	Store   store.Store
	Runtime *ChatAgentRuntime

	// Signaler is the optional Redis pub/sub hint that wakes up
	// connected SSE clients when a new event lands in
	// chat_message_events. Phase 3.5b — DB stays the source of
	// truth; this is the live-delivery accelerator over the
	// 250ms poll fallback. Nil-safe: when unset (no Redis),
	// streams stay on the poll cadence with no behavioral
	// regression.
	Signaler *chatstream.Signaler
}

// Timeout is generous because the chat profile allows ≤120s of
// agent work plus we need headroom for the load + finalize
// round-trips. River cancels the job ctx if Work doesn't return
// by then; the top-of-Work `defer` block catches that case (sees
// finalized=false, runs FinalizeChatMessage on a fresh detached
// 5s ctx with status='failed' and code 'worker_terminated_unexpectedly').
// The lease sweeper (Phase 3.5) catches the case where THIS
// process dies before the timeout — same finalization shape via
// the sweeper's path.
func (w *ChatMessageRunWorker) Timeout(*river.Job[ChatMessageRunArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *ChatMessageRunWorker) Work(ctx context.Context, job *river.Job[ChatMessageRunArgs]) error {
	args := job.Args

	// Belt-and-braces finalize-on-exit guard. River cancels the
	// job context on timeout / shutdown, and a model provider
	// hang or a panic anywhere in the body below would leave
	// the message stuck in 'queued' (and the session locked)
	// until the lease sweeper fires. Codex's Phase 3.4a review
	// flagged the gap; this guard closes it by:
	//
	//   - capturing a panic into a structured error
	//   - on exit, IF the message is still non-terminal,
	//     finalizing it with a fresh (detached + 5s) context so
	//     a cancelled job ctx doesn't also kill the cleanup
	//
	// The CAS gate inside FinalizeChatMessage (Phase 3.4a v2)
	// makes this idempotent: a cleanup-finalize after a
	// happy-path finalize is a no-op (returns ErrChatMessageAlreadyFinalized).
	finalized := false
	// cancelObserved is set by the per-turn cancel watcher when it
	// sees the assistant row transition to status='canceling'. The
	// happy-path finalize and the deferred safety-net both read it
	// to decide between status='canceled' (user cancel) and
	// status='failed' (any other terminal). Atomic because the
	// watcher writes from its own goroutine while the main path
	// reads after agent.Run returns.
	var cancelObserved atomic.Bool
	// fail is the local closure every pre-flight reject path
	// uses. Wraps w.failMessage and ONLY sets `finalized=true`
	// if the persistence actually landed (or returned a benign
	// "already-finalized" / "not-found" race-loss); a true error
	// leaves `finalized=false` so the deferred cleanup retries
	// with a fresh detached context. Codex's v2 review caught
	// the gap where the prior version optimistically set
	// `finalized` and skipped the cleanup retry.
	fail := func(code, msg string) {
		if w.failMessage(ctx, args, code, msg) {
			finalized = true
		}
	}
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "chat run: worker panic",
				"assistant_id", args.AssistantMessageID, "panic", r)
		}
		if finalized {
			return
		}
		// Detached context — survives the job's cancellation.
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// Cancel-aware terminal selection: if the watcher saw the
		// row flip to 'canceling' before we got here, the user-
		// intended terminal is 'canceled', not the generic
		// 'worker_terminated_unexpectedly' failure. Surfacing this
		// distinction matters because the UI renders cancel
		// differently from failure (no error toast, no retry CTA),
		// and downstream usage accounting may treat user-canceled
		// turns differently from worker faults.
		fin := buildChatCleanupFinalize(cancelObserved.Load())
		err := w.Store.FinalizeChatMessage(bg, args.AssistantMessageID, args.OwnerID, fin)
		if errors.Is(err, store.ErrChatMessageCancelPending) {
			// A cancel raced our cleanup write — observed via the
			// store's CAS gate, not the watcher (the watcher may
			// have already exited via runCtx cancellation before
			// the cancel committed). Re-issue as canceled. Codex's
			// Phase 3.7b review caught this defense-in-depth gap.
			canceled := buildChatCanceledFinalize(agent.RunResult{},
				"user_canceled",
				"the user canceled this turn (cleanup path)")
			err = w.Store.FinalizeChatMessage(bg, args.AssistantMessageID, args.OwnerID, canceled)
		}
		if err != nil {
			if errors.Is(err, store.ErrChatMessageAlreadyFinalized) || errors.Is(err, store.ErrChatMessageNotFound) {
				return // someone else won, that's fine
			}
			slog.ErrorContext(bg, "chat run: cleanup finalize failed",
				"assistant_id", args.AssistantMessageID, "err", err)
			return
		}
		// Phase 3.5b — wake any subscribed SSE client so they
		// see the failed terminal frame immediately. Detached
		// context (the job ctx may be cancelled).
		w.Signaler.Notify(bg, args.AssistantMessageID)
	}()

	// 1. Load the assistant message FIRST — before the runtime
	// check. Order matters: a job for a message we don't own
	// (cross-owner due to a misrouted enqueue, or a sweeper-
	// finalized row) should NOT cause us to write a failed
	// status, because there's no row we have authority to
	// touch. Same for already-finalized: we must not refinalize
	// over a real run's terminal state.
	assistant, err := w.Store.GetChatMessage(ctx, args.AssistantMessageID, args.OwnerID)
	if errors.Is(err, store.ErrChatMessageNotFound) {
		slog.WarnContext(ctx, "chat run: assistant not found (cross-owner or already finalized)",
			"assistant_id", args.AssistantMessageID, "owner_id", args.OwnerID)
		return river.JobCancel(err)
	}
	if err != nil {
		return fmt.Errorf("load assistant message: %w", err)
	}
	if assistant.Status == model.ChatMessageStatusCanceling {
		// Cancel landed BEFORE the worker picked the job up. We
		// own the only path that can move the row to a terminal
		// state from here, so the sweeper would otherwise wait out
		// the lease before writing 'failed{server_crashed}' — a bad
		// cancel UX (multi-minute delay, wrong terminal). Finalize
		// as 'canceled' immediately. Idempotent against any other
		// finalize that might race in: FinalizeChatMessage's CAS
		// gate makes this a no-op if someone else won.
		bg, cancelBg := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelBg()
		fin := buildChatCanceledFinalize(agent.RunResult{}, "user_canceled_before_pickup",
			"the user canceled the message before the worker started")
		if err := w.Store.FinalizeChatMessage(bg, args.AssistantMessageID, args.OwnerID, fin); err != nil {
			if !errors.Is(err, store.ErrChatMessageAlreadyFinalized) && !errors.Is(err, store.ErrChatMessageNotFound) {
				slog.ErrorContext(bg, "chat run: pre-pickup cancel finalize failed",
					"assistant_id", args.AssistantMessageID, "err", err)
				// Leave finalized=false so the deferred guard retries.
				return river.JobCancel(fmt.Errorf("pre-pickup cancel finalize: %w", err))
			}
		}
		w.Signaler.Notify(bg, args.AssistantMessageID)
		finalized = true
		return nil
	}
	if assistant.Status != model.ChatMessageStatusQueued {
		// Already finalized (sweeper, prior worker run, regenerate
		// flow). Skip cleanly — and tell the deferred guard not to
		// touch it; the row already has its terminal state.
		slog.InfoContext(ctx, "chat run: assistant not queued, skipping",
			"assistant_id", assistant.ID, "status", assistant.Status)
		finalized = true
		return nil
	}

	// 2. Now that we know the row is ours and queued, check
	// runtime configuration. A nil runtime means we MUST mark
	// the message as failed — otherwise the user sees a stuck
	// "thinking..." state and the session lock stays held until
	// the lease sweeper finalizes it (same effect, much worse
	// UX). JobCancel so River doesn't retry.
	if !w.Runtime.IsConfigured() {
		fail("runtime_not_configured",
			"the agent runtime is not configured on this worker")
		return river.JobCancel(fmt.Errorf("chat runtime not configured"))
	}
	if assistant.ParentMessageID == "" {
		// Phase 3.3 always sets parent_message_id to the user
		// message; absence means the row was crafted by hand
		// (test fixture) or a future code path bypassed
		// BeginChatMessageTurn. Fail-closed.
		fail("missing_parent_user_message",
			"assistant message has no parent_message_id")
		return river.JobCancel(fmt.Errorf("assistant %s missing parent_message_id", assistant.ID))
	}

	// 2a. Load the chat session so we can pick the right model.
	// Codex's Phase 3.4b1 review caught the gap: the original
	// shape used a process-wide CHAT_MODEL constant, which meant a
	// session created with Haiku still ran on Sonnet. The session
	// row's `model` field is validated at create-time against the
	// chat allowlist, so we trust it here without re-validating.
	//
	// **Use assistant.SessionID, not args.SessionID.** The
	// assistant row IS the source of truth for which session
	// this turn belongs to (it's a NOT NULL FK by schema).
	// args.SessionID is set by the handler at enqueue-time and
	// could in theory drift if a future enqueue path bypasses
	// the assistant lookup. Codex's v2 review caught this.
	if args.SessionID != "" && args.SessionID != assistant.SessionID {
		// Job's session_id doesn't match the row's. This shouldn't
		// happen but indicates a malformed enqueue — fail-closed
		// rather than silently follow one over the other.
		fail("session_id_mismatch",
			"job session_id does not match assistant message's session_id")
		return river.JobCancel(fmt.Errorf("session_id mismatch: job=%s row=%s",
			args.SessionID, assistant.SessionID))
	}
	chatSess, err := w.Store.GetChatSession(ctx, assistant.SessionID, args.OwnerID)
	if err != nil {
		fail("session_unavailable",
			"could not load the chat session for this turn")
		return fmt.Errorf("load chat session: %w", err)
	}
	modelClient := w.Runtime.ModelFor(chatSess.Model)
	if modelClient == nil {
		fail("no_model_client",
			"the agent runtime has no provider client for this session's model")
		return river.JobCancel(fmt.Errorf("no model client for %q", chatSess.Model))
	}

	// 2b. Walk the parent chain to find the user message that
	// drives THIS turn's prompt. The shape of the walk depends on
	// regenerate depth:
	//
	//   - Fresh send: assistant.parent → user (one hop).
	//   - First regenerate: A2.parent → A1 → user (two hops).
	//   - N-th regenerate: An.parent → An-1 → ... → A1 → user
	//     (N+1 hops). Each regenerate creates a NEW assistant whose
	//     parent points at the PRIOR ASSISTANT (supersession link),
	//     so chains can grow arbitrarily deep as the user clicks
	//     "Regenerate" repeatedly.
	//
	// The walk is bounded by chatRegenerateChainMaxHops to defend
	// against pathologically deep chains AND any future cycle (which
	// the schema prevents today via the parent-same-session FK, but
	// belt-and-braces). Cross-session and cycle checks fire on every
	// hop.
	//
	// Codex's Phase 3.7b v2 review caught the bug where v1 only
	// walked one extra hop, breaking the second regenerate.
	userMsg, err := w.resolveTurnUserMessage(ctx, assistant, args.OwnerID)
	if err != nil {
		// resolveTurnUserMessage returns a structured *chatChainError
		// describing the failure code; callers map it onto the
		// failMessage path with the same code.
		var cerr *chatChainError
		if errors.As(err, &cerr) {
			fail(cerr.code, cerr.message)
			return river.JobCancel(cerr)
		}
		fail("parent_user_message_unavailable",
			"could not load the user message that produced this turn")
		return fmt.Errorf("resolve turn user message: %w", err)
	}
	if strings.TrimSpace(userMsg.Content) == "" {
		fail("empty_user_prompt", "the user message has no content")
		return river.JobCancel(fmt.Errorf("user message %s has empty content", userMsg.ID))
	}
	afterSequence := userMsg.Sequence - chatSDKTranscriptWindow
	if afterSequence < 0 {
		afterSequence = 0
	}
	history, _, err := w.Store.ListChatMessagesBySession(ctx, store.ChatMessageListOpts{
		SessionID:         assistant.SessionID,
		OwnerID:           args.OwnerID,
		AfterSequence:     afterSequence,
		Limit:             chatSDKTranscriptWindow,
		IncludeSuperseded: false,
	})
	if err != nil {
		fail("chat_history_unavailable",
			"could not load this session's prior chat history")
		return fmt.Errorf("load chat session history: %w", err)
	}
	sdkSessions := newChatSDKSessionStore(chatSess, history, userMsg.Sequence)

	// 3. Construct the scope-bound recall_memories tool. The
	// per-turn ScopeHubIDsUsed snapshot (set by BeginChatMessageTurn
	// inside FOR UPDATE) is the source of truth — NOT the session's
	// frozen scope_hub_ids — because plan 24's read-time redaction
	// pivots on this stamp, and the runtime's tool resolution must
	// match what the redaction logic will eventually use.
	if len(assistant.ScopeHubIDsUsed) == 0 {
		// Should be impossible (Phase 3.3 rejects empty scope at
		// BeginChatMessageTurn time via ErrChatSessionNoAccessibleHubs)
		// but defensive — a hand-crafted row could trigger it.
		fail("empty_scope",
			"assistant message has no scope_hub_ids_used; cannot bind recall tool")
		return river.JobCancel(fmt.Errorf("assistant %s has empty scope_hub_ids_used", assistant.ID))
	}
	scopeType, err := chatScopeTypeFromMessage(assistant)
	if err != nil {
		fail("invalid_scope_shape", err.Error())
		return river.JobCancel(err)
	}
	scopeHubIDs := append([]string(nil), assistant.ScopeHubIDsUsed...)
	descriptor := buildSessionDescriptor(args.OwnerID, scopeType, scopeHubIDs, chatSess.WriteHubID)

	// Phase 3.6b3c — per-turn approver MUST be built BEFORE
	// buildSessionTools. The request_approval tool (registered
	// inside buildSessionTools when any session tool requires
	// approval) closes over the per-turn approver; if we built
	// tools first, the approver wouldn't exist yet. Codex's
	// Phase 3.6b3b review caught this ordering trap before it
	// became a refactor.
	turnSeq := &chatTurnSeq{}
	streamObserver := newChatStreamObserver(w.Store, args.OwnerID, args.AssistantMessageID, turnSeq).withSignaler(w.Signaler)

	// Per-turn approver. Built even when no approval-gated
	// tools exist; the cost is one struct allocation. The
	// approver is unused at the SDK level (no agent code path
	// invokes it) until the session's tools include at least
	// one ApprovalRequired entry — that's when buildSessionTools
	// registers the SDK's request_approval tool wired to this
	// approver, and the ApprovalBeforeTool policy gates the
	// mutating tool calls.
	var turnApprover sdkapproval.Approver
	if w.Runtime.Approver != nil {
		emitter := &chatApprovalEventEmitter{
			store:     w.Store,
			signaler:  w.Signaler,
			ownerID:   args.OwnerID,
			messageID: args.AssistantMessageID,
			seq:       turnSeq,
		}
		turnApprover = w.Runtime.Approver.ForTurn(approver.TurnConfig{
			OwnerID:   args.OwnerID,
			MessageID: args.AssistantMessageID,
			Emitter:   emitter,
		})
	}

	// Build the per-turn tool set from session.Tools, intersected
	// with what this worker can actually construct. Codex's
	// Phase 3.6a v3 review pinned the gap: v2 hardcoded
	// recall_memories regardless of session.Tools, so a session
	// with `tools: []` (legal per Phase 3.6a v2 patch test)
	// would still get recall fired. Now the worker derives the
	// agent's tool list from chatSess.Tools — exact match to
	// the catalog contract clients see.
	//
	// chatToolBuilders is the worker-side registry of available
	// tool wrappers; it must stay in sync with
	// handler.chatToolCatalog's Available=true entries. When
	// 3.6b/3.6c lands new tool wrappers, both sides flip
	// together (catalog Available=true + new builder here).
	//
	// 3.6b3c: the per-turn context carries turnApprover so
	// builders for ApprovalRequired tools can register the
	// SDK's request_approval tool alongside, and so the worker
	// can derive the ApprovalBeforeTool policy for the agent's
	// hook runner.
	agentTools, hookRunner, err := buildSessionTools(w.Runtime, descriptor, chatSess.Tools, chatTurnContext{
		TurnApprover: turnApprover,
		// Fresh tracker per turn. State must not leak across
		// turns — a memory id forgotten in turn N must not
		// short-circuit a turn N+1 call on the same id.
		ForgottenIDs: &tools.ForgottenTurnTracker{},
	})
	if err != nil {
		fail("tool_construction_failed", err.Error())
		return fmt.Errorf("build session tools: %w", err)
	}
	if len(agentTools) == 0 {
		// Session opted out of tools (or all opted-in tools are
		// no longer Available). agent.Run requires ≥1 tool —
		// this run can't proceed. Fail the message with a
		// structured error so the user sees a clean "enable a
		// tool first" UI instead of a stuck "thinking…" state.
		// 3.6c+ may add a single-call no-tools code path; until
		// then this is the contract.
		fail("no_tools_enabled",
			"this chat session has no available tools enabled; enable at least one before sending")
		return river.JobCancel(fmt.Errorf("session %s has no available tools", chatSess.ID))
	}

	// 4. Run the agent. Phase 3.4b2.a wires the streaming
	// observer: each SDK event is mirrored into chat_message_events
	// so a disconnected client can resume the SSE stream from
	// Last-Event-ID. Phase 3.4b2.b lands the SSE handler that
	// reads from this buffer + tails live events.
	//
	// Phase 3.6b3c: hookRunner gates approval-required tools
	// via agentpolicy.ApprovalBeforeTool. nil hook runner = no
	// policy installed (read-only sessions); the SDK runs
	// the agent path unchanged.
	//
	// Phase 3.7b — cancel watcher. We derive a child context
	// from the worker's ctx and pass it to agent.Run so a user-
	// initiated cancel can short-circuit the model loop without
	// killing the worker's own cleanup paths (which run on the
	// parent ctx, then a detached background ctx in the deferred
	// safety net). The watcher subscribes to the chat-stream
	// signaler and, when no signaler is wired or pub/sub stalls,
	// polls the assistant row periodically; on observing
	// status='canceling' it cancels runCtx so agent.Run unwinds
	// promptly. The atomic flag steers the post-Run finalize
	// onto the canceled-shape path.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		cw := &chatCancelWatcher{
			store:     w.Store,
			signaler:  w.Signaler,
			ownerID:   args.OwnerID,
			messageID: args.AssistantMessageID,
		}
		if cw.run(runCtx) {
			cancelObserved.Store(true)
			cancelRun()
		}
	}()
	res, runErr := agent.Run(runCtx, agent.RunInput{
		Prompt:       userMsg.Content,
		Profile:      agent.ProfileChat,
		Tools:        agentTools,
		Model:        modelClient,
		Session:      descriptor,
		SessionID:    chatSess.ID,
		Sessions:     sdkSessions,
		SystemPrompt: w.buildChatSystemPrompt(ctx, args.OwnerID, scopeHubIDs, chatSess),
		OnEvent:      streamObserver.observe,
		Hooks:        hookRunner,
	})
	// Stop the watcher and wait for it to drain. cancelRun() is a
	// no-op after the watcher already triggered it; calling it
	// unconditionally keeps the cleanup path symmetric. The
	// watcher's run() exits on runCtx.Done so the join is bounded
	// by ctx propagation latency (microseconds).
	cancelRun()
	<-watcherDone
	if runErr != nil {
		// Pre-flight error — no model calls happened. If the
		// watcher fired before agent.Run could even start (cancel
		// landed in the same window the worker was constructing
		// its run input), the most accurate terminal is still
		// 'canceled' rather than 'failed{preflight_error}'.
		if cancelObserved.Load() {
			fin := buildChatCanceledFinalize(res, "user_canceled", "the user canceled this turn")
			if err := w.Store.FinalizeChatMessage(ctx, args.AssistantMessageID, args.OwnerID, fin); err != nil {
				if errors.Is(err, store.ErrChatMessageAlreadyFinalized) {
					finalized = true
					return nil
				}
				slog.ErrorContext(ctx, "chat run: cancel-on-preflight finalize failed",
					"assistant_id", args.AssistantMessageID, "err", err)
				return river.JobCancel(fmt.Errorf("cancel-on-preflight finalize: %w", err))
			}
			finalized = true
			w.Signaler.Notify(ctx, args.AssistantMessageID)
			return nil
		}
		fail("agent_preflight_error", runErr.Error())
		return fmt.Errorf("agent.Run preflight: %w", runErr)
	}

	// 5. Map RunResult to a ChatMessageFinalize. Even on non-success
	// terminals we persist whatever ToolCalls + Usage actually
	// happened; the user has paid for those round-trips and we
	// owe them a record of what ran. The Status mapping is the
	// only behavioral fork.
	//
	// Cancel-aware: when the watcher saw 'canceling' during the
	// run, route to the canceled-shape finalize regardless of
	// what RunResult says. The SDK may have observed the ctx
	// cancellation as a generic error (non-success status) and
	// without this branch the user would see 'failed' for an
	// action they explicitly initiated. Whatever streamed before
	// cancel still lands in the row (Content/ToolCalls/Usage),
	// so the UI can render the partial transcript.
	if cancelObserved.Load() {
		fin := buildChatCanceledFinalize(res, "user_canceled", "the user canceled this turn")
		if err := w.Store.FinalizeChatMessage(ctx, args.AssistantMessageID, args.OwnerID, fin); err != nil {
			if errors.Is(err, store.ErrChatMessageAlreadyFinalized) {
				slog.InfoContext(ctx, "chat run: cancel finalize lost race; row already terminal",
					"assistant_id", args.AssistantMessageID)
				finalized = true
				return nil
			}
			slog.ErrorContext(ctx, "chat run: cancel finalize failed",
				"assistant_id", args.AssistantMessageID, "err", err)
			return river.JobCancel(fmt.Errorf("cancel finalize: %w", err))
		}
		finalized = true
		w.Signaler.Notify(ctx, args.AssistantMessageID)
		slog.InfoContext(ctx, "chat run: canceled by user",
			"assistant_id", args.AssistantMessageID,
			"model_calls", res.ModelCalls,
			"tool_calls", len(res.ToolCalls),
			"input_tokens", res.Usage.InputTokens,
			"output_tokens", res.Usage.OutputTokens,
		)
		return nil
	}

	finalize, finalizeErr := buildChatMessageFinalize(res, runErr)
	err = w.Store.FinalizeChatMessage(ctx, args.AssistantMessageID, args.OwnerID, finalize)
	if errors.Is(err, store.ErrChatMessageCancelPending) {
		// Race-edge: cancel landed AFTER agent.Run returned but
		// BEFORE this finalize committed (the watcher's signaler
		// wake fired into a closed runCtx, so cancelObserved was
		// never set). The CAS gate caught it; rewrite as canceled
		// preserving whatever the agent actually produced before
		// the user pulled the plug. Codex's Phase 3.7b review
		// caught the gap.
		canceled := buildChatCanceledFinalize(res, "user_canceled",
			"the user canceled this turn (post-run race)")
		err = w.Store.FinalizeChatMessage(ctx, args.AssistantMessageID, args.OwnerID, canceled)
	}
	if err != nil {
		if errors.Is(err, store.ErrChatMessageAlreadyFinalized) {
			// A racing finalize (cancel, sweeper) won. Our
			// run's terminal state is dropped on the floor —
			// safe because the user-visible state on the row
			// is already terminal. Mark finalized so the defer
			// doesn't try again; return nil to River.
			slog.InfoContext(ctx, "chat run: lost finalize race, terminal state preserved",
				"assistant_id", args.AssistantMessageID)
			finalized = true
			// The race winner already published; no-op here.
			return nil
		}
		// We did the LLM work but couldn't persist — log loudly
		// so SRE can recover (the lease sweeper will eventually
		// reset the lock). Don't return to River as an error
		// because retrying would re-spend the user's quota.
		slog.ErrorContext(ctx, "chat run: finalize failed (lease sweeper will recover lock)",
			"assistant_id", args.AssistantMessageID, "error", err)
		return river.JobCancel(fmt.Errorf("finalize: %w", err))
	}
	finalized = true

	// Phase 3.4b2.b v2 — the terminal SSE row is now written
	// inside FinalizeChatMessage's transaction (postgres_chat.go).
	// The worker no longer appends a separate terminal row here;
	// codex caught the race in v1 where a stream poll between
	// FinalizeChatMessage commit and a separate terminal write
	// would see status=terminal + empty events and exit without
	// delivering the terminal frame.
	//
	// Phase 3.5b — wake up subscribed SSE clients so they
	// pick up the terminal row immediately rather than waiting
	// for the next 250ms poll tick.
	w.Signaler.Notify(ctx, args.AssistantMessageID)

	if finalizeErr != nil {
		// Successful persistence of a non-success terminal.
		// Return nil to River so the job is marked succeeded (the
		// terminal state is captured on the row, not the job).
		slog.InfoContext(ctx, "chat run: terminated non-success",
			"assistant_id", args.AssistantMessageID,
			"status", finalize.Status, "error", finalizeErr)
		return nil
	}

	slog.InfoContext(ctx, "chat run: completed",
		"assistant_id", args.AssistantMessageID,
		"model_calls", res.ModelCalls,
		"tool_calls", len(res.ToolCalls),
		"input_tokens", res.Usage.InputTokens,
		"output_tokens", res.Usage.OutputTokens,
	)
	return nil
}

// failMessage finalizes the assistant row with a structured error.
// Used on every pre-flight reject path so the user sees a clean
// "this turn failed" state instead of a stuck "thinking…" — and
// so the session lock gets released the same way a successful run
// would release it.
//
// **Returns true** when the row is now durably terminal (either
// this call's UPDATE landed, or another finalize had already won,
// or a concurrent cancel preempted us and we re-finalized as
// canceled). **Returns false** when the persistence attempt
// errored — the caller MUST leave `finalized = false` so the
// deferred cleanup retries with a fresh detached context. Codex's
// Phase 3.4a v2 review caught a gap where this returned void and
// `fail()` would optimistically set `finalized = true`, masking a
// failed persistence and leaving the same stuck-lock class the
// defer was meant to close.
//
// **Cancel preemption.** Codex's Phase 3.7b review caught a race
// where a cancel landing between worker pickup and watcher setup
// could have its 'canceling' state stomped by a 'failed' write
// from this path. The store now returns ErrChatMessageCancelPending
// when a non-canceled finalize hits a canceling row; this function
// recognizes that sentinel and re-issues the finalize as 'canceled'
// — the user asked for it, that's the correct terminal.
//
// **Detached context.** We deliberately do NOT take the worker's
// `ctx` here — a pre-flight reject path can land while River has
// already cancelled the job ctx (timeout / shutdown), which would
// kill the persistence the user really needs. The detached + 5s
// pattern matches the deferred cleanup; both run on a context
// that can't be killed by the same cancellation that triggered
// the exit.
func (w *ChatMessageRunWorker) failMessage(parent context.Context, args ChatMessageRunArgs, code, msg string) bool {
	bg, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	errPayload, _ := json.Marshal(map[string]string{"code": code, "message": msg})
	finalize := store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusFailed,
		Content:    "",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		Error:      errPayload,
		FinishedAt: time.Now().UTC(),
	}
	err := w.Store.FinalizeChatMessage(bg, args.AssistantMessageID, args.OwnerID, finalize)
	if err == nil {
		// Phase 3.5b — wake subscribed SSE clients so the
		// failed terminal frame lands without polling delay.
		w.Signaler.Notify(bg, args.AssistantMessageID)
		return true
	}
	// "Already finalized" still satisfies the "row is terminal"
	// invariant — another finalize won the race; we don't need to
	// retry from the deferred cleanup.
	if errors.Is(err, store.ErrChatMessageAlreadyFinalized) || errors.Is(err, store.ErrChatMessageNotFound) {
		return true
	}
	if errors.Is(err, store.ErrChatMessageCancelPending) {
		// Cancel landed while we were trying to write 'failed'.
		// Re-issue as 'canceled' — that's the user-intent terminal.
		// The original error code is preserved in the canceled
		// finalize so logs still show what the worker observed.
		canceled := buildChatCanceledFinalize(agent.RunResult{},
			"user_canceled",
			"the user canceled this turn (preflight: "+code+")")
		if err2 := w.Store.FinalizeChatMessage(bg, args.AssistantMessageID, args.OwnerID, canceled); err2 != nil {
			if errors.Is(err2, store.ErrChatMessageAlreadyFinalized) || errors.Is(err2, store.ErrChatMessageNotFound) {
				return true
			}
			slog.ErrorContext(bg, "chat run: failMessage canceled retry errored (deferred cleanup will retry)",
				"assistant_id", args.AssistantMessageID, "code", code, "err", err2)
			return false
		}
		w.Signaler.Notify(bg, args.AssistantMessageID)
		return true
	}
	slog.ErrorContext(bg, "chat run: failMessage finalize errored (deferred cleanup will retry)",
		"assistant_id", args.AssistantMessageID, "code", code, "err", err)
	return false
}

// buildChatCleanupFinalize is the deferred-safety-net finalize.
// When the worker exits without finalizing through a happy or
// failure path (panic, ctx timeout, unexpected return), the
// deferred guard writes one of these to release the session lock.
//
// The shape forks on `canceled`: if the per-turn watcher saw the
// row flip to 'canceling' before exit, the user's intent was a
// cancel and we owe them that terminal — not a generic 'failed'
// with a worker_terminated error code that would render as a bug
// in the UI. Same row, two faithful representations of the same
// bottoming-out path.
func buildChatCleanupFinalize(canceled bool) store.ChatMessageFinalize {
	if canceled {
		return buildChatCanceledFinalize(agent.RunResult{}, "user_canceled",
			"the user canceled this turn")
	}
	errPayload, _ := json.Marshal(map[string]string{
		"code":    "worker_terminated_unexpectedly",
		"message": "the worker exited before finalizing this turn",
	})
	return store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusFailed,
		Content:    "",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		Error:      errPayload,
		FinishedAt: time.Now().UTC(),
	}
}

// buildChatCanceledFinalize shapes a status='canceled' terminal
// from whatever the agent had streamed before the cancel landed.
// Three distinct callers:
//
//   - Happy-path post-Run: the watcher fired during agent.Run and
//     RunResult carries partial Content/ToolCalls/Usage. Persist
//     all of it so the UI can show "I was working on this when
//     you stopped me" instead of a black hole.
//   - Pre-pickup cancel: the cancel landed before the worker even
//     loaded the row. RunResult is zero and the row gets minimal
//     bookkeeping.
//   - Deferred safety-net cancel: same shape as pre-pickup; the
//     worker bottomed out before agent.Run could populate any
//     state.
//
// The error blob is intentionally non-empty even on cancel: a
// machine-readable `code` lets clients distinguish "user cancel"
// from other terminals without status-string parsing, and the
// message gives logs a one-liner for grep.
func buildChatCanceledFinalize(res agent.RunResult, code, message string) store.ChatMessageFinalize {
	toolCallsJSON, _ := json.Marshal(serializeToolCalls(res.ToolCalls))
	usageJSON, _ := json.Marshal(res.Usage)
	errPayload, _ := json.Marshal(map[string]string{
		"code":    code,
		"message": message,
	})
	return store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCanceled,
		Content:    res.Final,
		ToolCalls:  toolCallsJSON,
		Citations:  json.RawMessage(`[]`),
		Usage:      usageJSON,
		Error:      errPayload,
		FinishedAt: time.Now().UTC(),
	}
}

// chatRegenerateChainMaxHops bounds the parent-walk depth when
// resolving the user message that drives a regenerate turn. Each
// regenerate creates a new assistant whose parent is the PRIOR
// ASSISTANT, so an N-times-regenerated turn forms a chain of N+1
// assistants ending at the original user message.
//
// 50 hops is comfortably above any realistic user behavior — even
// power users rarely hit ten regenerates on the same message — and
// well below the 250-message session hard cap. Hitting this limit
// indicates a corrupt parent chain or a malicious construction;
// either way, fail-closed rather than walk indefinitely.
const chatRegenerateChainMaxHops = 50

// chatChainError carries a structured failure-code/message pair
// from the parent-walk back to the worker entry. It satisfies the
// error interface so callers can use errors.As to extract the
// code without parsing strings.
type chatChainError struct {
	code    string
	message string
}

func (e *chatChainError) Error() string {
	return e.code + ": " + e.message
}

// resolveTurnUserMessage walks the assistant's parent chain to
// find the user message whose content drives this turn's prompt.
// See the inline comment in Work() for chain shapes and depth
// semantics; this helper enforces depth bound, cycle detection,
// same-session invariant, and role expectations.
//
// The chain MUST terminate at a user-role message in the same
// session. Hops through assistant rows (regenerate supersession)
// are allowed and bounded; any other role (system, unknown) is
// fail-closed. A hop with a cleared parent_message_id means the
// chain is broken — we can't proceed without a user prompt.
func (w *ChatMessageRunWorker) resolveTurnUserMessage(ctx context.Context, assistant *model.ChatMessage, ownerID string) (*model.ChatMessage, error) {
	if assistant.ParentMessageID == "" {
		// Caller already screened for this; defensive.
		return nil, &chatChainError{
			code:    "missing_parent_user_message",
			message: "assistant message has no parent_message_id",
		}
	}
	visited := map[string]struct{}{
		assistant.ID: {},
	}
	currentID := assistant.ParentMessageID
	for hop := 0; hop < chatRegenerateChainMaxHops; hop++ {
		if _, seen := visited[currentID]; seen {
			// Cycle: A→B→A. Schema FK should prevent this but
			// belt-and-braces — never loop indefinitely on a
			// corrupt chain.
			return nil, &chatChainError{
				code:    "parent_chain_cycle",
				message: "parent chain contains a cycle",
			}
		}
		visited[currentID] = struct{}{}
		msg, err := w.Store.GetChatMessage(ctx, currentID, ownerID)
		if err != nil {
			return nil, &chatChainError{
				code:    "parent_user_message_unavailable",
				message: "could not load a parent message in the chain",
			}
		}
		if msg.SessionID != assistant.SessionID {
			return nil, &chatChainError{
				code:    "parent_chain_cross_session",
				message: "parent chain crosses sessions",
			}
		}
		switch msg.Role {
		case model.ChatMessageRoleUser:
			return msg, nil
		case model.ChatMessageRoleAssistant:
			// Supersession hop. Continue walking.
			if msg.ParentMessageID == "" {
				return nil, &chatChainError{
					code:    "regenerate_chain_broken",
					message: "an ancestor assistant has no parent; cannot resolve original user prompt",
				}
			}
			currentID = msg.ParentMessageID
		default:
			return nil, &chatChainError{
				code:    "parent_chain_invalid_role",
				message: "parent chain contains a message with an unexpected role",
			}
		}
	}
	return nil, &chatChainError{
		code:    "parent_chain_too_deep",
		message: "parent chain exceeds the regenerate depth limit",
	}
}

// chatCancelWatchPollInterval is the cancel watcher's fallback
// poll cadence. The signaler subscription is the primary path —
// a CancelChatMessage commit publishes on the per-message channel
// and the watcher reacts within milliseconds. Polling exists for
// the brown-out case (Redis unavailable, signaler is nil, pubsub
// stall) so cancel still lands in bounded time. 5s is a tradeoff
// between cancel latency and worker DB load: with chat-message
// cancels being rare events (user has to click a button, not a
// hot-loop trigger), 5s extra latency in the brown-out path is
// invisible UX-wise and keeps the per-turn DB cost at one extra
// SELECT every 5s of run time.
const chatCancelWatchPollInterval = 5 * time.Second

// chatCancelWatcher is the per-turn sentinel that watches the
// assistant row for status='canceling' and triggers the run-side
// ctx cancel when it sees the flip. One instance per Work() call;
// run() blocks until either the parent ctx is done (run finished
// normally) or it observes a cancel.
//
// **Why poll AND subscribe.** The signaler is the live-delivery
// hint over the DB; treating it as the only signal means a Redis
// brownout or a missing signaler wiring (dev environments) makes
// cancel non-functional. The DB is the source of truth — we poll
// it at chatCancelWatchPollInterval as the fallback. In a healthy
// system the watcher reacts to the signaler wake within <100ms;
// the poll just guarantees liveness.
//
// **Why GetChatMessage and not a status-only query.** The
// existing Store interface already has GetChatMessage with the
// owner check baked in; adding a one-off "GetChatMessageStatus"
// method would duplicate the auth surface for a hot-path query
// that fires at most twice (once on signaler wake, once on poll
// fallback). The minor row-fetch overhead is fine — it's one
// indexed lookup per ~5s, dwarfed by the agent's per-turn cost.
type chatCancelWatcher struct {
	store     store.Store
	signaler  *chatstream.Signaler // nil-safe
	ownerID   string
	messageID string
}

// run blocks until ctx is done OR the assistant row is observed in
// status='canceling'. Returns true ONLY when the cancel was
// observed (the caller uses this to decide whether to invoke
// cancelRun on the agent's child ctx). Returns false on any
// non-cancel exit — parent ctx done, agent finished normally,
// terminal status seen (which means another path won the
// finalize race; the main worker loop handles that case via
// FinalizeChatMessage's CAS gate, watcher just exits).
//
// Transient errors during the row fetch are logged and skipped:
// a flaky DB read shouldn't kill the watcher; the next signaler
// wake or poll tick retries. The watcher's job is best-effort
// observation — the user-facing safety net is FinalizeChatMessage
// + the deferred guard, not this loop.
func (cw *chatCancelWatcher) run(ctx context.Context) (canceled bool) {
	sub := cw.signaler.Subscribe(ctx, cw.messageID)
	if sub != nil {
		defer sub.Close()
	}
	var wakes <-chan struct{}
	if sub != nil {
		wakes = sub.Wakes()
	}
	poll := time.NewTicker(chatCancelWatchPollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-wakes:
		case <-poll.C:
		}
		m, err := cw.store.GetChatMessage(ctx, cw.messageID, cw.ownerID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return false
			}
			// Transient — log at debug and keep watching. A
			// persistent failure won't break cancel because
			// FinalizeChatMessage's CAS gate is the actual
			// terminal-state defender.
			slog.DebugContext(ctx, "chat cancel watcher: row fetch errored",
				"message_id", cw.messageID, "err", err)
			continue
		}
		if m.Status == model.ChatMessageStatusCanceling {
			return true
		}
		if store.IsChatMessageTerminal(m.Status) {
			// Row already terminal — another path won. Watcher's
			// job is done; the main worker loop's
			// FinalizeChatMessage call will get
			// ErrChatMessageAlreadyFinalized and short-circuit.
			return false
		}
	}
}

// chatTurnSeq is the per-turn monotonic sequence counter for
// chat_message_events rows. Both the SDK event observer (running
// on the drain goroutine) and the approval-required emitter
// (running on whichever goroutine the SDK tool handler picked
// up) share one of these per Work() call so their writes
// interleave with distinct seq values regardless of ordering.
//
// **Why a mutex, not just an atomic.** Codex's Phase 3.6b3b
// review caught a real bug in the v1 atomic-only design: the
// counter prevents duplicate seq values, but it does NOT
// prevent out-of-order COMMITS. Under MaxToolConcurrency=4,
// emitter A can allocate seq 8 and block in
// AppendChatMessageEvent while emitter/observer B allocates
// seq 9 and commits first. The SSE handler advances
// Last-Event-ID to 9, then seq 8 commits later — clients
// querying `seq > 9` will never see it, and an
// approval.required card can be invisible while the approver
// waits.
//
// Fix: serialize the WHOLE chain (allocate seq → append row →
// signal). LockedNext returns the next seq AND keeps the
// mutex held; the caller appends + signals + unlocks. This
// guarantees commit order matches allocation order. The lock
// is per-turn (one chatTurnSeq per Work() call); cross-session
// contention is zero, and within-session contention is
// bounded by the number of concurrent tool handlers (≤4).
//
// Append + signal hold the lock for one DB INSERT + one
// optional Redis PUBLISH — single-digit milliseconds at most.
// The SDK's tool concurrency would have to issue many
// approval requests in parallel to feel the cost, which is
// not a realistic scenario (request_approval is the
// gatekeeper for ALL gated tool calls; the agent typically
// requests approval one at a time).
type chatTurnSeq struct {
	mu  sync.Mutex
	val int
}

// LockedNext allocates the next sequence number AND returns a
// release func the caller MUST call (defer is the idiomatic
// shape) when its append + signal chain is done. The caller
// runs while holding the mutex, so commit order matches
// allocation order.
//
// Returning a release func instead of relying on (Lock, Next,
// Unlock) ordering keeps the caller from accidentally
// allocating without holding the lock for the append.
func (c *chatTurnSeq) LockedNext() (seq int, release func()) {
	c.mu.Lock()
	c.val++
	return c.val, c.mu.Unlock
}

// chatStreamObserver is the per-message replay-buffer sink for
// the chat agent's SDK event stream. Plan 24 §"Lifecycle policies"
// pins chat_message_events as a 24h replay buffer; when a client
// connects to GET /messages/{msg_id}/stream with Last-Event-ID,
// the SSE handler (Phase 3.4b2.b) reads from this buffer and
// then tails live events.
//
// One observer per turn, sharing a chatTurnSeq with the
// per-turn ChatApprovalEventEmitter so SDK events and host-
// emitted approval events interleave with distinct seq values.
//
// **Persistence-failure semantics.** observe is the
// agent.RunInput.OnEvent callback — its contract says errors
// are observability-only. A transient DB failure here logs at
// error and skips the row; the run continues. The user-visible
// terminal state still lands via FinalizeChatMessage; the only
// loss is that the disconnected-then-resumed client misses some
// replay events from the gap. Acceptable tradeoff: better than
// blocking the run on a slow buffer write.
type chatStreamObserver struct {
	store     store.Store
	ownerID   string
	messageID string
	seq       *chatTurnSeq
	signaler  *chatstream.Signaler // optional; nil-safe

	// Thinking-delta coalescing. Deltas stream in at token cadence;
	// persisting each one as a replay row would bloat the buffer, so
	// the observer accumulates and flushes a partial row at most every
	// thinkingFlushInterval / thinkingFlushChars. Each partial carries
	// the FULL text so far (idempotent — the client replaces, never
	// appends), and the completed artifact row carries the final text
	// plus duration_ms. OnEvent is invoked sequentially per run, so
	// these fields need no locking.
	thinkingBuf        strings.Builder
	thinkingStart      time.Time
	thinkingLastFlush  time.Time
	thinkingFlushedLen int
}

// Coalescing tuning: flush at most ~1.4×/sec or every 400 new chars —
// live enough to read as streaming, cheap enough for the replay buffer.
const (
	thinkingFlushInterval = 700 * time.Millisecond
	thinkingFlushChars    = 400
)

func newChatStreamObserver(s store.Store, ownerID, messageID string, seq *chatTurnSeq) *chatStreamObserver {
	if seq == nil {
		seq = &chatTurnSeq{}
	}
	return &chatStreamObserver{
		store:     s,
		ownerID:   ownerID,
		messageID: messageID,
		seq:       seq,
	}
}

// withSignaler attaches the Phase 3.5b live-delivery hint.
// Returns the observer for chaining: every successful row
// append fires a Redis PUBLISH on `chat:stream:<message_id>`
// so subscribed SSE clients can re-query the DB without
// waiting for the next poll tick.
func (o *chatStreamObserver) withSignaler(s *chatstream.Signaler) *chatStreamObserver {
	o.signaler = s
	return o
}

func (o *chatStreamObserver) observe(ctx context.Context, ev memaxagent.Event) error {
	// Thinking deltas coalesce in memory and flush partial rows on a
	// time/size budget — see the struct comment.
	if ev.Kind == memaxagent.EventThinkingDelta {
		if ev.ThinkingDelta == "" {
			return nil
		}
		now := time.Now()
		if o.thinkingBuf.Len() == 0 {
			o.thinkingStart = now
			o.thinkingLastFlush = now
		}
		o.thinkingBuf.WriteString(ev.ThinkingDelta)
		grown := o.thinkingBuf.Len() - o.thinkingFlushedLen
		if grown < thinkingFlushChars && now.Sub(o.thinkingLastFlush) < thinkingFlushInterval {
			return nil
		}
		o.thinkingLastFlush = now
		o.thinkingFlushedLen = o.thinkingBuf.Len()
		return o.appendRow(ctx, "thinking_delta", map[string]any{
			"text":    o.thinkingBuf.String(),
			"partial": true,
		})
	}
	// Provider artifacts without readable reasoning (redacted/encrypted
	// blocks, non-thinking artifact types) have nothing to render — skip
	// the row entirely rather than persist empty payloads in the replay
	// buffer.
	if ev.Kind == memaxagent.EventProviderArtifact {
		text := readableThinking(ev.ProviderArtifact)
		if text == "" {
			return nil
		}
		payload := map[string]any{"text": text}
		if !o.thinkingStart.IsZero() {
			payload["duration_ms"] = time.Since(o.thinkingStart).Milliseconds()
		}
		// Reset the accumulator — interleaved thinking can produce
		// multiple blocks per turn, each with its own duration.
		o.thinkingBuf.Reset()
		o.thinkingFlushedLen = 0
		o.thinkingStart = time.Time{}
		return o.appendRow(ctx, "provider_artifact", payload)
	}
	payload, err := marshalChatStreamPayload(ev)
	if err != nil {
		// Pathological — every relevant Event field is a JSON-
		// serializable type. If marshaling somehow fails, log
		// and continue; the user-facing terminal state is the
		// load-bearing record, not the per-event log.
		slog.WarnContext(ctx, "chat stream observe: marshal payload failed",
			"message_id", o.messageID, "kind", ev.Kind, "err", err)
		return nil
	}
	// Phase 3.6b3b codex-followup: LockedNext allocates the
	// next seq AND holds the mutex through append + signal so
	// commit order matches allocation order. Without this, an
	// approval emitter racing on a tool-handler goroutine could
	// commit seq=N+1 before the observer's seq=N lands, and an
	// SSE client reading past N would never see seq=N. See
	// chatTurnSeq doc for the full failure mode.
	seq, release := o.seq.LockedNext()
	defer release()
	// CreatedAt MUST be set explicitly. The store's INSERT binds
	// `ev.CreatedAt` directly (no DB default fallback), so a zero
	// time here would persist as `0001-01-01T00:00:00Z` — the 24h
	// purge sweeper would then treat the row as ancient and delete
	// fresh replay events immediately. Codex caught this on the
	// Phase 3.4b2.a v1 review.
	row := &model.ChatMessageEvent{
		MessageID: o.messageID,
		Seq:       seq,
		EventType: string(ev.Kind),
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	if err := o.store.AppendChatMessageEvent(ctx, o.ownerID, row); err != nil {
		// Non-fatal — see contract above. We log loudly so SRE
		// can spot a stuck buffer write under load (e.g., the
		// 24h purge worker isn't keeping up, or a Postgres
		// hiccup).
		slog.ErrorContext(ctx, "chat stream observe: AppendChatMessageEvent failed",
			"message_id", o.messageID, "seq", seq, "kind", ev.Kind, "err", err)
		return nil
	}
	// Phase 3.5b — wake up subscribed SSE clients. The signal
	// carries no payload (DB is source of truth); subscribers
	// re-query and read up to MAX(seq) on every wake. No-op
	// when signaler is nil (REDIS unset) → streams stay on the
	// 250ms poll fallback. We publish AFTER the DB write
	// commits so a wake-up never arrives before the row is
	// readable.
	o.signaler.Notify(ctx, o.messageID)
	return nil
}

// appendRow allocates the next seq, persists one replay row, and wakes
// SSE subscribers — the single write path every observer event uses.
func (o *chatStreamObserver) appendRow(ctx context.Context, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.WarnContext(ctx, "chat stream observe: marshal payload failed",
			"message_id", o.messageID, "kind", eventType, "err", err)
		return nil
	}
	seq, release := o.seq.LockedNext()
	defer release()
	row := &model.ChatMessageEvent{
		MessageID: o.messageID,
		Seq:       seq,
		EventType: eventType,
		Payload:   body,
		CreatedAt: time.Now().UTC(),
	}
	if err := o.store.AppendChatMessageEvent(ctx, o.ownerID, row); err != nil {
		slog.ErrorContext(ctx, "chat stream observe: AppendChatMessageEvent failed",
			"message_id", o.messageID, "seq", seq, "kind", eventType, "err", err)
		return nil
	}
	o.signaler.Notify(ctx, o.messageID)
	return nil
}

// readableThinking extracts human-readable reasoning text from a provider
// artifact. Anthropic thinking blocks carry it under "thinking" in the
// artifact JSON; redacted_thinking and other providers' encrypted blocks
// return "" (nothing to render).
func readableThinking(artifact *sdkmodel.ProviderArtifact) string {
	if artifact == nil || artifact.Type != "thinking" || len(artifact.Data) == 0 {
		return ""
	}
	var fields struct {
		Thinking string `json:"thinking"`
	}
	if err := json.Unmarshal(artifact.Data, &fields); err != nil {
		return ""
	}
	return strings.TrimSpace(fields.Thinking)
}

// marshalChatStreamPayload condenses an SDK event into a
// stable JSON shape for chat_message_events.payload. Per-kind
// fields are serialized only when non-nil so the payload stays
// minimal and the SSE handler doesn't have to filter empties.
//
// We deliberately do NOT serialize the entire memaxagent.Event
// struct — its shape is the SDK's contract, not ours, and the
// SDK can add fields between releases that would silently
// inflate every row. Selecting the fields the chat surface
// renders gives us a stable wire format under our control.
func marshalChatStreamPayload(ev memaxagent.Event) ([]byte, error) {
	out := map[string]any{}
	if ev.SessionID != "" {
		out["session_id"] = ev.SessionID
	}
	if ev.Turn != 0 {
		out["turn"] = ev.Turn
	}
	switch ev.Kind {
	case memaxagent.EventAssistant:
		// Per-message text — what the SSE client renders as
		// model.delta. SDK populates `ev.Message` for assistant
		// events (NOT `ev.Result`; that field is reserved for
		// the terminal `EventResult`). Codex's Phase 3.4b2.a v1
		// review caught the bug — without ev.Message.PlainText(),
		// the live assistant deltas would never make it into the
		// replay buffer and the SSE handler would have nothing
		// to stream until the terminal event landed.
		if ev.Message != nil {
			if text := ev.Message.PlainText(); text != "" {
				out["text"] = text
			}
		}
	case memaxagent.EventToolUse:
		if ev.ToolUse != nil {
			out["id"] = ev.ToolUse.ID
			out["name"] = ev.ToolUse.Name
			if len(ev.ToolUse.Input) > 0 {
				out["input"] = json.RawMessage(ev.ToolUse.Input)
			}
		}
	case memaxagent.EventToolResult:
		if ev.ToolResult != nil {
			out["tool_use_id"] = ev.ToolResult.ToolUseID
			out["name"] = ev.ToolResult.Name
			if ev.ToolResult.Content != "" {
				out["content"] = ev.ToolResult.Content
			}
			if ev.ToolResult.IsError {
				out["is_error"] = true
			}
		}
	case memaxagent.EventUsage:
		if ev.Usage != nil {
			out["input_tokens"] = ev.Usage.InputTokens
			out["output_tokens"] = ev.Usage.OutputTokens
			out["total_tokens"] = ev.Usage.TotalTokens
		}
	case memaxagent.EventResult:
		if ev.Result != "" {
			out["result"] = ev.Result
		}
	case memaxagent.EventError:
		if ev.Err != nil {
			out["error"] = ev.Err.Error()
		}
	}
	return json.Marshal(out)
}

// buildSessionTools constructs the agent.RunInput.Tools list for
// one turn. Iterates session.Tools in order, looks up each name
// in chatToolBuilders, and appends the constructed wrapper.
//
// **Fails loudly on missing names.** Phase 3.6a v4 review:
// silent-skip masked catalog/worker drift — a session opted
// into a tool that the worker can't build (rollback of an
// Available flag, programmer error, stale row) would have
// produced a degraded "answer" with the tool quietly absent.
// Now we return a structured error so the message lands in
// failed{tool_unavailable} state, surfacing the drift to the
// user (and to SRE via the deferred failMessage cleanup).
//
// 3.6b adds approval-gated tools to this registry; 3.6c the
// remaining read tools.
// chatTurnContext carries per-turn dependencies into the tool
// builders. Phase 3.6b3c added it so ApprovalRequired tool
// builders can close over the per-turn approver (used by
// buildSessionTools to register the SDK's request_approval
// tool alongside).
type chatTurnContext struct {
	// TurnApprover is the per-(message) sdkapproval.Approver
	// implementation. Nil when the runtime's approver service
	// is not configured (e.g., dev/staging without REDIS for
	// the broker subscription); buildSessionTools rejects
	// session toolsets with ApprovalRequired entries when this
	// is nil so the failure surfaces at construction time
	// rather than at agent.Run time.
	TurnApprover sdkapproval.Approver

	// ForgottenIDs is the turn-local tracker that lets a
	// model-driven retry of the same forget_memory(memory_id=…)
	// resolve to already_forgotten instead of NotFound. Wired
	// into tools.ForgetToolConfig at builder time. One tracker
	// per turn — sharing across turns would let stale ids leak
	// into the next turn's idempotency check.
	ForgottenIDs *tools.ForgottenTurnTracker
}

// buildSessionTools constructs the per-turn tool set from the
// session's opted-in tool list. Returns:
//
//   - agentTools: the sdktool.Tool slice to pass to agent.Run.
//     Includes the SDK's request_approval tool when any
//     session tool has ApprovalRequired=true.
//   - hookRunner: a *hook.Runner with the
//     agentpolicy.RequireApprovalBeforeTools policy when any
//     session tool has ApprovalRequired=true; nil otherwise.
//
// Plan 24 §"approval flow" pins the SDK semantics: the agent's
// model emits `tool_use` for `request_approval` first; our
// host-owned Approver (constructed in Work() before this
// function runs) blocks for the user's decide; once approved,
// `agentpolicy.ApprovalBeforeTool` consumes the grant on the
// subsequent mutating tool call.
//
// `WithInputBoundApprovals` is the security boundary: an
// approval is bound to the canonical args hash, so a "yes" for
// `forget_memory(memory_id=A)` cannot be reused to delete
// `memory_id=B`. Different action = different hash = fresh
// approval required.
//
// We INTENTIONALLY do not enable `WithSingleUseApprovals`. The
// original Phase 3.6b2 reasoning was to prevent a model from
// re-using one approval across multiple invocations, but the
// args-binding above already prevents the only abuse path that
// matters (running a DIFFERENT action under a stale grant).
// Single-use's only remaining effect was to demand re-approval
// when the model retries the SAME tool call with the SAME args
// — which is the model-loop pattern produced by tool_result
// processing hiccups in real agent runs. Users saw a wall of
// duplicate approval prompts for one delete because of it.
//
// Defense in depth for the redundant-retry case:
//   - bindInput pins each grant to a specific (tool, args) pair.
//   - The mutating tool wrappers themselves are idempotent at
//     the turn level (see forget_memory's deletedThisTurn
//     tracker). A second identical call returns the success
//     sentinel for "already done in this turn" instead of an
//     error the model would interpret as a failure and retry.
//   - The store layers below them are idempotent by design:
//     push_memory dedups by content hash, propose_topic_merge
//     short-circuits with already_* status codes, forget_memory
//     hard-deletes (so a second hit naturally NotFound's).
func buildSessionTools(runtime *ChatAgentRuntime, descriptor agent.SessionDescriptor, sessionTools []string, turn chatTurnContext) ([]sdktool.Tool, *sdkhook.Runner, error) {
	out := make([]sdktool.Tool, 0, len(sessionTools)+1)
	approvalRequiredNames := make([]string, 0, len(sessionTools))
	seenNames := make(map[string]struct{}, len(sessionTools)+1)

	// Reserve sdkapproval.ToolName ("request_approval") so a
	// future builder can't accidentally emit a tool with the
	// same name and clash with the SDK-host approver
	// registration. Codex's Phase 3.6b3c review flagged this
	// as a Low: a collision currently surfaces as
	// agent_preflight_error (the SDK registry rejects the
	// duplicate), but that's late — making it
	// tool_construction_failed at this layer keeps the failure
	// diagnosable.
	seenNames[sdkapproval.ToolName] = struct{}{}

	for _, name := range sessionTools {
		entry, ok := chatToolBuilders[name]
		if !ok {
			return nil, nil, fmt.Errorf("tool not registered in this worker: %q", name)
		}
		tool, err := entry.Build(runtime, descriptor, turn)
		if err != nil {
			return nil, nil, fmt.Errorf("build tool %q: %w", name, err)
		}
		// Defensive nil check — codex's Phase 3.6b3c v2 review
		// caught that a builder bug returning (nil, nil) would
		// panic on the Spec() call below and surface as worker
		// cleanup rather than tool_construction_failed. Cheap
		// guard, clear failure message.
		if tool == nil {
			return nil, nil, fmt.Errorf("build tool %q returned nil tool with no error", name)
		}
		// Defensive name check — the builder may have produced
		// a tool whose Spec().Name doesn't match the registry
		// key, or two builders may have produced tools with the
		// same Spec().Name. Either way, fail at construction
		// rather than at SDK registry-time.
		toolName := tool.Spec().Name
		if _, dup := seenNames[toolName]; dup {
			return nil, nil, fmt.Errorf("tool %q produces duplicate or reserved spec name %q", name, toolName)
		}
		seenNames[toolName] = struct{}{}
		out = append(out, tool)
		if entry.ApprovalRequired {
			if turn.TurnApprover == nil {
				// Misconfiguration: the catalog says this tool
				// requires approval but the runtime has no
				// approver. Fail at construction so the user
				// sees a clean error, not a stuck "thinking"
				// after the agent calls request_approval.
				return nil, nil, fmt.Errorf("tool %q requires approval but runtime has no approver service configured", name)
			}
			approvalRequiredNames = append(approvalRequiredNames, name)
		}
	}

	if len(approvalRequiredNames) == 0 {
		// No approval-gated tools in this session — skip the
		// SDK request_approval tool registration AND the
		// hook runner. The agent path runs unchanged.
		return out, nil, nil
	}

	// At least one approval-gated tool. Register the SDK's
	// request_approval tool wired to our per-turn approver,
	// and build the hook runner with the
	// agentpolicy.RequireApprovalBeforeTools policy.
	out = append(out, sdkapproval.NewTool(sdkapproval.Config{
		Approver: turn.TurnApprover,
	}))

	policy := agentpolicy.RequireApprovalBeforeToolsWithOptions(
		approvalRequiredNames,
		// See the long comment above this function for why
		// single-use is intentionally OFF here.
		agentpolicy.WithInputBoundApprovals(),
	)
	hookRunner := sdkhook.NewRunner(policy.Options()...)
	return out, hookRunner, nil
}

// chatToolBuilder constructs one tool wrapper bound to the
// session descriptor and the per-turn context. Each builder
// closes over the runtime's shared dependencies (RecallService,
// Resolver, etc.) and the per-turn approver if needed,
// producing an sdktool.Tool the agent runtime registers.
type chatToolBuilder func(*ChatAgentRuntime, agent.SessionDescriptor, chatTurnContext) (sdktool.Tool, error)

// chatToolBuilderEntry pairs a builder with its
// ApprovalRequired flag. The flag MUST mirror
// handler.chatToolCatalog's ApprovalRequired for the same
// tool name; the parity test
// TestChatToolBuildersMatchCatalogApproval verifies this
// invariant so a future drift between the two layers fails
// loudly.
type chatToolBuilderEntry struct {
	Build            chatToolBuilder
	ApprovalRequired bool
}

// chatToolBuilders is the worker-side registry of available
// tool wrappers. The key MUST match the corresponding
// handler.ChatToolDescriptor.Name + that descriptor's
// Available=true. Phase 3.6a wires only `recall_memories`;
// 3.6b3c sets up the request_approval + ApprovalBeforeTool
// scaffolding; 3.6b3d adds the first ApprovalRequired tool
// (push_memory).
var chatToolBuilders = map[string]chatToolBuilderEntry{
	"recall_memories": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewRecallTool(tools.RecallToolConfig{
				Service:  r.RecallService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"list_memories": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewListMemoriesTool(tools.BrowseToolConfig{
				Service:  r.BrowseService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"get_memory": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewGetMemoryTool(tools.BrowseToolConfig{
				Service:  r.BrowseService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"list_hubs": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewListHubsTool(tools.BrowseToolConfig{
				Service:  r.BrowseService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"get_hub": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewGetHubTool(tools.BrowseToolConfig{
				Service:  r.BrowseService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"list_topics": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			return tools.NewListTopicsTool(tools.BrowseToolConfig{
				Service:  r.BrowseService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: false,
	},
	"push_memory": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			if r.PushService == nil {
				return nil, fmt.Errorf("push_memory: ChatAgentRuntime.PushService is required")
			}
			return tools.NewPushTool(tools.PushToolConfig{
				Service:  r.PushService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: true,
	},
	"forget_memory": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, turn chatTurnContext) (sdktool.Tool, error) {
			if r.ForgetService == nil {
				return nil, fmt.Errorf("forget_memory: ChatAgentRuntime.ForgetService is required")
			}
			return tools.NewForgetTool(tools.ForgetToolConfig{
				Service:  r.ForgetService,
				Resolver: r.Resolver,
				Session:  d,
				// Plumb the per-turn forgotten-ids tracker so a
				// repeated forget_memory call within the turn
				// returns already_forgotten instead of NotFound.
				Tracker: turn.ForgottenIDs,
			})
		},
		ApprovalRequired: true,
	},
	"propose_topic_merge": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			if r.ProposeTopicMergeService == nil {
				return nil, fmt.Errorf("propose_topic_merge: ChatAgentRuntime.ProposeTopicMergeService is required")
			}
			return tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
				Service:  r.ProposeTopicMergeService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		ApprovalRequired: true,
	},
	"fetch_url": {
		Build: func(r *ChatAgentRuntime, d agent.SessionDescriptor, _ chatTurnContext) (sdktool.Tool, error) {
			if r.FetchURLService == nil {
				return nil, fmt.Errorf("fetch_url: ChatAgentRuntime.FetchURLService is required")
			}
			return tools.NewFetchURLTool(tools.FetchURLToolConfig{
				Service:  r.FetchURLService,
				Resolver: r.Resolver,
				Session:  d,
			})
		},
		// fetch_url is opt-in but does NOT require per-call
		// approval — read-only by design (no mutation), the
		// SSRF transport is the security boundary, and the
		// session-create opt-in is the user's consent surface.
		// Plan 24 §"Tool table" pins this distinction.
		ApprovalRequired: false,
	},
}

// ChatToolBuilderNames returns the sorted set of tool names
// the worker can construct. Exported for the parity test that
// pins this set against handler.AvailableChatToolNames.
func ChatToolBuilderNames() []string {
	out := make([]string, 0, len(chatToolBuilders))
	for name := range chatToolBuilders {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ChatToolBuilderApprovalRequired reports whether the worker's
// chatToolBuilders entry for name has ApprovalRequired=true.
// ok=false when the name isn't in the registry. Exported for
// the parity test in queue_test that verifies these flags
// match handler.ChatToolApprovalRequired (Phase 3.6b3c —
// drift between worker-side gating and catalog-side display
// would produce silent UX failures).
func ChatToolBuilderApprovalRequired(name string) (required bool, ok bool) {
	entry, found := chatToolBuilders[name]
	if !found {
		return false, false
	}
	return entry.ApprovalRequired, true
}

// buildSessionDescriptor composes the per-turn agent session
// descriptor from the message's resolved scope. Phase 3.6b3d v2
// extracted this helper from Work() so the WriteHubID
// propagation logic can be unit-tested in isolation:
//
//   - scopeHubIDs: the per-turn snapshot
//     (chat_messages.scope_hub_ids_used). This is the live
//     intersection of session.ScopeHubIDs with the caller's
//     accessible hubs at message-start time.
//   - sessionWriteHubID: the chat session's pinned default
//     write hub. Validated at session-create against the
//     caller's accessible hubs. May be stale by send-time
//     (membership churn between session-create and now); we
//     drop it from the descriptor if it's no longer in the
//     per-turn snapshot.
//
// The descriptor's HubIDs is ALWAYS the per-turn snapshot.
// WriteHubID is included ONLY when it's still in scope.
func buildSessionDescriptor(ownerID string, scopeType agent.ScopeType, scopeHubIDs []string, sessionWriteHubID string) agent.SessionDescriptor {
	descriptor := agent.SessionDescriptor{
		OwnerID: ownerID,
		Type:    scopeType,
		HubIDs:  scopeHubIDs,
	}
	if sessionWriteHubID != "" {
		for _, h := range scopeHubIDs {
			if h == sessionWriteHubID {
				descriptor.WriteHubID = sessionWriteHubID
				break
			}
		}
	}
	return descriptor
}

// chatScopeTypeFromMessage infers the scope type for the runtime's
// SessionDescriptor from the per-turn snapshot. We don't store
// scope_type on chat_messages (it's session-level and frozen), so
// we rely on the count of hubs in ScopeHubIDsUsed:
//
//   - 0 hubs: rejected upstream — should not reach here.
//   - 1 hub: single_hub. The agent's recall fans out within one hub.
//   - 2+ hubs: hub_subset (a multi-hub fan-out).
//
// Note: a session created as user_all_hubs but resolved to a single
// live hub at send time will be classified as single_hub here. The
// SessionDescriptor's Type only affects the resolver's expectations
// about scope shape, not data containment — the HubIDs list is the
// real boundary the recall tool enforces against. So this minor
// reclassification has no security or behavior consequence.
func chatScopeTypeFromMessage(m *model.ChatMessage) (agent.ScopeType, error) {
	switch len(m.ScopeHubIDsUsed) {
	case 0:
		return "", fmt.Errorf("scope_hub_ids_used is empty")
	case 1:
		return agent.ScopeTypeSingleHub, nil
	default:
		return agent.ScopeTypeHubSubset, nil
	}
}

// buildChatMessageFinalize converts an agent.RunResult into the
// store's ChatMessageFinalize payload. Returns the finalize struct
// and a non-nil error iff the run terminated non-success — the
// caller treats that as "log + return nil to River" (the terminal
// state is captured on the chat_messages row).
func buildChatMessageFinalize(res agent.RunResult, _ error) (store.ChatMessageFinalize, error) {
	toolCallsJSON, _ := json.Marshal(serializeToolCalls(res.ToolCalls))
	usageJSON, _ := json.Marshal(res.Usage)

	out := store.ChatMessageFinalize{
		Content:    res.Final,
		ToolCalls:  toolCallsJSON,
		Citations:  json.RawMessage(`[]`), // Phase 3.4b will populate citations from tool results.
		Usage:      usageJSON,
		FinishedAt: time.Now().UTC(),
	}

	// Map RunStatus to ChatMessageStatus.
	switch res.Status {
	case agent.RunStatusSuccess:
		out.Status = model.ChatMessageStatusCompleted
		return out, nil
	case agent.RunStatusBudgetExceeded, agent.RunStatusMaxTurns:
		// The agent did real work but didn't reach a final reply.
		// PartialFailed preserves whatever Final the SDK emitted
		// (often empty for these terminals) and signals the UI to
		// show a "got cut off" state.
		out.Status = model.ChatMessageStatusPartialFailed
		errStruct := map[string]string{
			"code":    string(res.Status),
			"message": describeNonSuccess(res),
		}
		errJSON, _ := json.Marshal(errStruct)
		out.Error = errJSON
		return out, fmt.Errorf("agent terminated %s", res.Status)
	default:
		// RunStatusError or any unexpected value — full failure.
		out.Status = model.ChatMessageStatusFailed
		errStruct := map[string]string{
			"code":    string(res.Status),
			"message": describeNonSuccess(res),
		}
		errJSON, _ := json.Marshal(errStruct)
		out.Error = errJSON
		return out, fmt.Errorf("agent terminated %s: %v", res.Status, res.Err)
	}
}

func describeNonSuccess(res agent.RunResult) string {
	if res.Err != nil {
		return res.Err.Error()
	}
	return string(res.Status)
}

// serializeToolCalls converts the runtime's ToolCallRecord slice
// into the JSON shape persisted on chat_messages.tool_calls. Plan 24
// §"Lifecycle policies" pins the per-result hub_id stamp for read-
// time redaction; for Phase 3.4a we record name/args/result/error
// only — citations + per-result hub_id land in 3.4b alongside the
// SSE event-streaming work.
func serializeToolCalls(records []agent.ToolCallRecord) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, r := range records {
		entry := map[string]any{
			"id":   r.ID,
			"name": r.Name,
		}
		if r.ArgsJSON != "" {
			// Pass through the raw args string verbatim — the
			// audit trail is "what did the model send", not "what
			// did our parser interpret".
			entry["args"] = json.RawMessage(r.ArgsJSON)
		}
		if r.Result != "" {
			entry["result"] = r.Result
		}
		if r.IsError {
			entry["is_error"] = true
		}
		out = append(out, entry)
	}
	return out
}

// chatSystemPromptBase is the static behavioral persona. The runtime
// prefixes a small Context block (current time, user display name,
// active hubs) before sending — see buildChatSystemPrompt. Keeping
// the persona separate from the context block makes the dynamic
// portion easy to audit/snapshot and the prompt token cost bounded.
const chatSystemPromptBase = `You are memax, a personal AI agent for the user's knowledge hub AND the public web — you ground answers in their saved memories first, and reach for live information when the question needs it.

You can see the current chat transcript. Each prior message is prefixed with its send time as ` + "`[YYYY-MM-DD HH:MM UTC]`" + ` so you can reason about when things happened relative to the current time shown above. For questions about this conversation, such as "what was my last question?" or "what did we just discuss?", answer from the transcript before using tools.

When you need facts about the user — their preferences, prior decisions, ongoing projects — call the recall_memories tool with a focused query. Use browse tools such as list_memories, get_memory, list_hubs, get_hub, and list_topics when the user asks to inspect or manage their stored knowledge. list_memories accepts an optional ` + "`since`" + ` (ISO-8601 UTC) when the user asks for activity after a specific point. Use mutating tools such as push_memory, forget_memory, and propose_topic_merge only when the user asks you to take that action; those tools require explicit user approval.

Mutating tool results are terminal. When a tool returns ` + "`status: \"forgotten\"`" + ` or ` + "`status: \"already_forgotten\"`" + ` the memory is gone — do NOT call forget_memory again for the same id in this turn, and report the outcome to the user as done. The same goes for push_memory and propose_topic_merge: a successful status (or an ` + "`already_*`" + ` status) means the action is complete; retrying duplicates work and creates a confusing approval prompt for the user.

When the user asks for real-world information you don't have stored (weather, news, public docs, package versions, what's at a URL), use the fetch_url tool to retrieve it from a public URL. Useful patterns: ` + "`https://wttr.in/<city>?format=3`" + ` for current weather in one line, the source URL itself for "what does X say", and search-engine landing pages when you need to find a URL. Don't reach for fetch_url to answer general knowledge you already know — that wastes a turn.

If recall returns nothing useful and fetch_url is unavailable or unhelpful, say so plainly and answer from general knowledge with the caveat. Never fabricate memory content.`

// buildChatSystemPrompt renders the per-turn system prompt with a
// small Context block prepended to chatSystemPromptBase. Lookups
// (GetUser + GetHub per scope-hub) are best-effort: a failure
// degrades to "current time only" rather than failing the turn,
// because the agent still works without user/hub names — they're
// nice-to-have grounding, not load-bearing context.
// chatPersonaMaxChars bounds how much of a persona document is injected
// into the system prompt. SOUL files can be large (config sync allows up
// to 512KB) — beyond this cap we truncate with a marker rather than blow
// the context budget.
const chatPersonaMaxChars = 8000

// chatPinnedMemoryMaxChars bounds the whole pinned-context block, and
// chatPinnedMemoryExcerptChars bounds each memory inside it. A board
// card's citations are few and short, but a hand-built session could
// pin long documents — the block is grounding, not the conversation.
const (
	chatPinnedMemoryMaxChars     = 6000
	chatPinnedMemoryExcerptChars = 1200
)

// truncateRunesForPrompt cuts on a rune boundary so multi-byte
// characters (CJK memories are common) never ship split into the
// model API.
func truncateRunesForPrompt(s string, maxBytes int, marker string) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}

// buildPinnedMemoryBlock renders the session's pinned memories (plan
// 25 P3) into the system prompt. Best-effort and access-checked: ids
// the owner can no longer read are silently dropped, so a session
// outliving a memory degrades instead of leaking or failing.
func (w *ChatMessageRunWorker) buildPinnedMemoryBlock(ownerID string, hubIDs []string, sess *model.ChatSession) string {
	if sess == nil || len(sess.PinnedMemoryIDs) == 0 {
		return ""
	}
	accessible, err := w.Store.GetAccessibleMemories(sess.PinnedMemoryIDs, ownerID, hubIDs)
	if err != nil || len(accessible) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Pinned context\n\nThe user opened this conversation from these specific memories. Treat them as already-known context — don't re-recall them, and reference them naturally instead of restating them wholesale.\n\n")
	for _, id := range sess.PinnedMemoryIDs {
		mem, ok := accessible[id]
		if !ok {
			continue
		}
		body := strings.TrimSpace(mem.Summary)
		if body == "" {
			body = strings.TrimSpace(mem.Content)
		}
		body = truncateRunesForPrompt(body, chatPinnedMemoryExcerptChars, "…")
		title := strings.TrimSpace(mem.Title)
		if title == "" {
			title = "(untitled)"
		}
		b.WriteString(fmt.Sprintf("### %s\n_%s · memory %s_\n\n%s\n\n",
			title, mem.CreatedAt.UTC().Format("2006-01-02"), mem.ID, body))
		if b.Len() > chatPinnedMemoryMaxChars {
			break
		}
	}
	return truncateRunesForPrompt(b.String(), chatPinnedMemoryMaxChars, "\n\n[pinned context truncated]\n\n")
}

// resolveChatPersona returns the persona content to inject for a session:
// session binding wins ("none" disables), otherwise the user's
// chat_default_persona_id setting. Best-effort — a dangling id resolves
// to no persona rather than failing the turn.
func (w *ChatMessageRunWorker) resolveChatPersona(ownerID string, sess *model.ChatSession) *model.Persona {
	personaID := ""
	if sess != nil {
		personaID = sess.PersonaID
	}
	if personaID == model.ChatPersonaNone {
		return nil
	}
	if personaID == "" {
		if prefs, err := w.Store.GetUserPreferences(ownerID); err == nil && prefs != nil {
			if v, ok := prefs.MergedSettings()["chat_default_persona_id"].(string); ok {
				personaID = v
			}
		}
	}
	if personaID == "" || personaID == model.ChatPersonaNone {
		return nil
	}
	persona, err := w.Store.GetPersona(personaID, ownerID)
	if err != nil {
		return nil
	}
	return persona
}

func (w *ChatMessageRunWorker) buildChatSystemPrompt(ctx context.Context, ownerID string, hubIDs []string, sess *model.ChatSession) string {
	var ctxBlock strings.Builder
	ctxBlock.WriteString("## Context\n\n")
	ctxBlock.WriteString(fmt.Sprintf("- Current time: %s\n", time.Now().UTC().Format("2006-01-02 15:04 UTC")))

	if user, err := w.Store.GetUser(ownerID); err == nil && user != nil {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = strings.TrimSpace(user.Name)
		}
		if name != "" {
			ctxBlock.WriteString(fmt.Sprintf("- User: %s\n", name))
		}
	}

	if len(hubIDs) > 0 {
		names := make([]string, 0, len(hubIDs))
		for _, id := range hubIDs {
			hub, err := w.Store.GetHub(id)
			if err != nil || hub == nil {
				continue
			}
			if n := strings.TrimSpace(hub.Name); n != "" {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			ctxBlock.WriteString(fmt.Sprintf("- Active hubs: %s\n", strings.Join(names, ", ")))
		}
	}

	ctxBlock.WriteString("\n")

	// Persona block — the extracted identity (SOUL.md etc.) the user bound
	// to this session or set as their default. Injected ABOVE the base
	// prompt so the behavioral persona colors everything, while the base
	// prompt's grounding/tool rules still apply verbatim.
	personaBlock := ""
	if persona := w.resolveChatPersona(ownerID, sess); persona != nil {
		content := truncateRunesForPrompt(
			strings.TrimSpace(persona.Content), chatPersonaMaxChars, "\n\n[persona truncated]")
		personaBlock = fmt.Sprintf(
			"## Persona: %s\n\nAdopt the identity below for this conversation. It shapes voice, values, and behavior; it never overrides the grounding, privacy, or tool rules that follow.\n\n%s\n\n",
			persona.Name, content)
	}

	// Pinned memories sit between persona and base: the identity
	// colors the voice, the pinned context grounds the subject, and the
	// base prompt's tool/privacy rules still land last and win.
	pinnedBlock := w.buildPinnedMemoryBlock(ownerID, hubIDs, sess)

	return ctxBlock.String() + personaBlock + pinnedBlock + chatSystemPromptBase
}

// chatSessionLeaseDuration is the deadline by which a worker
// must finalize an in-flight chat message; past that, the lease
// sweeper treats the session as orphaned and finalizes it as
// failed{server_crashed}.
//
// Sized comfortably above the chat profile's MaxRunDuration
// (currently 120s in agent.ProfileChat) PLUS headroom for the
// finalize round-trip. Codex flagged on Phase 3.5a v1 that the
// lease must always be > MaxRunDuration so a turn that runs the
// full profile budget never races the sweeper. If MaxRunDuration
// ever rises (e.g., a future paid-tier 5min profile), this
// constant must rise alongside it — the right relationship is
// `lease > MaxRunDuration + finalize_p99`, not a fixed value.
const chatSessionLeaseDuration = 5 * time.Minute

// chatLeaseSweepBatch caps how many stuck sessions one sweep
// finalizes. Larger backlogs (deploy storm) drain across
// successive sweeps. 200 is small enough that a single sweep
// fits well under the periodic-job timeout (next firing is in
// 1m); large enough that even a 1000-session backlog clears in
// 5 sweeps.
const chatLeaseSweepBatch = 200

// chatLeaseSweepTimeout caps the worker invocation. The store
// query + per-session finalize is bounded by chatLeaseSweepBatch;
// each finalize is a single-tx round trip. 90s is generous
// headroom — under normal load this completes in <1s.
const chatLeaseSweepTimeout = 90 * time.Second

// chatReplayWindow is the 24h window plan 24 §"Send-message +
// resume" pins for the SSE replay buffer. The chat_event_purge
// worker deletes rows older than this on each sweep.
const chatReplayWindow = 24 * time.Hour

// ChatLeaseSweepWorker finalizes chat sessions whose worker
// dropped the lease. Pairs with the periodic ChatLeaseSweepArgs
// firing in workerapp.
//
// **Why a periodic worker instead of an inline timer.** River's
// periodic-job machinery already gives us deduped firing across
// multiple worker processes (a re-deploy doesn't double-fire if
// two pods overlap), and the implementation is the same shape
// as the existing dream-cycle / waitlist-expiry sweepers — keeps
// the codebase consistent. Finalize semantics (CAS gate +
// terminal SSE row + session lock release) are inherited from
// FinalizeChatMessage; the worker just identifies orphans.
//
// **Terminal selection.** The sweeper's default finalize is
// `failed{server_crashed}` — appropriate for a runaway worker
// that never wrote a terminal. But canceling sessions need
// `canceled{user_canceled}` instead: a cancel that landed before
// any worker picked up the row, then sat past the lease window,
// MUST honor the user's intent. The store's CAS rejects a failed
// write over a canceling row with ErrChatMessageCancelPending
// (Phase 3.7b); this worker handles that sentinel by retrying as
// canceled. Net: orphan with no cancel pending → message.failed;
// orphan with cancel pending → message.canceled. Codex's Phase
// 3.7b v3 review caught the gap before the doc drifted out of
// sync with behavior.
type ChatLeaseSweepWorker struct {
	river.WorkerDefaults[ChatLeaseSweepArgs]
	Store store.Store
	// Signaler is the optional Phase 3.5b live-delivery hint.
	// Sweeper finalizes orphaned messages as either failed or
	// canceled (see the type doc above for which); without the
	// signaler, SSE clients streaming a dead turn wait up to
	// 250ms for the next poll to see the terminal frame. With
	// it, the wake-up arrives within Redis's typical pub-sub
	// latency (<10ms).
	Signaler *chatstream.Signaler
}

func (w *ChatLeaseSweepWorker) Timeout(*river.Job[ChatLeaseSweepArgs]) time.Duration {
	return chatLeaseSweepTimeout
}

func (w *ChatLeaseSweepWorker) Work(ctx context.Context, _ *river.Job[ChatLeaseSweepArgs]) error {
	cutoff := time.Now().UTC().Add(-chatSessionLeaseDuration)
	leases, err := w.Store.ListExpiredChatLeases(ctx, cutoff, chatLeaseSweepBatch)
	if err != nil {
		return fmt.Errorf("list expired leases: %w", err)
	}
	if len(leases) == 0 {
		return nil
	}

	finalized := 0
	skipped := 0
	for _, lease := range leases {
		// Build a structured error payload describing why this
		// turn died. The SSE client sees this in the
		// message.failed terminal frame's `error.code`.
		errPayload, _ := json.Marshal(map[string]string{
			"code":    "server_crashed",
			"message": "the worker handling this turn did not complete within the lease window",
		})
		fin := store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusFailed,
			Content:    "",
			ToolCalls:  json.RawMessage(`[]`),
			Citations:  json.RawMessage(`[]`),
			Usage:      json.RawMessage(`{}`),
			Error:      errPayload,
			FinishedAt: time.Now().UTC(),
		}
		err := w.Store.FinalizeChatMessage(ctx, lease.ActiveMessageID, lease.OwnerID, fin)
		// Cancel preemption: the lease sweeper's default finalize
		// is `failed{server_crashed}`, but if the row is in
		// 'canceling' state (cancel landed and no worker ever
		// picked the job up), the store's CAS rejects the failed
		// write with ErrChatMessageCancelPending. Codex's Phase
		// 3.7b v2 review caught the gap: without this retry the
		// session would stay locked indefinitely. Re-issue as
		// 'canceled' — that's the user-intent terminal.
		if errors.Is(err, store.ErrChatMessageCancelPending) {
			canceled := buildChatCanceledFinalize(agent.RunResult{},
				"user_canceled",
				"the user canceled this turn (lease sweeper)")
			err = w.Store.FinalizeChatMessage(ctx, lease.ActiveMessageID, lease.OwnerID, canceled)
		}
		if err == nil {
			finalized++
			// Phase 3.5b — wake any SSE client streaming this
			// orphaned message so the `message.failed` frame
			// lands immediately rather than after the next poll.
			w.Signaler.Notify(ctx, lease.ActiveMessageID)
			continue
		}
		// ErrChatMessageAlreadyFinalized = a real worker
		// committed before we got there. Benign race; count as
		// skipped for telemetry but don't surface as an error.
		if errors.Is(err, store.ErrChatMessageAlreadyFinalized) || errors.Is(err, store.ErrChatMessageNotFound) {
			skipped++
			continue
		}
		// Per-session error — log and continue rather than
		// abort the whole sweep. Next sweep retries this lease.
		slog.WarnContext(ctx, "chat lease sweep: finalize failed (will retry next sweep)",
			"session_id", lease.SessionID,
			"assistant_id", lease.ActiveMessageID,
			"locked_at", lease.LockedAt,
			"err", err)
	}
	slog.InfoContext(ctx, "chat lease sweep: complete",
		"considered", len(leases),
		"finalized", finalized,
		"skipped_already_terminal", skipped)
	return nil
}

// ChatApprovalSweepWorker flips chat_tool_approvals rows past
// expires_at to `timed_out` and publishes
// approval.decided{state=timed_out} for each. Catches the case
// where the SDK-host approver crashed / disconnected before
// its own local deadline timer could fire — without this
// sweep, the row stays pending forever. Pairs with periodic
// ChatApprovalSweepArgs firing in workerapp.
//
// The publish lets any blocked SDK-host approver still alive
// (e.g., one that lost its broker subscription) unblock at the
// expiry boundary via the poll fallback's row read.
type ChatApprovalSweepWorker struct {
	river.WorkerDefaults[ChatApprovalSweepArgs]
	Store     store.Store
	Publisher events.Publisher // optional — nil-safe via PublishApprovalDecided
}

func (w *ChatApprovalSweepWorker) Timeout(*river.Job[ChatApprovalSweepArgs]) time.Duration {
	return 60 * time.Second
}

func (w *ChatApprovalSweepWorker) Work(ctx context.Context, _ *river.Job[ChatApprovalSweepArgs]) error {
	expired, err := w.Store.ExpirePendingChatToolApprovals(ctx)
	if err != nil {
		return fmt.Errorf("expire chat tool approvals: %w", err)
	}
	for _, row := range expired {
		events.PublishApprovalDecided(ctx, w.Publisher, row.OwnerID, row.ApprovalID, "timed_out")
	}
	if len(expired) > 0 {
		slog.InfoContext(ctx, "chat approval sweep: complete",
			"timed_out", len(expired))
	}
	return nil
}

// ChatEventPurgeWorker deletes chat_message_events older than
// 24h. Plan 24's replay window. Pairs with the periodic
// ChatEventPurgeArgs firing in workerapp.
//
// The handler also enforces the 24h cap at connect time
// (returns 410 chat_replay_expired). This worker is the
// row-level cleanup so the table doesn't grow unboundedly.
type ChatEventPurgeWorker struct {
	river.WorkerDefaults[ChatEventPurgeArgs]
	Store store.Store
}

func (w *ChatEventPurgeWorker) Timeout(*river.Job[ChatEventPurgeArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *ChatEventPurgeWorker) Work(ctx context.Context, _ *river.Job[ChatEventPurgeArgs]) error {
	cutoff := time.Now().UTC().Add(-chatReplayWindow)
	deleted, err := w.Store.DeleteChatMessageEventsOlderThan(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete chat message events: %w", err)
	}
	slog.InfoContext(ctx, "chat event purge: complete", "deleted", deleted, "cutoff", cutoff)
	return nil
}
