package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// planLimitsResolver resolves a user's effective plan limits.
// Matches the plans.Registry interface without importing it (avoids cycle).
type planLimitsResolver interface {
	GetUserLimits(ctx context.Context, userID, planID string) model.UserLimits
}

// hubAwareResolver resolves limits with per-hub elevation.
// Matches planresolver.Resolver. When available, replaces planLimitsResolver.
type hubAwareResolver interface {
	ResolveForRequest(ctx context.Context, userID, billingHubID string) model.UserLimits
	ResolveReadEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolvePersonalWriteEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolveHubWriteEntitlements(ctx context.Context, userID, hubID string) model.ResolvedEntitlements
}

// userResolver reads a user from the store.
type userResolver interface {
	GetUser(id string) (*model.User, error)
}

var globalRateLimitRPM = func() int {
	if v := os.Getenv("GLOBAL_RATE_LIMIT_RPM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 10000
}()

// pathLimitOverrides caps specific endpoints tighter than their
// opClass default. Maps "METHOD path" → requests-per-minute.
//
// Use this when an endpoint inside an opClass is materially more
// expensive than its siblings — e.g. /v1/ask costs an LLM synthesis
// call plus retrieval, while /v1/recall is retrieval only. Both are
// "heavy" but ask is ~3× the cost.
//
// The override is an absolute RPM, NOT a multiplier, so it doesn't
// silently scale with plan tier. If a pro user's HeavyRPM is 120 and
// this cap is 60, they see 60 for ask and 120 for recall — the
// override is the floor-regardless-of-plan. If the plan's class
// limit is LOWER than the override, the plan limit wins (via min).
//
// Keep keys as exact static paths. Dynamic resource IDs would create
// high-cardinality endpoint buckets in Redis.
var pathLimitOverrides = map[string]int{
	// Ask is the most expensive heavy op — LLM call on top of retrieval.
	// Even pro plans should see this capped a notch tighter than recall.
	"POST /v1/ask": 30,
	// Batch delete and batch move can each touch hundreds of rows.
	// Classify as heavy but let the per-endpoint cap limit the fanout
	// rate specifically.
	"POST /v1/memories/batch-delete": 15,
	"POST /v1/memories/batch-move":   15,
}

// applyPathOverride returns min(defaultLimit, pathLimitOverrides[key])
// when an override exists, else defaultLimit. method+path gets
// normalized to "METHOD /normalized/path" before lookup so trailing
// slashes and query strings don't cause misses.
func applyPathOverride(method, path string, defaultLimit int) (int, bool) {
	if defaultLimit <= 0 {
		return defaultLimit, false
	}
	key := method + " " + strings.TrimRight(path, "/")
	if override, ok := pathLimitOverrides[key]; ok && override > 0 && override < defaultLimit {
		return override, true
	}
	return defaultLimit, false
}

// Middleware returns an HTTP middleware that enforces per-user and global
// request rate limits. Runs BEFORE metering (no point counting a rate-limited
// request).
//
// Sets standard rate limit headers on every response:
//   - X-RateLimit-Limit
//   - X-RateLimit-Remaining
//   - X-RateLimit-Reset (Unix timestamp)
//
// On 429: adds Retry-After header (seconds).
// Middleware returns an HTTP middleware that enforces per-user and global
// request rate limits. When a hubResolver is available, uses per-hub elevation.
func (l *Limiter) Middleware(registry planLimitsResolver, users userResolver, hubResolver ...hubAwareResolver) func(http.Handler) http.Handler {
	var resolver hubAwareResolver
	if len(hubResolver) > 0 {
		resolver = hubResolver[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Classify the request
			path := strings.TrimRight(r.URL.Path, "/")
			opClass := ClassifyRequest(r.Method, path)
			if opClass == OpClassNone {
				next.ServeHTTP(w, r)
				return
			}

			userID := handler.GetUserID(r)
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Resolve per-user rate limit from plan (with hub elevation if available),
			// then apply per-endpoint override (tighter ceiling for
			// expensive endpoints within the same opClass).
			limit := resolveLimitWithHub(r, userID, path, opClass, registry, users, resolver)
			limit, useEndpointBucket := applyPathOverride(r.Method, path, limit)

			// Global safety valve — check first
			globalKey := fmt.Sprintf("global:%s", opClass)
			globalResult := l.Check(r.Context(), globalKey, globalRateLimitRPM)
			if !globalResult.Allowed {
				writeRateLimited(w, globalResult)
				return
			}

			// Per-user check
			userKey := fmt.Sprintf("%s:%s", userID, opClass)
			if useEndpointBucket {
				userKey = fmt.Sprintf("%s:%s:%s:%s", userID, opClass, r.Method, strings.TrimRight(path, "/"))
			}
			result := l.Check(r.Context(), userKey, limit)

			// Always set rate limit headers (even on success)
			setRateLimitHeaders(w, result)

			if !result.Allowed {
				writeRateLimited(w, result)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveLimitWithHub determines the per-minute rate limit, using scoped
// entitlement resolution when available.
func resolveLimitWithHub(r *http.Request, userID, path string, opClass OpClass, registry planLimitsResolver, users userResolver, resolver hubAwareResolver) int {
	var limits model.UserLimits
	if resolver != nil {
		// Route to the correct scoped resolver method based on the operation:
		//   - push to team hub → ResolveHubWriteEntitlements (target hub limits)
		//   - push to personal hub → ResolvePersonalWriteEntitlements
		//   - recall/ask → ResolveReadEntitlements (max personal + best hub)
		if path == "/v1/memories" && r.Method == "POST" {
			writeHub := handler.GetWriteHubID(r)
			if writeHub != "" {
				limits = resolver.ResolveHubWriteEntitlements(r.Context(), userID, writeHub).Limits
			} else {
				limits = resolver.ResolvePersonalWriteEntitlements(r.Context(), userID).Limits
			}
		} else if path == "/v1/recall" || path == "/v1/ask" || strings.HasPrefix(path, "/v1/ask") {
			limits = resolver.ResolveReadEntitlements(r.Context(), userID).Limits
		} else {
			// Default: use read entitlements for metadata/other requests
			limits = resolver.ResolveReadEntitlements(r.Context(), userID).Limits
		}
	} else if registry != nil && users != nil {
		user, err := users.GetUser(userID)
		if err != nil || user == nil {
			return 60
		}
		// Prefer scoped personal_plan_id for limit resolution
		planID := user.PersonalPlanID
		if planID == "" {
			planID = user.Plan
		}
		limits = registry.GetUserLimits(r.Context(), userID, planID)
	} else {
		switch opClass {
		case OpClassHeavy:
			return 10
		case OpClassLight:
			return 60
		default:
			return 120
		}
	}

	switch opClass {
	case OpClassHeavy:
		return limits.RateLimitHeavyRPM
	case OpClassLight:
		return limits.RateLimitLightRPM
	case OpClassMetadata:
		return limits.RateLimitRPM
	default:
		return limits.RateLimitRPM
	}
}

// setRateLimitHeaders sets the standard rate limit headers on the response.
func setRateLimitHeaders(w http.ResponseWriter, result Result) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
}

// writeRateLimited writes a 429 Too Many Requests response with standard headers.
func writeRateLimited(w http.ResponseWriter, result Result) {
	setRateLimitHeaders(w, result)
	retryAfterSec := int(result.RetryAfter.Seconds())
	if retryAfterSec < 1 {
		retryAfterSec = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))

	handler.WriteErrorWithDetails(w, http.StatusTooManyRequests, "rate_limited",
		fmt.Sprintf("Rate limit exceeded. Try again in %d seconds.", retryAfterSec),
		map[string]any{
			"limit":       result.Limit,
			"current":     result.Current,
			"retry_after": retryAfterSec,
			"reset_at":    result.ResetAt.Format(time.RFC3339),
		})
}
