package forget_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// count runs a count query on a pool, as the login role (no RLS).
func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// sealAndVerify seals a space's receipts and verifies its chain.
func sealAndVerify(t *testing.T, l *ledger.Ledger, space uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := l.SealSpace(ctx, space, nil, 1000); err != nil {
			t.Fatalf("seal: %v", err)
		}
		v, err := l.VerifySpace(ctx, space, nil)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if v.Unsealed == 0 || time.Now().After(deadline) {
			if !v.OK() || v.Unsealed != 0 {
				t.Errorf("space %s: the chain doesn't verify (%d unsealed): %v", space, v.Unsealed, v.Problems)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A point-in-time restore brings forgotten words back; re-applying the
// forget ledger from object storage forgets them again, with the
// tombstones the ledger recorded, retires the deleted space again and
// deletes its hub, and leaves both chains verifying. A second run changes
// nothing.
func TestReapplyAfterARestore(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space, gone := f.space(zz), f.space(zz)
	const words, deleted = "Numbat ferries rotate the staging keys", "Okapi ledgers live in the deleted space"
	m := f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: words + ".", Section: ledger.SectionConventions}}).Memory
	kept := f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: "Use pnpm workspaces only.", Section: ledger.SectionConventions}}).Memory
	f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: gone, Statement: deleted + ".", Section: ledger.SectionConventions}})

	// The backup: the database as it was before anything was forgotten.
	backup := testdb.Copy(t, f.pool)

	res := f.apply(&ledger.Forget{Meta: f.meta(zz, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1})
	sp := f.apply(&ledger.ForgetSpace{Meta: f.meta(zz, policy.ViaWeb), SpaceID: gone, Retire: true})
	f.exec(`DELETE FROM hubs WHERE id = $1`, gone) // V1's delete, after the retire
	prop := forget.New(f.l, f.svc, nil, quiet)
	for _, a := range []ledger.ForgetPropagateArgs{{OpID: res.Tombstone.ID, SpaceID: space}, {OpID: sp.Tombstone.ID, SpaceID: gone}} {
		if rep, err := prop.Run(ctx, a); err != nil || !rep.Done {
			t.Fatalf("propagate %s: %+v %v", a.OpID, rep, err)
		}
	}
	for _, key := range []string{ledger.ForgetLedgerKey(space, res.Tombstone.ID), ledger.ForgetLedgerKey(gone, sp.Tombstone.ID)} {
		if !f.store.Has(key) {
			t.Fatalf("no forget-ledger copy at %s", key)
		}
	}

	// The restore: the backup, with the words, the hub and no tombstones.
	restored := ledger.New(backup, ledger.WithLogger(quiet))
	if n := count(t, backup, `SELECT count(*) FROM v2.memory_versions WHERE statement LIKE '%Numbat%' OR statement LIKE '%Okapi%'`); n != 2 {
		t.Fatalf("the backup holds %d of the words", n)
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.tombstones`); n != 0 {
		t.Fatalf("the backup has %d tombstones", n)
	}

	dry, err := forget.Reapply(ctx, restored, backup, f.store, forget.ReapplyOptions{DryRun: true}, quiet)
	if err != nil || dry.Ops != 2 || dry.FromObjectStore != 2 || len(dry.Reapplied) != 0 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.memory_versions WHERE statement LIKE '%Numbat%'`); n != 1 {
		t.Fatal("the dry run wrote")
	}

	rep, err := forget.Reapply(ctx, restored, backup, f.store, forget.ReapplyOptions{}, quiet)
	if err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if rep.Ops != 2 || len(rep.Reapplied) != 2 || len(rep.HubsDeleted) != 1 || rep.HubsDeleted[0] != gone {
		t.Errorf("reapply = %+v", rep)
	}
	for _, q := range []string{
		`SELECT count(*) FROM v2.memory_versions WHERE (statement LIKE '%Numbat%' OR statement LIKE '%Okapi%') AND $1::uuid IS NOT NULL`,
		`SELECT count(*) FROM v2.memories WHERE search::text LIKE '%numbat%' AND $1::uuid IS NOT NULL`,
		`SELECT count(*) FROM v2.memories WHERE space_id = $1`,
		`SELECT count(*) FROM hubs WHERE id = $1`,
	} {
		if n := count(t, backup, q, gone); n != 0 {
			t.Errorf("after reapply, %q = %d", q, n)
		}
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1 AND statement IS NOT NULL`, kept.ID); n != 1 {
		t.Error("reapply took the memory nobody forgot")
	}
	// The tombstones are the ones the ledger recorded, stamped.
	if n := count(t, backup, `SELECT count(*) FROM v2.tombstones WHERE id = $1 AND object_id = $2 AND reapplied_at IS NOT NULL`,
		res.Tombstone.ID, m.ID); n != 1 {
		t.Error("the memory's tombstone isn't the recorded one")
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.tombstones WHERE id = $1 AND object_kind = 'space'`, sp.Tombstone.ID); n != 1 {
		t.Error("the space's tombstone isn't the recorded one")
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.space_ledgers WHERE space_id = $1 AND retired_at IS NOT NULL`, gone); n != 1 {
		t.Error("the deleted space isn't retired")
	}
	sealAndVerify(t, restored, space)
	sealAndVerify(t, restored, gone)

	// Again: nothing is back, so nothing is written.
	receipts := count(t, backup, `SELECT count(*) FROM v2.receipts`)
	again, err := forget.Reapply(ctx, restored, backup, f.store, forget.ReapplyOptions{}, quiet)
	if err != nil || len(again.Reapplied) != 0 || len(again.Gone) != 1 || len(again.Unchanged) != 1 {
		t.Errorf("again: %+v %v", again, err)
	}
	if n := count(t, backup, `SELECT count(*) FROM v2.receipts`); n != receipts {
		t.Errorf("a second run wrote %d receipts", n-receipts)
	}
	// The live database was never touched by any of it.
	if n := count(t, f.pool, `SELECT count(*) FROM v2.tombstones WHERE reapplied_at IS NOT NULL`); n != 0 {
		t.Error("reapply wrote to the live database")
	}
}
