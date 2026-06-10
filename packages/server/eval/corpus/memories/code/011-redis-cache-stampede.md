## Debug: Redis Cache Stampede on Recall Endpoint

Investigated a spike in p99 recall latency (jumped from ~120ms to ~2.8s) that occurred every day around 6:00 UTC. Grafana showed a clear pattern: Redis hit rate dropped to 0% at the same time, then slowly recovered over 60-90 seconds while PostgreSQL CPU spiked to 85%.

### Timeline

```
06:00:00 UTC — Redis TTL expires for all popular recall cache entries
06:00:01 UTC — 47 concurrent recall requests arrive (from scheduled agent syncs)
06:00:01 UTC — All 47 requests miss cache simultaneously
06:00:01 UTC — All 47 requests hit PostgreSQL + Voyage API in parallel
06:00:03 UTC — PostgreSQL CPU: 85%, active connections: 47/50
06:00:08 UTC — First responses return, cache repopulated
06:00:15 UTC — Hit rate recovers to 92%
```

This is a classic **cache stampede** (also called "thundering herd"). All cached entries expired at the same time because they were all set with the same TTL (5 minutes), and the scheduled agent syncs all fire at the top of each hour.

### Grafana Dashboard Observations

```
Panel: Recall Cache Hit Rate (5m rolling)
  05:55 — 94%
  06:00 — 0%
  06:01 — 23%
  06:05 — 89%
  06:10 — 95%

Panel: PostgreSQL Active Connections
  05:55 — 8
  06:00 — 47
  06:01 — 41
  06:05 — 12

Panel: Recall p99 Latency
  05:55 — 118ms
  06:00 — 2,847ms
  06:01 — 1,423ms
  06:05 — 134ms
```

### Root Cause

The Redis cache layer in `internal/cache/redis.go` used a fixed TTL for all recall cache entries:

```go
// BEFORE — fixed TTL causes synchronized expiration
func (c *RedisCache) SetRecallResult(ctx context.Context, key string, result []byte) error {
    return c.client.Set(ctx, key, result, 5*time.Minute).Err()
}
```

### Solution: Jittered TTL

Added random jitter (±20%) to the base TTL so entries expire at staggered times:

```go
// AFTER — jittered TTL prevents synchronized expiration
func (c *RedisCache) SetRecallResult(ctx context.Context, key string, result []byte) error {
    baseTTL := 5 * time.Minute
    jitter := time.Duration(rand.Int63n(int64(baseTTL) / 5)) // ±60s
    if rand.Intn(2) == 0 {
        jitter = -jitter
    }
    ttl := baseTTL + jitter // 4m00s to 6m00s

    return c.client.Set(ctx, key, result, ttl).Err()
}
```

### Additional Mitigations

1. **Singleflight for cache misses** — Used `golang.org/x/sync/singleflight` to deduplicate concurrent cache misses for the same key:

```go
func (c *RedisCache) GetOrSetRecallResult(ctx context.Context, key string, fetch func() ([]byte, error)) ([]byte, error) {
    // Check cache first
    cached, err := c.client.Get(ctx, key).Bytes()
    if err == nil {
        return cached, nil
    }

    // Deduplicate concurrent fetches for the same key
    result, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
        data, err := fetch()
        if err != nil {
            return nil, err
        }
        // Cache with jittered TTL
        _ = c.SetRecallResult(ctx, key, data)
        return data, nil
    })

    if err != nil {
        return nil, err
    }
    return result.([]byte), nil
}
```

2. **Stale-while-revalidate** — For non-critical recall results, we serve the stale cached value while refreshing in the background. This is opt-in via a `Cache-Control: stale-while-revalidate=30` pattern.

### Results After Fix

- p99 recall latency at 06:00 UTC dropped from ~2.8s to ~180ms
- PostgreSQL peak connections at the stampede window dropped from 47 to 8
- Redis hit rate never drops below 75% (previously hit 0%)
- Deployed in commit `f4a8b23` on 2026-04-06
