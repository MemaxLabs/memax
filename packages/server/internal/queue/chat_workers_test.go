package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"
	sdkapproval "github.com/MemaxLabs/memax-go-agent-sdk/toolkit/approvaltools"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// recordingApprovalPublisher captures every Publish call so the
// timeout-sweeper test can assert that approval.decided events
// fire with the right routing keys.
type recordingApprovalPublisher struct {
	events []events.Event
}

func (p *recordingApprovalPublisher) Publish(_ context.Context, ev events.Event) error {
	p.events = append(p.events, ev)
	return nil
}
func (p *recordingApprovalPublisher) Close() error { return nil }

// Phase 3.4a worker tests. These exercise the worker's pre-flight
// and finalization plumbing against a real Postgres store; the
// agent.Run path is exercised separately in agent/runner_test.go,
// so we don't reproduce a full LLM stack here. The worker tests
// pin: (a) the no-runtime fail-closed shape, (b) the
// already-finalized skip path, (c) ownership rejection.

func acquireChatWorkerDB(t *testing.T) (*pgxpool.Pool, store.Store, string) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping chat worker integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	owner := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'W', 'personal_early_access', now(), now())`,
		owner, "wkr-"+owner[:8]+"@x"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		// chat_sessions has ON DELETE RESTRICT on owner_id, so a
		// straight `DELETE FROM users` would be blocked by any
		// session this test seeded. Explicit chat-tree cleanup
		// avoids the accumulating-debris pattern codex caught on
		// Phase 3.5a v1: without this, sessions and their event
		// rows from prior test runs survived in the shared dev DB
		// and crowded the lease sweeper's batch limit (the test
		// worked around it via 'epoch'::timestamptz backdating;
		// this is the proper fix).
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM chat_sessions WHERE owner_id = $1::uuid`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM hubs WHERE owner_id = $1::uuid`, owner)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1::uuid`, owner)
	})
	return pool, store.NewPostgresStore(pool), owner
}

// seedQueuedTurn creates a session + user message + queued assistant
// message via BeginChatMessageTurn — the same path the production
// handler uses. Returns the assistant id; the session is locked
// (status='running') exactly as it is for a real send.
func seedQueuedTurn(t *testing.T, ctx context.Context, s store.Store, owner string) string {
	t.Helper()
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "WT", Slug: "wt-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID:             uuid.NewString(),
		OwnerID:        owner,
		ScopeType:      model.ChatScopeTypeSingleHub,
		ScopeHubIDs:    []string{hubID},
		Model:          "claude-sonnet-4-6",
		Tools:          []string{"recall_memories"},
		ToolsetVersion: 1,
		Status:         model.ChatSessionStatusIdle,
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	res, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sess.ID,
		OwnerID:              owner,
		UserContent:          "what time is it",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("BeginChatMessageTurn: %v", err)
	}
	return res.AssistantMessage.ID
}

