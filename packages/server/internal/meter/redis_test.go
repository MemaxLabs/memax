package meter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// setupWithRedis stands up a miniredis + Meter pair. Every test
// that exercises a Redis path goes through here so teardown is
// consistent and the store/registry shapes stay aligned.
func setupWithRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client, *Meter, *redisTestStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ms := &redisTestStore{}
	registry := plans.NewRegistry(ms)
	return mr, client, New(client, ms, registry), ms
}

// ─── Key helper shape regression guards ─────────────────────────

func TestKeyHelpersShape(t *testing.T) {
	t.Parallel()

	// Key shapes end up in Redis; a rename silently breaks existing
	// deployments that still hold counters under the old names.
	// This test pins the prefix + separator structure so anyone
	// changing it has to update here and think about migration.
	if got := hubMemoryGateKey("hub-1"); got != "memax:usage:hub:hub-1:memory_count" {
		t.Errorf("hubMemoryGateKey = %q", got)
	}
	if got := hubMemoryCommittedKey("hub-1"); got != "memax:usage:hub:hub-1:memory_count:committed" {
		t.Errorf("hubMemoryCommittedKey = %q", got)
	}
	if got := storageBytesGateKey("u1"); got != "memax:usage:u1:storage_bytes" {
		t.Errorf("storageBytesGateKey = %q", got)
	}
	if got := storageBytesCommittedKey("u1"); got != "memax:usage:u1:storage_bytes:committed" {
		t.Errorf("storageBytesCommittedKey = %q", got)
	}
}

// ─── SetResolver ────────────────────────────────────────────────

func TestSetResolverTolerantOfNilReceiver(t *testing.T) {
	t.Parallel()

	// SetResolver is called during startup wiring and must be
	// tolerant of a nil meter (degraded-mode paths sometimes
	// pass nil).
	var m *Meter
	m.SetResolver(nil) // must not panic
}

// ─── Memory count ───────────────────────────────────────────────

func TestReserveMemoryCountUnlimited(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	ok, count := m.ReserveMemoryCount(context.Background(), "u1", -1)
	if !ok || count != 0 {
		t.Errorf("unlimited limit should allow and return 0, got (%v, %d)", ok, count)
	}
}

func TestReserveMemoryCountNilRedisAllows(t *testing.T) {
	t.Parallel()

	// Degraded mode: no Redis, no enforcement. Must allow through.
	ms := &redisTestStore{}
	m := New(nil, ms, plans.NewRegistry(ms))
	ok, count := m.ReserveMemoryCount(context.Background(), "u1", 100)
	if !ok || count != 0 {
		t.Errorf("nil redis should allow with count=0, got (%v, %d)", ok, count)
	}
}

func TestReserveMemoryCountRejectsOverLimit(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	// Limit 2: first two succeed, third is denied.
	ok1, c1 := m.ReserveMemoryCount(context.Background(), "u1", 2)
	ok2, c2 := m.ReserveMemoryCount(context.Background(), "u1", 2)
	ok3, c3 := m.ReserveMemoryCount(context.Background(), "u1", 2)
	if !ok1 || c1 != 1 {
		t.Errorf("reserve #1 should allow at count=1, got (%v, %d)", ok1, c1)
	}
	if !ok2 || c2 != 2 {
		t.Errorf("reserve #2 should allow at count=2 (limit hit), got (%v, %d)", ok2, c2)
	}
	if ok3 {
		t.Errorf("reserve #3 should be denied, got (%v, %d)", ok3, c3)
	}
	// Denied reservation must have been rolled back — next call
	// should see count=2, not 3.
	ok4, c4 := m.ReserveMemoryCount(context.Background(), "u1", 3)
	if !ok4 || c4 != 3 {
		t.Errorf("rollback failed: next call got (%v, %d), want (true, 3)", ok4, c4)
	}
}

