// Package approver builds per-turn SDK approvers for the chat
// agent. Each call to ForTurn returns a fresh sdkapproval.Approver
// bound to one (owner, message) tuple — that approver writes a
// chat_tool_approvals row, blocks waiting for the user's decide,
// and returns the SDK Decision.
//
// Plan 24 §"approval flow" pins the dual-signal contract:
//
//   - The events.Broker (user-audience, EntityID=approval_id)
//     delivers approval.decided wakeups across processes — the
//     decide HTTP request can land on ANY API server, the worker
//     holding the blocked goroutine can be on a different one,
//     and the Redis pub/sub fan-out routes the wakeup wherever
//     the subscription lives.
//
//   - A 250ms poll loop on chat_tool_approvals.status runs in
//     parallel as a defense-in-depth fallback. Pub/sub is
//     best-effort (subscribe-after-publish, broker brown-out,
//     dropped frames) and the DB row is the source of truth, so
//     the poll path catches whatever the broker missed without
//     diverging — both signals trigger the same fresh DB read.
//
//   - A local deadline timer at expires_at. Codex's Phase 3.6b2
//     review caught that simply writing expires_at and waiting
//     for the 1m sweeper left an "expired-but-still-pending"
//     window where the agent run's ctx could deadline first
//     (90s TTL + up to 60s of sweeper latency = 150s, but the
//     run cap is 120s). The local timer flips the row via
//     ExpireChatToolApproval at the boundary, so the agent
//     gets a clean timed_out result before ctx expires. The
//     CAS guard on the store-side UPDATE means a user-decide
//     racing with the timer at the boundary still wins — the
//     approver re-fetches and returns the user's decision.
//
// "Winner stands": whichever signal arrives first triggers a
// fresh GetChatToolApproval, and the row's terminal status is the
// final answer. The SDK's Decision shape is derived from that row.
//
// **Why a separate package, not inline in queue/chat_workers.go.**
// The chat worker already owns a lot of per-turn plumbing
// (runtime, scope resolver, recall, stream signaler, finalize).
// Folding the approver inline would push another ~150 lines of
// race-sensitive concurrency into a file already tracking budget
// envelopes, terminal-state finalization, lease semantics, and
// SDK event drain. The approver is its own primitive with its
// own tests; the worker just holds a Service reference and calls
// ForTurn at message-start time.
//
// **Phase 3.6b2 scope.** This package is the wait-for-decision
// primitive in isolation. It does NOT yet emit the
// `approval.required` SSE-side event into chat_message_events
// (that wiring lands in Phase 3.6b3 alongside the tool wrappers
// for push_memory / forget_memory / propose_topic_merge). The
// SDK's request_approval tool result already lands in the
// transcript as a normal tool_result event, so the in-stream
// audit trail is intact even before the host emits the
// dedicated approval.required wake-up event.
package approver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	sdkapproval "github.com/MemaxLabs/memax-go-agent-sdk/toolkit/approvaltools"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Default knobs. Each is overridable via Config.
const (
	// DefaultPollInterval is the cadence of the DB poll fallback.
	// 250ms is plan 24's pinned value: ≤125ms average miss-detect
	// time when broker pub/sub fails, low enough to feel
	// responsive when broker is healthy and the poll is a no-op,
	// high enough that 100 concurrent in-flight approvals add
	// 400 row reads/sec across the fleet — bounded.
	DefaultPollInterval = 250 * time.Millisecond

	// DefaultTTL is the per-approval expiry window the approver
	// uses when ForTurn callers don't override it. 90s fits inside
	// the chat agent's 120s MaxRunDuration with 30s of headroom
	// for the rest of the turn (the model's post-decision response
	// + any other tool calls). Plan 24 §"chat_tool_approvals"
	// originally pinned 10min, but Phase 3.6b1's codex v3 review
	// surfaced the budget mismatch — 10min approvals can't fit
	// inside a 120s run without restructuring the run/job/lease
	// budgets. 90s is the tight-but-workable MVP shape; the
	// pause-and-resume design (run pauses, worker releases the
	// session lock, a NEW River job resumes when approval lands)
	// remains the upgrade path if real users push back on the
	// click window.
	DefaultTTL = 90 * time.Second

	// DefaultPostDecisionGrace is the time the approver reserves
	// inside the run's ctx deadline AFTER the approval boundary,
	// so the approver's local deadline timer can: (a) call
	// ExpireChatToolApproval to CAS-flip the row to timed_out,
	// (b) return the timed_out Decision to the SDK, (c) let the
	// agent run see the result and emit a tool_result event before
	// ctx fires.
	//
	// 250ms is 1× the poll interval — enough for one indexed CAS
	// UPDATE round-trip in healthy operation, much shorter than
	// the 5s value an earlier draft used (which broke tests with
	// 2s ctx timeouts). Codex's Phase 3.6b3c review caught the
	// stale-prompt failure mode that motivated the clamp — too
	// much grace overcorrects and invalidates legitimate short
	// runs; too little leaves no time for the cleanup. 250ms is
	// the inflection point.
	DefaultPostDecisionGrace = 250 * time.Millisecond
)

