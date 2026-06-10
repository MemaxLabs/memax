package meterctx

import (
	"context"
	"sync"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ─── Token state machine ────────────────────────────────────────

func TestTokenCommitFiresCallbackOnce(t *testing.T) {
	t.Parallel()
	var calls int
	tk := NewToken(func() { calls++ }, nil)

	tk.Commit()
	tk.Commit() // idempotent — callback must NOT fire twice

	if calls != 1 {
		t.Errorf("onCommit fired %d times, want 1", calls)
	}
	if !tk.IsCommitted() {
		t.Error("IsCommitted should be true after Commit")
	}
}

func TestTokenRollbackFiresCallbackOnce(t *testing.T) {
	t.Parallel()
	var calls int
	tk := NewToken(nil, func() { calls++ })

	tk.Rollback()
	tk.Rollback()

	if calls != 1 {
		t.Errorf("onRollback fired %d times, want 1", calls)
	}
	if tk.IsCommitted() {
		t.Error("IsCommitted should be false after Rollback")
	}
}

func TestTokenCommitBlocksRollback(t *testing.T) {
	t.Parallel()
	var rollbackCalls int
	tk := NewToken(nil, func() { rollbackCalls++ })

	tk.Commit()
	tk.Rollback() // must NOT fire — token is terminal after Commit

	if rollbackCalls != 0 {
		t.Errorf("rollback after commit should no-op, onRollback fired %d times", rollbackCalls)
	}
}

func TestTokenRollbackBlocksCommit(t *testing.T) {
	t.Parallel()
	var commitCalls int
	tk := NewToken(func() { commitCalls++ }, nil)

	tk.Rollback()
	tk.Commit()

	if commitCalls != 0 {
		t.Errorf("commit after rollback should no-op, onCommit fired %d times", commitCalls)
	}
}

func TestTokenMarkRolledBackSkipsCallback(t *testing.T) {
	t.Parallel()
	var rollbackCalls int
	tk := NewToken(nil, func() { rollbackCalls++ })

	// MarkRolledBack is used when the meter already decremented
	// in Reserve (over-quota path). Calling the rollback callback
	// again would double-decrement.
	tk.MarkRolledBack()
	tk.Rollback() // should be no-op because MarkRolledBack already moved state

	if rollbackCalls != 0 {
		t.Errorf("MarkRolledBack must not call onRollback, got %d calls", rollbackCalls)
	}
}

func TestTokenNilReceiverTolerant(t *testing.T) {
	t.Parallel()
	var tk *Token
	// Every method must no-op on nil — handlers often look up via
	// TokenFromContext which can return nil when the request came
	// through without the metering middleware.
	tk.Commit()
	tk.Rollback()
	tk.MarkRolledBack()
	if tk.IsCommitted() {
		t.Error("IsCommitted on nil receiver should be false")
	}
}

func TestTokenConcurrencySafeCommit(t *testing.T) {
	t.Parallel()
	var calls int
	var mu sync.Mutex
	tk := NewToken(func() { mu.Lock(); calls++; mu.Unlock() }, nil)

	// Hammer Commit from N goroutines — the idempotency guard
	// must ensure onCommit fires exactly once under contention.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk.Commit()
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Errorf("concurrent Commit fired callback %d times, want 1", calls)
	}
}

// ─── Context helpers ────────────────────────────────────────────

func TestWithTokenRoundtrip(t *testing.T) {
	t.Parallel()
	tk := NewToken(nil, nil)
	ctx := WithToken(context.Background(), tk)
	if got := TokenFromContext(ctx); got != tk {
		t.Error("TokenFromContext did not return the injected token")
	}
}

func TestTokenFromContextMissing(t *testing.T) {
	t.Parallel()
	// Bare context — no token injected.
	if tk := TokenFromContext(context.Background()); tk != nil {
		t.Errorf("bare context should produce nil token, got %T", tk)
	}
}

func TestCommitFromContextNoopOnMissing(t *testing.T) {
	t.Parallel()
	// Must not panic when the context has no token.
	CommitFromContext(context.Background())
	RollbackFromContext(context.Background())
}

func TestCommitFromContextFiresOnPresent(t *testing.T) {
	t.Parallel()
	var committed bool
	tk := NewToken(func() { committed = true }, nil)
	ctx := WithToken(context.Background(), tk)

	CommitFromContext(ctx)
	if !committed {
		t.Error("CommitFromContext did not invoke the token's commit")
	}
}