func TestReserveMemoryCountBackfillsFromStore(t *testing.T) {
	t.Parallel()

	// Key doesn't exist yet; store reports an existing memory
	// count. The Reserve call should backfill so the limit check
	// accounts for memories that already exist in Postgres.
	_, _, m, ms := setupWithRedis(t)
	ms.memoryCount = 95
	ok, count := m.ReserveMemoryCount(context.Background(), "u1", 100)
	if !ok {
		t.Error("should allow — limit 100, existing 95, reserve hits 96")
	}
	if count != 96 {
		t.Errorf("count = %d, want 96 (95 existing + 1 new)", count)
	}
}

func TestCommitMemoryCountIncrementsCommitted(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.CommitMemoryCount(ctx, "u1")
	m.CommitMemoryCount(ctx, "u1")
	m.CommitMemoryCount(ctx, "u1")
	got, _ := client.Get(ctx, memoryCommittedKey("u1")).Int()
	if got != 3 {
		t.Errorf("committed count = %d, want 3", got)
	}
}

func TestRollbackMemoryCountClampsAtZero(t *testing.T) {
	t.Parallel()

	// Rolling back on an empty gate must not produce a negative
	// counter (which would silently grant extra allowance on
	// the next Reserve).
	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.RollbackMemoryCount(ctx, "u1")
	got, _ := client.Get(ctx, memoryGateKey("u1")).Int()
	if got < 0 {
		t.Errorf("rollback drove counter negative: %d", got)
	}
}

// ─── Hub memory count ───────────────────────────────────────────

func TestReserveHubMemoryCountEmptyHubAllows(t *testing.T) {
	t.Parallel()

	// Empty hub id = personal context; hub memory cap not in play.
	_, _, m, _ := setupWithRedis(t)
	ok, count := m.ReserveHubMemoryCount(context.Background(), "", 100)
	if !ok || count != 0 {
		t.Errorf("empty hubID should allow with 0, got (%v, %d)", ok, count)
	}
}

func TestReserveHubMemoryCountLimitEnforced(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	ctx := context.Background()
	ok1, _ := m.ReserveHubMemoryCount(ctx, "hub-1", 1)
	ok2, _ := m.ReserveHubMemoryCount(ctx, "hub-1", 1)
	if !ok1 || ok2 {
		t.Errorf("limit 1 should allow exactly one; got ok1=%v ok2=%v", ok1, ok2)
	}
}

func TestCommitAndRollbackHubMemoryCountSkipEmptyHubID(t *testing.T) {
	t.Parallel()

	// Both MUST silently no-op on empty hubID — they're called on
	// every push regardless of hub type, and team-only paths would
	// churn the global namespace if they accidentally ran.
	_, _, m, _ := setupWithRedis(t)
	m.CommitHubMemoryCount(context.Background(), "")
	m.RollbackHubMemoryCount(context.Background(), "")
	m.AdjustHubMemoryCount(context.Background(), "", -1)
}

func TestAdjustHubMemoryCountClampsAtZero(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()

	// Seed a small counter, then try to deduct more than exists.
	// Must clamp at zero, not go negative.
	m.ReserveHubMemoryCount(ctx, "hub-1", 100) // count = 1
	m.AdjustHubMemoryCount(ctx, "hub-1", -10)  // try to go to -9
	got, _ := client.Get(ctx, hubMemoryGateKey("hub-1")).Int()
	if got != 0 {
		t.Errorf("clamp failed: got %d, want 0", got)
	}
}

func TestBackfillHubMemoryCountSeedsFromStore(t *testing.T) {
	t.Parallel()

	// Empty Redis + store reports 77 memories → next reserve
	// should land the counter at 78 (backfill + increment).
	_, _, m, ms := setupWithRedis(t)
	ms.hubMemoryCount = 77
	ok, count := m.ReserveHubMemoryCount(context.Background(), "hub-1", 100)
	if !ok {
		t.Error("should allow (77+1 < 100)")
	}
	if count != 78 {
		t.Errorf("count = %d, want 78 (backfilled 77 + 1)", count)
	}
}

// ─── Storage bytes ──────────────────────────────────────────────

