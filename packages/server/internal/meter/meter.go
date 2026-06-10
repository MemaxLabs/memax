// Package meter provides usage metering with Redis-backed dual counters
// and a reserve/commit/rollback pattern that ensures only successful
// operations count against quota.
//
// Key design:
//   - Each operation has two Redis counters: "gate" (includes in-flight
//     reservations) and "committed" (successful only).
//   - Quota checks read the gate counter (conservative).
//   - Postgres sync reads the committed counter (no corruption from rollbacks).
//   - Memory count is managed by handlers, not middleware (v3 findings #1/#2).
package meter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

const (
	keyPrefix = "memax:usage"
	keyTTL    = 45 * 24 * time.Hour // 45 days — covers billing period + buffer
)

// Meter is the core metering service. It manages Redis counters for quota
// enforcement and logs usage events to Postgres for billing/analytics.
//
// If redis is nil, the meter degrades gracefully: all requests are allowed,
// a warning is logged, and usage events are still written to Postgres.
// planResolver resolves per-request limits with hub context.
// When set, the meter uses scoped entitlement resolution. When nil,
// falls back to the existing registry.GetUserLimits (user plan only).
type planResolver interface {
	// Legacy method — kept for backward compat callers
	ResolveForRequest(ctx context.Context, userID, billingHubID string) model.UserLimits

	// Scoped methods — used by meter and ratelimit middleware
	ResolveReadEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolvePersonalWriteEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolveHubWriteEntitlements(ctx context.Context, userID, hubID string) model.ResolvedEntitlements
}

type Meter struct {
	redis    *redis.Client
	store    store.Store
	registry *plans.Registry
	resolver planResolver // nil = user-plan-only (backward compat)
	logger   *slog.Logger

	// warnOnce prevents spamming "redis unreachable" warnings.
	warnOnce sync.Once
}

// SetResolver wires the plan resolver for per-hub elevation.
// Call after creating the Meter and resolver (avoids circular deps).
func (m *Meter) SetResolver(r planResolver) {
	if m != nil {
		m.resolver = r
	}
}

// New creates a Meter. If redisClient is nil, the meter operates in
// degraded mode (allows all requests, logs warnings).
func New(redisClient *redis.Client, s store.Store, registry *plans.Registry) *Meter {
	return &Meter{
		redis:    redisClient,
		store:    s,
		registry: registry,
		logger:   slog.Default(),
	}
}

// --- Key helpers ---

func currentPeriod() string {
	return time.Now().UTC().Format("2006-01")
}

func gateKey(userID, period, op string) string {
	return fmt.Sprintf("%s:%s:%s:%s", keyPrefix, userID, period, op)
}

func committedKey(userID, period, op string) string {
	return fmt.Sprintf("%s:%s:%s:%s:committed", keyPrefix, userID, period, op)
}

func memoryGateKey(userID string) string {
	return fmt.Sprintf("%s:%s:memory_count", keyPrefix, userID)
}

func memoryCommittedKey(userID string) string {
	return fmt.Sprintf("%s:%s:memory_count:committed", keyPrefix, userID)
}

// Hub-scoped memory count keys — independent namespace from the owner
// counters. Both get checked on every push to a team hub: the owner
// counter keeps a user from exceeding their personal allowance across
// all hubs; the hub counter keeps a single hub from exceeding the cap
// its subscription plan allows. A hub reaching its cap freezes the
// hub even if individual members still have personal headroom.
func hubMemoryGateKey(hubID string) string {
	return fmt.Sprintf("%s:hub:%s:memory_count", keyPrefix, hubID)
}

func hubMemoryCommittedKey(hubID string) string {
	return fmt.Sprintf("%s:hub:%s:memory_count:committed", keyPrefix, hubID)
}

func storageBytesGateKey(userID string) string {
	return fmt.Sprintf("%s:%s:storage_bytes", keyPrefix, userID)
}

