// Package chatstream contains the live-delivery hint over the
// chat_message_events replay buffer. Plan 24 §"Send-message +
// resume" pins chat_message_events as the SSE source of truth;
// codex's Phase 3.4b2.b feedback added the layering rule:
// "Redis live events are a delivery hint over the DB replay
// source, not a second source of truth."
//
// This package implements that hint as a focused, minimal
// signal-only pub/sub over Redis. The chat worker's stream
// observer publishes "wake up, new event" notifications on a
// per-message channel after each chat_message_events row
// commits. The SSE handler subscribes for the duration of the
// stream and re-queries the DB on every signal.
//
// **Why signal-only.** No payload travels over Redis — every
// subscriber re-reads from the DB. Three reasons:
//
//   - The DB is the source of truth. A signal-with-payload
//     scheme requires every consumer to validate the payload
//     against the DB anyway (broker pub/sub is best-effort);
//     skipping the payload shortens the round-trip to "DB read,
//     period."
//   - At-least-once is built in: the DB query reads everything
//     past Last-Event-ID regardless of which signals were
//     delivered. A missed Redis frame just means the next
//     signal (or the poll fallback) catches up the gap.
//   - Per-message channels give natural fan-out — one channel
//     per in-flight stream — without the routing complexity of
//     events.Broker (which carries audience + hub-roles + user
//     identity). Nothing about chat-stream delivery needs that
//     surface; per-message scope IS the access boundary.
//
// **Why a separate package, not internal/events.** events.Broker
// has rich audience-based routing (hub-fan-out, user-direct,
// notification audiences) that the chat stream doesn't need.
// Reusing it would force chat-stream signals through the
// shouldDeliver decision tree just to discard most of it.
// Keeping chatstream focused makes the layering explicit:
// events.Broker carries app-level events (agent.changed, etc);
// chatstream.Signaler carries per-stream wake-up signals.
package chatstream

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// chatStreamSubscribeProbeTimeout caps how long
// pubsub.Receive() blocks waiting for Redis to confirm the
// SUBSCRIBE. The parent (request) context lives up to
// chatStreamMaxDuration (5m); without this bound, a Redis
// stall would hold the SSE handler open for minutes before
// degrading to poll-only delivery. 2s is generous for a
// healthy Redis (typical SUBSCRIBE acks under 10ms) and short
// enough that a brown-out fails fast into the fallback path.
const chatStreamSubscribeProbeTimeout = 2 * time.Second

// chatStreamChannelPrefix is the Redis pub/sub channel prefix
// for per-message wake-up signals. Full channel name:
// `chat:stream:<assistant_message_id>`. Per-message scope means
// concurrent streams for different messages don't share a
// channel — Redis fan-out is bounded to the actual subscribers
// of that specific message, not "all chat streams everywhere".
const chatStreamChannelPrefix = "chat:stream:"

// Signaler is the chat-stream live-delivery hint over Redis
// pub/sub. Constructed once at app startup; both the worker
// (publisher) and the SSE handler (subscriber) hold a reference.
//
// **Nil semantics.** A nil *Signaler is a valid value. Notify
// becomes a no-op; Subscribe returns nil. The SSE handler
// detects nil and falls back to its 250ms poll cadence — exactly
// the Phase 3.4b2.b shape, no behavioral regression in
// dev/staging environments without Redis.
//
// Field type is `*redis.Client` (concrete) instead of
// `redis.UniversalClient` (interface) on purpose: callers wire
// `var redisClient *redis.Client` and pass it through. A typed
// nil pointer assigned to an interface variable is NOT nil at
// the interface level (Go gotcha) — so an interface-typed field
// would let `NewSignaler(nil-pointer)` return a non-nil
// Signaler wrapping a nil client, and the first Publish would
// panic. The concrete pointer makes the nil check safe. Codex
// caught this on the Phase 3.5b v1 review.
type Signaler struct {
	client *redis.Client
}

// NewSignaler wraps a Redis client. Pass nil to get a no-op
// signaler; callers don't need to nil-check at usage sites.
func NewSignaler(client *redis.Client) *Signaler {
	if client == nil {
		return nil
	}
	return &Signaler{client: client}
}