// Decision reason strings. The SDK surfaces them on the
// request_approval tool result so the agent (and downstream
// policy gates) can branch on them. Exporting as constants makes
// the contract explicit and lets callers compare via ==
// without typo risk.
const (
	ReasonUserApproved = "user_approved"
	ReasonUserDenied   = "user_denied"
	ReasonTimedOut     = "timed_out"
	// ReasonUnknownStatus is returned when the row is in a
	// terminal state the approver doesn't recognize (e.g., a
	// future migration adds a new status). The raw status is
	// logged at warn level; the agent gets a deny-shaped
	// Decision with a stable reason so it can fail closed.
	ReasonUnknownStatus = "unknown_status"
)

// EventSubscriber is the small surface the approver needs from
// the events.Broker — exposed as an interface so tests can
// inject controlled events without standing up a real Redis.
//
// The events channel is closed by the implementation when the
// returned cancel func is called; callers must always invoke
// cancel (defer is the idiomatic shape) to release subscription
// resources.
type EventSubscriber interface {
	Subscribe(userID string) (events <-chan events.Event, cancel func())
}

// ApprovalEventEmitter is the per-turn pre-block hook the
// approver fires after the chat_tool_approvals row is committed
// but BEFORE entering the wait loop. Production wiring uses
// this to:
//
//  1. Append a chat_message_events row of type
//     `approval_requested` (stored type) carrying the prompt
//     payload, so a reconnecting SSE client picking up past
//     Last-Event-ID receives the prompt. The SSE translator
//     in handler/chat_stream.go maps this stored type to the
//     wire event `approval.required` (plan 24 §"approval flow"
//     names the wire event; the stored type uses the SDK's
//     event vocabulary so the chat_message_events buffer is
//     uniform across SDK-emitted and host-emitted rows).
//  2. Notify the chatstream signaler so the live SSE stream
//     wakes immediately and writes the event to subscribers.
//
// **Per-turn lifetime.** The emitter is supplied via
// Service.ForTurn(...) and must share the per-message
// chatStreamObserver's seq counter so the new row's seq
// interleaves correctly with the agent's assistant/tool
// events. A Service-level emitter would have no access to
// per-turn state — codex's Phase 3.6b3a review caught this.
//
// The interface lives in this package (not in a shared
// chatstream/queue package) so the approver's contract stays
// explicit: the host MUST emit the user-facing prompt event
// before it blocks, otherwise the UI never sees it. Plan 24
// §"approval.required is host-emitted" pins this.
//
// **Nil semantics.** A nil emitter is a no-op — the row is
// still created and the wait loop still runs. Used in unit
// tests + the Phase 3.6b3a wiring-only flow before mutating
// tools land. When mutating tools register in 3.6b3b/c, the
// per-turn approver MUST be constructed with a non-nil
// emitter; if emit fails (DB hiccup, etc.), the approver
// fails closed: rolls the row to timed_out and returns the
// emit error so the agent run terminates with a clear
// failure rather than blocking 90s on an invisible prompt.
type ApprovalEventEmitter interface {
	EmitApprovalRequired(ctx context.Context, evt ApprovalRequiredEvent) error
}

