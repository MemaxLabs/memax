package sealer_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/sealer"
)

// The sealer as the worker runs it: two River workers (two machines), the
// periodic sweep on whichever leads, seal jobs on both. Receipts written
// all along are each sealed once, in a chain that verifies.
func TestSealingThroughRiverWithTwoWorkers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 9)
	s := sealer.New(f.l, sealer.Config{Signer: signer, Keys: keys, Batch: 4, Interval: 200 * time.Millisecond, Log: quiet})
	start := func() {
		workers := river.NewWorkers()
		sealer.AddWorkers(workers, s)
		c, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{
			Logger: quiet, Workers: workers, PeriodicJobs: sealer.PeriodicJobs(s),
			Queues:            map[string]river.QueueConfig{ledger.QueueSeal: {MaxWorkers: sealer.MaxWorkers}},
			FetchPollInterval: 100 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Stop(context.Background()) })
	}
	start()
	start()
	for i := range 30 {
		f.remember("Written while two workers seal " + uuid.NewString()[:8])
		if i%10 == 0 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	eventually(t, "every receipt sealed", func() bool {
		return f.count(`SELECT COALESCE(max(position), 0) FROM v2.receipt_chain_heads WHERE space_id = $1`, f.space) == 30
	})
	if n := f.count(`SELECT sum(receipts) FROM v2.receipt_checkpoints WHERE space_id = $1`, f.space); n != 30 {
		t.Errorf("checkpoints cover %d receipts, want 30", n)
	}
	if v := f.verify(s); !v.OK() || v.Receipts != 30 || v.Signed != v.Checkpoints {
		t.Errorf("verify = %+v", v.Problems)
	}
}
