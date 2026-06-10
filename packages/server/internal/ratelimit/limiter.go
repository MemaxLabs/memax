// Package ratelimit provides per-user request rate limiting using a
// weighted sliding window algorithm implemented as an atomic Redis Lua script.
//
// The sliding window smooths boundary bursts: a user who consumed 55/60 of
// their limit at the end of minute N can only use 5 more at the start of
// minute N+1, not a fresh 60.
//
// If Redis is nil, rate limiting is disabled (all requests pass through).
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "memax:rl"
	keyTTL    = 120 // 2-minute TTL on each bucket key
	windowSec = 60  // 1-minute window
)

// slidingWindowScript implements the atomic weighted two-bucket sliding window.
//
// KEYS[1] = previous window key
// KEYS[2] = current window key
// ARGV[1] = weight for previous window (1 - elapsed_fraction)
// ARGV[2] = rate limit
// ARGV[3] = TTL in seconds
//
// Returns: {allowed (0/1), weighted_count, limit}
var slidingWindowScript = redis.NewScript(`
local prev = tonumber(redis.call('GET', KEYS[1]) or '0')
local curr = tonumber(redis.call('GET', KEYS[2]) or '0')
local weight = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

local weighted = prev * weight + curr
-- Check if adding this request would exceed the limit.
-- Use weighted + 1 (the projected count after this request),
-- not weighted (the count before this request).
if weighted + 1 > limit then
    return {0, math.ceil(weighted), limit}
end

redis.call('INCR', KEYS[2])
if redis.call('TTL', KEYS[2]) < 0 then
    redis.call('EXPIRE', KEYS[2], ttl)
end
return {1, math.ceil(weighted + 1), limit}
`)

// Result holds the outcome of a rate limit check.
type Result struct {
	Allowed    bool
	Limit      int
	Current    int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration // only meaningful when !Allowed
}

// Limiter enforces per-key request rate limits using Redis.
// If redis is nil, all requests are allowed (degraded mode).
type Limiter struct {
	redis  *redis.Client
	logger *slog.Logger
}

// New creates a Limiter. If redisClient is nil, rate limiting is disabled.
func New(redisClient *redis.Client) *Limiter {
	return &Limiter{
		redis:  redisClient,
		logger: slog.Default(),
	}
}

// Check performs a rate limit check for the given key and limit (requests per minute).
// The key should encode user + operation class, e.g. "u_abc:heavy".
//
// Returns a Result indicating whether the request is allowed, current count,
// remaining quota, and when the window resets.
func (l *Limiter) Check(ctx context.Context, key string, limit int) Result {
	now := time.Now()
	resetAt := nextWindowBoundary(now)

	if l.redis == nil || limit <= 0 {
		return Result{
			Allowed:   true,
			Limit:     limit,
			Current:   0,
			Remaining: limit,
			ResetAt:   resetAt,
		}
	}

	currentBucket := now.Unix() / windowSec
	previousBucket := currentBucket - 1
	elapsedFraction := float64(now.Unix()%windowSec) / float64(windowSec)
	prevWeight := 1.0 - elapsedFraction

	prevKey := fmt.Sprintf("%s:%s:%d", keyPrefix, key, previousBucket)
	currKey := fmt.Sprintf("%s:%s:%d", keyPrefix, key, currentBucket)

	vals, err := slidingWindowScript.Run(ctx, l.redis, []string{prevKey, currKey},
		fmt.Sprintf("%.6f", prevWeight), limit, keyTTL,
	).Int64Slice()
	if err != nil {
		// Redis error — allow the request (fail open)
		l.logger.Warn("ratelimit: lua script failed, allowing request",
			"error", err, "key", key)
		return Result{
			Allowed:   true,
			Limit:     limit,
			Current:   0,
			Remaining: limit,
			ResetAt:   resetAt,
		}
	}

	allowed := vals[0] == 1
	current := int(vals[1])
	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	result := Result{
		Allowed:   allowed,
		Limit:     limit,
		Current:   current,
		Remaining: remaining,
		ResetAt:   resetAt,
	}
	if !allowed {
		result.RetryAfter = time.Until(resetAt)
		if result.RetryAfter < time.Second {
			result.RetryAfter = time.Second
		}
	}
	return result
}

// nextWindowBoundary returns the start of the next 1-minute window.
func nextWindowBoundary(now time.Time) time.Time {
	bucket := now.Unix()/windowSec + 1
	return time.Unix(bucket*windowSec, 0)
}

// --- Operation classification ---

// OpClass represents the compute cost classification of an operation.
type OpClass string

const (
	OpClassHeavy    OpClass = "heavy"    // recall, push, ask — embedding, rerank, LLM
	OpClassLight    OpClass = "light"    // get, list, topics, forget
	OpClassMetadata OpClass = "metadata" // hubs, hub_members
	OpClassNone     OpClass = ""         // not rate-limited (auth, settings, etc.)
)

// ClassifyRequest maps an HTTP method + path to a rate limit operation class.
func ClassifyRequest(method, path string) OpClass {
	switch {
	case isUnmeteredControlPlane(path):
		return OpClassNone
	case method == "POST" && (path == "/v1/memories" || path == "/v1/memories/"):
		return OpClassHeavy
	case method == "POST" && (path == "/v1/recall" || path == "/v1/recall/"):
		return OpClassHeavy
	case method == "POST" && (path == "/v1/ask" || path == "/v1/ask/"):
		return OpClassHeavy
	case method == "GET" && (path == "/v1/hubs" || startsWithPrefix(path, "/v1/hubs/")):
		return OpClassMetadata
	case isLightDataPlane(method, path):
		return OpClassLight
	default:
		return OpClassNone
	}
}

func isUnmeteredControlPlane(path string) bool {
	switch {
	case path == "/v1/settings",
		path == "/v1/usage",
		path == "/v1/usage/dreams",
		path == "/v1/account/data",
		startsWithPrefix(path, "/v1/auth/"),
		startsWithPrefix(path, "/v1/configs"),
		startsWithPrefix(path, "/v1/agents"),
		startsWithPrefix(path, "/v1/admin/"),
		path == "/v1/events",
		startsWithPrefix(path, "/v1/events/"),
		path == "/v1/notifications",
		startsWithPrefix(path, "/v1/notifications/"),
		startsWithPrefix(path, "/v1/invites/"):
		return true
	default:
		return false
	}
}

func isLightDataPlane(method, path string) bool {
	switch method {
	case "GET", "PATCH", "DELETE":
		return true
	case "POST":
		return path != ""
	default:
		return false
	}
}

func startsWithPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
