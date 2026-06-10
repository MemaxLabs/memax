package meter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"POST", "/v1/memories", "push"},
		{"POST", "/v1/memories/", "push"},
		{"POST", "/v1/recall", "recall"},
		{"POST", "/v1/ask", "ask"},
		{"GET", "/v1/memories", ""},
		{"GET", "/v1/recall", ""},
		{"POST", "/v1/settings", ""},
		{"POST", "/v1/hubs", ""},
		{"POST", "/v1/memories?foo=bar", "push"},
		{"DELETE", "/v1/memories/abc", ""},
	}
	for _, tt := range tests {
		got := ClassifyOperation(tt.method, tt.path)
		if got != tt.want {
			t.Errorf("ClassifyOperation(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestDetectSourcePrefersTransportOverAgentHeuristic(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		userAgent string
		want      string
	}{
		{
			name: "mcp route stays mcp",
			path: "/mcp",
			want: "mcp",
		},
		{
			name:      "cli agent key stays cli",
			path:      "/v1/memories",
			userAgent: "memax-cli/1.0",
			want:      "cli",
		},
		{
			name:      "sdk agent key stays api",
			path:      "/v1/memories",
			userAgent: "memax-sdk/1.0",
			want:      "api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}
			if got := detectSource(req); got != tt.want {
				t.Fatalf("detectSource() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToken_CommitIdempotent(t *testing.T) {
	committed := 0
	token := meterctx.NewToken(func() { committed++ }, nil)
	token.Commit()
	if !token.IsCommitted() {
		t.Fatal("expected committed after Commit()")
	}
	token.Commit() // second commit should be no-op
	if committed != 1 {
		t.Fatalf("expected onCommit called once, got %d", committed)
	}
}

func TestToken_RollbackIdempotent(t *testing.T) {
	rolledBack := 0
	token := meterctx.NewToken(nil, func() { rolledBack++ })
	token.Rollback()
	token.Rollback() // second rollback should be no-op
	if token.IsCommitted() {
		t.Fatal("should not be committed after rollback")
	}
	if rolledBack != 1 {
		t.Fatalf("expected onRollback called once, got %d", rolledBack)
	}
}

func TestToken_CommitPreventsRollback(t *testing.T) {
	rolledBack := false
	token := meterctx.NewToken(func() {}, func() { rolledBack = true })
	token.Commit()
	token.Rollback()
	if !token.IsCommitted() {
		t.Fatal("commit should prevent subsequent rollback")
	}
	if rolledBack {
		t.Fatal("onRollback should not be called after commit")
	}
}

func TestToken_RollbackPreventsCommit(t *testing.T) {
	committed := false
	token := meterctx.NewToken(func() { committed = true }, func() {})
	token.Rollback()
	token.Commit()
	if token.IsCommitted() {
		t.Fatal("rollback should prevent subsequent commit")
	}
	if committed {
		t.Fatal("onCommit should not be called after rollback")
	}
}

func TestToken_NilSafe(t *testing.T) {
	var token *meterctx.Token
	// These should not panic
	token.Commit()
	token.Rollback()
	if token.IsCommitted() {
		t.Fatal("nil token should not be committed")
	}
}

func TestReserve_UnlimitedPlan(t *testing.T) {
	m := New(nil, nil, nil)
	token, allowed, _ := m.Reserve(context.Background(), "user-1", "push", -1)
	if !allowed {
		t.Fatal("unlimited plan should always allow")
	}
	if token == nil {
		t.Fatal("token should not be nil")
	}
}

func TestReserve_NilRedis_NilStore_FailsClosed(t *testing.T) {
	m := New(nil, nil, nil)
	token, allowed, _ := m.Reserve(context.Background(), "user-1", "push", 100)
	if allowed {
		t.Fatal("nil redis + nil store should fail closed (block request)")
	}
	if token == nil {
		t.Fatal("token should not be nil")
	}
}

func TestReserve_NilRedis_WithStore_AllowsUnderLimit(t *testing.T) {
	ms := &countingMockStore{counts: map[string]int{}}
	m := New(nil, ms, nil)
	_, allowed, count := m.Reserve(context.Background(), "user-1", "push", 10)
	if !allowed {
		t.Fatal("under-limit should allow")
	}
	if count != 1 {
		t.Errorf("expected count 1 after first reserve, got %d", count)
	}
	if ms.counts["push_count"] != 1 {
		t.Errorf("expected store push_count 1, got %d", ms.counts["push_count"])
	}
}

func TestReserve_NilRedis_AtLimit_Blocks(t *testing.T) {
	ms := &countingMockStore{counts: map[string]int{"push_count": 10}}
	m := New(nil, ms, nil)
	_, allowed, count := m.Reserve(context.Background(), "user-1", "push", 10)
	if allowed {
		t.Fatal("at-limit should block")
	}
	if count != 10 {
		t.Errorf("expected count 10 (pre-rollback), got %d", count)
	}
	// DecrementUsage should have been called to undo the increment
	if ms.decremented != 1 {
		t.Errorf("expected 1 decrement call, got %d", ms.decremented)
	}
}

func TestReserve_NilRedis_StoreError_FailsClosed(t *testing.T) {
	ms := &countingMockStore{counts: map[string]int{}, errOnIncrement: true}
	m := New(nil, ms, nil)
	_, allowed, _ := m.Reserve(context.Background(), "user-1", "push", 10)
	if allowed {
		t.Fatal("store error should fail closed")
	}
}

func TestReserve_NilRedis_Rollback_Decrements(t *testing.T) {
	ms := &countingMockStore{counts: map[string]int{}}
	m := New(nil, ms, nil)
	token, allowed, _ := m.Reserve(context.Background(), "user-1", "push", 10)
	if !allowed {
		t.Fatal("should allow under limit")
	}
	// Simulate handler failure — rollback
	token.Rollback()
	if ms.decremented != 1 {
		t.Errorf("rollback should decrement postgres, got %d decrements", ms.decremented)
	}
}

// countingMockStore tracks increment/decrement calls for Postgres fallback tests.
type countingMockStore struct {
	store.InMemoryStore
	counts         map[string]int // field → current count
	decremented    int
	errOnIncrement bool
}

func (m *countingMockStore) IncrementUsageReturning(_ string, field string) (int, error) {
	if m.errOnIncrement {
		return 0, fmt.Errorf("simulated postgres error")
	}
	m.counts[field]++
	return m.counts[field], nil
}

func (m *countingMockStore) DecrementUsage(_ string, _ string) error {
	m.decremented++
	return nil
}

func TestReserveMemoryCount_Unlimited(t *testing.T) {
	m := New(nil, nil, nil)
	allowed, _ := m.ReserveMemoryCount(context.Background(), "user-1", -1)
	if !allowed {
		t.Fatal("unlimited memory limit should always allow")
	}
}

func TestReserveMemoryCount_NilRedis(t *testing.T) {
	m := New(nil, nil, nil)
	allowed, _ := m.ReserveMemoryCount(context.Background(), "user-1", 300)
	if !allowed {
		t.Fatal("nil redis should degrade to allowing")
	}
}

// ---- Storage bytes: degraded-mode and input-validation behaviour ----
// The Redis-hit paths (actual atomic increment, backfill from Postgres)
// require a live Redis and are covered by integration tests in
// semantics_test.go if wired there. These exercise the cheap branches.

func TestReserveStorageBytes_Unlimited(t *testing.T) {
	m := New(nil, nil, nil)
	allowed, _ := m.ReserveStorageBytes(context.Background(), "user-1", 1<<30, -1)
	if !allowed {
		t.Fatal("unlimited storage (-1) should always allow")
	}
}

func TestReserveStorageBytes_ZeroBytes_Allowed(t *testing.T) {
	m := New(nil, nil, nil)
	// Zero-size upload is a no-op reservation; shouldn't consume any
	// quota and shouldn't fail. Caller (memory create with text-only
	// body) shouldn't hit this path at all but we handle it gracefully.
	allowed, _ := m.ReserveStorageBytes(context.Background(), "user-1", 0, 100)
	if !allowed {
		t.Fatal("zero bytes should short-circuit to allowed")
	}
}

func TestReserveStorageBytes_NilRedis_Degrades(t *testing.T) {
	m := New(nil, nil, nil)
	allowed, _ := m.ReserveStorageBytes(context.Background(), "user-1", 500<<20, 500<<20)
	if !allowed {
		t.Fatal("nil redis should degrade to allowing (same contract as ReserveMemoryCount)")
	}
}

func TestAdjustStorageBytes_NilRedis_NoPanic(t *testing.T) {
	m := New(nil, nil, nil)
	// Must not panic; nothing else to assert in degraded mode.
	m.AdjustStorageBytes(context.Background(), "user-1", -1024)
	m.AdjustStorageBytes(context.Background(), "user-1", 0)
}

func TestRollbackStorageBytes_NilRedis_NoPanic(t *testing.T) {
	m := New(nil, nil, nil)
	m.RollbackStorageBytes(context.Background(), "user-1", 1024)
	m.RollbackStorageBytes(context.Background(), "user-1", 0)
}

func TestCommitStorageBytes_NilRedis_NoPanic(t *testing.T) {
	m := New(nil, nil, nil)
	m.CommitStorageBytes(context.Background(), "user-1", 1024)
}

func TestInvalidateStorageBytes_NilRedis_NoPanic(t *testing.T) {
	m := New(nil, nil, nil)
	m.InvalidateStorageBytes(context.Background(), "user-1")
}

func TestGetCommittedStorageBytes_NilRedis_ReturnsZero(t *testing.T) {
	m := New(nil, nil, nil)
	if v := m.GetCommittedStorageBytes(context.Background(), "user-1"); v != 0 {
		t.Fatalf("nil redis should return 0, got %d", v)
	}
}

func TestCommitFromContext_NilToken(t *testing.T) {
	meterctx.CommitFromContext(context.Background())
}

func TestRollbackFromContext_NilToken(t *testing.T) {
	meterctx.RollbackFromContext(context.Background())
}

func TestContextRoundTrip(t *testing.T) {
	m := New(nil, nil, nil)
	token, _, _ := m.Reserve(context.Background(), "user-1", "push", 100)

	ctx := meterctx.WithToken(context.Background(), token)
	got := meterctx.TokenFromContext(ctx)
	if got != token {
		t.Fatal("context round-trip should return same token")
	}
}

func TestUserLimitsContext(t *testing.T) {
	ctx := context.Background()
	if meterctx.UserLimitsFromContext(ctx) != nil {
		t.Fatal("should be nil when not set")
	}

	limits := &model.UserLimits{PlanID: "pro"}
	ctx = meterctx.WithUserLimits(ctx, limits)
	got := meterctx.UserLimitsFromContext(ctx)
	if got == nil || got.PlanID != "pro" {
		t.Fatal("should round-trip user limits")
	}
}
