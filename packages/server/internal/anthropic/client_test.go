package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests exercise the Client directly from its own package so
// coverage is attributed to internal/anthropic/client.go (mockllm's
// tests exercise the same code from a sibling package and don't count
// toward this file's coverage).

// fakeAnthropic spins up an httptest server that speaks /v1/messages
// enough for the Client to parse. Kept narrow on purpose — the
// mockllm package has the full-featured double.
func fakeAnthropic(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func successResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
	})
}

func TestNewFromEnvReturnsNilWithoutAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if c := NewFromEnv(); c != nil {
		t.Errorf("no API key should return nil, got %+v", c)
	}
}

func TestNewFromEnvConstructsFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	c := NewFromEnv()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q", c.apiKey)
	}
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL fallback wrong: %q", c.baseURL)
	}
}

func TestNewRejectsEmptyAPIKey(t *testing.T) {
	t.Parallel()
	if c := New("", "https://x"); c != nil {
		t.Error("empty API key should return nil")
	}
}

func TestNewUsesDefaultBaseURL(t *testing.T) {
	t.Parallel()
	c := New("key", "")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestCompleteHappyPath(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("missing x-api-key: %s", r.Header.Get("x-api-key"))
		}
		successResponse(w, "hello")
	})
	c := New("k", srv.URL)
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Model: "claude-test", MaxTokens: 50, Prompt: "hi", Purpose: "test",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.InputTokens != 10 {
		t.Errorf("InputTokens = %d", resp.InputTokens)
	}
}

func TestCompleteReturnsErrorOn5xx(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	c := New("k", srv.URL)
	_, err := c.Complete(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("expected error on 5xx")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500, got %v", err)
	}
}

func TestCompleteReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	c := New("k", srv.URL)
	_, err := c.Complete(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("expected error on malformed body")
	}
}

func TestCompleteReturnsErrorOnEmptyContent(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":0}}`))
	})
	c := New("k", srv.URL)
	_, err := c.Complete(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("expected error on empty content")
	}
}

func TestCompleteIncludesSystemMessage(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["system"] != "you are a helper" {
			t.Errorf("system prompt missing or wrong: %v", body["system"])
		}
		successResponse(w, "ok")
	})
	c := New("k", srv.URL)
	_, err := c.Complete(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p", System: "you are a helper",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestCompleteMessagesHappyPath(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		successResponse(w, "multimodal")
	})
	c := New("k", srv.URL)
	resp, err := c.CompleteMessages(context.Background(), "claude-test", 100,
		[]map[string]any{{"role": "user", "content": "hi"}}, "test.purpose")
	if err != nil {
		t.Fatalf("CompleteMessages: %v", err)
	}
	if resp.Text != "multimodal" {
		t.Errorf("Text = %q", resp.Text)
	}
}

func TestCompleteMessagesDefaultsModelWhenEmpty(t *testing.T) {
	t.Parallel()
	var gotModel string
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		successResponse(w, "ok")
	})
	c := New("k", srv.URL)
	_, _ = c.CompleteMessages(context.Background(), "", 100,
		[]map[string]any{{"role": "user", "content": "hi"}})
	if gotModel != DefaultModel {
		t.Errorf("empty model should default to %q, got %q", DefaultModel, gotModel)
	}
}

func TestWithTrackingMergesWithExisting(t *testing.T) {
	t.Parallel()
	// Bare context → inserts tracking.
	ctx := WithTracking(context.Background(), Tracking{
		DistinctID: "u1",
		TraceID:    "trace-1",
		Metadata:   map[string]any{"a": 1},
	})
	got := trackingFromContext(ctx)
	if got.DistinctID != "u1" {
		t.Errorf("DistinctID = %q", got.DistinctID)
	}

	// Nested WithTracking preserves absent fields from the outer call
	// and merges metadata.
	ctx2 := WithTracking(ctx, Tracking{
		SessionID: "sess-1",
		Metadata:  map[string]any{"b": 2},
	})
	got2 := trackingFromContext(ctx2)
	if got2.DistinctID != "u1" {
		t.Errorf("outer DistinctID lost: %q", got2.DistinctID)
	}
	if got2.SessionID != "sess-1" {
		t.Errorf("inner SessionID lost: %q", got2.SessionID)
	}
	if got2.Metadata["a"] != 1 || got2.Metadata["b"] != 2 {
		t.Errorf("metadata merge wrong: %+v", got2.Metadata)
	}

	// trackingFromContext on nil ctx returns zero.
	if got := trackingFromContext(nil); got.DistinctID != "" {
		t.Error("nil ctx should yield zero tracking")
	}
}

func TestSetAnalyticsNilSafe(t *testing.T) {
	t.Parallel()
	var c *Client
	c.SetAnalytics(nil) // should not panic on nil receiver
	c2 := New("k", "https://x")
	c2.SetAnalytics(nil) // clearing is allowed
}

