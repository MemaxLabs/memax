package ask

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
)

// The answer tier goes through the shared client: its slug, the system
// prompt, a stream, and zero-data-retention routing (D14).
func TestAnthropicModelStreamsWithZDR(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.Enqueue(mockllm.Response{StreamChunks: []string{"Jobs run ", "on River.[M-0219]"}, InputTokens: 333, OutputTokens: 12})
	m := NewAnthropicModel(srv.Client(), true)
	var got strings.Builder
	u, err := m.Stream(context.Background(), Call{Model: DefaultModel, System: "sys", Prompt: "p", MaxTokens: 400},
		func(s string) { got.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Jobs run on River.[M-0219]" || u != (Usage{InputTokens: 333, OutputTokens: 12}) {
		t.Errorf("streamed %q, usage %+v", got.String(), u)
	}
	req := srv.Requests()[0]
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatal(err)
	}
	if req.Model != DefaultModel || !req.Stream || req.System != "sys" || req.MaxTokens != 400 {
		t.Errorf("request = %+v", req)
	}
	if p, _ := body["provider"].(map[string]any); p["zdr"] != true {
		t.Errorf("provider = %v, want zdr", body["provider"])
	}
	if NewAnthropicModel(nil, true) != nil {
		t.Error("no client should mean no model")
	}
}

// Cancelling mid-answer ends the upstream call.
func TestAnthropicModelCancels(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.Enqueue(mockllm.Response{StreamChunks: []string{"a", "b", "c"}, ChunkDelay: 300 * time.Millisecond})
	m := NewAnthropicModel(srv.Client(), false)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := m.Stream(ctx, Call{Model: DefaultModel, Prompt: "p", MaxTokens: 10}, func(string) {})
	if err == nil {
		t.Fatal("a cancelled answer returned no error")
	}
	if took := time.Since(start); took > 800*time.Millisecond {
		t.Errorf("the call took %v after the deadline", took)
	}
}
