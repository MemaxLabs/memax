package cache

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	// SetNX sets `key` to `value` with `ttl` only if the key does not
	// already exist. Returns true when the key was set (caller is the
	// "winner"), false when the key already existed (caller should
	// treat the operation as a duplicate). Atomic per Redis semantics
	// — `SET key value NX EX ttl`. Used by handlers that need to
	// dedup near-simultaneous writes (plan 21 phase 0:
	// memories/access tracking).
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	// Del removes `key` if present. Used by SetNX-based locks that
	// want to release the dedup window early on operation failure
	// (e.g. TrackAccessed releasing the access-dedup key when the
	// store increment errors so a retry within the original window
	// can succeed). No-op if the key doesn't exist.
	Del(ctx context.Context, key string) error
	Close() error
}

type RedisCache struct {
	client *redis.Client
}

func NewFromEnv() Cache {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return nil
	}
	return New(url)
}

func New(url string) Cache {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil
	}
	return &RedisCache{client: redis.NewClient(opts)}
}

func (c *RedisCache) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis cache not configured")
	}
	return c.client.Ping(ctx).Err()
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	if c == nil || c.client == nil {
		return "", redis.Nil
	}
	return c.client.Get(ctx, key).Result()
}

func (c *RedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Del wraps Redis `DEL key`. No-op when the cache is not configured.
// Used by SetNX-lock callers that need to release the dedup window
// early on failure.
func (c *RedisCache) Del(ctx context.Context, key string) error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Del(ctx, key).Err()
}

// SetNX wraps Redis `SET key value NX EX ttl` via go-redis's
// `SetArgs{Mode: "NX", TTL: ttl}` form (the legacy `SetNX` shorthand
// is deprecated). Returns true when the key was set, false when it
// already existed. When the cache is not configured, returns
// (true, nil) so callers acting on the result fall open — i.e., a
// missing Redis behaves like "you're the winner, go ahead and do the
// work" rather than blocking. Plan 21 §4.4.
func (c *RedisCache) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if c == nil || c.client == nil {
		return true, nil
	}
	res, err := c.client.SetArgs(ctx, key, value, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err != nil {
		// go-redis returns redis.Nil when SET NX failed because the
		// key already existed. That's not an error from the caller's
		// perspective — translate to (false, nil) so handlers can
		// treat "not the winner" the same as a successful no-op.
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	// `OK` indicates the SET succeeded. Anything else (defensive) is
	// also treated as "set."
	return res == "OK" || res != "", nil
}

func (c *RedisCache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// Client returns the underlying redis.Client for advanced operations
// (INCR, DECR, Lua scripts) that the Cache interface doesn't expose.
// Returns nil if the cache is not configured.
func (c *RedisCache) Client() *redis.Client {
	if c == nil {
		return nil
	}
	return c.client
}

// CriticalFromEnv creates a Redis client from the REDIS_CRITICAL_URL env var.
// This instance should use noeviction policy for rate limit and usage counters.
// Falls back to REDIS_URL if REDIS_CRITICAL_URL is not set (dev/staging).
// Returns nil if neither is set.
func CriticalFromEnv() *RedisCache {
	url := os.Getenv("REDIS_CRITICAL_URL")
	if url == "" {
		url = os.Getenv("REDIS_URL")
	}
	if url == "" {
		return nil
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil
	}
	return &RedisCache{client: redis.NewClient(opts)}
}