func TestReserveStorageBytesZeroBytes(t *testing.T) {
	t.Parallel()

	// Zero-byte reservation — defensive return true without
	// touching Redis (callers that ignore this contract would
	// churn the counter for no reason).
	_, _, m, _ := setupWithRedis(t)
	ok, tot := m.ReserveStorageBytes(context.Background(), "u1", 0, 1000)
	if !ok || tot != 0 {
		t.Errorf("zero bytes: got (%v, %d), want (true, 0)", ok, tot)
	}
	// Negative bytes defensively pass too — spec says "caller
	// shouldn't hit this path" but we tolerate.
	ok2, tot2 := m.ReserveStorageBytes(context.Background(), "u1", -5, 1000)
	if !ok2 || tot2 != 0 {
		t.Errorf("negative bytes: got (%v, %d), want (true, 0)", ok2, tot2)
	}
}

func TestReserveStorageBytesUnlimited(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	ok, tot := m.ReserveStorageBytes(context.Background(), "u1", 100, -1)
	if !ok || tot != 0 {
		t.Errorf("unlimited: got (%v, %d), want (true, 0)", ok, tot)
	}
}

func TestReserveStorageBytesEnforcesLimit(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	ctx := context.Background()
	ok, tot := m.ReserveStorageBytes(ctx, "u1", 500, 1000)
	if !ok || tot != 500 {
		t.Errorf("first reserve: got (%v, %d), want (true, 500)", ok, tot)
	}
	// Second 500 → total 1000 (at limit, still allowed — check is
	// total > limit, not >=).
	ok2, tot2 := m.ReserveStorageBytes(ctx, "u1", 500, 1000)
	if !ok2 || tot2 != 1000 {
		t.Errorf("second reserve: got (%v, %d), want (true, 1000)", ok2, tot2)
	}
	// Third 1-byte → 1001, over → denied + rolled back.
	ok3, tot3 := m.ReserveStorageBytes(ctx, "u1", 1, 1000)
	if ok3 {
		t.Error("over limit should deny")
	}
	if tot3 != 1000 {
		t.Errorf("denied reserve should report the pre-increment total, got %d", tot3)
	}
}

func TestCommitStorageBytesIncrements(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.CommitStorageBytes(ctx, "u1", 250)
	m.CommitStorageBytes(ctx, "u1", 750)
	got, _ := client.Get(ctx, storageBytesCommittedKey("u1")).Int64()
	if got != 1000 {
		t.Errorf("committed bytes = %d, want 1000", got)
	}
	// Zero / negative inputs no-op.
	m.CommitStorageBytes(ctx, "u1", 0)
	m.CommitStorageBytes(ctx, "u1", -5)
	after, _ := client.Get(ctx, storageBytesCommittedKey("u1")).Int64()
	if after != 1000 {
		t.Errorf("zero/negative commit should no-op, got %d", after)
	}
}

func TestRollbackStorageBytesClampsAtZero(t *testing.T) {
	t.Parallel()

	// Rolling back more than was reserved should floor at 0, not
	// go negative and grant extra headroom later.
	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.ReserveStorageBytes(ctx, "u1", 100, 10_000)
	m.RollbackStorageBytes(ctx, "u1", 500)
	got, _ := client.Get(ctx, storageBytesGateKey("u1")).Int64()
	if got != 0 {
		t.Errorf("rollback should clamp at 0, got %d", got)
	}
	// Zero/negative bytes no-op.
	m.RollbackStorageBytes(ctx, "u1", 0)
	m.RollbackStorageBytes(ctx, "u1", -10)
}

func TestAdjustStorageBytesClampsAtZero(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.ReserveStorageBytes(ctx, "u1", 100, 10_000)
	m.CommitStorageBytes(ctx, "u1", 100)
	// Now adjust -500 → both counters floor at 0.
	m.AdjustStorageBytes(ctx, "u1", -500)
	gate, _ := client.Get(ctx, storageBytesGateKey("u1")).Int64()
	commit, _ := client.Get(ctx, storageBytesCommittedKey("u1")).Int64()
	if gate != 0 || commit != 0 {
		t.Errorf("adjust -500: gate=%d commit=%d, want 0/0", gate, commit)
	}
	// delta=0 no-op.
	m.AdjustStorageBytes(ctx, "u1", 0)
}

