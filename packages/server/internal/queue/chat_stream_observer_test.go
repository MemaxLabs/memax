package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	memaxagent "github.com/MemaxLabs/memax-go-agent-sdk"
	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// recordingChatStore captures AppendChatMessageEvent calls for
// observer assertions. Embeds InMemoryStore so it satisfies the
// rest of the Store interface; the chat methods InMemoryStore
// rejects with errChatRequiresPostgres get overridden here.
type recordingChatStore struct {
	store.Store // unimplemented surface — keeps the embedding compiler-happy
	appended    []model.ChatMessageEvent
	appendErr   error
}

func (s *recordingChatStore) AppendChatMessageEvent(_ context.Context, _ string, ev *model.ChatMessageEvent) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.appended = append(s.appended, *ev)
	return nil
}

// Per-event observe writes one row with the right seq + event
// type + payload. Three events → three rows with seq=1,2,3.
func TestChatStreamObserver_AssignsMonotonicSeq(t *testing.T) {
	t.Parallel()
	rec := &recordingChatStore{}
	o := newChatStreamObserver(rec, "owner-x", "msg-x", nil)

	events := []memaxagent.Event{
		{Kind: memaxagent.EventModelRequest, SessionID: "s1", Turn: 1},
		{Kind: memaxagent.EventAssistant, SessionID: "s1", Turn: 1, Result: "hello"},
		{Kind: memaxagent.EventResult, SessionID: "s1", Turn: 1, Result: "final answer"},
	}
	for _, ev := range events {
		if err := o.observe(context.Background(), ev); err != nil {
			t.Fatalf("observe: %v", err)
		}
	}

	if len(rec.appended) != 3 {
		t.Fatalf("got %d rows, want 3", len(rec.appended))
	}
	for i, row := range rec.appended {
		wantSeq := i + 1
		if row.Seq != wantSeq {
			t.Errorf("row[%d].Seq = %d, want %d", i, row.Seq, wantSeq)
		}
		if row.MessageID != "msg-x" {
			t.Errorf("row[%d].MessageID = %q", i, row.MessageID)
		}
	}
	// Event types map 1:1 from SDK kinds.
	if rec.appended[0].EventType != "model_request" {
		t.Errorf("row[0].EventType = %q", rec.appended[0].EventType)
	}
	if rec.appended[1].EventType != "assistant" {
		t.Errorf("row[1].EventType = %q", rec.appended[1].EventType)
	}
	if rec.appended[2].EventType != "result" {
		t.Errorf("row[2].EventType = %q", rec.appended[2].EventType)
	}
}

// Per-event payload marshaling: assistant→text, tool_use→id+name+input,
// tool_result→tool_use_id+content+is_error, usage→token counts,
// error→error string.
func TestMarshalChatStreamPayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ev      memaxagent.Event
		wantKey string
		wantVal string
	}{
		{
			name: "assistant text",
			ev: memaxagent.Event{
				Kind: memaxagent.EventAssistant,
				// Real SDK shape — assistant events populate
				// Message (not Result, which is terminal-only).
				Message: &sdkmodel.Message{
					Role: "assistant",
					Content: []sdkmodel.ContentBlock{
						{Type: sdkmodel.ContentText, Text: "Hello"},
					},
				},
			},
			wantKey: "text", wantVal: "Hello",
		},
		{
			name: "tool_use",
			ev: memaxagent.Event{
				Kind: memaxagent.EventToolUse,
				ToolUse: &sdkmodel.ToolUse{
					ID: "toolu_1", Name: "recall_memories", Input: json.RawMessage(`{"query":"x"}`),
				},
			},
			wantKey: "name", wantVal: "recall_memories",
		},
		{
			name: "tool_result with error",
			ev: memaxagent.Event{
				Kind: memaxagent.EventToolResult,
				ToolResult: &sdkmodel.ToolResult{
					ToolUseID: "toolu_1", Name: "recall_memories",
					Content: "err msg", IsError: true,
				},
			},
			wantKey: "is_error", wantVal: "true",
		},
		{
			name: "usage",
			ev: memaxagent.Event{
				Kind:  memaxagent.EventUsage,
				Usage: &sdkmodel.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			},
			wantKey: "input_tokens", wantVal: "10",
		},
		{
			name:    "error",
			ev:      memaxagent.Event{Kind: memaxagent.EventError, Err: errors.New("provider 5xx")},
			wantKey: "error", wantVal: "provider 5xx",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := marshalChatStreamPayload(tc.ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(payload, &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			v, ok := got[tc.wantKey]
			if !ok {
				t.Fatalf("payload missing %q key, got %v", tc.wantKey, got)
			}
			if got, want := jsonValString(v), tc.wantVal; got != want {
				t.Errorf("payload[%q] = %q, want %q", tc.wantKey, got, want)
			}
		})
	}
}