func TestPureHelpers(t *testing.T) {
	t.Parallel()
	if got := firstString(nil); got != "" {
		t.Errorf("firstString(nil) = %q", got)
	}
	if got := firstString([]string{"a", "b"}); got != "a" {
		t.Errorf("firstString: %q", got)
	}

	if got := firstNonEmpty("", "  ", "b", "c"); got != "b" {
		t.Errorf("firstNonEmpty: %q", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty empty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty all empty = %q", got)
	}

	base := map[string]any{"a": 1}
	merged := mergeMetadata(base, map[string]any{"b": 2})
	if merged["a"] != 1 || merged["b"] != 2 {
		t.Errorf("mergeMetadata: %+v", merged)
	}
	if got := mergeMetadata(nil, nil); got != nil {
		t.Errorf("mergeMetadata(nil, nil) should return nil, got %+v", got)
	}

	id := newTraceID()
	if len(id) != 36 {
		t.Errorf("newTraceID length = %d, want 36", len(id))
	}
	id2 := newTraceID()
	if id == id2 {
		t.Error("newTraceID should be unique across calls")
	}
}

func TestTruncateAndRedact(t *testing.T) {
	t.Parallel()
	// Short string passes through (after redaction).
	if got := truncateAndRedact("hello", 100); got != "hello" {
		t.Errorf("short: %q", got)
	}
	// Long string gets truncated with marker.
	long := strings.Repeat("x", 200)
	got := truncateAndRedact(long, 50)
	if !strings.HasSuffix(got, "[truncated]") {
		t.Errorf("long not truncated with marker: %q", got[len(got)-20:])
	}
	// zero maxChars → falls back to default.
	_ = truncateAndRedact("xyz", 0)

	// Secrets get redacted.
	withSecret := "api_key=sk-abc123 more text"
	redacted := truncateAndRedact(withSecret, 1000)
	if strings.Contains(redacted, "sk-abc123") {
		t.Errorf("secret not redacted: %q", redacted)
	}
}

func TestExtractJSONHelpers(t *testing.T) {
	t.Parallel()
	// JSON inside fenced block.
	fenced := "```json\n{\"k\":\"v\"}\n```"
	got := ExtractJSONObject(fenced)
	if got != `{"k":"v"}` {
		t.Errorf("fenced object: %q", got)
	}
	// JSON raw.
	raw := `noise {"k":"v"} trailing`
	got = ExtractJSONObject(raw)
	if got != `{"k":"v"}` {
		t.Errorf("raw object: %q", got)
	}
	// Passthrough if no braces.
	if got := ExtractJSONObject("no braces"); got != "no braces" {
		t.Errorf("no-braces passthrough: %q", got)
	}

	// Arrays.
	arrFenced := "```json\n[1,2]\n```"
	if got := ExtractJSONArray(arrFenced); got != "[1,2]" {
		t.Errorf("fenced array: %q", got)
	}
	if got := ExtractJSONArray("start [1,2] end"); got != "[1,2]" {
		t.Errorf("raw array: %q", got)
	}

	// StripMarkdownFences removes both opening and closing fences.
	if got := StripMarkdownFences("```json\nhello\n```"); got != "hello" {
		t.Errorf("strip fences: %q", got)
	}
	if got := StripMarkdownFences("no fences"); got != "no fences" {
		t.Errorf("passthrough: %q", got)
	}
}

func TestModelFromEnvHelpers(t *testing.T) {
	// env var absent → default.
	t.Setenv("SOME_NONEXISTENT_MODEL_VAR", "")
	if got := ModelFromEnv("SOME_NONEXISTENT_MODEL_VAR"); got != DefaultModel {
		t.Errorf("empty env: %q", got)
	}
	// env var set → returned value.
	t.Setenv("EXPLICIT_MODEL_VAR", "claude-custom")
	if got := ModelFromEnv("EXPLICIT_MODEL_VAR"); got != "claude-custom" {
		t.Errorf("explicit env: %q", got)
	}
	// Fallback variant.
	t.Setenv("ABSENT_FALLBACK_VAR", "")
	if got := ModelFromEnvWithDefault("ABSENT_FALLBACK_VAR", "custom-default"); got != "custom-default" {
		t.Errorf("fallback: %q", got)
	}
}

func TestCompleteStreamDispatchesDeltas(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{"Hello, ", "world", "!"} {
			evt := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"` + chunk + `"}}`
			_, _ = w.Write([]byte("data: " + evt + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		final := `{"type":"message_delta","usage":{"output_tokens":7}}`
		_, _ = w.Write([]byte("data: " + final + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	})
	c := New("k", srv.URL)
	var got strings.Builder
	outTokens, err := c.CompleteStream(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 50, Prompt: "p",
	}, func(delta string) { got.WriteString(delta) })
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if got.String() != "Hello, world!" {
		t.Errorf("streamed = %q", got.String())
	}
	if outTokens != 7 {
		t.Errorf("outTokens = %d, want 7", outTokens)
	}
}

func TestCompleteStreamReturnsErrorOn5xx(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	})
	c := New("k", srv.URL)
	_, err := c.CompleteStream(context.Background(), CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	}, func(string) {})
	if err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestCompleteContextCancellationPropagates(t *testing.T) {
	t.Parallel()
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		successResponse(w, "late")
	})
	c := New("k", srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Complete(ctx, CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("canceled ctx should error")
	}
}