func TestGetCommittedStorageBytesReturnsZeroWhenAbsent(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	got := m.GetCommittedStorageBytes(context.Background(), "u1")
	if got != 0 {
		t.Errorf("absent key should return 0, got %d", got)
	}
}

func TestGetCommittedStorageBytesReadsCounter(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	m.CommitStorageBytes(context.Background(), "u1", 12345)
	if got := m.GetCommittedStorageBytes(context.Background(), "u1"); got != 12345 {
		t.Errorf("got %d, want 12345", got)
	}
}

func TestInvalidateStorageBytesResetsCounters(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	m.ReserveStorageBytes(ctx, "u1", 100, 10_000)
	m.CommitStorageBytes(ctx, "u1", 100)
	m.InvalidateStorageBytes(ctx, "u1")

	// Both keys should be gone now.
	exists, _ := client.Exists(ctx, storageBytesGateKey("u1"), storageBytesCommittedKey("u1")).Result()
	if exists != 0 {
		t.Errorf("invalidate left %d keys, want 0", exists)
	}
}

func TestBackfillStorageBytesSeedsFromStore(t *testing.T) {
	t.Parallel()

	// Redis empty + store reports 5000 bytes → next reserve
	// accounts for the baseline.
	_, _, m, ms := setupWithRedis(t)
	ms.storageBytes = 5000
	ok, total := m.ReserveStorageBytes(context.Background(), "u1", 100, 10_000)
	if !ok {
		t.Error("should allow (5000 + 100 < 10000)")
	}
	if total != 5100 {
		t.Errorf("total = %d, want 5100 (5000 baseline + 100)", total)
	}
}

// ─── Committed usage ────────────────────────────────────────────

func TestGetCommittedUsageReturnsZerosWhenAbsent(t *testing.T) {
	t.Parallel()

	_, _, m, _ := setupWithRedis(t)
	push, recall, ask := m.GetCommittedUsage(context.Background(), "u1")
	if push != 0 || recall != 0 || ask != 0 {
		t.Errorf("absent keys should be 0/0/0, got %d/%d/%d", push, recall, ask)
	}
}

func TestGetCommittedUsageReadsAllThreeCounters(t *testing.T) {
	t.Parallel()

	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	period := currentPeriod()
	client.Set(ctx, committedKey("u1", period, "push"), 5, 0)
	client.Set(ctx, committedKey("u1", period, "recall"), 17, 0)
	client.Set(ctx, committedKey("u1", period, "ask"), 42, 0)
	push, recall, ask := m.GetCommittedUsage(ctx, "u1")
	if push != 5 || recall != 17 || ask != 42 {
		t.Errorf("got %d/%d/%d, want 5/17/42", push, recall, ask)
	}
}

// ─── ResetCounters ──────────────────────────────────────────────

