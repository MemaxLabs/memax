package chatstream

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// acquireRedis returns a Redis client backed by REDIS_URL or the
// default dev compose endpoint. Tests skip if Redis isn't
// reachable — matches the existing rate-limit/meter test
// pattern.
func acquireRedis(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://redis:6379"
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Skipf("redis url parse: %v", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unreachable (%v) — skipping signaler integration test", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Nil signaler is the dev/staging path. Notify is a no-op,
// Subscribe returns nil. Both must be safe to call without
// panicking.
func TestNilSignaler_IsNoOp(t *testing.T) {
	t.Parallel()
	var s *Signaler
	s.Notify(context.Background(), "msg-anything")
	if got := s.Subscribe(context.Background(), "msg-anything"); got != nil {
		t.Errorf("nil signaler Subscribe = %v, want nil", got)
	}
	// Also: a Signaler constructed from a nil client.
	s = NewSignaler(nil)
	if s != nil {
		t.Errorf("NewSignaler(nil) = %v, want nil", s)
	}
}

// Typed-nil-pointer regression test. The classic Go gotcha: a
// nil *redis.Client passed through an interface field becomes
// non-nil at the interface level. v1 used redis.UniversalClient
// (interface) for the Signaler.client field — `var c *redis.Client;
// NewSignaler(c)` would have returned a non-nil Signaler wrapping
// a nil client, and the first Notify/Subscribe call would panic
// with a nil pointer dereference. Codex caught this on the v1
// review. The fix uses *redis.Client (concrete) so a literal
// nil is detected.
func TestNewSignaler_TypedNilPointerReturnsNil(t *testing.T) {
	t.Parallel()
	var nilClient *redis.Client
	if got := NewSignaler(nilClient); got != nil {
		t.Errorf("NewSignaler with typed-nil pointer returned %v, want nil", got)
	}
}

// nil Subscription Wakes() returns the nil channel; nil
// Subscription.Close() must not panic.
func TestNilSubscription_IsSafe(t *testing.T) {
	t.Parallel()
	var sub *Subscription
	if got := sub.Wakes(); got != nil {
		t.Errorf("nil sub.Wakes() = %v, want nil chan", got)
	}
	sub.Close() // must not panic
}

// Round-trip: publisher emits a Notify; subscriber receives a
// wake-up on its Wakes channel within a reasonable timeout.
// This is the load-bearing contract — without it the SSE
// handler would never see live deltas via the broker.
func TestSignaler_NotifyDeliversToSubscriber(t *testing.T) {
	t.Parallel()
	client := acquireRedis(t)
	signaler := NewSignaler(client)
	if signaler == nil {
		t.Fatal("NewSignaler returned nil with a non-nil client")
	}

	msgID := "msg-" + strings.ReplaceAll(t.Name(), "/", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := signaler.Subscribe(ctx, msgID)
	if sub == nil {
		t.Fatal("Subscribe returned nil — Redis subscription failed")
	}
	defer sub.Close()

	// PUBLISH from a separate goroutine; the subscriber's
	// Receive() probe in Subscribe() has already settled by
	// the time we get here, so this Notify will land on a
	// ready subscriber.
	go signaler.Notify(ctx, msgID)

	select {
	case <-sub.Wakes():
		// success
	case <-time.After(2 * time.Second):
		t.Fatalf("subscriber did not receive wake-up signal within timeout")
	}
}

// Burst coalescing: 10 rapid Notify calls produce at most
// `len(Wakes-buffer)` wake-up signals (the rest are dropped on
// a full buffer). The SSE handler reads to MAX(seq) on every
// wake regardless, so dropped wakes don't lose data.
func TestSignaler_BurstCoalescing(t *testing.T) {
	t.Parallel()
	client := acquireRedis(t)
	signaler := NewSignaler(client)

	msgID := "msg-" + strings.ReplaceAll(t.Name(), "/", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := signaler.Subscribe(ctx, msgID)
	if sub == nil {
		t.Fatal("Subscribe returned nil")
	}
	defer sub.Close()

	// Burst more than the Wakes buffer (size 4). Some will
	// drop. The contract: at LEAST one signal arrives.
	for i := 0; i < 10; i++ {
		signaler.Notify(ctx, msgID)
	}

	// Drain whatever's there within 1 second. Assert we got
	// at least one — the contract is "wake-ups are best-effort
	// signal-only; the DB read catches up regardless."
	got := 0
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-sub.Wakes():
			got++
		case <-deadline:
			if got == 0 {
				t.Fatal("burst produced zero wake-ups")
			}
			// Coalescing is fine — we just need ≥1.
			return
		}
	}
}

// Per-message scoping: a Notify on msg-A doesn't wake a
// subscriber on msg-B. Critical for fan-out efficiency at
// scale (one channel per stream, not one fat channel).
func TestSignaler_PerMessageChannelScope(t *testing.T) {
	t.Parallel()
	client := acquireRedis(t)
	signaler := NewSignaler(client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	subA := signaler.Subscribe(ctx, "msg-A-"+t.Name())
	subB := signaler.Subscribe(ctx, "msg-B-"+t.Name())
	if subA == nil || subB == nil {
		t.Fatal("Subscribe returned nil")
	}
	defer subA.Close()
	defer subB.Close()

	// Notify only on A. B must not receive.
	signaler.Notify(ctx, "msg-A-"+t.Name())

	select {
	case <-subA.Wakes():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("subA did not receive its own message's wake")
	}

	select {
	case <-subB.Wakes():
		t.Error("subB received a wake intended for msg-A — channel scoping broken")
	case <-time.After(500 * time.Millisecond):
		// expected — no wake
	}
}