// ApprovalRequiredEvent is the payload the emitter receives.
// All fields are populated by the approver from the SDK
// Request + the row it just wrote. Carries the SDK Request's
// human-prompt fields (Reason, Details, Risk, Summary,
// Metadata) so the UI surface can render a meaningful
// approval card; without these, the prompt would be just
// "agent wants to call X with Y args" — codex's Phase 3.6b3a
// review flagged this gap.
type ApprovalRequiredEvent struct {
	OwnerID     string
	MessageID   string
	ApprovalID  string
	ToolName    string
	Args        []byte
	ArgsHash    string
	RequestedAt time.Time
	ExpiresAt   time.Time

	// Reason is a short explanation of why approval is
	// needed (the SDK Request's `reason` field). Non-empty
	// per the SDK schema.
	Reason string

	// Details is optional concrete details (paths, commands,
	// proposed change summary).
	Details string

	// Risk is an optional risk level / risk explanation.
	Risk string

	// Summary is the SDK's structured host-facing summary
	// (title, description, paths, change counts). The wire
	// payload includes this verbatim so the UI can render a
	// rich card.
	Summary sdkapproval.Summary

	// Metadata is the SDK's free-form host-defined metadata
	// the agent attached to the request (passed through
	// unchanged for UI rendering / audit).
	Metadata map[string]any
}

// approvalStore is the small store surface the approver uses.
// Defined here so the approver's compile-time contract is the
// minimum it needs, not the full Store interface — easier for
// tests, and any future addition has to be deliberate.
type approvalStore interface {
	CreateChatToolApproval(ctx context.Context, ownerID string, ap *model.ChatToolApproval) error
	GetChatToolApproval(ctx context.Context, approvalID, ownerID string) (*model.ChatToolApproval, error)
	ExpireChatToolApproval(ctx context.Context, approvalID, ownerID string) error
}

// Service builds per-turn approvers. Constructed once at app
// startup; both the chat worker (which calls ForTurn at message
// start) and any test harness hold a reference.
//
// **Nil semantics.** A nil *Service is NOT a valid approver
// source — ForTurn would panic. The chat worker's pre-flight
// runtime check (ChatAgentRuntime.IsConfigured) is responsible
// for short-circuiting messages that have no approver service
// before the agent.Run path is invoked. This is the same shape
// as ChatAgentRuntime: a missing dependency is a structured
// `runtime_not_configured` failure, NOT a silent degradation.
type Service struct {
	store             approvalStore
	subscriber        EventSubscriber
	pollInterval      time.Duration
	ttl               time.Duration
	postDecisionGrace time.Duration
	logger            *slog.Logger
}

// Config carries the dependencies and overridable knobs for New.
//
// Store is required. Everything else is optional with sensible
// defaults; passing zero values gets you the production shape.
type Config struct {
	// Store is the persistence layer. Required. Any value that
	// satisfies the small approvalStore interface works — in
	// production this is the *PostgresStore; in tests, a fake
	// with just the three methods.
	Store approvalStore

	// Subscriber is the cross-process events router. Optional —
	// pass nil to run in poll-only mode (dev/staging without
	// Redis, or local tests). The poll-only path adds up to
	// PollInterval of latency to a decide; otherwise correct.
	//
	// **Operational note**: nil subscriber should be visible at
	// startup (logged by the wiring code, not this package) so
	// the fleet doesn't silently run poll-only when it should
	// be broker-driven. This package surfaces poll-only as a
	// debug log at ForTurn time but doesn't enforce a health
	// gate — that's the wiring layer's call.
	Subscriber EventSubscriber

	// PollInterval overrides DefaultPollInterval. Use a tighter
	// cadence in tests (e.g., 5ms) for deterministic, fast
	// scenarios. Zero means default.
	PollInterval time.Duration

	// TTL is the row's expires_at offset. Zero means
	// DefaultTTL. Plan 24's pinned ChatToolApprovalTTL (10min)
	// does NOT fit inside the chat run budget; if you're wiring
	// this for chat, leave at default or set to ≤ MaxRunDuration.
	TTL time.Duration

	// PostDecisionGrace overrides DefaultPostDecisionGrace —
	// the time the approver reserves inside the request ctx
	// deadline so its self-flip + Decision return + tool_result
	// emission can complete before ctx fires. Tests with very
	// short ctx timeouts may need to tune this down; production
	// callers should leave it at default. Zero means default.
	PostDecisionGrace time.Duration

	// Logger is the structured logger; nil falls through to
	// slog.Default(). Logs are sparse — only bracketed events
	// (subscribe/unsubscribe, terminal decision, fetch errors).
	Logger *slog.Logger
}

