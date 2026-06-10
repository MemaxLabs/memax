package mockllm_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
)

func TestMockLLMEnqueueText(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("canned answer")

	c := srv.Client()
	if c == nil {
		t.Fatal("Client() returned nil")
	}
	resp, err := c.Complete(t.Context(), anthropic.CompleteRequest{
		Model: "claude-test", MaxTokens: 50,
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "canned answer" {
		t.Errorf("Text = %q, want canned answer", resp.Text)
	}
	if srv.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", srv.CallCount())
	}
	reqs := srv.Requests()
	if reqs[0].UserPrompt() != "hello" {
		t.Errorf("UserPrompt = %q, want hello", reqs[0].UserPrompt())
	}
	if reqs[0].Model != "claude-test" {
		t.Errorf("Model = %q", reqs[0].Model)
	}
}

func TestMockLLMEnqueueJSON(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueJSON(map[string]any{"title": "Hello world", "tags": []string{"a", "b"}})

	resp, err := srv.Client().Complete(t.Context(), anthropic.CompleteRequest{
		Model: "claude-test", MaxTokens: 50, Prompt: "x",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Text, `"title":"Hello world"`) {
		t.Errorf("Text should include marshaled title: %q", resp.Text)
	}
}

func TestMockLLMQueueFIFO(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("first")
	srv.EnqueueText("second")
	srv.EnqueueText("third")

	c := srv.Client()
	for i, want := range []string{"first", "second", "third"} {
		resp, _ := c.Complete(t.Context(), anthropic.CompleteRequest{
			Model: "m", MaxTokens: 10, Prompt: "p",
		})
		if resp.Text != want {
			t.Errorf("call %d: got %q, want %q", i+1, resp.Text, want)
		}
	}
	if srv.CallCount() != 3 {
		t.Errorf("CallCount = %d, want 3", srv.CallCount())
	}
}

func TestMockLLMDefaultEmptyWhenQueueEmpty(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// No Enqueue — Complete should still return (empty Text, nil err).
	// Callers that expected a canned response can assert CallCount=0
	// to detect "oops, nobody seeded".
	resp, err := srv.Client().Complete(t.Context(), anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err != nil {
		t.Fatalf("Complete on empty queue: %v", err)
	}
	if resp.Text != "" {
		t.Errorf("empty queue should yield empty Text, got %q", resp.Text)
	}
}

func TestMockLLMEnqueueStatusForcesError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusTooManyRequests, "rate limit")

	_, err := srv.Client().Complete(t.Context(), anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("Complete should error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429, got %v", err)
	}
}

func TestMockLLMSetHandlerOverridesQueue(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	// Enqueue a response that SHOULD be ignored because SetHandler wins.
	srv.EnqueueText("from queue — ignored")
	srv.SetHandler(func(req mockllm.CapturedRequest) mockllm.Response {
		return mockllm.Response{Text: "handled: " + req.UserPrompt()}
	})

	resp, err := srv.Client().Complete(t.Context(), anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "echo-me",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "handled: echo-me" {
		t.Errorf("handler should win, got %q", resp.Text)
	}

	// Clearing the handler restores queue behavior.
	srv.SetHandler(nil)
	resp, _ = srv.Client().Complete(t.Context(), anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if resp.Text != "from queue — ignored" {
		t.Errorf("after clearing handler, queue should resume; got %q", resp.Text)
	}
}

func TestMockLLMStreamingDeltas(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.Enqueue(mockllm.Response{
		StreamChunks: []string{"Hello, ", "world", "!"},
	})

	var got strings.Builder
	outTokens, err := srv.Client().CompleteStream(t.Context(), anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	}, func(delta string) {
		got.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if got.String() != "Hello, world!" {
		t.Errorf("streamed text = %q", got.String())
	}
	if outTokens == 0 {
		t.Error("outTokens should be non-zero from message_delta event")
	}
}

func TestMockLLMRequireAPIKeyHeader(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)

	// Fire a raw request WITHOUT the x-api-key header — mock must 401.
	req, _ := http.NewRequestWithContext(t.Context(), "POST",
		srv.URL()+"/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("raw POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no api key: got %d, want 401", resp.StatusCode)
	}
}

func TestMockLLMRejectsNon_v1_messages(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL()+"/health", nil)
	req.Header.Set("x-api-key", "mock-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("wrong endpoint: got %d, want 404", resp.StatusCode)
	}
}

// Small guard: ctx cancellation on the caller side must propagate; the
// mock doesn't need to do anything special, but if the caller cancels
// mid-flight we expect a ctx error, not a success.
func TestMockLLMContextCancelationPropagates(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("never arrives")

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // pre-cancel

	_, err := srv.Client().Complete(ctx, anthropic.CompleteRequest{
		Model: "m", MaxTokens: 10, Prompt: "p",
	})
	if err == nil {
		t.Fatal("canceled ctx should produce an error")
	}
}
