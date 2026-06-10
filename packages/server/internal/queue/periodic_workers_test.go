package queue

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// These tests exercise periodic sweep workers end-to-end against a
// live Postgres via testdb. They don't spin up a River queue — we
// invoke Work() directly on the worker struct, which is the contract
// River calls at run time.

func TestExpireWaitlistInvitesWorker_NoOpOnEmptyDB(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &ExpireWaitlistInvitesWorker{Store: s}

	// Timeout method is trivial but covered too.
	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}

	// No invites → no-op, no error.
	if err := w.Work(context.Background(), &river.Job[ExpireWaitlistInvitesArgs]{}); err != nil {
		t.Errorf("empty-DB Work: %v", err)
	}
}

func TestExpireNotificationsWorker_NoOpOnEmptyDB(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &ExpireNotificationsWorker{Store: s}

	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}
	if err := w.Work(context.Background(), &river.Job[ExpireNotificationsArgs]{}); err != nil {
		t.Errorf("Work: %v", err)
	}
}

func TestExpireNotificationsWorker_NilStoreIsNoOp(t *testing.T) {
	t.Parallel()
	w := &ExpireNotificationsWorker{Store: nil}
	if err := w.Work(context.Background(), &river.Job[ExpireNotificationsArgs]{}); err != nil {
		t.Errorf("nil store: %v", err)
	}
}

// UsageSyncWorker's nil-meter short-circuit — the common "dev without
// Redis" path must not fail.
func TestUsageSyncWorker_NilMeterIsNoOp(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &UsageSyncWorker{Store: s, Meter: nil}
	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}
	if err := w.Work(context.Background(), &river.Job[UsageSyncArgs]{}); err != nil {
		t.Errorf("nil meter: %v", err)
	}
}

func TestHubFreezeSweepWorker_NoOpOnEmptyDB(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &HubFreezeSweepWorker{Store: s}

	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}
	if err := w.Work(context.Background(), &river.Job[HubFreezeSweepArgs]{}); err != nil {
		t.Errorf("Work: %v", err)
	}
}

// fakeUsageReader is a tiny stub satisfying usageReader so we can drive
// UsageSyncWorker's happy path without bringing up miniredis.
type fakeUsageReader struct {
	push, recall, ask int
}

func (f *fakeUsageReader) GetCommittedUsage(_ context.Context, _ string) (int, int, int) {
	return f.push, f.recall, f.ask
}

func TestUsageSyncWorker_WithMeter_EmptyDB(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	w := &UsageSyncWorker{Store: s, Meter: &fakeUsageReader{}}
	// Empty users table → worker iterates zero rows and exits cleanly.
	if err := w.Work(context.Background(), &river.Job[UsageSyncArgs]{}); err != nil {
		t.Errorf("Work with meter: %v", err)
	}
}

// Quick sanity that the periodic timeouts are all sane (bounded).
func TestPeriodicWorkerTimeoutsAreBounded(t *testing.T) {
	t.Parallel()
	if (&ExpireWaitlistInvitesWorker{}).Timeout(nil) > 5*time.Minute {
		t.Error("ExpireWaitlistInvites timeout > 5m")
	}
	if (&UsageSyncWorker{}).Timeout(nil) > 5*time.Minute {
		t.Error("UsageSync timeout > 5m")
	}
	if (&HubFreezeSweepWorker{}).Timeout(nil) > 5*time.Minute {
		t.Error("HubFreezeSweep timeout > 5m")
	}
	if (&ExpireNotificationsWorker{}).Timeout(nil) > 5*time.Minute {
		t.Error("ExpireNotifications timeout > 5m")
	}
}
