// Package mockllm provides a reusable httptest-based double for the
// Anthropic Messages API. Use it in any test that exercises code
// calling anthropic.Client.Complete / CompleteMessages / CompleteStream
// without hitting the real API.
//
// # Why this exists
//
// Several packages (dreams, ingest/*, retrieval/distill) branch on LLM
// responses but can't test those branches without either:
//   - Hitting the real Anthropic API (slow, flaky, paid, non-deterministic)
//   - Hand-rolling httptest.Server + JSON handcoding in each _test.go
//
// The second approach was already in use in ingest/summarize,
// ingest/process, and dreams — each with subtly different shapes.
// This package pulls the pattern into one place so tests can write:
//
//	srv := mockllm.New(t)
//	srv.EnqueueText("some canned response")
//	client := srv.Client()
//	// ... call any anthropic.Client method against client ...
//	reqs := srv.Requests() // inspect what was sent
//
// # What the harness emulates
//
// The harness speaks the minimum the production client needs:
//   - POST /v1/messages for both non-streaming and streaming paths
//   - content[0].text and stop_reason in the response body
//   - usage.input_tokens / output_tokens for analytics passthrough
//   - SSE framing (event: content_block_delta etc.) when the request
//     has "stream": true in the body
//
// It deliberately does NOT emulate rate limits, retries, partial
// responses mid-stream, or token accounting quirks — those belong to
// contract tests of the real API. The harness is for testing callers'
// behavior given well-formed LLM responses, not for testing the
// anthropic package itself.
package mockllm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

// Server is the lifecycle handle returned by New. Close is registered
// via t.Cleanup, so callers don't need to defer it themselves.
type Server struct {
	t  *testing.T
	hs *httptest.Server

	mu       sync.Mutex
	queue    []Response  // FIFO canned responses
	handler  HandlerFunc // optional per-request override, overrides queue
	requests []CapturedRequest
	apiKey   string // expected x-api-key header; empty to skip check
}

// Response is the canned output the mock emits for one request.
// At minimum set Text or StreamChunks. StopReason defaults to "end_turn",
// InputTokens/OutputTokens default to 10/20 for plausible analytics data.
type Response struct {
	Text         string
	StopReason   string
	InputTokens  int
	OutputTokens int
	// StreamChunks, when non-empty, is emitted as SSE deltas instead of
	// a single Text block. Use for tests of CompleteStream's onDelta.
	StreamChunks []string
	// StatusCode overrides 200. Useful for exercising error paths in
	// callers (5xx → retry, 429 → backoff, etc.).
	StatusCode int
}

// HandlerFunc lets callers override the default queue behavior for
// advanced scenarios (e.g. "first call returns malformed JSON, retries
// return valid"). If set via SetHandler, the queue is ignored.
type HandlerFunc func(req CapturedRequest) Response

// CapturedRequest is a decoded /v1/messages body exposed to the test.
// Body is the raw bytes if the caller needs to inspect something the
// struct doesn't decode (e.g. image blocks, metadata fields).
type CapturedRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Stream    bool             `json:"stream"`
	System    string           `json:"system,omitempty"`
	Messages  []map[string]any `json:"messages"`
	Headers   http.Header
	Body      []byte
}

// UserPrompt returns the first user message's content as a string, or
// "" if no user message is present. Convenience for the common case.
func (c CapturedRequest) UserPrompt() string {
	for _, m := range c.Messages {
		if m["role"] == "user" {
			switch v := m["content"].(type) {
			case string:
				return v
			case []any:
				// Multimodal content — concat text parts only.
				var sb strings.Builder
				for _, part := range v {
					if pm, ok := part.(map[string]any); ok {
						if text, ok := pm["text"].(string); ok {
							sb.WriteString(text)
						}
					}
				}
				return sb.String()
			}
		}
	}
	return ""
}

// New spins up an httptest server and returns a handle. The server is
// torn down via t.Cleanup. Tests remain t.Parallel-safe because the
// harness does NOT mutate process-wide env vars — build the client
// via srv.Client() or pass srv.URL() to your own anthropic.New().
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{t: t, apiKey: "mock-key"}
	s.hs = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.hs.Close)
	return s
}

// URL is the base URL of the mock server — useful for tests that build
// their own http.Client or pass the URL to a non-anthropic consumer
// that also targets /v1/messages.
func (s *Server) URL() string { return s.hs.URL }

