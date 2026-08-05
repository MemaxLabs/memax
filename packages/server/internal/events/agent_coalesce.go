package events

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// DefaultAgentActivityWindow is the minimum interval between two
// `agent.changed` events of state "activity" for the same
// (userID, agentSlug). A CLI user doing 50 recalls in a minute
// would otherwise fan out 50 events, each invalidating three
// query families in every open tab — this coalescer caps that
// at one event per window per user+agent without losing the
// "last-activity" freshness signal.
const DefaultAgentActivityWindow = 20 * time.Second

// defaultAgentActivityCapacity bounds the LRU so a pathological
// caller (many distinct agent slugs) can't grow the map
// unboundedly. 2048 is comfortably larger than any realistic
// (user × agent) fan-out; eviction is cheap because we're
// only storing timestamps.
const defaultAgentActivityCapacity = 2048

type agentActivityKey struct {
	UserID    string
	AgentSlug string
}

// AgentActivityCoalescer suppresses back-to-back `agent.changed`
// events of state "activity" per (userID, agentSlug) within a
// configurable window. Safe for concurrent use — all reads and
// writes are guarded by a single mutex since the critical
// section is trivial (map lookup + timestamp compare + list
// rotate).
//
// The clock is injected so tests can advance time deterministically.
type AgentActivityCoalescer struct {
	window   time.Duration
	capacity int
	now      func() time.Time

	mu    sync.Mutex
	index map[agentActivityKey]*list.Element
	lru   *list.List
}

type agentActivityEntry struct {
	key           agentActivityKey
	lastEmittedAt time.Time
}

// NewAgentActivityCoalescer returns a coalescer with production
// defaults (20s window, 2048-entry LRU, wall clock).
func NewAgentActivityCoalescer() *AgentActivityCoalescer {
	return newAgentActivityCoalescer(DefaultAgentActivityWindow, defaultAgentActivityCapacity, time.Now)
}

func newAgentActivityCoalescer(window time.Duration, capacity int, now func() time.Time) *AgentActivityCoalescer {
	return &AgentActivityCoalescer{
		window:   window,
		capacity: capacity,
		now:      now,
		index:    make(map[agentActivityKey]*list.Element, capacity),
		lru:      list.New(),
	}
}

// TryPublishAgentActivity fires a single `agent.changed` event
// with state="activity" for (userID, agentSlug) iff no event
// has been emitted for this pair within the coalesce window.
// Returns true if the event was emitted, false if suppressed.
//
// No-op when the publisher is nil, userID is empty, or
// agentSlug is empty — matches the fire-and-forget contract of
// the other publish helpers so callers don't need to guard.
func (c *AgentActivityCoalescer) TryPublishAgentActivity(ctx context.Context, p Publisher, userID, agentSlug string) bool {
	if c == nil || p == nil || userID == "" || agentSlug == "" {
		return false
	}

	key := agentActivityKey{UserID: userID, AgentSlug: agentSlug}
	now := c.now()

	c.mu.Lock()
	if elem, ok := c.index[key]; ok {
		entry := elem.Value.(*agentActivityEntry)
		if now.Sub(entry.lastEmittedAt) < c.window {
			// Suppressed — move to MRU so fresh-entry evictions
			// don't throw away an actively-coalesced key.
			c.lru.MoveToFront(elem)
			c.mu.Unlock()
			return false
		}
		entry.lastEmittedAt = now
		c.lru.MoveToFront(elem)
	} else {
		entry := &agentActivityEntry{key: key, lastEmittedAt: now}
		elem := c.lru.PushFront(entry)
		c.index[key] = elem
		if c.lru.Len() > c.capacity {
			oldest := c.lru.Back()
			if oldest != nil {
				c.lru.Remove(oldest)
				delete(c.index, oldest.Value.(*agentActivityEntry).key)
			}
		}
	}
	c.mu.Unlock()

	PublishAgentChanged(ctx, p, userID, agentSlug, "activity")
	return true
}

// defaultCoalescer is the process-wide instance used by the
// convenience wrapper TryPublishAgentActivity. A single instance
// is correct: the server and worker are distinct processes and
// the coalesce window is per-process. Tests that need isolation
// construct their own via newAgentActivityCoalescer.
var defaultCoalescer = NewAgentActivityCoalescer()

// TryPublishAgentActivity is the convenience entry point for
// handlers. Delegates to the process-wide default coalescer
// with production defaults (20s window, 2048-entry LRU).
func TryPublishAgentActivity(ctx context.Context, p Publisher, userID, agentSlug string) bool {
	return defaultCoalescer.TryPublishAgentActivity(ctx, p, userID, agentSlug)
}
