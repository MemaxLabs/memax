package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// finalizedTurn drives a chat session to a terminal assistant
// message via the Send handler then FinalizeChatMessage. The
// store now appends the terminal chat_message_events row inside
// the same finalize transaction (Phase 3.4b2.b v3 fix), so the
// test no longer needs to manually append a terminal row — the
// SSE handler reads what's there.
//
// preEvents lets a test pre-record per-event rows BEFORE
// finalize so the terminal lands at MAX(seq)+1 of those rows.
// Empty pre-event list → terminal lands at seq=1.
//
// We piggyback on setupChatHandlerEnv from chat_test.go.
func finalizedTurn(t *testing.T, h *handler.ChatHandler, env *chatTestEnv, finalContent string, preEvents []model.ChatMessageEvent) string {
	t.Helper()
	rec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType:   model.ChatScopeTypeSingleHub,
		ScopeHubIDs: []string{env.hubA},
	}, env.owner, []string{env.hubA})
	h.Create(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	sess := decodeChatSession(t, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	sendReq := reqWithCaller(t, "POST",
		"/v1/chat/sessions/"+sess.ID+"/messages",
		handler.SendMessageRequest{Content: "hi", IdempotencyKey: uuid.NewString()},
		env.owner, []string{env.hubA})
	sendReq.SetPathValue("id", sess.ID)
	h.Send(rec, sendReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d (body=%s)", rec.Code, rec.Body.String())
	}
	var sent struct {
		Data handler.SendMessageResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)

	// Pre-record any per-event rows BEFORE finalize so the
	// terminal row's MAX(seq)+1 computation lands the terminal
	// at the right spot.
	for i := range preEvents {
		preEvents[i].MessageID = sent.Data.AssistantMessage.ID
		if preEvents[i].CreatedAt.IsZero() {
			preEvents[i].CreatedAt = time.Now().UTC()
		}
		if err := env.store.AppendChatMessageEvent(context.Background(), env.owner, &preEvents[i]); err != nil {
			t.Fatalf("append pre-event %d: %v", i, err)
		}
	}

	now := time.Now().UTC()
	if err := env.store.FinalizeChatMessage(context.Background(),
		sent.Data.AssistantMessage.ID, env.owner, store.ChatMessageFinalize{
			Status:     model.ChatMessageStatusCompleted,
			Content:    finalContent,
			ToolCalls:  []byte(`[]`),
			Citations:  []byte(`[]`),
			Usage:      []byte(`{"input_tokens":10}`),
			FinishedAt: now,
		}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return sent.Data.AssistantMessage.ID
}

// streamRequest builds a GET request to the SSE endpoint with
// the right path values + auth injection.
func streamRequest(t *testing.T, sessionID, messageID, ownerID string, hubs []string, lastEventID string) *http.Request {
	t.Helper()
	req := reqWithCaller(t, "GET",
		"/v1/chat/sessions/"+sessionID+"/messages/"+messageID+"/stream",
		nil, ownerID, hubs)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("msg_id", messageID)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	return req
}

// streamRecorder is httptest.ResponseRecorder with a Flush()
// method so the SSE handler's `w.(http.Flusher)` assertion
// succeeds. The standard recorder DOESN'T implement Flusher.
type streamRecorder struct {
	*httptest.ResponseRecorder
}

func (s *streamRecorder) Flush() {} // no-op; recorder buffer is direct

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{httptest.NewRecorder()}
}

// Happy path: a terminal message with two pre-recorded events
// streams them in order, ends with the synthetic terminal event,
// and includes the final content from chat_messages.content.
func TestChatHandler_Stream_TerminalMessageReplaysAndCompletes(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// preEvents pre-records 2 SDK events; the store-side terminal
	// row write inside FinalizeChatMessage lands at seq=3.
	assistantID := finalizedTurn(t, h, env, "the final answer", []model.ChatMessageEvent{
		{Seq: 1, EventType: "assistant", Payload: []byte(`{"text":"the "}`)},
		{Seq: 2, EventType: "assistant", Payload: []byte(`{"text":"final answer"}`)},
	})

	rec := newStreamRecorder()
	// Need to find the session id — the handler reads it from
	// path. Resolve it from the message.
	msg, err := env.store.GetChatMessage(context.Background(), assistantID, env.owner)
	if err != nil {
		t.Fatalf("get msg: %v", err)
	}
	req := streamRequest(t, msg.SessionID, assistantID, env.owner, []string{env.hubA}, "")
	h.Stream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stream status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// SSE wire format: `id: N\nevent: T\ndata: {...}\n\n`. We
	// assert presence + ordering by substring rather than
	// parsing the full SSE stream.
	if !strings.Contains(body, "id: 1\nevent: model.delta\n") {
		t.Errorf("missing seq=1 model.delta frame:\n%s", body)
	}
	if !strings.Contains(body, "id: 2\nevent: model.delta\n") {
		t.Errorf("missing seq=2 model.delta frame:\n%s", body)
	}
	// Terminal frame (synthetic — seq 3 because last replay was 2).
	if !strings.Contains(body, "event: message.completed\n") {
		t.Errorf("missing message.completed terminal frame:\n%s", body)
	}
	// Terminal payload includes the final content. Postgres
	// JSONB output may add whitespace ("content": "x" vs
	// "content":"x") so we strip whitespace from the body for
	// the substring check.
	stripped := strings.ReplaceAll(strings.ReplaceAll(body, " ", ""), "\t", "")
	if !strings.Contains(stripped, `"content":"thefinalanswer"`) {
		t.Errorf("missing final content in terminal frame:\n%s", body)
	}
	// model.delta order: seq 1 BEFORE seq 2.
	idx1 := strings.Index(body, "id: 1\n")
	idx2 := strings.Index(body, "id: 2\n")
	if idx1 < 0 || idx2 < 0 || idx2 <= idx1 {
		t.Errorf("frame ordering wrong: idx1=%d idx2=%d", idx1, idx2)
	}
}

// Resume: client provides Last-Event-ID=1 → only seq>1 events
// are streamed. Pre-seq-1 events are silently skipped (the
// client already has them).
func TestChatHandler_Stream_ResumesFromLastEventID(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// preEvents takes seq 1+2; FinalizeChatMessage's terminal
	// row lands at seq=3.
	assistantID := finalizedTurn(t, h, env, "answer", []model.ChatMessageEvent{
		{Seq: 1, EventType: "assistant", Payload: []byte(`{"text":"first"}`)},
		{Seq: 2, EventType: "assistant", Payload: []byte(`{"text":"second"}`)},
	})

	rec := newStreamRecorder()
	msg, _ := env.store.GetChatMessage(context.Background(), assistantID, env.owner)
	req := streamRequest(t, msg.SessionID, assistantID, env.owner, []string{env.hubA}, "1")
	h.Stream(rec, req)

	body := rec.Body.String()
	// seq=1 must NOT appear; seq=2 must.
	if strings.Contains(body, "id: 1\n") {
		t.Errorf("resume from 1 should skip seq=1, got body:\n%s", body)
	}
	if !strings.Contains(body, "id: 2\n") {
		t.Errorf("resume from 1 should include seq=2:\n%s", body)
	}
}

// Cross-owner: stream URL uses a real msg id but the caller is
// a different user. Returns 404 (no existence leak).
func TestChatHandler_Stream_CrossOwnerReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	assistantID := finalizedTurn(t, h, env, "answer", nil)
	msg, _ := env.store.GetChatMessage(context.Background(), assistantID, env.owner)

	rec := newStreamRecorder()
	req := streamRequest(t, msg.SessionID, assistantID, env.other, []string{env.hubA, env.hubB}, "")
	h.Stream(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner stream = %d, want 404", rec.Code)
	}
}

