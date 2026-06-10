package rerank

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type stubCache struct {
	values map[string]string
}

func (s *stubCache) Get(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", io.EOF
	}
	return value, nil
}

func (s *stubCache) Set(_ context.Context, key string, value string, _ time.Duration) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *stubCache) SetNX(_ context.Context, key string, value string, _ time.Duration) (bool, error) {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	if _, exists := s.values[key]; exists {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func (s *stubCache) Del(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func (s *stubCache) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCohereRerankUsesCache(t *testing.T) {
	cache := &stubCache{values: make(map[string]string)}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("expected cached rerank result, but HTTP call was made")
		return nil, nil
	})}
	cohere := &Cohere{
		apiKey:     "test",
		model:      "rerank-v4.0-fast",
		topN:       2,
		cacheTTL:   time.Minute,
		cache:      cache,
		httpClient: client,
	}

	docs := []Document{{ID: "m1", Content: "deploy docs"}}
	key := cohere.cacheKey("deploy", docs)
	cache.values[key] = `[{"ID":"m1","Score":0.9,"Rank":0}]`

	results, err := cohere.Rerank(context.Background(), "deploy", docs)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(results) != 1 || results[0].ID != "m1" {
		t.Fatalf("unexpected cached results: %#v", results)
	}
}

func TestCohereRerankStoresCache(t *testing.T) {
	cache := &stubCache{values: make(map[string]string)}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"results":[{"index":0,"relevance_score":0.8}]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	cohere := &Cohere{
		apiKey:     "test",
		model:      "rerank-v4.0-fast",
		topN:       1,
		cacheTTL:   time.Minute,
		cache:      cache,
		httpClient: client,
	}

	docs := []Document{{ID: "m1", Content: "deploy docs"}}
	_, err := cohere.Rerank(context.Background(), "deploy", docs)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if _, ok := cache.values[cohere.cacheKey("deploy", docs)]; !ok {
		t.Fatalf("expected rerank result to be cached")
	}
}
