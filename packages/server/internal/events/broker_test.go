package events

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestBroker returns a Broker wired to a miniredis so tests get
// full pub/sub semantics without any external service.
func newTestBroker(t *testing.T) *Broker {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &Broker{
		logger:  slog.Default(),
		client:  client,
		channel: "memax:events:test",
		subs:    map[string]*Subscription{},
	}
}

func TestNewBrokerFromEnvNilWithoutRedisURL(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if b := NewBrokerFromEnv(nil); b != nil {
		t.Errorf("no REDIS_URL should return nil, got %+v", b)
	}
}

func TestNewBrokerFromEnvBuildsClientWithParsableURL(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_URL", "redis://"+mr.Addr())
	b := NewBrokerFromEnv(nil)
	if b == nil {
		t.Fatal("expected non-nil broker")
	}
	if b.client == nil {
		t.Error("broker should have a redis client")
	}
	_ = b.Close()
}

func TestNewBrokerFromEnvIgnoresMalformedURL(t *testing.T) {
	t.Setenv("REDIS_URL", "not a url")
	if b := NewBrokerFromEnv(nil); b != nil {
		t.Errorf("malformed URL should return nil, got %+v", b)
	}
}

func TestBrokerNilMethodsAreNoOps(t *testing.T) {
	t.Parallel()
	var b *Broker
	// Nil receivers on all public methods must not panic — the
	// graceful-degradation path for dev without Redis.
	b.Start(context.Background())
	if err := b.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
	if err := b.Publish(context.Background(), Event{}); err != nil {
		t.Errorf("nil Publish: %v", err)
	}
	if sub := b.Subscribe("u1", nil); sub != nil {
		t.Error("nil Subscribe should return nil")
	}
}

func TestBrokerPublishSubscribeRoundTrip(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.Start(context.Background())
	// Give the subscribe goroutine a beat to attach.
	time.Sleep(50 * time.Millisecond)

	sub := b.Subscribe("u1", map[string]string{"h1": "owner"})
	if sub == nil {
		t.Fatal("Subscribe returned nil")
	}
	t.Cleanup(sub.Close)

	evt := Event{Type: "hub.memories.changed", HubID: "h1", RecipientUserID: "u1"}
	if err := b.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Event must arrive within a second — miniredis local roundtrip is
	// typically < 10ms but don't fail fast on CI noise.
	select {
	case got := <-sub.Events:
		if got.Type != "hub.memories.changed" {
			t.Errorf("event type wrong: %q", got.Type)
		}
		if got.HubID != "h1" {
			t.Errorf("event hub wrong: %q", got.HubID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBrokerPublishStampsIDAndTimestamp(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	sub := b.Subscribe("u1", nil)
	t.Cleanup(sub.Close)

	if err := b.Publish(context.Background(), Event{
		Type: "notification.created", RecipientUserID: "u1",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case got := <-sub.Events:
		if got.ID == "" {
			t.Error("Publish should stamp an ID")
		}
		if got.CreatedAt.IsZero() {
			t.Error("Publish should stamp CreatedAt")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestBrokerSubscriptionCloseRemovesSub(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.Start(context.Background())

	sub := b.Subscribe("u1", nil)
	b.mu.RLock()
	if _, ok := b.subs[sub.ID]; !ok {
		t.Error("sub not registered")
	}
	b.mu.RUnlock()

	sub.Close()
	b.mu.RLock()
	if _, ok := b.subs[sub.ID]; ok {
		t.Error("sub still registered after Close")
	}
	b.mu.RUnlock()

	// Nil receiver is safe.
	var nilSub *Subscription
	nilSub.Close()
}

func TestBrokerPublishRejectsEventWithoutRouting(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.Start(context.Background())
	time.Sleep(50 * time.Millisecond)

	sub := b.Subscribe("u1", map[string]string{"h1": "owner"})
	t.Cleanup(sub.Close)

	// Event with neither HubID nor RecipientUserID — fanout drops it.
	if err := b.Publish(context.Background(), Event{Type: "unrouted"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Give some time; no event should arrive.
	select {
	case got := <-sub.Events:
		t.Errorf("unrouted event leaked: %+v", got)
	case <-time.After(200 * time.Millisecond):
		// expected timeout
	}
}