func storageBytesCommittedKey(userID string) string {
	return fmt.Sprintf("%s:%s:storage_bytes:committed", keyPrefix, userID)
}

// --- Token creation ---

// --- Reserve / quota check ---

// Reserve atomically increments the gate counter and checks against the
// plan limit. If the limit is exceeded, the gate counter is decremented
// and the returned token has Allowed=false.
//
// If Redis is unreachable, returns an always-allowed token (degraded mode).
//
// Counter recovery: if the gate key is missing mid-month (Redis restart,
// TTL expiry), Reserve back-fills from Postgres before proceeding. This
// prevents users from getting fresh quota after a Redis disruption.
func (m *Meter) Reserve(ctx context.Context, userID, op string, limit int) (token *meterctx.Token, allowed bool, current int64) {
	period := currentPeriod()

	// Build commit/rollback callbacks that capture the Redis logic.
	// When Redis is nil, the Postgres increment happens at reserve time
	// (atomic), not at commit time — so the commit callback is a no-op
	// for the no-Redis path.
	commitFn := func() {
		if m.redis != nil {
			bgCtx := context.Background()
			key := committedKey(userID, period, op)
			m.redis.Incr(bgCtx, key)
			m.redis.ExpireNX(bgCtx, key, keyTTL)
		}
		// No Redis: Postgres was already incremented at reserve time.
	}
	rollbackFn := func() {
		if m.redis != nil {
			bgCtx := context.Background()
			key := gateKey(userID, period, op)
			m.redis.Decr(bgCtx, key)
		} else if m.store != nil {
			// No Redis: Postgres was incremented at reserve time,
			// so rollback must decrement it back.
			field := op + "_count"
			m.store.DecrementUsage(userID, field)
		}
	}
	token = meterctx.NewToken(commitFn, rollbackFn)

	// Unlimited plan
	if model.IsUnlimited(limit) {
		return token, true, 0
	}

	if m.redis == nil {
		// No Redis: single-statement Postgres reservation.
		// IncrementUsageReturning does INSERT ... ON CONFLICT DO UPDATE +1 RETURNING
		// in one SQL statement — no TOCTOU race, no separate read.
		if m.store != nil {
			field := op + "_count"
			newCount, err := m.store.IncrementUsageReturning(userID, field)
			if err != nil {
				// Fail closed: Postgres error means we can't enforce quota
				m.logger.Warn("meter: postgres reservation failed, blocking request",
					"error", err, "user_id", userID, "op", op)
				token.MarkRolledBack()
				return token, false, 0
			}
			if newCount > limit {
				// Over limit — rollback the increment
				m.store.DecrementUsage(userID, field)
				token.MarkRolledBack()
				return token, false, int64(newCount - 1)
			}
			// Allowed — token commit is a no-op since we already incremented
			return token, true, int64(newCount)
		}
		// No store either — fail closed
		token.MarkRolledBack()
		return token, false, 0
	}

	key := gateKey(userID, period, op)

	// Back-fill from Postgres if this key doesn't exist yet (Redis restart recovery).
	m.backfillIfMissing(ctx, userID, period, op, key)

	count, err := m.redis.Incr(ctx, key).Result()
	if err != nil {
		m.logger.Warn("meter: redis INCR failed, allowing request",
			"error", err, "user_id", userID, "op", op)
		return token, true, 0
	}
	// Set TTL on first write
	m.redis.ExpireNX(ctx, key, keyTTL)

	if count > int64(limit) {
		// Over limit — release the reservation
		m.redis.Decr(ctx, key)
		token.MarkRolledBack()
		return token, false, count - 1
	}

	return token, true, count
}