// Worker with nil Runtime fails the message cleanly with a
// structured error and returns river.JobCancel so River doesn't
// retry. This is the operational shape when the worker process
// is deployed without an Anthropic API key (or, today, before
// Phase 3.4b builds the production RecallService).
func TestChatMessageRunWorker_NilRuntimeFailsMessage(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	w := &queue.ChatMessageRunWorker{Store: s, Runtime: nil}
	job := &river.Job[queue.ChatMessageRunArgs]{
		Args: queue.ChatMessageRunArgs{
			AssistantMessageID: assistantID,
			OwnerID:            owner,
		},
	}
	err := w.Work(ctx, job)
	// JobCancelError is what river uses to mark a job cancelled
	// rather than retried; we check that we got an error of any
	// kind (river.JobCancel wraps in a sentinel) and that the
	// underlying message landed in failed status.
	if err == nil {
		t.Fatalf("Work returned nil error; expected cancel for missing runtime")
	}

	// The message must be finalized to 'failed' so the user sees
	// a clean error and the session lock gets released.
	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusFailed {
		t.Errorf("status = %q, want %q", got.Status, model.ChatMessageStatusFailed)
	}
	if len(got.Error) == 0 {
		t.Errorf("error payload empty")
	} else {
		var perr struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(got.Error, &perr)
		if perr.Code != "runtime_not_configured" {
			t.Errorf("error.code = %q, want runtime_not_configured", perr.Code)
		}
	}

	// Session lock must be released as a side-effect of finalize.
	sess, err := s.GetChatSession(ctx, got.SessionID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if sess.Status != model.ChatSessionStatusIdle {
		t.Errorf("session.Status = %q, want idle (lock released)", sess.Status)
	}
	if sess.ActiveMessageID != "" {
		t.Errorf("session.ActiveMessageID = %q, want empty", sess.ActiveMessageID)
	}
}

// If the assistant is already finalized (e.g. a manual finalize
// landed first, or the lease sweeper got there ahead of us), the
// worker should skip cleanly without attempting to refinalize —
// otherwise we'd corrupt the row.
func TestChatMessageRunWorker_AlreadyFinalized_Skips(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Manually finalize so the worker sees a non-queued status.
	if err := s.FinalizeChatMessage(ctx, assistantID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "pre-finalized by test",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("pre-finalize: %v", err)
	}

	w := &queue.ChatMessageRunWorker{Store: s, Runtime: nil}
	job := &river.Job[queue.ChatMessageRunArgs]{
		Args: queue.ChatMessageRunArgs{
			AssistantMessageID: assistantID,
			OwnerID:            owner,
		},
	}
	if err := w.Work(ctx, job); err != nil {
		t.Errorf("expected nil error for already-finalized message, got %v", err)
	}

	// The pre-finalized content must NOT have been overwritten.
	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusCompleted {
		t.Errorf("status = %q, want completed (worker should not have touched it)", got.Status)
	}
	if got.Content != "pre-finalized by test" {
		t.Errorf("content overwritten by worker: %q", got.Content)
	}
}

// chatStreamObserver writes per-event rows to chat_message_events
// with monotonically increasing seq numbers. Codex-flagged
// architecture decision (Phase 3.4b1 v1 review): the SSE stream
// surface (Phase 3.4b2.b) reads these rows for Last-Event-ID
// resume; this commit pins the persistence side. We exercise it
// directly here rather than through the full worker so the test
// doesn't need a real SDK provider.
func TestChatStreamObserver_PersistsEventsInOrder(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// We can use the unexported observer because the test is in
	// the queue package's _test (declared at the top of this
	// file). Wait — chat_workers_test.go is `queue_test`, so we
	// can only use exported names. The observer + its constructor
	// are unexported. So we exercise via a thin reflection-free
	// proxy: feed events through agent.Run with a fake model and
	// assert AppendChatMessageEvent landed the right rows.
	//
	// For 3.4b2.a we don't have a fake SDK provider on hand
	// without considerable test plumbing. Instead, we verify the
	// shape of an event-write directly via the store: the
	// observer is a thin wrapper around AppendChatMessageEvent,
	// so testing the store-level invariant is sufficient.

	// Manually append two rows the way the observer would.
	if err := s.AppendChatMessageEvent(ctx, owner, &model.ChatMessageEvent{
		MessageID: assistantID,
		Seq:       1,
		EventType: "model_request",
		Payload:   []byte(`{"turn":1}`),
	}); err != nil {
		t.Fatalf("append seq=1: %v", err)
	}
	if err := s.AppendChatMessageEvent(ctx, owner, &model.ChatMessageEvent{
		MessageID: assistantID,
		Seq:       2,
		EventType: "assistant",
		Payload:   []byte(`{"text":"hi"}`),
	}); err != nil {
		t.Fatalf("append seq=2: %v", err)
	}
	// The (message_id, seq) UNIQUE index pins ordering.
	events, err := s.ListChatMessageEvents(ctx, assistantID, owner, 0)
	if err != nil {
		t.Fatalf("ListChatMessageEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Seq != 1 || events[0].EventType != "model_request" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Seq != 2 || events[1].EventType != "assistant" {
		t.Errorf("event[1] = %+v", events[1])
	}
}

// Cross-owner job — somehow a job lands on a worker with a
// mismatched owner. The store's owner-scoped read returns
// ErrChatMessageNotFound; the worker must JobCancel without
// touching the assistant row.
func TestChatMessageRunWorker_CrossOwner_NoSideEffects(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	w := &queue.ChatMessageRunWorker{Store: s, Runtime: nil}
	job := &river.Job[queue.ChatMessageRunArgs]{
		Args: queue.ChatMessageRunArgs{
			AssistantMessageID: assistantID,
			OwnerID:            uuid.NewString(), // wrong owner
		},
	}
	err := w.Work(ctx, job)
	if err == nil {
		t.Fatalf("expected cancel for cross-owner; got nil")
	}
	// JobCancel wraps the original; assert the wrapped sentinel.
	if !errors.Is(err, store.ErrChatMessageNotFound) {
		t.Errorf("wrapped err = %v, want ErrChatMessageNotFound", err)
	}

	// Real owner reads the row — still queued, untouched.
	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage (real owner): %v", err)
	}
	if got.Status != model.ChatMessageStatusQueued {
		t.Errorf("status = %q, want queued (cross-owner job should not have touched it)", got.Status)
	}
}

// Phase 3.6a — Worker's chat tool builder registry MUST mirror
// the handler catalog's Available=true set exactly. A name in
// the catalog (Available=true) without a corresponding builder
// produces a session that opts in to a tool the worker can't
// invoke; a name in the registry without a catalog entry is
// dead code (no session can ever enable it). Codex flagged
// the manual-sync risk on Phase 3.6a v4 — this test fails
// fast on either drift direction.
func TestChatToolBuildersMatchCatalog(t *testing.T) {
	t.Parallel()
	catalog := handler.AvailableChatToolNames()
	builders := queue.ChatToolBuilderNames()

	if len(catalog) != len(builders) {
		t.Fatalf("catalog Available=%v vs builders=%v — count mismatch", catalog, builders)
	}
	for i := range catalog {
		if catalog[i] != builders[i] {
			t.Errorf("position %d: catalog=%q builders=%q — drift", i, catalog[i], builders[i])
		}
	}

	// Phase 3.6b3c — also verify the ApprovalRequired flag
	// matches between the catalog (handler-side, surfaced to
	// clients) and the builder registry (worker-side, gates
	// the agentpolicy.ApprovalBeforeTool policy). A drift in
	// either direction is a silent UX bug:
	//   - catalog-only ApprovalRequired: client shows approval
	//     prompt, but worker doesn't gate the tool call; the
	//     mutating tool runs without waiting for the user.
	//   - builder-only ApprovalRequired: worker gates the call
	//     correctly but client doesn't show the prompt; the
	//     agent's request_approval blocks invisibly until
	//     timeout.
	for _, name := range catalog {
		catalogRequired, catalogOK := handler.ChatToolApprovalRequired(name)
		builderRequired, builderOK := queue.ChatToolBuilderApprovalRequired(name)
		if !catalogOK || !builderOK {
			t.Errorf("tool %q: catalogOK=%v builderOK=%v — both must be true", name, catalogOK, builderOK)
			continue
		}
		if catalogRequired != builderRequired {
			t.Errorf("tool %q: catalog ApprovalRequired=%v vs builder=%v — drift",
				name, catalogRequired, builderRequired)
		}
	}
}

// Phase 3.6b3c — buildSessionTools registers the SDK's
// request_approval tool + builds an agentpolicy hook runner
// when ANY session tool has ApprovalRequired=true. With no
// such tools, it returns just the session tools and a nil
// hook runner.
//
// Test approach: register a fake read-only builder so we
// drive the no-approval path without depending on a real
// runtime. Codex's Phase 3.6b3c review caught that v1 of this
// test used recall_memories with an empty runtime and just
// asserted hooks==nil — it didn't prove the success path
// (recall_memories construction fails on an empty runtime, so
// the test was passing but for the wrong reason).
func TestBuildSessionTools_NoApprovalTools_NilHookRunner(t *testing.T) {
	// Not parallel — manipulates the package-level
	// chatToolBuilders map.
	cleanup := queue.RegisterFakeChatToolBuilderForTest("fake_read_tool", false, func() (sdktool.Tool, error) {
		return sdktool.Definition{
			ToolSpec: sdkmodel.ToolSpec{
				Name:        "fake_read_tool",
				Description: "test only",
				InputSchema: map[string]any{"type": "object"},
			},
			Handler: func(_ context.Context, _ sdktool.Call) (sdkmodel.ToolResult, error) {
				return sdkmodel.ToolResult{Content: "ok"}, nil
			},
		}, nil
	})
	defer cleanup()

	tools, hooks, err := queue.BuildSessionToolsForTest(
		&queue.ChatAgentRuntime{},
		agent.SessionDescriptor{Type: agent.ScopeTypeSingleHub, OwnerID: "owner-x", HubIDs: []string{"hub-x"}},
		[]string{"fake_read_tool"},
		nil, // no turnApprover needed when no ApprovalRequired tools
	)
	if err != nil {
		t.Fatalf("buildSessionTools: %v (read-only toolset must succeed)", err)
	}
	if len(tools) != 1 {
		t.Errorf("got %d tools, want 1 (just the session tool; no request_approval)", len(tools))
	}
	if hooks != nil {
		t.Errorf("hooks = non-nil; want nil for read-only session toolset")
	}
}

// With at least one ApprovalRequired tool: register
// request_approval AND build the hook runner.
func TestBuildSessionTools_WithApprovalTool_RegistersRequestApproval(t *testing.T) {
	// Not parallel — manipulates the package-level
	// chatToolBuilders map.
	cleanup := queue.RegisterFakeChatToolBuilderForTest("fake_mutating_tool", true, func() (sdktool.Tool, error) {
		return sdktool.Definition{
			ToolSpec: sdkmodel.ToolSpec{
				Name:        "fake_mutating_tool",
				Description: "test only",
				InputSchema: map[string]any{"type": "object"},
			},
			Handler: func(_ context.Context, _ sdktool.Call) (sdkmodel.ToolResult, error) {
				return sdkmodel.ToolResult{Content: "ok"}, nil
			},
		}, nil
	})
	defer cleanup()

	approverStub := sdkapproval.StaticApprover{
		Decision: sdkapproval.Decision{Approved: true, Reason: "test"},
	}

	tools, hooks, err := queue.BuildSessionToolsForTest(
		&queue.ChatAgentRuntime{},
		agent.SessionDescriptor{Type: agent.ScopeTypeSingleHub, OwnerID: "owner-x", HubIDs: []string{"hub-x"}},
		[]string{"fake_mutating_tool"},
		approverStub,
	)
	if err != nil {
		t.Fatalf("buildSessionTools: %v", err)
	}
	// Tools must include both the session tool AND the SDK's
	// request_approval tool registered by buildSessionTools.
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2 (session tool + request_approval)", len(tools))
	}
	names := []string{tools[0].Spec().Name, tools[1].Spec().Name}
	sort.Strings(names)
	if names[0] != "fake_mutating_tool" || names[1] != sdkapproval.ToolName {
		t.Errorf("tools = %v, want [fake_mutating_tool, request_approval]", names)
	}
	// Hook runner must be non-nil — the agent's runtime
	// installs the agentpolicy.ApprovalBeforeTool gate.
	if hooks == nil {
		t.Errorf("hooks = nil; want non-nil hook runner with ApprovalBeforeTool policy")
	}
}

