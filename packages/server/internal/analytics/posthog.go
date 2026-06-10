package analytics

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// Client sends events to PostHog. Non-blocking — events are batched and sent
// in the background. Safe to call from hot paths (recall, push).
// Returns nil if POSTHOG_API_KEY is not set (all Track calls become no-ops).
type Client struct {
	apiKey string
	host   string
	env    string
	queue  chan event
	wg     sync.WaitGroup
}

type event struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
	Timestamp  time.Time      `json:"timestamp"`
}

// New returns a PostHog client if POSTHOG_API_KEY is set, nil otherwise.
func New() *Client {
	apiKey := os.Getenv("POSTHOG_API_KEY")
	if apiKey == "" {
		return nil
	}

	host := os.Getenv("POSTHOG_HOST")
	if host == "" {
		host = "https://us.i.posthog.com"
	}

	env := os.Getenv("MEMAX_ENV")
	if env == "" {
		env = "dev"
	}

	c := &Client{
		apiKey: apiKey,
		host:   host,
		env:    env,
		queue:  make(chan event, 1000),
	}

	// Background worker flushes events every 5 seconds or when batch hits 20
	c.wg.Add(1)
	go c.worker()

	slog.Info("posthog analytics enabled", "env", env)
	return c
}

// Track sends an event. Non-blocking — drops if queue is full.
func (c *Client) Track(userID string, eventName string, props map[string]any) {
	if c == nil {
		return
	}

	if props == nil {
		props = map[string]any{}
	}
	props["environment"] = c.env
	props["$lib"] = "memax-server"

	select {
	case c.queue <- event{
		Event:      eventName,
		DistinctID: userID,
		Properties: props,
		Timestamp:  time.Now(),
	}:
	default:
		// Queue full — drop event rather than blocking hot path
	}
}

// Close flushes remaining events and stops the worker.
func (c *Client) Close() {
	if c == nil {
		return
	}
	close(c.queue)
	c.wg.Wait()
}

func (c *Client) worker() {
	defer c.wg.Done()

	batch := make([]event, 0, 20)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt, ok := <-c.queue:
			if !ok {
				// Channel closed — flush remaining
				if len(batch) > 0 {
					c.flush(batch)
				}
				return
			}
			batch = append(batch, evt)
			if len(batch) >= 20 {
				c.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				c.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (c *Client) flush(events []event) {
	// PostHog batch API: POST /batch
	type batchPayload struct {
		APIKey string  `json:"api_key"`
		Batch  []event `json:"batch"`
	}

	payload := batchPayload{
		APIKey: c.apiKey,
		Batch:  events,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("posthog: failed to marshal batch", "error", err)
		return
	}

	req, err := http.NewRequest("POST", c.host+"/batch", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("posthog: failed to send batch", "error", err, "count", len(events))
		return
	}
	resp.Body.Close()
}