// Client returns a fresh *anthropic.Client pointed at this mock. Use
// this over anthropic.NewFromEnv() so each test's client is isolated
// from other tests running in parallel.
func (s *Server) Client() *anthropic.Client {
	return anthropic.New(s.apiKey, s.hs.URL)
}

// EnqueueText pushes a single-text response onto the FIFO. Each call
// consumes one entry; callers that need different per-call shapes push
// in order.
func (s *Server) EnqueueText(text string) {
	s.Enqueue(Response{Text: text})
}

// EnqueueJSON pushes a response whose Text is the JSON-marshaled payload.
// Handy for LLM flows that expect the model to return JSON.
func (s *Server) EnqueueJSON(payload any) {
	buf, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatalf("mockllm: marshal payload: %v", err)
	}
	s.Enqueue(Response{Text: string(buf)})
}

// EnqueueStatus pushes a non-200 response with optional body. Use for
// exercising error branches (5xx / 429 / 4xx).
func (s *Server) EnqueueStatus(code int, body string) {
	s.Enqueue(Response{StatusCode: code, Text: body})
}

// Enqueue pushes a fully-formed Response.
func (s *Server) Enqueue(r Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, r)
}

// SetHandler installs a per-request override. Useful when the queue
// shape isn't expressive enough (e.g. different responses based on
// prompt content). Pass nil to clear.
func (s *Server) SetHandler(fn HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = fn
}

// Requests returns a snapshot of captured requests in arrival order.
// Safe to call concurrently; returns a copy.
func (s *Server) Requests() []CapturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CapturedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

// CallCount is a convenience for tests that only care "did I hit the
// LLM once/twice/zero times" without inspecting the request bodies.
func (s *Server) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		http.Error(w, "mockllm: only POST /v1/messages is supported", http.StatusNotFound)
		return
	}
	if s.apiKey != "" && r.Header.Get("x-api-key") != s.apiKey {
		http.Error(w, "mockllm: missing or wrong x-api-key header", http.StatusUnauthorized)
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("mockllm: read body: %v", err), http.StatusBadRequest)
		return
	}
	captured := CapturedRequest{Headers: r.Header.Clone(), Body: raw}
	if err := json.Unmarshal(raw, &captured); err != nil {
		http.Error(w, fmt.Sprintf("mockllm: decode body: %v", err), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, captured)
	var resp Response
	var havePayload bool
	switch {
	case s.handler != nil:
		handler := s.handler
		s.mu.Unlock()
		resp = handler(captured)
		havePayload = true
	case len(s.queue) > 0:
		resp = s.queue[0]
		s.queue = s.queue[1:]
		havePayload = true
		s.mu.Unlock()
	default:
		s.mu.Unlock()
	}

	if !havePayload {
		// Default when nothing is queued: a plausible empty reply so
		// tests that don't care about the content still succeed.
		resp = Response{Text: ""}
	}

	if resp.StatusCode != 0 && resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		if resp.Text != "" {
			_, _ = io.WriteString(w, resp.Text)
		}
		return
	}

	if captured.Stream || len(resp.StreamChunks) > 0 {
		writeStream(w, resp)
		return
	}

	writeBlock(w, resp)
}

func writeBlock(w http.ResponseWriter, resp Response) {
	stop := resp.StopReason
	if stop == "" {
		stop = "end_turn"
	}
	inTok := resp.InputTokens
	if inTok == 0 {
		inTok = 10
	}
	outTok := resp.OutputTokens
	if outTok == 0 {
		outTok = 20
	}
	body := map[string]any{
		"content":     []map[string]any{{"type": "text", "text": resp.Text}},
		"stop_reason": stop,
		"usage": map[string]any{
			"input_tokens":  inTok,
			"output_tokens": outTok,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeStream(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	chunks := resp.StreamChunks
	if len(chunks) == 0 && resp.Text != "" {
		chunks = []string{resp.Text}
	}
	for _, chunk := range chunks {
		evt := map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": chunk},
		}
		buf, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", buf)
		if flusher != nil {
			flusher.Flush()
		}
	}
	outTok := resp.OutputTokens
	if outTok == 0 {
		outTok = 20
	}
	final := map[string]any{
		"type":  "message_delta",
		"usage": map[string]any{"output_tokens": outTok},
	}
	buf, _ := json.Marshal(final)
	fmt.Fprintf(w, "data: %s\n\n", buf)
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