// 410 chat_replay_expired: terminal message with finished_at
// older than 24h returns 410 with structured details so the
// client knows to re-fetch via GET /messages/{id}.
func TestChatHandler_Stream_ReplayExpiredReturns410(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	assistantID := finalizedTurn(t, h, env, "answer", nil)
	msg, _ := env.store.GetChatMessage(context.Background(), assistantID, env.owner)

	// Backdate finished_at to 25h ago via raw SQL — there's no
	// public API for adjusting it, and the 24h purge sweeper
	// doesn't run inline.
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE chat_messages SET finished_at = now() - interval '25 hours' WHERE id = $1::uuid`,
		assistantID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	rec := newStreamRecorder()
	req := streamRequest(t, msg.SessionID, assistantID, env.owner, []string{env.hubA}, "")
	h.Stream(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("expired = %d, want 410 (body=%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				MessageID  string `json:"message_id"`
				ContentURL string `json:"content_url"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Error.Code != "chat_replay_expired" {
		t.Errorf("error.code = %q, want chat_replay_expired", got.Error.Code)
	}
	if got.Error.Details.ContentURL == "" {
		t.Errorf("missing content_url hint in details")
	}
}

// Reconnect after terminal already received: client's
// Last-Event-ID >= terminal seq → stream is empty (the terminal
// row has already been consumed). Codex's Phase 3.4b2.b v1
// review caught the synthetic-terminal shape: the original code
// emitted a fresh terminal at id=N+1 on every reconnect, which
// violated the (msg_id, seq) dedupe contract and could trigger
// EventSource reconnect loops. This test pins the persisted-row
// fix: terminal seq is fixed at write time, reconnect sees no
// new events, exits cleanly.
func TestChatHandler_Stream_ReconnectAfterTerminalIsIdempotent(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	// One delta pre-recorded at seq=1; FinalizeChatMessage's
	// terminal row lands at seq=2.
	assistantID := finalizedTurn(t, h, env, "ack", []model.ChatMessageEvent{
		{Seq: 1, EventType: "assistant", Payload: []byte(`{"text":"ack"}`)},
	})

	// First connection: streams delta + terminal (seqs 1 + 2).
	rec1 := newStreamRecorder()
	msg, _ := env.store.GetChatMessage(context.Background(), assistantID, env.owner)
	req1 := streamRequest(t, msg.SessionID, assistantID, env.owner, []string{env.hubA}, "")
	h.Stream(rec1, req1)
	body1 := rec1.Body.String()
	if !strings.Contains(body1, "id: 2\nevent: message.completed\n") {
		t.Fatalf("first stream missing terminal frame: %s", body1)
	}

	// Reconnect with Last-Event-ID=2 (we saw the terminal).
	// Stream MUST NOT emit any new event frames — the only valid
	// content is heartbeat comments (none expected because the
	// stream returns immediately on terminal+drained).
	rec2 := newStreamRecorder()
	req2 := streamRequest(t, msg.SessionID, assistantID, env.owner, []string{env.hubA}, "2")
	h.Stream(rec2, req2)
	body2 := rec2.Body.String()
	if strings.Contains(body2, "event:") {
		t.Errorf("reconnect after terminal emitted unexpected events:\n%s", body2)
	}
}

// Path-id mismatch: streaming a message ID with a different
// session id in the URL → 404 (same protection as GetMessage).
func TestChatHandler_Stream_SessionMismatchReturns404(t *testing.T) {
	t.Parallel()
	h, env := setupChatHandlerEnv(t)
	assistantID := finalizedTurn(t, h, env, "answer", nil)

	// A different session, also owned by env.owner.
	rec := httptest.NewRecorder()
	createReq := reqWithCaller(t, "POST", "/v1/chat/sessions", handler.CreateChatSessionRequest{
		ScopeType: model.ChatScopeTypeUserAllHubs,
	}, env.owner, []string{env.hubA})
	h.Create(rec, createReq)
	otherSession := decodeChatSession(t, rec.Body.Bytes())

	streamRec := newStreamRecorder()
	req := streamRequest(t, otherSession.ID, assistantID, env.owner, []string{env.hubA}, "")
	h.Stream(streamRec, req)

	if streamRec.Code != http.StatusNotFound {
		t.Errorf("session-id mismatch = %d, want 404 (body=%s)", streamRec.Code, streamRec.Body.String())
	}
}