// Notify publishes a wake-up signal for one message's stream
// subscribers. The payload is intentionally a single byte (`1`)
// — the signal carries no data; subscribers re-query the DB.
//
// Errors are logged but not returned: the caller (the worker's
// stream observer) should NOT abort the run on a Redis hiccup.
// The 250ms poll fallback in the handler covers the gap.
func (s *Signaler) Notify(ctx context.Context, messageID string) {
	if s == nil || s.client == nil || messageID == "" {
		return
	}
	channel := chatStreamChannelPrefix + messageID
	if err := s.client.Publish(ctx, channel, "1").Err(); err != nil {
		// Log at debug — Redis blips during deploy storms are
		// expected and the poll fallback covers them. Anything
		// worse than transient surfaces in the broker's own
		// monitoring.
		slog.DebugContext(ctx, "chat stream signaler: publish failed",
			"message_id", messageID, "err", err)
	}
}

// Subscribe opens a Redis subscription for one message's wake-up
// signals. Returns a `*Subscription` whose Wakes() channel fires
// on each PUBLISH. The handler typically:
//
//	sub := signaler.Subscribe(ctx, msgID)
//	if sub != nil { defer sub.Close() }
//	for {
//	    select {
//	    case <-sub.Wakes(): // re-query DB
//	    case <-pollTicker.C: // fallback
//	    case <-ctx.Done(): return
//	    }
//	}
//
// nil signaler returns nil subscription; handler MUST handle
// the nil case (its poll loop runs unchanged).
func (s *Signaler) Subscribe(ctx context.Context, messageID string) *Subscription {
	if s == nil || s.client == nil || messageID == "" {
		return nil
	}
	// Readiness probe with a BOUNDED context. The handler's
	// parent ctx may live the full chatStreamMaxDuration (5m);
	// using it directly would leave a stalled Redis hanging the
	// handler for minutes before degrading to poll-only. The
	// bounded probe fails fast into the fallback path. Codex
	// caught this on Phase 3.5b v1.
	//
	// Use the bounded probe ctx for BOTH the Subscribe call AND
	// the Receive readiness ack — codex flagged on v2 that
	// passing the unbounded request ctx to Subscribe would let
	// a Redis brownout stall in the dial/write phase before the
	// probe even starts. The redis client's dial/write timeouts
	// still apply, but using probeCtx everywhere makes the
	// "Subscribe returns within ~2s" contract explicit.
	probeCtx, cancel := context.WithTimeout(ctx, chatStreamSubscribeProbeTimeout)
	defer cancel()
	channel := chatStreamChannelPrefix + messageID
	pubsub := s.client.Subscribe(probeCtx, channel)
	if _, err := pubsub.Receive(probeCtx); err != nil {
		// Log + return nil so the handler's poll fallback runs.
		// Nil-safe consumer pattern.
		slog.DebugContext(ctx, "chat stream signaler: subscribe probe failed",
			"message_id", messageID, "err", err)
		_ = pubsub.Close()
		return nil
	}
	wakes := make(chan struct{}, 4)
	sub := &Subscription{
		pubsub:    pubsub,
		wakes:     wakes,
		messageID: messageID,
	}
	go sub.pump(ctx)
	return sub
}

// Subscription is one open Redis SUBSCRIBE for a single
// message's stream signals. Pump goroutine forwards Redis
// messages onto Wakes() as `struct{}{}` (signal-only).
type Subscription struct {
	pubsub    *redis.PubSub
	wakes     chan struct{}
	messageID string
}

// Wakes returns the channel handlers select on. Buffered (size
// 4) to absorb burst patterns where the worker emits multiple
// events in a tight loop; a slow consumer drops extras (we only
// need ONE wake-up to trigger a DB re-read, since the read
// catches up to MAX(seq) regardless).
func (s *Subscription) Wakes() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.wakes
}

// Close ends the subscription. Idempotent.
func (s *Subscription) Close() {
	if s == nil {
		return
	}
	_ = s.pubsub.Close()
}

// pump bridges Redis pubsub.Channel() onto the Wakes() channel.
// One goroutine per Subscription; exits when the pubsub is
// closed (Redis disconnect or Subscription.Close).
//
// Burst coalescing: if Wakes is full (4 pending signals), drop
// the new one — the next handler tick will read up to MAX(seq)
// from the DB, so an unread signal isn't a missed event.
func (s *Subscription) pump(ctx context.Context) {
	defer close(s.wakes)
	ch := s.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			select {
			case s.wakes <- struct{}{}:
			default:
				// Wakes channel full — coalesce. The handler
				// reads to MAX(seq) on every wake, so a missed
				// signal doesn't lose data.
			}
		}
	}
}

// IsRedisNil reports whether err is the redis-nil sentinel.
// Helper for callers that want to distinguish "channel unset"
// from real errors. Currently unused but exported for parity
// with the events package; future health-probe or admin tools
// may need it.
func IsRedisNil(err error) bool {
	return errors.Is(err, redis.Nil)
}
