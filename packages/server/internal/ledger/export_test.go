package ledger_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// collector keeps what ReadExport hands over.
type collector struct {
	space      ledger.ExportSpaceInfo
	memories   []ledger.ExportMemory
	receipts   []receiptchain.Receipt
	tombstones []ledger.Tombstone
	batches    int
	seal       ledger.ExportSeal
}

func (c *collector) Space(s ledger.ExportSpaceInfo) error   { c.space = s; return nil }
func (c *collector) Briefs([]ledger.Brief) error            { return nil }
func (c *collector) Gates([]ledger.Gate) error              { return nil }
func (c *collector) Targets([]ledger.Target) error          { return nil }
func (c *collector) Agents([]ledger.ExportAgent) error      { return nil }
func (c *collector) Tombstones(ts []ledger.Tombstone) error { c.tombstones = ts; return nil }
func (c *collector) Reads(ledger.ReadSummary) error         { return nil }
func (c *collector) Seal(s ledger.ExportSeal) error         { c.seal = s; return nil }
func (c *collector) Memories(ms []ledger.ExportMemory) error {
	c.memories = append(c.memories, ms...)
	c.batches++
	return nil
}
func (c *collector) Receipts(rs []receiptchain.Receipt) error {
	c.receipts = append(c.receipts, rs...)
	return nil
}