// backfillIfMissing seeds a Redis counter from Postgres if the key doesn't
// exist. This handles Redis restarts, key expiry, or manual deletion.
func (m *Meter) backfillIfMissing(ctx context.Context, userID, period, op, gateKeyStr string) {
	exists, err := m.redis.Exists(ctx, gateKeyStr).Result()
	if err != nil || exists > 0 {
		return // key exists or Redis error — don't backfill
	}

	// Key missing — read Postgres usage for this period
	if m.store == nil {
		return
	}
	usage, err := m.store.GetCurrentUsage(userID)
	if err != nil || usage == nil {
		return // no usage row yet — counter starts at 0 naturally
	}

	var baseline int
	switch op {
	case "push":
		baseline = usage.PushCount
	case "recall":
		baseline = usage.RecallCount
	case "ask":
		baseline = usage.AskCount
	}

	if baseline <= 0 {
		return // nothing to backfill
	}

	// Set both gate and committed to the Postgres baseline.
	// Use SetNX to avoid overwriting if a concurrent request already set it.
	committedKeyStr := committedKey(userID, period, op)
	m.redis.SetArgs(ctx, gateKeyStr, baseline, redis.SetArgs{Mode: "NX", TTL: keyTTL})
	m.redis.SetArgs(ctx, committedKeyStr, baseline, redis.SetArgs{Mode: "NX", TTL: keyTTL})
	m.logger.Info("meter: backfilled counter from postgres",
		"user_id", userID, "op", op, "baseline", baseline)
}

// --- Memory count (handler-managed, not middleware) ---

// ReserveMemoryCount atomically increments the memory gate counter and
// checks against the limit. Called by MemoriesHandler.Create only on the
// confirmed new-memory path (not dedup/update).
//
// Returns (allowed, currentCount). If not allowed, the gate counter has
// already been decremented (reservation released).
//
// Counter recovery: if the memory_count key is missing, back-fills from
// Postgres COUNT(*) on the memories table.
func (m *Meter) ReserveMemoryCount(ctx context.Context, ownerID string, limit int) (bool, int) {
	if model.IsUnlimited(limit) {
		return true, 0
	}
	if m.redis == nil {
		return true, 0
	}

	key := memoryGateKey(ownerID)

	// Back-fill from Postgres if key is missing
	m.backfillMemoryCountIfMissing(ctx, ownerID, key)

	count, err := m.redis.Incr(ctx, key).Result()
	if err != nil {
		m.logger.Warn("meter: redis memory count INCR failed",
			"error", err, "owner_id", ownerID)
		return true, 0
	}

	if count > int64(limit) {
		m.redis.Decr(ctx, key)
		return false, int(count - 1)
	}
	return true, int(count)
}

// backfillMemoryCountIfMissing seeds the memory_count Redis keys from
// a Postgres COUNT query if the key doesn't exist.
func (m *Meter) backfillMemoryCountIfMissing(ctx context.Context, ownerID, gateKeyStr string) {
	exists, err := m.redis.Exists(ctx, gateKeyStr).Result()
	if err != nil || exists > 0 {
		return
	}
	if m.store == nil {
		return
	}
	// Excluding seeds (plan 23 §5.5) — onboarding seed copies are
	// system-authored, not user-pushed content. They shouldn't count
	// toward plan-tier quota or activation triggers.
	count, err := m.store.CountMemoriesByOwnerExcludingSeeds(ctx, ownerID)
	if err != nil || count <= 0 {
		return
	}
	committedKeyStr := memoryCommittedKey(ownerID)
	m.redis.SetArgs(ctx, gateKeyStr, count, redis.SetArgs{Mode: "NX"}) // no TTL for memory count
	m.redis.SetArgs(ctx, committedKeyStr, count, redis.SetArgs{Mode: "NX"})
	m.logger.Info("meter: backfilled memory count from postgres",
		"owner_id", ownerID, "count", count)
}

// CommitMemoryCount increments the committed memory counter.
// Called after the DB insert succeeds.
func (m *Meter) CommitMemoryCount(ctx context.Context, ownerID string) {
	if m.redis == nil {
		return
	}
	key := memoryCommittedKey(ownerID)
	m.redis.Incr(ctx, key)
}