// New returns a Service from cfg. Panics if Store is nil — that
// is a wiring bug that should crash early, not surface as a
// per-request failure.
func New(cfg Config) *Service {
	if cfg.Store == nil {
		panic("approver: Config.Store is required")
	}
	pi := cfg.PollInterval
	if pi <= 0 {
		pi = DefaultPollInterval
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	grace := cfg.PostDecisionGrace
	if grace <= 0 {
		grace = DefaultPostDecisionGrace
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:             cfg.Store,
		subscriber:        cfg.Subscriber,
		pollInterval:      pi,
		ttl:               ttl,
		postDecisionGrace: grace,
		logger:            logger,
	}
}

// BrokerSubscriber wraps a *events.Broker as an EventSubscriber.
// Returns nil when broker is nil so the wiring code can pass
// `BrokerSubscriber(broker)` unconditionally and let New decide
// poll-only vs broker-driven.
func BrokerSubscriber(broker *events.Broker) EventSubscriber {
	if broker == nil {
		return nil
	}
	return brokerSubscriber{b: broker}
}

type brokerSubscriber struct{ b *events.Broker }

func (b brokerSubscriber) Subscribe(userID string) (<-chan events.Event, func()) {
	sub := b.b.Subscribe(userID, nil)
	if sub == nil {
		return nil, func() {}
	}
	return sub.Events, sub.Close
}

// TurnConfig binds per-turn dependencies. ForTurn captures
// these in the returned approver; they're scoped to one
// (owner, message) tuple. Codex's Phase 3.6b3a review caught
// that an emitter on the singleton Service couldn't share the
// per-message chatStreamObserver seq counter — moving it here
// fixes the lifetime mismatch.
type TurnConfig struct {
	OwnerID   string
	MessageID string

	// Emitter is the per-turn pre-block SSE hook. Nil = no
	// SSE prompt event written. When non-nil, an emit failure
	// causes RequestApproval to fail closed (CAS-flip the
	// row to timed_out, return the emit error). See
	// ApprovalEventEmitter doc for the contract.
	Emitter ApprovalEventEmitter
}

// ForTurn binds the per-turn (owner, message) context. The
// returned approver writes a chat_tool_approvals row tied to
// messageID at RequestApproval time, fires the optional
// emitter, blocks for the decide, and returns a Decision.
//
// One Approver per turn is the contract. Reusing an Approver
// across turns would share the (owner, message) tuple, which
// doesn't make sense — every approval row's message_id must
// point at the in-flight assistant message. The chat worker
// calls ForTurn once per Work() invocation, right before
// agent.Run.
func (s *Service) ForTurn(cfg TurnConfig) sdkapproval.Approver {
	return &turnApprover{
		svc:       s,
		ownerID:   cfg.OwnerID,
		messageID: cfg.MessageID,
		emitter:   cfg.Emitter,
	}
}

// turnApprover implements sdkapproval.Approver for one turn.
type turnApprover struct {
	svc       *Service
	ownerID   string
	messageID string
	emitter   ApprovalEventEmitter
}

// RequestApproval is the SDK's host hook. The agent goroutine
// blocks here until we return.
//
// Step order:
//
//  1. Open the broker subscription FIRST (before the row
//     INSERT). The subscribe must be live before any decide
//     could publish; otherwise an extremely fast decide between
//     INSERT and Subscribe could fire its broker event and
//     leave the subscriber with nothing to receive. The 250ms
//     poll catches that race anyway, but ordering subscribe-
//     before-insert eliminates the window entirely.
//
//  2. INSERT the row (status=pending). The store enforces
//     owner-scoped existence of the message, so a stale or
//     forged messageID returns store.ErrChatMessageNotFound and
//     we surface it as an error (the agent.Run sees a tool
//     error; the run terminates with a structured failure).
//
//  3. Probe the row once. Mostly a no-op (we just inserted), but
//     this is also the only place a "stale-state recovery"
//     resume could read a pre-decided row — keeps the wait loop
//     uniform.
//
//  4. Wait. Four signals race: the SDK ctx (run timeout / cancel
//     / shutdown), a broker event matching this approval id, the
//     poll ticker, and a local deadline timer at expires_at.
//     The first to fire triggers the appropriate action; if a
//     row read is needed, the row's status is the final answer.
func (t *turnApprover) RequestApproval(ctx context.Context, req sdkapproval.Request) (sdkapproval.Decision, error) {
	if t.svc == nil {
		return sdkapproval.Decision{}, errors.New("approver: turnApprover has no Service")
	}
	if t.ownerID == "" || t.messageID == "" {
		return sdkapproval.Decision{}, fmt.Errorf("approver: ForTurn was called with empty owner or message id (owner=%q msg=%q)", t.ownerID, t.messageID)
	}

	approvalID := uuid.NewString()

	// 1. Subscribe BEFORE insert — see step-order doc above.
	var subEvents <-chan events.Event
	if t.svc.subscriber != nil {
		ev, cancel := t.svc.subscriber.Subscribe(t.ownerID)
		if cancel != nil {
			defer cancel()
		}
		subEvents = ev
	}
	if subEvents == nil {
		t.svc.logger.DebugContext(ctx, "approver: poll-only mode (no broker subscription)",
			"approval_id", approvalID)
	}

	// 2. Persist the row. ToolInput from the SDK is the
	// canonical proposed args; ToolInputHash is its
	// stable sha256. Both are stored verbatim — the store
	// layer's args column is jsonb so empty input becomes
	// `{}` rather than NULL (jsonb NOT NULL constraint).
	args := []byte(req.ToolInput)
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	argsHash := req.ToolInputHash
	if argsHash == "" {
		// Defensive: the SDK currently always populates this,
		// but the field is documented as optional; computing
		// our own hash on empty makes us robust to future SDK
		// changes that drop the pre-computed value.
		sum := sha256.Sum256(args)
		argsHash = hex.EncodeToString(sum[:])
	}
	now := time.Now().UTC()
	// Clamp expires_at to the request context deadline minus a
	// post-decision grace, so a slow approval can never outlive
	// the agent run. Codex's Phase 3.6b3c review caught the
	// stale-prompt failure mode: a 90s TTL fits inside a 120s
	// run only when the approval starts in the first ~30s; a
	// late or repeat approval (multiple single-use approvals
	// within one turn) could leave the UI showing a "valid"
	// prompt after the worker has already failed the message
	// with ctx-deadline-exceeded. Clamping ensures the row's
	// expires_at is always inside the run's lifetime, so:
	//
	//   - Approver's local deadline timer fires within ctx.
	//   - Approver self-flips the row to timed_out (CAS).
	//   - Agent.Run receives a clean timed_out tool result.
	//   - The user's UI sees the row terminal at the same time
	//     the run terminates — no orphaned prompts.
	//
	// Grace is small because the decide commit + the
	// approval.decided publish both run on the API server, not
	// the worker — they don't need worker ctx. Worker-side, we
	// just need enough time to (a) self-flip the row if no
	// decision lands and (b) drain the wait loop. The default
	// (DefaultPostDecisionGrace, 250ms) is one poll-interval —
	// generous for a single CAS UPDATE round-trip. The grace
	// is configurable via Service.Config.PostDecisionGrace; an
	// earlier draft used 5s but that overcorrected for tests
	// with short ctx timeouts (Phase 3.6b3c codex follow-up).
	expiresAt := now.Add(t.svc.ttl)
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		clamped := deadline.Add(-t.svc.postDecisionGrace)
		if clamped.Before(expiresAt) {
			if !clamped.After(now) {
				// Run is already too close to its deadline to
				// host a meaningful approval; fail fast rather
				// than create a row that's effectively pre-expired.
				return sdkapproval.Decision{}, fmt.Errorf("approver: insufficient run time remaining for approval (deadline=%s, now=%s, grace=%s)", deadline, now, t.svc.postDecisionGrace)
			}
			expiresAt = clamped
		}
	}
	row := &model.ChatToolApproval{
		ID:          approvalID,
		MessageID:   t.messageID,
		ToolName:    req.Action,
		Args:        args,
		ArgsHash:    argsHash,
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: now,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}
	if err := t.svc.store.CreateChatToolApproval(ctx, t.ownerID, row); err != nil {
		return sdkapproval.Decision{}, fmt.Errorf("approver: create row: %w", err)
	}
	t.svc.logger.DebugContext(ctx, "approver: row created",
		"approval_id", approvalID,
		"message_id", t.messageID,
		"action", req.Action,
		"ttl", t.svc.ttl,
	)

	// Pre-block SSE emission. Plan 24 §"approval.required is
	// host-emitted" — the SDK's `approval_requested` lifecycle
	// event fires AFTER a decision exists, which is too late for
	// the UI prompt. The host emits its own approval_requested
	// event between row-INSERT and the wait loop so:
	//   - The chat_message_events buffer has it for resume.
	//   - The live SSE stream wakes immediately.
	// Stored type is `approval_requested` to match the SSE
	// translator in handler/chat_stream.go, which maps it to the
	// wire event `approval.required` (plan 24 §"approval flow"
	// names the wire event; the stored type uses the SDK's
	// event vocabulary so the chat_message_events buffer is
	// uniform across SDK-emitted and host-emitted rows).
	// Codex's Phase 3.6b3a review caught the type mismatch in
	// v1 — the doc said `approval.required` but the translator
	// expects `approval_requested`.
	//
	// Emitter is optional; nil = skip (the row + wait loop
	// still work, only the UI prompt is missing — fine for unit
	// tests). When non-nil, an emit failure means the user
	// can't see the prompt, so failing closed is safer than
	// blocking 90s on an invisible approval. The 3.6b3a review
	// flagged this: warn-and-wait without a fallback UI
	// surface (e.g. a pending-approvals list) is an invisible
	// hang. Fail-closed: CAS-flip the row to timed_out (best-
	// effort; ignore errors since the wait won't run anyway)
	// and return the emit error so the agent run terminates
	// with a clear failure.
	if t.emitter != nil {
		if err := t.emitter.EmitApprovalRequired(ctx, ApprovalRequiredEvent{
			OwnerID:     t.ownerID,
			MessageID:   t.messageID,
			ApprovalID:  approvalID,
			ToolName:    req.Action,
			Args:        args,
			ArgsHash:    argsHash,
			RequestedAt: row.RequestedAt,
			ExpiresAt:   row.ExpiresAt,
			Reason:      req.Reason,
			Details:     req.Details,
			Risk:        req.Risk,
			Summary:     req.Summary,
			Metadata:    req.Metadata,
		}); err != nil {
			t.svc.logger.WarnContext(ctx, "approver: emit approval.required failed; failing closed",
				"approval_id", approvalID,
				"err", err)
			// Best-effort row cleanup so the sweeper doesn't
			// have to reap a row no one will ever decide.
			// Errors are swallowed: the row's own expires_at
			// gives the sweeper a deterministic upper bound.
			if expireErr := t.svc.store.ExpireChatToolApproval(ctx, approvalID, t.ownerID); expireErr != nil &&
				!errors.Is(expireErr, store.ErrChatApprovalAlreadyDecided) {
				t.svc.logger.WarnContext(ctx, "approver: cleanup expire failed after emit failure",
					"approval_id", approvalID,
					"err", expireErr)
			}
			return sdkapproval.Decision{}, fmt.Errorf("approver: emit approval.required: %w", err)
		}
	}

	// 3. Initial probe — covers the (vanishingly small) window
	// where a fast user-decide commits between our INSERT and
	// our first poll/event. If the broker is healthy this is
	// almost always pending; if the broker is down this is the
	// only signal that runs faster than the poll cadence.
	if dec, ok, err := t.tryFetch(ctx, approvalID); err != nil {
		return sdkapproval.Decision{}, err
	} else if ok {
		return dec, nil
	}

	// 4. Wait loop.
	pollTicker := time.NewTicker(t.svc.pollInterval)
	defer pollTicker.Stop()

	deadline := row.ExpiresAt
	deadlineTimer := time.NewTimer(time.Until(deadline))
	defer deadlineTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			// Run cancelled / timed out / worker shutdown. The
			// row stays pending; the timeout sweeper (1m
			// cadence) will reap it. We do NOT mark it
			// canceled here — there is no "canceled" status,
			// and racing the sweeper to flip it would just
			// duplicate work. The run is already over from the
			// agent's perspective.
			t.svc.logger.DebugContext(ctx, "approver: ctx done while waiting",
				"approval_id", approvalID,
				"err", ctx.Err(),
			)
			return sdkapproval.Decision{}, ctx.Err()
		case <-deadlineTimer.C:
			// Local deadline at row.ExpiresAt — the approver's
			// authoritative timeout boundary. CAS-flip the row
			// to timed_out (winner stands; user-decide racing at
			// the boundary still wins because the store's UPDATE
			// keys on status='pending'). After the call, re-read
			// the row: if we won, status is timed_out; if user
			// won, status is approved/denied.
			if err := t.svc.store.ExpireChatToolApproval(ctx, approvalID, t.ownerID); err != nil &&
				!errors.Is(err, store.ErrChatApprovalAlreadyDecided) {
				// AlreadyDecided is the user-won race — fall
				// through to the row re-fetch which surfaces the
				// user's decision. Any other error is real (DB
				// down, owner-scope drift, NotFound). Surface it.
				return sdkapproval.Decision{}, fmt.Errorf("approver: expire row: %w", err)
			}
			if dec, ok, err := t.tryFetch(ctx, approvalID); err != nil {
				return sdkapproval.Decision{}, err
			} else if ok {
				return dec, nil
			}
			// Should be unreachable: ExpireChatToolApproval
			// either flipped the row to timed_out OR a user-decide
			// already committed approved/denied. tryFetch in both
			// cases sees a terminal status. If we somehow read
			// pending, the next tick will catch up.
		case ev, open := <-subEvents:
			if !open {
				// Subscription closed (broker shutdown). Fall
				// through to poll-only.
				subEvents = nil
				continue
			}
			if ev.Type != events.EventTypeApprovalDecided || ev.EntityID != approvalID {
				// Different approval (this user could have
				// multiple in-flight) or a different event type
				// landed in the user-audience channel.
				continue
			}
			if dec, ok, err := t.tryFetch(ctx, approvalID); err != nil {
				return sdkapproval.Decision{}, err
			} else if ok {
				return dec, nil
			}
			// Broker said "decided" but the row read still shows
			// pending. Possible if the publish raced ahead of the
			// row's commit visibility (READ COMMITTED snapshot).
			// Fall through; the next poll tick or repeat broker
			// event will catch it.
		case <-pollTicker.C:
			if dec, ok, err := t.tryFetch(ctx, approvalID); err != nil {
				return sdkapproval.Decision{}, err
			} else if ok {
				return dec, nil
			}
		}
	}
}