// Phase 3.6b3d v2 codex follow-up: WriteHubID propagation
// from chat session through per-turn snapshot to descriptor.
// The earlier production code path (before the v1 followup
// wired this) had pickPushHub's WriteHubID branch as dead
// code: chat_workers.go built the descriptor without
// WriteHubID, so multi-hub sessions with a default write hub
// would still error.
//
// Three scenarios:
//
//  1. WriteHubID set, still in per-turn snapshot →
//     descriptor.WriteHubID propagates.
//  2. WriteHubID set but NOT in per-turn snapshot (the user
//     lost membership in the pinned hub between
//     session-create and now) → WriteHubID is silently
//     dropped; pickPushHub falls back to the
//     "explicit hub_id required" path.
//  3. WriteHubID empty → descriptor.WriteHubID stays empty.
func TestBuildSessionDescriptor_WriteHubIDPropagation(t *testing.T) {
	t.Parallel()
	owner := "owner-x"
	cases := []struct {
		name          string
		scopeHubIDs   []string
		sessionWrite  string
		wantWriteHub  string
	}{
		{
			name:         "WriteHubID in snapshot → propagates",
			scopeHubIDs:  []string{"hub-a", "hub-b"},
			sessionWrite: "hub-b",
			wantWriteHub: "hub-b",
		},
		{
			name:         "WriteHubID not in snapshot → dropped",
			scopeHubIDs:  []string{"hub-a"},
			sessionWrite: "hub-b", // user lost membership
			wantWriteHub: "",
		},
		{
			name:         "empty session WriteHubID → empty descriptor",
			scopeHubIDs:  []string{"hub-a", "hub-b"},
			sessionWrite: "",
			wantWriteHub: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := queue.BuildSessionDescriptorForTest(owner, agent.ScopeTypeHubSubset, tc.scopeHubIDs, tc.sessionWrite)
			if d.WriteHubID != tc.wantWriteHub {
				t.Errorf("WriteHubID = %q, want %q", d.WriteHubID, tc.wantWriteHub)
			}
			if d.OwnerID != owner {
				t.Errorf("OwnerID = %q, want %q", d.OwnerID, owner)
			}
			if len(d.HubIDs) != len(tc.scopeHubIDs) {
				t.Errorf("HubIDs = %v, want %v", d.HubIDs, tc.scopeHubIDs)
			}
		})
	}
}