// RollbackMemoryCount decrements the gate memory counter when a push
// that passed ReserveMemoryCount ultimately fails before DB insert.
// Uses the clamping Lua script to prevent negative values (same as
// AdjustMemoryCount) in case the key disappeared between reserve and rollback.
func (m *Meter) RollbackMemoryCount(ctx context.Context, ownerID string) {
	if m.redis == nil {
		return
	}
	key := memoryGateKey(ownerID)
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{key}, -1).Err(); err != nil {
		m.logger.Warn("meter: rollback memory count failed", "error", err, "owner_id", ownerID)
	}
}

// adjustMemoryCountScript atomically adjusts a counter by delta, clamping at 0.
// KEYS[1] = counter key, ARGV[1] = delta (negative for decrements)
var adjustMemoryCountScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local delta = tonumber(ARGV[1])
local new_val = current + delta
if new_val < 0 then new_val = 0 end
redis.call('SET', KEYS[1], new_val)
return new_val
`)

// AdjustMemoryCount adjusts both gate and committed memory counters by delta,
// clamped at zero to prevent negative values (which would grant extra allowance).
// Used by delete paths. delta is typically negative.
// Owner-aware: uses the memory's owner, not the acting user.
func (m *Meter) AdjustMemoryCount(ctx context.Context, ownerID string, delta int) {
	if m.redis == nil || delta == 0 {
		return
	}
	gateK := memoryGateKey(ownerID)
	committedK := memoryCommittedKey(ownerID)
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{gateK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust memory gate count failed", "error", err, "owner_id", ownerID)
	}
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{committedK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust memory committed count failed", "error", err, "owner_id", ownerID)
	}
}

// --- Hub memory count (hub-scoped cap) ---
//
// Mirrors ReserveMemoryCount / CommitMemoryCount / RollbackMemoryCount /
// AdjustMemoryCount keyed by hub_id instead of owner_id. Called on every
// push destined for a team hub, in addition to the owner-scoped check.
// The hub counter represents aggregate memory count in a single hub
// regardless of which member pushed each memory; the owner counter is
// still enforced separately to keep any one user from monopolizing a
// hub's capacity.
//
// Personal hubs have no hub cap (no subscription → no plan ceiling) —
// callers should skip the hub path entirely when hubID is empty or
// when the hub is personal.

// ReserveHubMemoryCount atomically increments the hub memory gate
// counter and checks against the hub plan's memory_limit. Returns
// (allowed, currentCount). If not allowed, the counter is decremented.
func (m *Meter) ReserveHubMemoryCount(ctx context.Context, hubID string, limit int) (bool, int) {
	if hubID == "" || model.IsUnlimited(limit) {
		return true, 0
	}
	if m.redis == nil {
		return true, 0
	}

	key := hubMemoryGateKey(hubID)
	m.backfillHubMemoryCountIfMissing(ctx, hubID, key)

	count, err := m.redis.Incr(ctx, key).Result()
	if err != nil {
		m.logger.Warn("meter: redis hub memory count INCR failed",
			"error", err, "hub_id", hubID)
		return true, 0
	}

	if count > int64(limit) {
		m.redis.Decr(ctx, key)
		return false, int(count - 1)
	}
	return true, int(count)
}

func (m *Meter) backfillHubMemoryCountIfMissing(ctx context.Context, hubID, gateKeyStr string) {
	exists, err := m.redis.Exists(ctx, gateKeyStr).Result()
	if err != nil || exists > 0 {
		return
	}
	if m.store == nil {
		return
	}
	count, err := m.store.CountMemoriesByHub(ctx, hubID)
	if err != nil || count <= 0 {
		return
	}
	committedKeyStr := hubMemoryCommittedKey(hubID)
	m.redis.SetArgs(ctx, gateKeyStr, count, redis.SetArgs{Mode: "NX"})
	m.redis.SetArgs(ctx, committedKeyStr, count, redis.SetArgs{Mode: "NX"})
	m.logger.Info("meter: backfilled hub memory count from postgres",
		"hub_id", hubID, "count", count)
}

// CommitHubMemoryCount increments the hub-scoped committed counter
// after the DB insert succeeds. Paired with ReserveHubMemoryCount.
func (m *Meter) CommitHubMemoryCount(ctx context.Context, hubID string) {
	if hubID == "" || m.redis == nil {
		return
	}
	m.redis.Incr(ctx, hubMemoryCommittedKey(hubID))
}

// RollbackHubMemoryCount decrements the hub gate counter when a push
// that passed ReserveHubMemoryCount ultimately fails before DB insert.
func (m *Meter) RollbackHubMemoryCount(ctx context.Context, hubID string) {
	if hubID == "" || m.redis == nil {
		return
	}
	key := hubMemoryGateKey(hubID)
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{key}, -1).Err(); err != nil {
		m.logger.Warn("meter: rollback hub memory count failed",
			"error", err, "hub_id", hubID)
	}
}

// AdjustHubMemoryCount adjusts both hub gate and hub committed counters
// by delta, clamped at zero. Used by delete paths when a memory leaves
// a hub (delete, archive, hub reassignment).
func (m *Meter) AdjustHubMemoryCount(ctx context.Context, hubID string, delta int) {
	if hubID == "" || m.redis == nil || delta == 0 {
		return
	}
	gateK := hubMemoryGateKey(hubID)
	committedK := hubMemoryCommittedKey(hubID)
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{gateK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust hub memory gate count failed",
			"error", err, "hub_id", hubID)
	}
	if err := adjustMemoryCountScript.Run(ctx, m.redis, []string{committedK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust hub memory committed count failed",
			"error", err, "hub_id", hubID)
	}
}

// --- Storage bytes (handler-managed, lifetime-cumulative) ---
//
// Mirrors the memory-count pattern exactly: two Redis counters (gate +
// committed), back-fill from Postgres on cache miss. The only meaningful
// difference is the unit (int64 bytes instead of int count) and what the
// authoritative Postgres source is (SumStorageBytesByOwner instead of
// CountMemoriesByOwner).
//
// Gate  = in-flight reservations + committed. Read by ReserveStorageBytes
//         to decide whether the caller fits under the plan cap.
// Committed = bytes that actually landed in persistent storage. Used for
//             display and for billing reconciliation.
//
// If Redis is nil the meter is in degraded mode and storage quota is not
// enforced — consistent with every other Reserve* method. Production
// deploys with the critical-Redis instance wired in; dev without Redis
// trades enforcement for simpler local setup.

// ReserveStorageBytes atomically increments the storage_bytes gate
// counter by `bytes` and checks against `limit`. Returns (allowed,
// currentTotal). If the reservation pushes the total over the limit,
// the increment is rolled back and `allowed` is false.
//
// `bytes` must be positive (the claimed size of a single upload);
// `limit` follows the plan convention (-1 = unlimited).
//
// Counter recovery: if the gate key is missing (Redis restart, TTL
// expiry, first-time user), SumStorageBytesByOwner back-fills both
// keys from the authoritative Postgres sum before proceeding so users
// don't get fresh storage budget after a Redis outage.
func (m *Meter) ReserveStorageBytes(ctx context.Context, ownerID string, bytes int64, limit int64) (bool, int64) {
	if bytes <= 0 {
		// Zero/negative bytes = nothing to reserve; caller shouldn't hit
		// the quota path, but handle defensively.
		return true, 0
	}
	if limit < 0 {
		return true, 0
	}
	if m.redis == nil {
		// Degraded mode: same contract as ReserveMemoryCount — allow
		// through so dev without critical Redis still functions.
		return true, 0
	}

	key := storageBytesGateKey(ownerID)
	m.backfillStorageBytesIfMissing(ctx, ownerID, key)

	total, err := m.redis.IncrBy(ctx, key, bytes).Result()
	if err != nil {
		m.logger.Warn("meter: redis storage bytes INCR failed, allowing request",
			"error", err, "owner_id", ownerID)
		return true, 0
	}
	if total > limit {
		// Over limit — release the reservation
		m.redis.DecrBy(ctx, key, bytes)
		return false, total - bytes
	}
	return true, total
}

// backfillStorageBytesIfMissing seeds the storage_bytes Redis keys from
// the authoritative Postgres sum when the gate key doesn't exist. No-op
// once the keys are populated; the sum is expensive enough that we do
// NOT refresh it periodically from the hot path.
func (m *Meter) backfillStorageBytesIfMissing(ctx context.Context, ownerID, gateKeyStr string) {
	exists, err := m.redis.Exists(ctx, gateKeyStr).Result()
	if err != nil || exists > 0 {
		return
	}
	if m.store == nil {
		return
	}
	total, err := m.store.SumStorageBytesByOwner(ctx, ownerID)
	if err != nil || total <= 0 {
		return
	}
	committedKeyStr := storageBytesCommittedKey(ownerID)
	m.redis.SetArgs(ctx, gateKeyStr, total, redis.SetArgs{Mode: "NX"})
	m.redis.SetArgs(ctx, committedKeyStr, total, redis.SetArgs{Mode: "NX"})
	m.logger.Info("meter: backfilled storage bytes from postgres",
		"owner_id", ownerID, "bytes", total)
}

// CommitStorageBytes increments the committed counter by `bytes` after
// the DB insert of a memory_attachment row succeeds. Gate was already
// incremented by ReserveStorageBytes; commit only updates the "actually
// persisted" view.
func (m *Meter) CommitStorageBytes(ctx context.Context, ownerID string, bytes int64) {
	if m.redis == nil || bytes <= 0 {
		return
	}
	m.redis.IncrBy(ctx, storageBytesCommittedKey(ownerID), bytes)
}

// RollbackStorageBytes decrements the gate counter by `bytes` when a
// reservation is abandoned before the persist completes (DB insert
// error, caller cancelled, etc.). Clamped at 0 via the adjust script
// in case the key was evicted between reserve and rollback.
func (m *Meter) RollbackStorageBytes(ctx context.Context, ownerID string, bytes int64) {
	if m.redis == nil || bytes <= 0 {
		return
	}
	key := storageBytesGateKey(ownerID)
	if err := adjustStorageBytesScript.Run(ctx, m.redis, []string{key}, -bytes).Err(); err != nil {
		m.logger.Warn("meter: rollback storage bytes failed", "error", err, "owner_id", ownerID)
	}
}

// adjustStorageBytesScript atomically adjusts a byte counter by delta,
// clamping at 0. Identical shape to adjustMemoryCountScript — kept
// separate so a future storage-specific clamp policy (e.g. preserve the
// committed value even on over-decrement) doesn't back-contaminate
// memory count semantics.
var adjustStorageBytesScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local delta = tonumber(ARGV[1])
local new_val = current + delta
if new_val < 0 then new_val = 0 end
redis.call('SET', KEYS[1], new_val)
return new_val
`)