func TestResetCountersDeletesMonthlyOnly(t *testing.T) {
	t.Parallel()

	// Reset must delete push/recall/ask gate+committed keys for
	// the current period, but preserve memory_count keys (those
	// track a standing inventory limit, not a monthly quota).
	_, client, m, _ := setupWithRedis(t)
	ctx := context.Background()
	period := currentPeriod()
	client.Set(ctx, gateKey("u1", period, "push"), 10, 0)
	client.Set(ctx, committedKey("u1", period, "push"), 10, 0)
	client.Set(ctx, committedKey("u1", period, "recall"), 7, 0)
	client.Set(ctx, committedKey("u1", period, "ask"), 3, 0)
	client.Set(ctx, memoryGateKey("u1"), 50, 0)
	client.Set(ctx, memoryCommittedKey("u1"), 50, 0)

	if err := m.ResetCounters(ctx, "u1"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// Monthly counters gone.
	for _, k := range []string{
		gateKey("u1", period, "push"),
		committedKey("u1", period, "push"),
		committedKey("u1", period, "recall"),
		committedKey("u1", period, "ask"),
	} {
		if exists, _ := client.Exists(ctx, k).Result(); exists != 0 {
			t.Errorf("key %q should have been deleted", k)
		}
	}
	// Memory counters preserved.
	if got, _ := client.Get(ctx, memoryGateKey("u1")).Int(); got != 50 {
		t.Errorf("memory gate key should survive reset; got %d, want 50", got)
	}
}

func TestResetCountersNilRedisNoop(t *testing.T) {
	t.Parallel()

	m := New(nil, &redisTestStore{}, plans.NewRegistry(&redisTestStore{}))
	if err := m.ResetCounters(context.Background(), "u1"); err != nil {
		t.Errorf("nil-redis reset should be a no-op, got %v", err)
	}
}

// ─── LogEvent ───────────────────────────────────────────────────

func TestLogEventWritesAsynchronously(t *testing.T) {
	t.Parallel()

	ms := &redisTestStore{}
	m := New(nil, ms, plans.NewRegistry(ms))
	m.LogEvent("u1", "push", "h1", "api", "claude-code", map[string]any{"foo": "bar"})
	// LogEvent spawns a goroutine — give it time to land, then
	// verify the event shape hit the store.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		ms.mu.Lock()
		n := len(ms.usageEvents)
		ms.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.usageEvents) != 1 {
		t.Fatalf("got %d usage events, want 1", len(ms.usageEvents))
	}
	ev := ms.usageEvents[0]
	if ev.UserID != "u1" || ev.Operation != "push" || ev.HubID != "h1" || ev.Source != "api" || ev.AgentName != "claude-code" {
		t.Errorf("event fields wrong: %+v", ev)
	}
}

// ─── Mock store ─────────────────────────────────────────────────

// redisTestStore extends the semantics-test store with the extra
// methods the Redis-facing paths touch. Tests set individual
// counter fields and the store returns them verbatim from the
// relevant Store methods.
type redisTestStore struct {
	redisTestStoreBase

	pushCount      int
	recallCount    int
	askCount       int
	memoryCount    int
	hubMemoryCount int
	storageBytes   int64
}

func (m *redisTestStore) GetCurrentUsage(_ string) (*model.Usage, error) {
	if m.pushCount == 0 && m.recallCount == 0 && m.askCount == 0 {
		return nil, nil
	}
	return &model.Usage{
		PushCount:   m.pushCount,
		RecallCount: m.recallCount,
		AskCount:    m.askCount,
	}, nil
}

func (m *redisTestStore) CountMemoriesByOwner(_ context.Context, _ string) (int, error) {
	return m.memoryCount, nil
}

// Plan 23 §5.5: meter calls ExcludingSeeds for usage/quota now. The test
// store treats both methods identically (no seed/non-seed distinction
// in the mock corpus), so reservations + backfill behave consistently.
func (m *redisTestStore) CountMemoriesByOwnerExcludingSeeds(_ context.Context, _ string) (int, error) {
	return m.memoryCount, nil
}

func (m *redisTestStore) CountMemoriesByHub(_ context.Context, _ string) (int, error) {
	return m.hubMemoryCount, nil
}

func (m *redisTestStore) SumStorageBytesByOwner(_ context.Context, _ string) (int64, error) {
	return m.storageBytes, nil
}

func (m *redisTestStore) InsertUsageEvent(_ context.Context, event *model.UsageEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usageEvents = append(m.usageEvents, event)
	return nil
}

func (m *redisTestStore) ListPlans(_ context.Context) ([]model.Plan, error) { return nil, nil }

// redisTestStoreBase carries the mutex + captured events so redisTestStore
// stays small. Embedded so both the mutex and slice fields are
// usable directly.
type redisTestStoreBase struct {
	mu          sync.Mutex
	usageEvents []*model.UsageEvent
	// Embed InMemoryStore so the rest of the Store interface is
	// satisfied with sensible no-op behavior for methods we don't
	// override here.
	*store.InMemoryStore
}
