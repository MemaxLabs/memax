package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// These tests exercise the GenerateEmailCopy handler's validation
// lanes. The LLM-bound happy path is tested via mockllm elsewhere;
// here we focus on the input-guard surface that's at 0% coverage.

func TestAdminAIAssist_DisabledWithoutLLMClient(t *testing.T) {
	t.Parallel()
	h := &AdminAIAssistHandler{} // llm nil
	body, _ := json.Marshal(map[string]any{
		"field_kind": "subject", "intent": "rewrite", "tone": "warm",
		"locale": "en", "current": "hello",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ai/email/copy", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	h.GenerateEmailCopy(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil llm: expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ai_disabled") {
		t.Errorf("error code missing: %s", rec.Body.String())
	}
}

func TestAdminAIAssist_BadJSONRejected(t *testing.T) {
	t.Parallel()
	// Inject a non-nil llm via type assertion — we only need the
	// validation gates to fire before the LLM call, and they do the
	// nil check first. Skipping: we can satisfy the interface with
	// any non-nil pointer since the body parse fails before dispatch.
	h := newAIAssistHandlerWithStubLLM(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ai/email/copy",
		bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()
	h.GenerateEmailCopy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: expected 400, got %d", rec.Code)
	}
}

func TestAdminAIAssist_ValidationLanes(t *testing.T) {
	t.Parallel()
	h := newAIAssistHandlerWithStubLLM(t)

	cases := []struct {
		name     string
		payload  map[string]any
		wantCode string
	}{
		{
			name: "unknown field kind",
			payload: map[string]any{
				"field_kind": "headline", "intent": "rewrite",
				"tone": "warm", "locale": "en", "current": "hi",
			},
			wantCode: "invalid_field_kind",
		},
		{
			name: "unknown intent",
			payload: map[string]any{
				"field_kind": "subject", "intent": "magick",
				"tone": "warm", "locale": "en", "current": "hi",
			},
			wantCode: "invalid_intent",
		},
		{
			name: "unknown tone",
			payload: map[string]any{
				"field_kind": "subject", "intent": "rewrite",
				"tone": "neon", "locale": "en", "current": "hi",
			},
			wantCode: "invalid_tone",
		},
		{
			name: "unknown locale",
			payload: map[string]any{
				"field_kind": "subject", "intent": "rewrite",
				"tone": "warm", "locale": "xx", "current": "hi",
			},
			wantCode: "invalid_locale",
		},
		{
			name: "non-draft intent requires current content",
			payload: map[string]any{
				"field_kind": "subject", "intent": "rewrite",
				"tone": "warm", "locale": "en", "current": "",
			},
			wantCode: "missing_current",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPost,
				"/v1/admin/ai/email/copy", bytes.NewBuffer(body))
			rec := httptest.NewRecorder()
			h.GenerateEmailCopy(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Errorf("expected code %q in body: %s", tc.wantCode, rec.Body.String())
			}
		})
	}
}

func TestAdminAIAssist_OversizedInputsRejected(t *testing.T) {
	t.Parallel()
	h := newAIAssistHandlerWithStubLLM(t)

	long := strings.Repeat("x", maxAIAssistCurrentLen+1)
	payload := map[string]any{
		"field_kind": "subject", "intent": "polish",
		"tone": "warm", "locale": "en", "current": long,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/ai/email/copy",
		bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	h.GenerateEmailCopy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized current: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "content_too_long") {
		t.Errorf("wrong error: %s", rec.Body.String())
	}
}

func TestAdminAIAssist_SetRecallNilSafe(t *testing.T) {
	t.Parallel()
	h := newAIAssistHandlerWithStubLLM(t)
	h.SetRecall(nil) // should not panic
}

// newAIAssistHandlerWithStubLLM returns a handler whose llm is
// non-nil (so the ai_disabled gate doesn't fire) but points at a
// dead URL. Every test in this file asserts on paths that reject
// BEFORE the LLM dispatch, so the client is never actually called.
func newAIAssistHandlerWithStubLLM(t *testing.T) *AdminAIAssistHandler {
	t.Helper()
	c := anthropic.New("stub-key", "http://127.0.0.1:1")
	return &AdminAIAssistHandler{llm: c}
}