// tryFetch reads the row once. ok=true means the row is in a
// terminal state and dec carries the SDK Decision; ok=false
// means status is still pending and we should keep waiting.
//
// Reasons strings are stable identifiers, not user-facing copy:
// the SDK surfaces them on the request_approval tool result so
// the agent (and downstream policy gates) can branch on them.
func (t *turnApprover) tryFetch(ctx context.Context, approvalID string) (sdkapproval.Decision, bool, error) {
	row, err := t.svc.store.GetChatToolApproval(ctx, approvalID, t.ownerID)
	if err != nil {
		return sdkapproval.Decision{}, false, fmt.Errorf("approver: fetch row: %w", err)
	}
	switch row.Status {
	case model.ChatToolApprovalStatusPending:
		return sdkapproval.Decision{}, false, nil
	case model.ChatToolApprovalStatusApproved:
		return sdkapproval.Decision{Approved: true, Reason: ReasonUserApproved}, true, nil
	case model.ChatToolApprovalStatusDenied:
		return sdkapproval.Decision{Approved: false, Reason: ReasonUserDenied}, true, nil
	case model.ChatToolApprovalStatusTimedOut:
		return sdkapproval.Decision{Approved: false, Reason: ReasonTimedOut}, true, nil
	default:
		// Defensive: an unknown status is treated as a denial
		// rather than silently looping forever. If a future
		// migration adds a new status, this gives the agent a
		// terminal answer; the raw status is logged so divergence
		// is operationally visible.
		t.svc.logger.WarnContext(ctx, "approver: unknown row status",
			"approval_id", approvalID,
			"status", row.Status)
		return sdkapproval.Decision{Approved: false, Reason: ReasonUnknownStatus}, true, nil
	}
}
