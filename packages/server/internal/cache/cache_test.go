package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// ─── Constructor paths ──────────────────────────────────────────

func TestNewRejectsBadURL(t *testing.T) {
	t.Parallel()
	if c := New("not a url"); c != nil {
		t.Errorf("bad URL should return nil, got %T", c)
	}
}

func TestNewFromEnvReturnsNilWhenUnset(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if c := NewFromEnv(); c != nil {
		t.Errorf("unset REDIS_URL should return nil, got %T", c)
	}
}

func TestNewFromEnvUsesEnvWhenSet(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_URL", "redis://"+mr.Addr())
	c := NewFromEnv()
	if c == nil {
		t.Fatal("NewFromEnv should return a Cache when REDIS_URL is set")
	}
	t.Cleanup(func() { _ = c.Close() })

	// Smoke the roundtrip so we know the client actually connected.
	ctx := context.Background()
	if err := c.Set(ctx, "smoke", "ok", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := c.Get(ctx, "smoke")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "ok" {
		t.Errorf("Get = %q, want ok", v)
	}
}

func TestCriticalFromEnvPrefersCriticalURL(t *testing.T) {
	mrCritical := miniredis.RunT(t)
	mrFallback := miniredis.RunT(t)

	// When both are set, critical wins.
	t.Setenv("REDIS_CRITICAL_URL", "redis://"+mrCritical.Addr())
	t.Setenv("REDIS_URL", "redis://"+mrFallback.Addr())

	c := CriticalFromEnv()
	if c == nil {
		t.Fatal("CriticalFromEnv should return a RedisCache")
	}
	t.Cleanup(func() { _ = c.Close() })

	// Write a value via the cache — then look it up directly on
	// the critical miniredis. If the fallback's miniredis ends up
	// getting the write instead, the lookup on critical will miss.
	ctx := context.Background()
	if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := mrCritical.Get("k"); err != nil || got != "v" {
		t.Errorf("critical miniredis didn't see the write: got=%q err=%v", got, err)
	}
}

func TestCriticalFromEnvFallsBackToRedisURL(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_CRITICAL_URL", "")
	t.Setenv("REDIS_URL", "redis://"+mr.Addr())

	c := CriticalFromEnv()
	if c == nil {
		t.Fatal("CriticalFromEnv should fall back to REDIS_URL")
	}
	_ = c.Close()
}

func TestCriticalFromEnvReturnsNilWhenBothUnset(t *testing.T) {
	t.Setenv("REDIS_CRITICAL_URL", "")
	t.Setenv("REDIS_URL", "")
	if c := CriticalFromEnv(); c != nil {
		t.Errorf("both unset should return nil, got %T", c)
	}
}

func TestCriticalFromEnvRejectsBadURL(t *testing.T) {
	t.Setenv("REDIS_CRITICAL_URL", "not a url")
	t.Setenv("REDIS_URL", "")
	if c := CriticalFromEnv(); c != nil {
		t.Errorf("bad URL should return nil, got %T", c)
	}
}

// ─── Nil-receiver guards ────────────────────────────────────────

func TestRedisCacheNilReceiverGuards(t *testing.T) {
	t.Parallel()
	var c *RedisCache
	ctx := context.Background()

	// Every method on nil receiver must be safe — New returns nil
	// on bad URL and callers unwrap via type assertions that might
	// produce a typed-nil.
	if err := c.Ping(ctx); err == nil {
		t.Error("Ping on nil receiver should error ('not configured')")
	}
	if v, err := c.Get(ctx, "k"); err == nil {
		t.Errorf("Get on nil receiver should error, got (%q, nil)", v)
	}
	if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Errorf("Set on nil receiver should be no-op, got %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil receiver should be no-op, got %v", err)
	}
	if client := c.Client(); client != nil {
		t.Errorf("Client on nil receiver should return nil, got %T", client)
	}
}

// ─── Live Redis roundtrip ───────────────────────────────────────

func TestRedisCacheRoundtrip(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	c := New("redis://" + mr.Addr())
	if c == nil {
		t.Fatal("constructor returned nil for valid URL")
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	if err := c.Set(ctx, "foo", "bar", 30*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := c.Get(ctx, "foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "bar" {
		t.Errorf("Get = %q, want bar", v)
	}
}

func TestRedisCacheTTLExpires(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	c := New("redis://" + mr.Addr())
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	if err := c.Set(ctx, "k", "v", 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Fast-forward miniredis clock past the TTL.
	mr.FastForward(20 * time.Millisecond)
	if _, err := c.Get(ctx, "k"); err == nil {
		t.Error("expired key should return an error (redis.Nil)")
	}
}

func TestRedisCachePingSucceedsWhenConfigured(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	c := New("redis://" + mr.Addr()).(*RedisCache)
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping should succeed, got %v", err)
	}
}

func TestRedisCacheClientExposesUnderlying(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	c := New("redis://" + mr.Addr()).(*RedisCache)
	t.Cleanup(func() { _ = c.Close() })

	client := c.Client()
	if client == nil {
		t.Fatal("Client() should expose the underlying redis.Client")
	}
	// Use it for an op the Cache interface doesn't cover — INCR
	// is the canonical "we exposed this for advanced use" example.
	if err := client.Incr(context.Background(), "counter").Err(); err != nil {
		t.Errorf("INCR via exposed client: %v", err)
	}
}
