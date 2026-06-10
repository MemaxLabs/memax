package events

import (
	"context"
	"testing"
	"time"
)

func TestAgentActivityCoalescer_SuppressesWithinWindow(t *testing.T) {
	t.Parallel()
	capture := &capturePublisher{}
	clock := &manualClock{now: time.Unix(1000, 0)}
	c := newAgentActivityCoalescer(20*time.Second, 16, clock.Now)

	if !c.TryPublishAgentActivity(context.Background(), capture, "u1", "claude-code") {
		t.Fatal("first call should emit")
	}
	if c.TryPublishAgentActivity(context.Background(), capture, "u1", "claude-code") {
		t.Fatal("second call within window should suppress")
	}
	clock.Advance(19 * time.Second)
	if c.TryPublishAgentActivity(context.Background(), capture, "u1", "claude-code") {
		t.Fatal("19s after first call should still be within window")
	}
	clock.Advance(2 * time.Second) // now 21s after the first emit
	if !c.TryPublishAgentActivity(context.Background(), capture, "u1", "claude-code") {
		t.Fatal("21s after first call should re-emit")
	}

	if got := len(capture.events); got != 2 {
		t.Fatalf("expected 2 published events, got %d", got)
	}
	for i, evt := range capture.events {
		if evt.Type != EventTypeAgentChanged {
			t.Errorf("event %d: want type %q, got %q", i, EventTypeAgentChanged, evt.Type)
		}
		if evt.State != "activity" {
			t.Errorf("event %d: want state %q, got %q", i, "activity", evt.State)
		}
		if evt.RecipientUserID != "u1" {
			t.Errorf("event %d: want recipient %q, got %q", i, "u1", evt.RecipientUserID)
		}
	}
}

func TestAgentActivityCoalescer_DistinctKeysDoNotBlock(t *testing.T) {
	t.Parallel()
	capture := &capturePublisher{}
	clock := &manualClock{now: time.Unix(1000, 0)}
	c := newAgentActivityCoalescer(20*time.Second, 16, clock.Now)

	for _, tc := range []struct {
		userID, agent string
	}{
		{"u1", "claude-code"},
		{"u1", "codex"},
		{"u2", "claude-code"},
	} {
		if !c.TryPublishAgentActivity(context.Background(), capture, tc.userID, tc.agent) {
			t.Fatalf("%s/%s first call should emit", tc.userID, tc.agent)
		}
	}
	if got := len(capture.events); got != 3 {
		t.Fatalf("expected 3 events (distinct keys), got %d", got)
	}
}

func TestAgentActivityCoalescer_LRUEvicts(t *testing.T) {
	t.Parallel()
	capture := &capturePublisher{}
	clock := &manualClock{now: time.Unix(1000, 0)}
	c := newAgentActivityCoalescer(20*time.Second, 2, clock.Now)

	// Fill the LRU with two distinct keys.
	c.TryPublishAgentActivity(context.Background(), capture, "u1", "a")
	c.TryPublishAgentActivity(context.Background(), capture, "u1", "b")
	// Third key evicts "a" (the LRU tail; "b" is now MRU).
	c.TryPublishAgentActivity(context.Background(), capture, "u1", "c")
	// LRU state after this call: [c (MRU), b (LRU)].

	// "a" was evicted, so it should re-emit even though only 1ms has
	// passed since its original eviction — a dropped entry has no
	// timestamp to gate on.
	clock.Advance(1 * time.Millisecond)
	if !c.TryPublishAgentActivity(context.Background(), capture, "u1", "a") {
		t.Fatal("evicted key should re-emit without waiting for window")
	}
	// LRU state now: [a (MRU), c], evicted "b".

	// "c" is still warm (pushed 1ms ago, within the 20s window).
	if c.TryPublishAgentActivity(context.Background(), capture, "u1", "c") {
		t.Fatal("recently-pushed key should still be within window")
	}
}

func TestAgentActivityCoalescer_NilInputsNoOp(t *testing.T) {
	t.Parallel()
	c := NewAgentActivityCoalescer()
	ctx := context.Background()

	if c.TryPublishAgentActivity(ctx, nil, "u1", "a") {
		t.Fatal("nil publisher should suppress")
	}
	capture := &capturePublisher{}
	if c.TryPublishAgentActivity(ctx, capture, "", "a") {
		t.Fatal("empty userID should suppress")
	}
	if c.TryPublishAgentActivity(ctx, capture, "u1", "") {
		t.Fatal("empty agentSlug should suppress")
	}
	if len(capture.events) != 0 {
		t.Fatalf("expected zero events, got %d", len(capture.events))
	}
}

type manualClock struct {
	now time.Time
}

func (m *manualClock) Now() time.Time { return m.now }

func (m *manualClock) Advance(d time.Duration) { m.now = m.now.Add(d) }