func TestWithUserLimitsRoundtrip(t *testing.T) {
	t.Parallel()
	limits := &model.UserLimits{PlanID: "personal_free", AskLimit: 10}
	ctx := WithUserLimits(context.Background(), limits)
	if got := UserLimitsFromContext(ctx); got != limits {
		t.Error("UserLimitsFromContext mismatch")
	}
}

func TestUserLimitsFromContextMissing(t *testing.T) {
	t.Parallel()
	if l := UserLimitsFromContext(context.Background()); l != nil {
		t.Errorf("bare context should produce nil, got %+v", l)
	}
}

// ─── UsageEventInfo ─────────────────────────────────────────────

func TestNewUsageEventInfo(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	if info == nil || info.Metadata == nil {
		t.Errorf("NewUsageEventInfo should initialize non-nil Metadata, got %+v", info)
	}
}

func TestUsageEventInfoSetAndSnapshot(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	info.Set("user_id", "u1")
	info.Set("op", "push")
	snap := info.Snapshot()
	if snap["user_id"] != "u1" || snap["op"] != "push" {
		t.Errorf("snapshot mismatch: %+v", snap)
	}
}

func TestUsageEventInfoSnapshotIsolatedFromMutations(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	info.Set("k", "v")
	snap := info.Snapshot()
	// Mutating the snapshot must NOT leak back into the info's
	// internal map — otherwise a downstream consumer modifying
	// their copy would poison the authoritative map.
	snap["k"] = "tampered"
	// Take a second snapshot and confirm the original key is
	// unchanged.
	if got := info.Snapshot()["k"]; got != "v" {
		t.Errorf("snapshot aliases the internal map: got %q, want v", got)
	}
}

func TestUsageEventInfoSnapshotOnEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	info := &UsageEventInfo{Metadata: map[string]any{}}
	if snap := info.Snapshot(); snap != nil {
		t.Errorf("empty metadata should snapshot to nil, got %+v", snap)
	}
}

func TestUsageEventInfoNilReceiverTolerant(t *testing.T) {
	t.Parallel()
	var info *UsageEventInfo
	info.Set("k", "v") // no panic
	if snap := info.Snapshot(); snap != nil {
		t.Errorf("nil receiver Snapshot should be nil, got %+v", snap)
	}
}

func TestUsageEventInfoSetInitializesMetadataLazy(t *testing.T) {
	t.Parallel()
	// Direct struct init skips NewUsageEventInfo's constructor,
	// so Metadata is nil. Set must tolerate that and initialize
	// on first write.
	info := &UsageEventInfo{}
	info.Set("lazy", "init")
	if info.Metadata["lazy"] != "init" {
		t.Errorf("lazy init failed: %+v", info.Metadata)
	}
}

func TestWithUsageEventInfoRoundtrip(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	ctx := WithUsageEventInfo(context.Background(), info)
	if got := UsageEventInfoFromContext(ctx); got != info {
		t.Error("UsageEventInfoFromContext mismatch")
	}
}

func TestUsageEventInfoFromContextMissing(t *testing.T) {
	t.Parallel()
	if info := UsageEventInfoFromContext(context.Background()); info != nil {
		t.Errorf("bare context should be nil, got %+v", info)
	}
}

func TestSetUsageEventFieldNoopOnMissingContext(t *testing.T) {
	t.Parallel()
	// Handlers call this unconditionally; must be tolerant of
	// a context without the info injected.
	SetUsageEventField(context.Background(), "k", "v")
}

func TestSetUsageEventFieldWritesViaContext(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	ctx := WithUsageEventInfo(context.Background(), info)
	SetUsageEventField(ctx, "query", "hello")
	if info.Metadata["query"] != "hello" {
		t.Errorf("SetUsageEventField did not land via context: %+v", info.Metadata)
	}
}

func TestUsageEventInfoConcurrencySafeSet(t *testing.T) {
	t.Parallel()
	info := NewUsageEventInfo()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			info.Set("k", i)
		}(i)
	}
	wg.Wait()
	// No race — the value is whatever lands last, but it must
	// exist and not panic the test under `-race`.
	if _, ok := info.Metadata["k"]; !ok {
		t.Error("concurrent writes didn't land a value")
	}
}