// AdjustStorageBytes adjusts both gate and committed storage byte
// counters by delta, clamped at zero. Used by delete paths (memory
// delete cascades attachments, session delete/purge, snapshot delete,
// tombstone purge). delta is typically negative; passing 0 is a no-op.
func (m *Meter) AdjustStorageBytes(ctx context.Context, ownerID string, delta int64) {
	if m.redis == nil || delta == 0 {
		return
	}
	gateK := storageBytesGateKey(ownerID)
	committedK := storageBytesCommittedKey(ownerID)
	if err := adjustStorageBytesScript.Run(ctx, m.redis, []string{gateK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust storage gate bytes failed", "error", err, "owner_id", ownerID)
	}
	if err := adjustStorageBytesScript.Run(ctx, m.redis, []string{committedK}, delta).Err(); err != nil {
		m.logger.Warn("meter: adjust storage committed bytes failed", "error", err, "owner_id", ownerID)
	}
}

// GetCommittedStorageBytes returns the persisted storage-bytes counter
// for display purposes. Falls back to 0 if Redis is unreachable.
func (m *Meter) GetCommittedStorageBytes(ctx context.Context, ownerID string) int64 {
	if m.redis == nil {
		return 0
	}
	val, err := m.redis.Get(ctx, storageBytesCommittedKey(ownerID)).Int64()
	if err != nil {
		return 0
	}
	return val
}

// InvalidateStorageBytes deletes both Redis counters for this owner so
// the next ReserveStorageBytes re-backfills from the authoritative
// Postgres SUM. Called from background sweeps (tombstone/snapshot
// purges) that free bytes without a per-row hook: invalidation is the
// cheap alternative to threading the per-row size through every purge
// callsite. The backfill runs once on next Reserve and then the counter
// is accurate again until the next sweep.
func (m *Meter) InvalidateStorageBytes(ctx context.Context, ownerID string) {
	if m.redis == nil {
		return
	}
	m.redis.Del(ctx, storageBytesGateKey(ownerID), storageBytesCommittedKey(ownerID))
}

// --- Usage reading ---

// GetCommittedUsage reads the committed counters for a user (for display/sync).
func (m *Meter) GetCommittedUsage(ctx context.Context, userID string) (push, recall, ask int) {
	if m.redis == nil {
		return 0, 0, 0
	}
	period := currentPeriod()
	pushStr, _ := m.redis.Get(ctx, committedKey(userID, period, "push")).Result()
	recallStr, _ := m.redis.Get(ctx, committedKey(userID, period, "recall")).Result()
	askStr, _ := m.redis.Get(ctx, committedKey(userID, period, "ask")).Result()
	fmt.Sscanf(pushStr, "%d", &push)
	fmt.Sscanf(recallStr, "%d", &recall)
	fmt.Sscanf(askStr, "%d", &ask)
	return
}

// --- Redis counter reset (for billing service) ---

// ResetCounters deletes gate + committed counter keys for a user's current
// billing period. Called by billing.Service.ResetUsageCounters.
//
// Only deletes monthly operation counters (push, recall, ask). Does NOT
// touch memory_count keys — memory count is a standing inventory limit,
// not a monthly quota. Deleting it would let users exceed their memory
// limit until the counter is rebuilt.
func (m *Meter) ResetCounters(ctx context.Context, userID string) error {
	if m.redis == nil {
		return nil
	}
	period := currentPeriod()
	keys := []string{
		gateKey(userID, period, "push"),
		gateKey(userID, period, "recall"),
		gateKey(userID, period, "ask"),
		committedKey(userID, period, "push"),
		committedKey(userID, period, "recall"),
		committedKey(userID, period, "ask"),
	}
	return m.redis.Del(ctx, keys...).Err()
}

// --- Usage event logging ---

// LogEvent writes a usage event to Postgres asynchronously.
// Non-blocking: errors are logged but don't affect the request.
func (m *Meter) LogEvent(userID, op, hubID, source, agentName string, metadata map[string]any) {
	go func() {
		event := &model.UsageEvent{
			UserID:    userID,
			HubID:     hubID,
			Operation: op,
			Source:    source,
			AgentName: agentName,
			Metadata:  metadata,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.store.InsertUsageEvent(ctx, event); err != nil {
			m.logger.Warn("meter: failed to log usage event",
				"error", err, "user_id", userID, "op", op)
		}
	}()
}

// --- Operation classification ---

// ClassifyOperation maps an HTTP method+path to a metered operation name.
// Returns "" for non-metered operations.
func ClassifyOperation(method, path string) string {
	if method != "POST" {
		return ""
	}
	// Normalize: strip trailing slashes, query params
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	path = strings.TrimRight(path, "/")

	switch path {
	case "/v1/memories":
		return "push"
	case "/v1/recall":
		return "recall"
	case "/v1/ask":
		return "ask"
	default:
		return ""
	}
}