// Persistence failure is observability-only — the observer logs
// at error and returns nil so the agent run isn't aborted by a
// transient buffer-write hiccup. The SDK contract on
// agent.RunInput.OnEvent codifies this.
func TestChatStreamObserver_PersistFailureNonFatal(t *testing.T) {
	t.Parallel()
	rec := &recordingChatStore{appendErr: errors.New("DB hiccup")}
	o := newChatStreamObserver(rec, "owner-x", "msg-x", nil)
	err := o.observe(context.Background(), memaxagent.Event{Kind: memaxagent.EventAssistant, Result: "hi"})
	if err != nil {
		t.Errorf("observe returned %v, want nil (persistence failures are observability-only)", err)
	}
}

// leaseSweepFakeStore is a minimal in-memory Store fake that the
// lease-sweeper unit test drives directly — no Postgres. Lets us
// pin the ErrChatMessageAlreadyFinalized skip branch without
// the race shapes the integration test couldn't reproduce
// deterministically. Codex flagged this gap on Phase 3.5a v1.
type leaseSweepFakeStore struct {
	store.Store
	leases       []store.ExpiredChatLease
	finalizeCall int
	finalizeErrs []error // errs[i] returned for the i-th FinalizeChatMessage call
}

func (s *leaseSweepFakeStore) ListExpiredChatLeases(_ context.Context, _ time.Time, _ int) ([]store.ExpiredChatLease, error) {
	return s.leases, nil
}

func (s *leaseSweepFakeStore) FinalizeChatMessage(_ context.Context, _, _ string, _ store.ChatMessageFinalize) error {
	i := s.finalizeCall
	s.finalizeCall++
	if i < len(s.finalizeErrs) {
		return s.finalizeErrs[i]
	}
	return nil
}

// AlreadyFinalized errors from FinalizeChatMessage must be
// treated as a benign race-loss skip — the sweeper logs
// nothing and returns nil to River so the periodic firing
// continues unimpeded. Codex flagged on Phase 3.5a v1 that the
// integration variant of this test couldn't actually reach the
// branch (FinalizeChatMessage moved the session to idle, so the
// lease query came back empty before the second sweep). This
// fake-store variant exercises the branch deterministically.
func TestChatLeaseSweepWorker_AlreadyFinalizedBranch(t *testing.T) {
	t.Parallel()
	fake := &leaseSweepFakeStore{
		leases: []store.ExpiredChatLease{
			{SessionID: "sess1", OwnerID: "u1", ActiveMessageID: "msg1", LockedAt: time.Now().Add(-10 * time.Minute), Status: "running"},
			{SessionID: "sess2", OwnerID: "u1", ActiveMessageID: "msg2", LockedAt: time.Now().Add(-10 * time.Minute), Status: "running"},
		},
		// First lease's finalize loses the race; second wins.
		finalizeErrs: []error{store.ErrChatMessageAlreadyFinalized, nil},
	}
	w := &ChatLeaseSweepWorker{Store: fake}
	if err := w.Work(context.Background(), nil); err != nil {
		t.Fatalf("Work returned %v, want nil (sweeper must not fail on race-loss)", err)
	}
	if fake.finalizeCall != 2 {
		t.Errorf("finalizeCall = %d, want 2 (sweeper should attempt both)", fake.finalizeCall)
	}
}

