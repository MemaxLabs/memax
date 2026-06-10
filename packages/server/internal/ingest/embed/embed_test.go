package embed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
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
