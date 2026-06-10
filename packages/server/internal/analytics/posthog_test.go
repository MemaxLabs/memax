package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNilClientTrackAndCloseAreNoOps(t *testing.T) {
	t.Parallel()
	// nil client must not panic on Track or Close — the graceful-
	// degradation path is what ships when POSTHOG_API_KEY isn't set.
	var c *Client
	c.Track("user-1", "some_event", map[string]any{"k": "v"})
	c.Close()
	// nil props also survive.
	c.Track("user-1", "no_props_event", nil)
}

func TestTrackDropsWhenQueueFull(t *testing.T) {
	t.Parallel()
	// Build a client manually with a tiny queue to force the
	// drop-on-full branch without burning the 1000-slot default.
	c := &Client{
		apiKey: "key",
		host:   "http://unreachable.invalid", // no worker, no flushing
		env:    "test",
		queue:  make(chan event, 1),
	}
	// First Track lands in the queue slot.
	c.Track("u", "evt", nil)
	// Second Track must drop silently (default branch of the select),
	// not block.
	done := make(chan struct{})
	go func() {
		c.Track("u", "evt", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Track blocked when queue was full — select default branch missing")
	}
}

func TestNewReturnsNilWithoutAPIKey(t *testing.T) {
	// Not parallel — t.Setenv forbids it.
	t.Setenv("POSTHOG_API_KEY", "")
	if c := New(); c != nil {
		t.Errorf("no API key should return nil, got %+v", c)
	}
}

func TestNewConstructsRealClientWithAPIKey(t *testing.T) {
	// Not parallel — t.Setenv forbids it.
	t.Setenv("POSTHOG_API_KEY", "test-key")
	t.Setenv("POSTHOG_HOST", "")
	t.Setenv("MEMAX_ENV", "")
	c := New()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q", c.apiKey)
	}
	// Defaults kick in for empty env vars.
	if c.host != "https://us.i.posthog.com" {
		t.Errorf("host = %q", c.host)
	}
	if c.env != "dev" {
		t.Errorf("env = %q", c.env)
	}
	c.Close()
}

func TestTrackAndFlushEndToEnd(t *testing.T) {
	// Not parallel — Setenv drives New(). One httptest server
	// captures the flushed /batch request.
	var mu sync.Mutex
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("POSTHOG_API_KEY", "test-key")
	t.Setenv("POSTHOG_HOST", srv.URL)
	t.Setenv("MEMAX_ENV", "test")
	c := New()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	c.Track("user-42", "sample_event", map[string]any{"value": 7})
	c.Close() // flushes remaining events before returning

	mu.Lock()
	payload := captured
	mu.Unlock()
	if len(payload) == 0 {
		t.Fatal("expected captured batch payload, got empty")
	}
	var decoded struct {
		APIKey string `json:"api_key"`
		Batch  []struct {
			Event      string         `json:"event"`
			DistinctID string         `json:"distinct_id"`
			Properties map[string]any `json:"properties"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode captured: %v", err)
	}
	if decoded.APIKey != "test-key" {
		t.Errorf("api_key = %q", decoded.APIKey)
	}
	if len(decoded.Batch) != 1 {
		t.Fatalf("batch size = %d, want 1", len(decoded.Batch))
	}
	if decoded.Batch[0].Event != "sample_event" {
		t.Errorf("event name = %q", decoded.Batch[0].Event)
	}
	if decoded.Batch[0].DistinctID != "user-42" {
		t.Errorf("distinct_id = %q", decoded.Batch[0].DistinctID)
	}
	if decoded.Batch[0].Properties["environment"] != "test" {
		t.Errorf("environment prop = %v", decoded.Batch[0].Properties["environment"])
	}
	if decoded.Batch[0].Properties["$lib"] != "memax-server" {
		t.Errorf("$lib prop = %v", decoded.Batch[0].Properties["$lib"])
	}
}