// An export is one `exported` receipt by the person, on the space's own
// stream; exports at once, and an export beside a Forget of the whole
// space, take turns on that stream rather than collide.
func TestExportIsOneReceiptOnTheSpacesStream(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz).Narrow(space)
	f.remember(zz, space, "Background jobs run on River.")

	var wg sync.WaitGroup
	errs := make(chan error, 9)
	for i := range 9 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if i == 4 {
				_, err = f.l.Apply(context.Background(), &ledger.ForgetSpace{Meta: meta(person(zz), scope, policy.ViaWeb), SpaceID: space})
			} else {
				var res ledger.Result
				res, err = f.l.Apply(context.Background(), &ledger.Export{Meta: meta(person(zz), scope, policy.ViaCLI), SpaceID: space})
				if err == nil && (res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 1 ||
					res.Receipts[0].Action != ledger.ActionExported || res.Receipts[0].ObjectKind != ledger.ObjectSpace) {
					t.Errorf("export = %+v", res)
				}
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1`, space); n != 9 {
		t.Errorf("the space's stream holds %d receipts, want 8 exports and a forget", n)
	}
	if n := f.count(`SELECT max(stream_version) FROM v2.receipts WHERE stream_id = $1`, space); n != 9 {
		t.Errorf("the space's stream reaches version %d", n)
	}

	// The same key is the same export: no second receipt.
	m := meta(person(zz), scope, policy.ViaCLI)
	first := f.apply(&ledger.Export{Meta: m, SpaceID: space})
	again := f.apply(&ledger.Export{Meta: m, SpaceID: space})
	if !again.Replayed || again.Receipts[0].ID != first.Receipts[0].ID {
		t.Errorf("a retried export = %+v", again)
	}

	// Agents don't export; someone outside the space doesn't find it.
	refusedWith(t, f.apply(&ledger.Export{Meta: meta(agentFor(policy.AutonomyWrite), scope, policy.ViaMCP), SpaceID: space}),
		policy.CodeExportByPerson)
	jy := f.user("jy")
	if _, err := f.l.Apply(context.Background(), &ledger.Export{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), SpaceID: space}); err != ledger.ErrNotFound {
		t.Errorf("an outsider's export: %v", err)
	}
	if err := f.l.ReadExport(context.Background(), f.scope(jy), space, &collector{}); err != ledger.ErrNotFound {
		t.Errorf("an outsider's read: %v", err)
	}
}

// ReadExport reads the whole record in batches, the receipts in chain
// order, and a forgotten memory without its words.
func TestReadExportReadsTheWholeRecord(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz).Narrow(space)
	// More than one batch of memories.
	for range 503 {
		f.remember(zz, space, "Fact number "+uuid.NewString())
	}
	gone := f.remember(zz, space, "A word to forget: periwinkle.")
	f.apply(&ledger.Forget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: gone.Ref, ExpectedVersion: 1})

	var c collector
	if err := f.l.ReadExport(context.Background(), scope, space, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.memories) != 504 || c.batches != 2 {
		t.Fatalf("%d memories in %d batches", len(c.memories), c.batches)
	}
	for i := 1; i < len(c.memories); i++ {
		if c.memories[i-1].Memory.Ref >= c.memories[i].Memory.Ref {
			t.Fatalf("memories out of order at %s", c.memories[i].Memory.Ref)
		}
	}
	last := c.memories[len(c.memories)-1]
	if last.Memory.ID != gone.ID || last.Memory.Statement != "" || len(last.Versions) != 1 || last.Versions[0].Statement != "" ||
		len(last.Receipts) != 2 || last.Receipts[1].Action != ledger.ActionForgot {
		t.Errorf("the forgotten memory reads %+v", last)
	}
	if len(c.tombstones) != 1 || c.tombstones[0].ObjectID != gone.ID {
		t.Errorf("tombstones = %+v", c.tombstones)
	}
	if int64(len(c.receipts)) != c.space.Receipts || len(c.receipts) != 505 || c.space.AsOf.IsZero() {
		t.Errorf("%d receipts, the space says %d as of %v", len(c.receipts), c.space.Receipts, c.space.AsOf)
	}
	if c.seal.Head != nil || c.seal.Unsealed != int64(len(c.receipts)) {
		t.Errorf("seal = %+v", c.seal)
	}
	if c.memories[0].Memory.Statement == "" || len(c.memories[0].Receipts) != 1 || c.memories[0].Receipts[0].Action != ledger.ActionKept {
		t.Errorf("first memory = %+v", c.memories[0])
	}
}

// An export reads through the ledger's read path: every statement on a v2
// table runs inside its transaction, after its scope, as memax_v2
// (netsim.Audit), and the read is pipelined. Its round trips are counted
// so a change that adds some shows here; an export is no hot path, and
// ForgetSpace, which shares the space's stream lock, gains none from it.
func TestExportIsScopedAndPipelined(t *testing.T) {
	t.Parallel()
	audit := netsim.NewAudit(ledger.DBRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	f := newFixtureOn(t, db)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz).Narrow(space)
	for _, s := range []string{"River is our queue.", "pnpm workspaces only.", "Tabs in Go."} {
		f.remember(zz, space, s)
	}
	gone := f.remember(zz, space, "A word to forget: periwinkle.")
	f.apply(&ledger.Forget{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: gone.Ref, ExpectedVersion: 1})
	audit.Arm()
	defer audit.Require(t)

	trips := func(run func(ctx context.Context)) int {
		t.Helper()
		ctx, c := netsim.Track(context.Background())
		db.Trips.SetFallback(c)
		defer db.Trips.SetFallback(nil)
		run(ctx)
		if !netsim.Settle(db.Pool, 2*time.Second) {
			t.Fatal("connections still checked out after 2 s")
		}
		return c.RoundTrips()
	}
	command := trips(func(ctx context.Context) {
		if _, err := f.l.Apply(ctx, &ledger.Export{Meta: meta(person(zz), scope, policy.ViaCLI), SpaceID: space}); err != nil {
			t.Fatal(err)
		}
	})
	read := trips(func(ctx context.Context) {
		if err := f.l.ReadExport(ctx, scope, space, &collector{}); err != nil {
			t.Fatal(err)
		}
	})
	forgetSpace := trips(func(ctx context.Context) {
		if _, err := f.l.Apply(ctx, &ledger.ForgetSpace{Meta: meta(person(zz), scope, policy.ViaWeb), SpaceID: space}); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("export: the command %d round trips, the read %d; a Forget of the space %d", command, read, forgetSpace)
	// The command: open with the space, claim the key, the stream (its
	// lock riding along), the receipt, COMMIT with the key's record. The
	// read: the head in one, the gates' rule, the tombstones' companions,
	// the targets, the memories in two per batch, the receipts, the seal.
	if command > 6 || read > 12 {
		t.Errorf("export: %d and %d round trips, budgets 6 and 12", command, read)
	}
	if audit.Checked() == 0 {
		t.Error("the audit saw no statement")
	}
}