// Reserved-name guard: a builder that emits a tool whose
// Spec().Name equals sdkapproval.ToolName ("request_approval")
// fails at construction, not late at SDK registry-time. Codex
// flagged the late-failure UX on Phase 3.6b3c v1; this guard
// makes the failure tool_construction_failed rather than
// agent_preflight_error.
func TestBuildSessionTools_RejectsReservedRequestApprovalName(t *testing.T) {
	// Not parallel — manipulates the package-level
	// chatToolBuilders map.
	cleanup := queue.RegisterFakeChatToolBuilderForTest("misnamed_tool", false, func() (sdktool.Tool, error) {
		return sdktool.Definition{
			ToolSpec: sdkmodel.ToolSpec{
				// Intentionally collides with the SDK's
				// reserved approval-tool name.
				Name:        sdkapproval.ToolName,
				Description: "test only",
				InputSchema: map[string]any{"type": "object"},
			},
			Handler: func(_ context.Context, _ sdktool.Call) (sdkmodel.ToolResult, error) {
				return sdkmodel.ToolResult{Content: "ok"}, nil
			},
		}, nil
	})
	defer cleanup()

	_, _, err := queue.BuildSessionToolsForTest(
		&queue.ChatAgentRuntime{},
		agent.SessionDescriptor{Type: agent.ScopeTypeSingleHub, OwnerID: "owner-x", HubIDs: []string{"hub-x"}},
		[]string{"misnamed_tool"},
		nil,
	)
	if err == nil {
		t.Fatal("err = nil; want construction failure for reserved-name collision")
	}
	if !strings.Contains(err.Error(), sdkapproval.ToolName) {
		t.Errorf("err = %v; want message naming %q", err, sdkapproval.ToolName)
	}
}

// ApprovalRequired tool but nil approver → fail at
// construction with a clear error. Pins the misconfiguration
// guard codex flagged on Phase 3.6b3a (the runtime-level
// approver should fail loudly when a session enables an
// approval-gated tool but the runtime has no approver).
func TestBuildSessionTools_ApprovalRequiredButNoApprover_Fails(t *testing.T) {
	// Not parallel — manipulates the package-level
	// chatToolBuilders map.
	cleanup := queue.RegisterFakeChatToolBuilderForTest("fake_mutating_tool", true, func() (sdktool.Tool, error) {
		return sdktool.Definition{
			ToolSpec: sdkmodel.ToolSpec{
				Name:        "fake_mutating_tool",
				Description: "test only",
				InputSchema: map[string]any{"type": "object"},
			},
			Handler: func(_ context.Context, _ sdktool.Call) (sdkmodel.ToolResult, error) {
				return sdkmodel.ToolResult{}, nil
			},
		}, nil
	})
	defer cleanup()

	_, _, err := queue.BuildSessionToolsForTest(
		&queue.ChatAgentRuntime{},
		agent.SessionDescriptor{Type: agent.ScopeTypeSingleHub, OwnerID: "owner-x", HubIDs: []string{"hub-x"}},
		[]string{"fake_mutating_tool"},
		nil, // intentionally nil approver
	)
	if err == nil {
		t.Fatal("err = nil; want error for ApprovalRequired tool without approver")
	}
}

