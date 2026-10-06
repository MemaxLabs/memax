package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestVoyageEmbedderBatchesAndPreservesOrder(t *testing.T) {
	var calls atomic.Int32
	embedder := &VoyageEmbedder{
		apiKey: "test-key",
		model:  "test-model",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			call := calls.Add(1)
			count := defaultBatchSize
			if call == 2 {
				count = 1
			}
			return jsonEmbeddingResponse(count), nil
		})},
	}

	texts := make([]string, defaultBatchSize+1)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}
	embeddings, err := embedder.EmbedContext(context.Background(), texts, "document")
	if err != nil {
		t.Fatalf("EmbedContext: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if len(embeddings) != len(texts) {
		t.Fatalf("len(embeddings) = %d, want %d", len(embeddings), len(texts))
	}
	for i, embedding := range embeddings {
		if len(embedding) != 2 {
			t.Fatalf("embedding %d length = %d, want 2", i, len(embedding))
		}
	}
}

func TestVoyageEmbedderRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	embedder := &VoyageEmbedder{
		apiKey: "test-key",
		model:  "test-model",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("temporary outage")),
					Header:     make(http.Header),
				}, nil
			}
			return jsonEmbeddingResponse(1), nil
		})},
	}

	if _, err := embedder.EmbedContext(context.Background(), []string{"hello"}, "query"); err != nil {
		t.Fatalf("EmbedContext: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestVoyageEmbedderUsesRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	embedder := &VoyageEmbedder{
		apiKey: "test-key",
		model:  "test-model",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := req.Context().Err(); err == nil {
				t.Fatal("request context was not canceled")
			}
			return nil, req.Context().Err()
		})},
	}

	if _, err := embedder.EmbedContext(ctx, []string{"hello"}, "query"); err == nil {
		t.Fatal("expected canceled context error")
	}
}

// NewVoyage takes its model and width from the caller, sends them, and is
// nil without a key or a model.
func TestNewVoyageIsExplicit(t *testing.T) {
	if NewVoyage(VoyageConfig{Model: "voyage-4"}) != nil || NewVoyage(VoyageConfig{APIKey: "k"}) != nil {
		t.Fatal("NewVoyage without a key or a model must be nil (disabled)")
	}
	var got voyageRequest
	e := NewVoyage(VoyageConfig{APIKey: "k", Model: "voyage-4-lite", OutputDimension: 1024, MaxAttempts: 1,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			return jsonEmbeddingResponse(1), nil
		})}})
	if e == nil || e.Dimensions() != 1024 {
		t.Fatalf("embedder = %v", e)
	}
	if _, err := e.EmbedContext(context.Background(), []string{"x"}, "query"); err != nil {
		t.Fatal(err)
	}
	if got.Model != "voyage-4-lite" || got.OutputDimension != 1024 || got.InputType != "query" {
		t.Errorf("request = %+v", got)
	}
}

// A 429 on the last attempt is a RateLimitError carrying Retry-After, so
// a job can back off for as long as Voyage asks; MaxAttempts 1 never
// retries.
func TestVoyageRateLimit(t *testing.T) {
	var calls atomic.Int32
	e := NewVoyage(VoyageConfig{APIKey: "k", Model: "voyage-4", MaxAttempts: 1,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			h := make(http.Header)
			h.Set("Retry-After", "17")
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h,
				Body: io.NopCloser(strings.NewReader("slow down"))}, nil
		})}})
	_, err := e.EmbedContext(context.Background(), []string{"x"}, "document")
	wait, limited := IsRateLimited(err)
	if !limited || wait != 17*time.Second || calls.Load() != 1 {
		t.Fatalf("err %v: limited %v wait %v calls %d", err, limited, wait, calls.Load())
	}
	if _, limited := IsRateLimited(fmt.Errorf("other")); limited {
		t.Error("a plain error is not a rate limit")
	}
}

func jsonEmbeddingResponse(count int) *http.Response {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"embedding":[%d,%d],"index":%d}`, i, i+1, i)
	}
	b.WriteString(`],"usage":{"total_tokens":1}}`)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(b.String())),
		Header:     make(http.Header),
	}
}
