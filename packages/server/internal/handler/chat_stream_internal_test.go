package handler

import (
	"encoding/json"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestTranslateChatStreamEvent_ApprovalRequested pins the
// stored→wire mapping for the host-emitted approval prompt
// (Phase 3.6b3b emitter writes EventType="approval_requested";
// the SSE wire event is "approval.required" per plan 24).
//
// Codex's Phase 3.6b3a v2 review asked for this regression
// guard alongside the production emitter — the doc commits in
// the approver call out the contract, but only this test
// proves end-to-end that an emitted row produces the right
// wire shape. If a future translator change drops the
// `approval_requested` case, this test fails immediately.
func TestTranslateChatStreamEvent_ApprovalRequested(t *testing.T) {
	t.Parallel()
	body := json.RawMessage(`{"approval_id":"app_xxx","tool":"forget_memory","args":{"id":"mem_xxx"}}`)
	ev := model.ChatMessageEvent{
		EventType: "approval_requested",
		Payload:   body,
	}
	wire, ok := translateChatStreamEvent(ev)
	if !ok {
		t.Fatal("translator returned ok=false; want true")
	}
	if wire.event != "approval.required" {
		t.Errorf("wire event = %q, want approval.required", wire.event)
	}
	if string(wire.payload) != string(body) {
		t.Errorf("wire payload = %q, want %q (must pass through unchanged)", wire.payload, body)
	}
}

// TestTranslateChatStreamEvent_AllStoredTypes pins the full
// translation table so adding a new SDK event_type without
// updating the translator (or vice versa) fails this test.
func TestTranslateChatStreamEvent_AllStoredTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		stored string
		want   string
	}{
		{"assistant", "model.delta"},
		{"provider_artifact", "model.thinking"},
		{"tool_use", "tool.call"},
		{"tool_result", "tool.result"},
		{"approval_requested", "approval.required"},
		{"error", "error"},
		// Worker-emitted post-finalize terminal rows pass through
		// with their wire-event names already.
		{"message.completed", "message.completed"},
		{"message.failed", "message.failed"},
		{"message.canceled", "message.canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			ev := model.ChatMessageEvent{
				EventType: tc.stored,
				Payload:   json.RawMessage(`{}`),
			}
			wire, ok := translateChatStreamEvent(ev)
			if !ok {
				t.Fatalf("translator returned ok=false for stored type %q", tc.stored)
			}
			if wire.event != tc.want {
				t.Errorf("stored %q → wire %q, want %q", tc.stored, wire.event, tc.want)
			}
		})
	}
}

// TestTranslateChatStreamEvent_UnknownTypesSkipped pins that
// non-mapped, non-message-prefix event types return ok=false
// (so the SSE handler skips them without surfacing internal
// SDK accounting events to the client).
func TestTranslateChatStreamEvent_UnknownTypesSkipped(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{"session_started", "model_request", "usage", "context_applied", "totally_unknown"} {
		t.Run(stored, func(t *testing.T) {
			ev := model.ChatMessageEvent{
				EventType: stored,
				Payload:   json.RawMessage(`{}`),
			}
			if _, ok := translateChatStreamEvent(ev); ok {
				t.Errorf("translator returned ok=true for unknown stored type %q; want false", stored)
			}
		})
	}
}

func TestPayloadWithSeqAddsSeqToObjectPayload(t *testing.T) {
	t.Parallel()

	got := payloadWithSeq(json.RawMessage(`{"text":"hello"}`), 7)
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["text"] != "hello" {
		t.Errorf("text = %v, want hello", payload["text"])
	}
	if payload["seq"] != float64(7) {
		t.Errorf("seq = %v, want 7", payload["seq"])
	}
}

func TestPayloadWithSeqPreservesExistingSeq(t *testing.T) {
	t.Parallel()

	got := payloadWithSeq(json.RawMessage(`{"seq":3,"text":"hello"}`), 7)
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["seq"] != float64(3) {
		t.Errorf("seq = %v, want 3", payload["seq"])
	}
}
