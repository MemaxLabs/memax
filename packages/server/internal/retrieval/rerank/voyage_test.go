package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The Voyage reranker sends the model, query, documents and top_k, and
// returns the documents best first; nil without a key.
func TestVoyageRerank(t *testing.T) {
	if NewVoyage(VoyageConfig{}) != nil {
		t.Fatal("no key must be nil (disabled)")
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":[{"index":2,"relevance_score":0.4},{"index":0,"relevance_score":0.9},{"index":7,"relevance_score":1}]}`))
	}))
	defer srv.Close()
	v := NewVoyage(VoyageConfig{APIKey: "k", URL: srv.URL, TopN: 2})
	if v.Model() != DefaultVoyageModel || v.TopN() != 2 {
		t.Errorf("model %q top %d", v.Model(), v.TopN())
	}
	res, err := v.Rerank(context.Background(), "deploy target", []Document{{ID: "a", Content: "A"}, {ID: "b", Content: "B"}, {ID: "c", Content: "C"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].ID != "a" || res[1].ID != "c" || res[0].Rank != 0 {
		t.Errorf("results = %+v (an index out of range is dropped)", res)
	}
	if got["model"] != DefaultVoyageModel || got["query"] != "deploy target" || got["top_k"] != float64(2) {
		t.Errorf("request = %v", got)
	}
}

// A slow reranker gives up at the caller's deadline; an error status is an
// error.
func TestVoyageRerankFailures(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(400 * time.Millisecond):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := NewVoyage(VoyageConfig{APIKey: "k", URL: slow.URL}).Rerank(ctx, "q", []Document{{ID: "a"}}); err == nil {
		t.Error("a slow reranker returned no error")
	}
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("waited %v past a 50 ms deadline", took)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	defer bad.Close()
	if _, err := NewVoyage(VoyageConfig{APIKey: "k", URL: bad.URL}).Rerank(context.Background(), "q", []Document{{ID: "a"}}); err == nil {
		t.Error("429 returned no error")
	}
}