// ErrChatMessageNotFound is also benign — covers the
// chat-message-cascade-deleted edge case.
func TestChatLeaseSweepWorker_NotFoundBranch(t *testing.T) {
	t.Parallel()
	fake := &leaseSweepFakeStore{
		leases: []store.ExpiredChatLease{
			{SessionID: "sess1", OwnerID: "u1", ActiveMessageID: "msg1", LockedAt: time.Now().Add(-10 * time.Minute), Status: "running"},
		},
		finalizeErrs: []error{store.ErrChatMessageNotFound},
	}
	w := &ChatLeaseSweepWorker{Store: fake}
	if err := w.Work(context.Background(), nil); err != nil {
		t.Errorf("Work returned %v, want nil (NotFound is a benign skip)", err)
	}
}

// Operational errors don't abort the whole sweep — they're
// logged per-session and the next periodic firing retries the
// remaining leases.
func TestChatLeaseSweepWorker_OperationalErrorContinues(t *testing.T) {
	t.Parallel()
	fake := &leaseSweepFakeStore{
		leases: []store.ExpiredChatLease{
			{SessionID: "sess1", OwnerID: "u1", ActiveMessageID: "msg1", LockedAt: time.Now().Add(-10 * time.Minute), Status: "running"},
			{SessionID: "sess2", OwnerID: "u1", ActiveMessageID: "msg2", LockedAt: time.Now().Add(-10 * time.Minute), Status: "running"},
		},
		// First errors operationally; second succeeds.
		finalizeErrs: []error{errors.New("DB hiccup"), nil},
	}
	w := &ChatLeaseSweepWorker{Store: fake}
	if err := w.Work(context.Background(), nil); err != nil {
		t.Errorf("Work returned %v, want nil (per-lease error must not abort sweep)", err)
	}
	if fake.finalizeCall != 2 {
		t.Errorf("finalizeCall = %d, want 2 (sweep continued after first error)", fake.finalizeCall)
	}
}

// jsonValString renders a decoded JSON value as the simplest
// string for comparison. `true` → "true", `10` → "10", "x" → "x".
func jsonValString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// Integer round-trip: JSON numbers decode as float64; we
		// strip the `.0` for integer assertions.
		if x == float64(int64(x)) {
			b, _ := json.Marshal(int64(x))
			return string(b)
		}
		b, _ := json.Marshal(x)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// Thinking artifacts with readable text persist as provider_artifact rows
// with the text payload; redacted/unreadable artifacts are skipped without
// consuming a seq.
func TestChatStreamObserver_ThinkingArtifacts(t *testing.T) {
	t.Parallel()
	rec := &recordingChatStore{}
	o := newChatStreamObserver(rec, "owner-x", "msg-x", nil)

	readable := &sdkmodel.ProviderArtifact{
		Provider: "anthropic",
		Type:     "thinking",
		Data:     json.RawMessage(`{"type":"thinking","thinking":"weigh the options"}`),
	}
	redacted := &sdkmodel.ProviderArtifact{
		Provider: "anthropic",
		Type:     "redacted_thinking",
		Data:     json.RawMessage(`{"type":"redacted_thinking","data":"opaque"}`),
	}
	events := []memaxagent.Event{
		{Kind: memaxagent.EventProviderArtifact, ProviderArtifact: redacted},
		{Kind: memaxagent.EventProviderArtifact, ProviderArtifact: readable},
		{Kind: memaxagent.EventProviderArtifact, ProviderArtifact: nil},
	}
	for _, ev := range events {
		if err := o.observe(context.Background(), ev); err != nil {
			t.Fatalf("observe: %v", err)
		}
	}

	if len(rec.appended) != 1 {
		t.Fatalf("got %d rows, want 1 (only the readable artifact persists)", len(rec.appended))
	}
	row := rec.appended[0]
	if row.EventType != "provider_artifact" || row.Seq != 1 {
		t.Fatalf("row = %+v, want provider_artifact seq=1", row)
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Text != "weigh the options" {
		t.Fatalf("payload text = %q", payload.Text)
	}
}