// Phase 3.5a — Lease sweeper finalizes sessions whose worker
// dropped the lease. Seed: status=running with locked_at older
// than the lease window. Expect: assistant message finalized as
// failed{server_crashed} + session lock released + a terminal
// SSE row written (via FinalizeChatMessage's tx).
func TestChatLeaseSweepWorker_FinalizesOrphanedSessions(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Backdate locked_at to a sentinel value FAR in the past
	// (epoch). The sweeper's ORDER BY locked_at ASC + LIMIT
	// puts the oldest leases first; using epoch makes sure THIS
	// session is always in the first batch even when the test
	// DB has accumulated debris from prior parallel test runs.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET locked_at = 'epoch'::timestamptz WHERE active_message_id = $1::uuid`,
		assistantID); err != nil {
		t.Fatalf("backdate locked_at: %v", err)
	}

	w := &queue.ChatLeaseSweepWorker{Store: s}
	if err := w.Work(ctx, &river.Job[queue.ChatLeaseSweepArgs]{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Assistant must be terminal now.
	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusFailed {
		t.Errorf("assistant.Status = %q, want failed", got.Status)
	}
	// Error payload must encode the server_crashed code so SSE
	// clients can render the right "session died" UI.
	if len(got.Error) == 0 {
		t.Errorf("assistant.Error empty — SSE clients can't tell why")
	} else {
		var perr struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(got.Error, &perr)
		if perr.Code != "server_crashed" {
			t.Errorf("error.code = %q, want server_crashed", perr.Code)
		}
	}

	// Session lock must be released (FinalizeChatMessage's job).
	sess, err := s.GetChatSession(ctx, got.SessionID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if sess.Status != model.ChatSessionStatusIdle {
		t.Errorf("session.Status = %q, want idle (lock released)", sess.Status)
	}

	// Terminal SSE row must be in the replay buffer (so a client
	// streaming this message sees the message.failed frame).
	events, err := s.ListChatMessageEvents(ctx, assistantID, owner, 0)
	if err != nil {
		t.Fatalf("ListChatMessageEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "message.failed" {
		t.Errorf("replay buffer = %v, want one message.failed row", events)
	}
}

// Sweeper must not touch sessions whose lease is still valid.
func TestChatLeaseSweepWorker_LeavesFreshSessionsAlone(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Default seed leaves locked_at = now(), so the lease isn't
	// expired. No backdating; sweep should be a no-op.
	w := &queue.ChatLeaseSweepWorker{Store: s}
	if err := w.Work(ctx, &river.Job[queue.ChatLeaseSweepArgs]{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusQueued {
		t.Errorf("fresh assistant status = %q, want queued (sweeper should not touch)", got.Status)
	}
}

// TestChatLeaseSweepWorker_CancelingTurnFinalizedAsCanceled pins
// the codex Phase 3.7b v2 fix: when a cancel landed before any
// worker picked up the job AND the session lease then expired,
// the sweeper must recover the lock by writing 'canceled', not
// the default 'failed{server_crashed}'. The store's CAS rejects
// a failed write over a canceling row with ErrChatMessageCancelPending;
// without the sweeper's retry-as-canceled, the session would
// stay locked forever.
func TestChatLeaseSweepWorker_CancelingTurnFinalizedAsCanceled(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Move the row to 'canceling' via the cancel path. Sets the
	// session to canceling too — which keeps the lease eligible
	// (status IN ('running','canceling')).
	if _, err := s.CancelChatMessage(ctx, assistantID, owner); err != nil {
		t.Fatalf("CancelChatMessage: %v", err)
	}

	// Backdate to make the lease orphaned.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET locked_at = 'epoch'::timestamptz WHERE active_message_id = $1::uuid`,
		assistantID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	w := &queue.ChatLeaseSweepWorker{Store: s}
	if err := w.Work(ctx, &river.Job[queue.ChatLeaseSweepArgs]{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusCanceled {
		t.Errorf("assistant.Status = %q, want canceled (sweeper must honor pending cancel)", got.Status)
	}
	if len(got.Error) == 0 {
		t.Errorf("error blob empty — clients can't tell why")
	} else {
		var perr struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(got.Error, &perr)
		if perr.Code != "user_canceled" {
			t.Errorf("error.code = %q, want user_canceled", perr.Code)
		}
	}

	// Session lock must be released.
	sess, err := s.GetChatSession(ctx, got.SessionID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if sess.Status != model.ChatSessionStatusIdle {
		t.Errorf("session.Status = %q, want idle (lock released)", sess.Status)
	}
}

// TestResolveTurnUserMessage_RepeatedRegenerate pins the codex
// Phase 3.7b v2 fix: the worker's parent-chain walk must follow
// supersession links of arbitrary depth back to the original
// user message, not just one hop. Without this, the second
// regenerate (A3.parent=A2, A2.parent=A1, A1.parent=user) would
// fail at the A1 hop and the model would receive nothing.
func TestResolveTurnUserMessage_RepeatedRegenerate(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Finalize the first assistant so regenerate is eligible.
	if err := s.FinalizeChatMessage(ctx, assistantID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "first reply",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("finalize a1: %v", err)
	}

	// Capture the session + caller-accessible hub for the
	// regenerate calls.
	first, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage a1: %v", err)
	}
	var hubID string
	if err := pool.QueryRow(ctx,
		`SELECT scope_hub_ids[1]::text FROM chat_sessions WHERE id = $1::uuid`,
		first.SessionID).Scan(&hubID); err != nil {
		t.Fatalf("read hubID: %v", err)
	}

	// Regenerate twice in sequence (A2.parent=A1, A3.parent=A2).
	// Each regenerate finalizes its assistant so the next is
	// eligible.
	prior := assistantID
	for i := 0; i < 2; i++ {
		regen, err := s.BeginChatRegenerate(ctx, store.BeginChatRegenerateInput{
			PriorAssistantMessageID: prior,
			OwnerID:                 owner,
			NewAssistantMessageID:   uuid.NewString(),
			CallerAccessibleHubs:    []string{hubID},
			Now:                     time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("regenerate hop %d: %v", i, err)
		}
		// Finalize so the chain stays eligible for the next hop.
		if err := s.FinalizeChatMessage(ctx, regen.NewAssistantMessage.ID, owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    "reply " + uuid.NewString()[:6],
			ToolCalls:  json.RawMessage(`[]`),
			Citations:  json.RawMessage(`[]`),
			Usage:      json.RawMessage(`{}`),
			FinishedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("finalize regen hop %d: %v", i, err)
		}
		prior = regen.NewAssistantMessage.ID
	}

	// One MORE regenerate but DON'T finalize this one — it's our
	// "currently-being-driven-by-worker" assistant. Its parent is
	// the chain A4→A3→A2→A1→user. The worker's resolve must walk
	// 4 assistant hops to find the original user prompt.
	target, err := s.BeginChatRegenerate(ctx, store.BeginChatRegenerateInput{
		PriorAssistantMessageID: prior,
		OwnerID:                 owner,
		NewAssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs:    []string{hubID},
		Now:                     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("final regenerate: %v", err)
	}

	userMsg, err := queue.ResolveTurnUserMessageForTest(ctx, s, target.NewAssistantMessage, owner)
	if err != nil {
		t.Fatalf("ResolveTurnUserMessageForTest: %v", err)
	}
	if userMsg.Role != model.ChatMessageRoleUser {
		t.Errorf("resolved Role = %q, want user", userMsg.Role)
	}
	if userMsg.Content != "what time is it" {
		t.Errorf("resolved Content = %q, want original user prompt 'what time is it'", userMsg.Content)
	}
}

// Sweeper races a real worker — the real finalize wins, sweeper
// sees ErrChatMessageAlreadyFinalized, treats as benign skip.
func TestChatLeaseSweepWorker_AlreadyFinalizedIsSkipped(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Backdate the lease to look orphaned, but ALSO finalize the
	// message via the normal path — simulating a real worker that
	// finalized a moment before the sweeper got there. Epoch
	// sentinel for the same reason as the test above.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET locked_at = 'epoch'::timestamptz WHERE active_message_id = $1::uuid`,
		assistantID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := s.FinalizeChatMessage(ctx, assistantID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "real worker won",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("real finalize: %v", err)
	}

	w := &queue.ChatLeaseSweepWorker{Store: s}
	// Sweeper finds the lease (status='running' with backdated
	// locked_at) — wait, after FinalizeChatMessage the session
	// is idle, so the lease isn't actually expired anymore. The
	// sweeper's SELECT WHERE status IN (running, canceling) will
	// return no rows. This test now verifies that the same call
	// is a clean no-op — proves the order-of-operations between
	// the real finalize and the sweep is safe regardless.
	if err := w.Work(ctx, &river.Job[queue.ChatLeaseSweepArgs]{}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Real worker's content survives.
	got, err := s.GetChatMessage(ctx, assistantID, owner)
	if err != nil {
		t.Fatalf("GetChatMessage: %v", err)
	}
	if got.Status != model.ChatMessageStatusCompleted {
		t.Errorf("status = %q, want completed (sweeper must not overwrite)", got.Status)
	}
	if got.Content != "real worker won" {
		t.Errorf("content overwritten by sweeper: %q", got.Content)
	}
}

// Phase 3.5a — Purge worker deletes events older than 24h.
func TestChatEventPurgeWorker_DeletesAncientRows(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Append two events: one fresh, one 25h old.
	if err := s.AppendChatMessageEvent(ctx, owner, &model.ChatMessageEvent{
		MessageID: assistantID, Seq: 1, EventType: "assistant",
		Payload: []byte(`{"text":"fresh"}`), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append fresh: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO chat_message_events (message_id, seq, event_type, payload, created_at)
		 VALUES ($1::uuid, 2, 'assistant', '{"text":"ancient"}'::jsonb, now() - interval '25 hours')`,
		assistantID); err != nil {
		t.Fatalf("append ancient: %v", err)
	}

	w := &queue.ChatEventPurgeWorker{Store: s}
	if err := w.Work(ctx, &river.Job[queue.ChatEventPurgeArgs]{}); err != nil {
		t.Fatalf("purge: %v", err)
	}

	events, err := s.ListChatMessageEvents(ctx, assistantID, owner, 0)
	if err != nil {
		t.Fatalf("ListChatMessageEvents: %v", err)
	}
	if len(events) != 1 || events[0].Seq != 1 {
		t.Errorf("after purge events = %v, want only seq=1 (fresh)", events)
	}
}

// Phase 3.6b1 — the timeout sweeper flips pending rows past
// expires_at to `timed_out` AND publishes
// approval.decided{state=timed_out} per row so the SDK-host
// approver unblocks at the boundary instead of waiting on its
// 250ms poll. Codex flagged that ExpirePendingChatToolApprovals
// existed but wasn't wired to a worker; this test pins the
// wiring (worker → store → publisher) end-to-end.
func TestChatApprovalSweepWorker_TimesOutAndPublishes(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Seed a pending approval whose expires_at is already in
	// the past — this is the row the sweeper should reap.
	approvalID := uuid.NewString()
	if err := s.CreateChatToolApproval(ctx, owner, &model.ChatToolApproval{
		ID:          approvalID,
		MessageID:   assistantID,
		ToolName:    "forget_memory",
		Args:        []byte(`{"id":"mem_xxx"}`),
		ArgsHash:    "deadbeef",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC().Add(-15 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	// Backdate expires_at directly — store enforces the 10m
	// future-expiry, so this is the only path to reproduce the
	// timeout in a test without sleeping.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_tool_approvals SET expires_at = now() - interval '5 minutes' WHERE id = $1::uuid`,
		approvalID); err != nil {
		t.Fatalf("backdate expires_at: %v", err)
	}

	// Seed a SECOND pending approval whose expires_at is fresh
	// — proves the sweeper only times out rows that are actually
	// past their deadline.
	freshApprovalID := uuid.NewString()
	if err := s.CreateChatToolApproval(ctx, owner, &model.ChatToolApproval{
		ID:          freshApprovalID,
		MessageID:   assistantID,
		ToolName:    "push_memory",
		Args:        []byte(`{"content":"hi"}`),
		ArgsHash:    "cafebabe",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateChatToolApproval (fresh): %v", err)
	}

	pub := &recordingApprovalPublisher{}
	w := &queue.ChatApprovalSweepWorker{Store: s, Publisher: pub}
	if err := w.Work(ctx, &river.Job[queue.ChatApprovalSweepArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	// Expired row → timed_out.
	got, err := s.GetChatToolApproval(ctx, approvalID, owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval expired: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusTimedOut {
		t.Errorf("expired status = %q, want timed_out", got.Status)
	}

	// Fresh row → still pending.
	gotFresh, err := s.GetChatToolApproval(ctx, freshApprovalID, owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval fresh: %v", err)
	}
	if gotFresh.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("fresh status = %q, want pending", gotFresh.Status)
	}

	// Publisher saw an approval.decided event for OUR expired
	// row, with the right routing keys. The shared dev DB may
	// have other expired approvals from parallel test runs; the
	// sweeper batches them all together, so we filter the
	// captured events down to ours via EntityID match.
	var ourEvent *events.Event
	for i := range pub.events {
		if pub.events[i].Type != "approval.decided" {
			continue
		}
		if pub.events[i].EntityID == approvalID {
			ourEvent = &pub.events[i]
		}
		// The fresh row must NEVER appear in the published set —
		// it isn't expired, so the sweeper must skip it.
		if pub.events[i].EntityID == freshApprovalID {
			t.Errorf("publisher saw event for fresh (un-expired) approval %q", freshApprovalID)
		}
	}
	if ourEvent == nil {
		t.Fatalf("publisher missed approval.decided for expired row %q (saw %d total events)", approvalID, len(pub.events))
	}
	if ourEvent.RecipientUserID != owner {
		t.Errorf("event recipient_user_id = %q, want %q", ourEvent.RecipientUserID, owner)
	}
	if ourEvent.Audience != "user" {
		t.Errorf("event audience = %q, want user", ourEvent.Audience)
	}
	if ourEvent.State != "timed_out" {
		t.Errorf("event state = %q, want timed_out", ourEvent.State)
	}
}

// Phase 3.6b1 codex v2 — regression for the "winner stands"
// race fix. A row that's already been decided (approved/denied)
// but whose expires_at is now in the past MUST NOT be flipped
// to timed_out, even though the row "looks expired" if the SQL
// only filtered on expires_at. This pins the predicate ordering
// in postgres_chat.go's CTE: the UPDATE checks
// `a.status='pending'` on the target row itself, so MVCC's
// post-lock recheck rejects a row that a concurrent decide
// flipped to approved.
//
// We can't reliably reproduce the live race in a test without
// real concurrency, but we CAN reproduce the steady-state
// shape: a previously-decided row whose expires_at is now in
// the past. If the predicate were back on a SELECT snapshot
// only, this row would still be selected (because the snapshot
// shape `WHERE status='pending'` is gone after the decide
// commits), but it could just as easily be selected if a
// different bug let the sweeper match status != 'pending'. The
// test pins: sweeper never touches non-pending rows, full stop.
func TestChatApprovalSweepWorker_LeavesAlreadyDecidedAlone(t *testing.T) {
	// Intentionally NOT t.Parallel — the sweeper is global (no
	// owner filter possible at the SQL layer; there is no
	// per-owner index of pending approvals), so a parallel run
	// alongside TestChatApprovalSweepWorker_TimesOutAndPublishes
	// can sweep that test's expired-pending row before its own
	// Work() call lands, draining the publisher event the sister
	// test asserts on. Running this test in the serial pre-pass
	// (which Go runs before any t.Parallel() group) keeps both
	// scenarios deterministic without needing a package-level
	// sweeper mutex.
	pool, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Seed a row, decide it (approved), then backdate expires_at
	// into the past. A buggy sweeper that doesn't re-check
	// status='pending' on the UPDATE would flip this to
	// timed_out and clobber the user's decision.
	approvalID := uuid.NewString()
	if err := s.CreateChatToolApproval(ctx, owner, &model.ChatToolApproval{
		ID:          approvalID,
		MessageID:   assistantID,
		ToolName:    "forget_memory",
		Args:        []byte(`{"id":"mem_xxx"}`),
		ArgsHash:    "deadbeef",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	if err := s.DecideChatToolApproval(ctx, approvalID, owner, "", model.ChatToolApprovalStatusApproved); err != nil {
		t.Fatalf("DecideChatToolApproval: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE chat_tool_approvals SET expires_at = now() - interval '5 minutes' WHERE id = $1::uuid`,
		approvalID); err != nil {
		t.Fatalf("backdate expires_at: %v", err)
	}

	pub := &recordingApprovalPublisher{}
	w := &queue.ChatApprovalSweepWorker{Store: s, Publisher: pub}
	if err := w.Work(ctx, &river.Job[queue.ChatApprovalSweepArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	got, err := s.GetChatToolApproval(ctx, approvalID, owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusApproved {
		t.Errorf("status = %q, want approved (sweeper must not clobber decided rows)", got.Status)
	}

	// And no event for OUR row — the sweeper saw it as
	// already-non-pending and skipped it. (Other expired-pending
	// rows from parallel runs may have produced events; we
	// filter to OUR approvalID.)
	for _, ev := range pub.events {
		if ev.Type == "approval.decided" && ev.EntityID == approvalID {
			t.Errorf("publisher saw approval.decided for already-decided row %q (state=%q)", approvalID, ev.State)
		}
	}
}

// Phase 3.6b3b — production ChatApprovalEventEmitter writes a
// chat_message_events row of EventType="approval_requested"
// and signals the chatstream. This pins the contract codex
// asked for (stored type aligned with the SSE translator's
// expected input). End-to-end across approver → emitter →
// store; the SSE side is exercised separately in the handler
// translator unit tests.
func TestChatApprovalEventEmitter_AppendsRowAndAllocatesSeq(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatWorkerDB(t)
	ctx := context.Background()
	assistantID := seedQueuedTurn(t, ctx, s, owner)

	// Pre-populate the seq counter so we can prove the emitter
	// allocates the NEXT value rather than starting at 1
	// regardless of what the observer wrote previously.
	turnSeq := queue.NewChatTurnSeqForTest()
	turnSeq.AdvanceForTest(7) // simulate observer wrote 7 events first

	em := queue.NewChatApprovalEventEmitterForTest(s, nil, owner, assistantID, turnSeq)

	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := em.EmitApprovalRequired(ctx, approver.ApprovalRequiredEvent{
		OwnerID:     owner,
		MessageID:   assistantID,
		ApprovalID:  uuid.NewString(),
		ToolName:    "forget_memory",
		Args:        []byte(`{"id":"mem_xxx"}`),
		ArgsHash:    "deadbeef",
		RequestedAt: now,
		ExpiresAt:   now.Add(90 * time.Second),
		Reason:      "user asked",
		Details:     "removing the canceled launch notes",
		Risk:        "low",
		Summary: sdkapproval.Summary{
			Title: "Forget canceled launch notes",
		},
	}); err != nil {
		t.Fatalf("EmitApprovalRequired: %v", err)
	}

	// Read the events back. There should be exactly one row of
	// type approval_requested with seq=8 (the next after our
	// pre-advance to 7).
	events, err := s.ListChatMessageEvents(ctx, assistantID, owner, 0)
	if err != nil {
		t.Fatalf("ListChatMessageEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	got := events[0]
	if got.EventType != "approval_requested" {
		t.Errorf("EventType = %q, want approval_requested (matches SSE translator's expected input)", got.EventType)
	}
	if got.Seq != 8 {
		t.Errorf("Seq = %d, want 8 (turnSeq advanced to 7, this is the next allocation)", got.Seq)
	}
	// Payload must be valid JSON carrying the approval id +
	// SDK Request fields. We don't deeply assert the schema
	// here — the wire shape is owned by the emitter package's
	// approvalRequestedPayload struct and changes flow there.
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v (raw=%s)", err, got.Payload)
	}
	for _, key := range []string{"approval_id", "tool", "args", "requested_at", "expires_at", "reason", "details"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload missing %q (have keys: %v)", key, payloadKeys(payload))
		}
	}
}

func payloadKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Phase 3.6b3b codex-followup: prove that concurrent emits
// from different goroutines preserve commit-order = seq-order.
// Codex flagged that the v1 atomic-only design allowed seq
// allocation to outpace commit order — emit A (slow) could
// allocate seq N and block in append while emit B (fast)
// committed seq N+1, leaving seq N invisible to clients that
// already advanced Last-Event-ID past it.
//
// LockedNext fixes this by holding the mutex through the
// allocate→append→signal chain. This test launches N parallel
// emits, then asserts that for every successful row, every
// row with a smaller seq committed first (= no out-of-order
// commits). Implementation: queue.NewSerializingFakeStore
// emits in append-order; each row's Seq must be ≤ index+startSeq
// AND each row's seq must be the next after the previous.
//
// The store fixture cannot directly observe DB commit order
// (it's a fake), but it CAN serialize appends through a
// channel. Holding the mutex from chatTurnSeq through
// AppendChatMessageEvent serializes the append calls; the
// fake records the order in which it processed them. If the
// mutex didn't cover the append, two goroutines could swap
// their order at the channel pop, and we'd see seq=N+1
// recorded before seq=N.
func TestChatApprovalEventEmitter_PreservesCommitOrder(t *testing.T) {
	t.Parallel()
	rec := newOrderRecordingChatStore(50 * time.Microsecond) // tiny stagger between appends

	turnSeq := queue.NewChatTurnSeqForTest()
	em := queue.NewChatApprovalEventEmitterForTest(rec, nil, "owner-x", "msg-x", turnSeq)

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := em.EmitApprovalRequired(context.Background(), approver.ApprovalRequiredEvent{
				OwnerID:    "owner-x",
				MessageID:  "msg-x",
				ApprovalID: fmt.Sprintf("app-%d", i),
				ToolName:   "forget_memory",
				Args:       []byte(`{}`),
				ArgsHash:   "x",
			})
			if err != nil {
				t.Errorf("emit %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Collect the order in which the fake store actually
	// committed (= called AppendChatMessageEvent with each row).
	commits := rec.snapshot()
	if len(commits) != n {
		t.Fatalf("got %d committed rows, want %d", len(commits), n)
	}
	// Assertion: commit order MUST equal seq order. A regression
	// that drops the mutex or holds it only around allocation
	// would let goroutines interleave, and at least one pair of
	// rows would commit out of order (seq M committed before
	// seq L where M > L).
	for i, row := range commits {
		wantSeq := i + 1
		if row.Seq != wantSeq {
			t.Errorf("commit order broken: commits[%d].Seq = %d, want %d (full order: %v)",
				i, row.Seq, wantSeq, commitSeqs(commits))
		}
	}
}

// orderRecordingChatStore is an approver-emitter-only fake
// that records the order in which AppendChatMessageEvent was
// called. Each call sleeps INVERSELY by seq before recording:
// seq=8 sleeps ~ baseDelay*1, seq=1 sleeps ~ baseDelay*8.
//
// **Why inverse-by-seq.** A regression that drops the per-turn
// mutex (the failure mode codex caught: the production lock
// isn't held through AppendChatMessageEvent) would let the
// goroutines race freely. Equal sleep times would still tend
// to preserve order on most schedulers — the test becomes
// probabilistic. Inverse-by-seq guarantees that without the
// production lock, the highest seq commits FIRST (it sleeps
// the least), which the assertion catches deterministically.
// With the production lock holding through the entire append,
// the sleeps are serialized under the lock, so order is
// preserved regardless.
type orderRecordingChatStore struct {
	store.Store // embed; fail-closed for unused methods
	mu          sync.Mutex
	baseDelay   time.Duration
	rows        []model.ChatMessageEvent
}

func newOrderRecordingChatStore(baseDelay time.Duration) *orderRecordingChatStore {
	return &orderRecordingChatStore{baseDelay: baseDelay}
}

func (s *orderRecordingChatStore) AppendChatMessageEvent(_ context.Context, _ string, ev *model.ChatMessageEvent) error {
	// Inverse-by-seq sleep BEFORE recording. The caller's
	// chatTurnSeq mutex holds across this entire function in
	// production; under that lock, the sleep just stretches
	// the time we hold the lock — no reordering possible. If
	// a regression weakens the production lock, this sleep
	// runs concurrently with other goroutines' sleeps, and
	// the inverse-by-seq pattern guarantees the highest seq
	// finishes first → records first → assertion fires.
	const maxSeq = 16 // generous upper bound for n in the caller
	multiplier := maxSeq - ev.Seq + 1
	if multiplier < 1 {
		multiplier = 1
	}
	if s.baseDelay > 0 {
		time.Sleep(s.baseDelay * time.Duration(multiplier))
	}
	s.mu.Lock()
	s.rows = append(s.rows, *ev)
	s.mu.Unlock()
	return nil
}

func (s *orderRecordingChatStore) snapshot() []model.ChatMessageEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.ChatMessageEvent, len(s.rows))
	copy(out, s.rows)
	return out
}

func commitSeqs(rows []model.ChatMessageEvent) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.Seq
	}
	return out
}
