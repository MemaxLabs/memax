package ledger_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The chain is in transaction order, numerically: receipts whose txids
// straddle a power of ten (9998, 9999, 10000, 10001) are sealed in that
// order, in batches, and none is skipped. Ordered as text, "10000" sorted
// before "9998", and the head (compared numerically) passed the two below.
func TestSealOrdersReceiptsAcrossAPowerOfTen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	owner := f.user("zz")
	sp := f.space(owner, policy.SpaceProject, "memax-v2")
	var tenant uuid.UUID
	var current int64
	if err := f.pool.QueryRow(ctx, `SELECT tenant_id, (SELECT pg_current_xact_id()::text::bigint) FROM hubs WHERE id = $1`, sp).
		Scan(&tenant, &current); err != nil {
		t.Fatal(err)
	}
	// The largest power of ten safely below the cluster's transactions, so
	// every one of these is below the sealer's watermark.
	p := int64(10)
	for p*10+2 < current {
		p *= 10
	}
	f.exec(`ALTER TABLE v2.receipts DISABLE TRIGGER receipts_stamp`)
	want := map[int64]uuid.UUID{}
	for _, txid := range []int64{p, p - 2, p + 1, p - 1} { // inserted out of order
		id := uuid.Must(uuid.NewV7())
		want[txid] = id
		f.exec(`INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via,
		                                 occurred_at, recorded_at, stream_id, stream_version, txid)
		        VALUES ($1, $2, $3, 'memory', $4, 'M-0001', 'edited', 'memax', 'system', now(), now(), $4, 1, $5::text::xid8)`,
			id, tenant, sp, uuid.New(), strconv.FormatInt(txid, 10))
	}
	f.exec(`ALTER TABLE v2.receipts ENABLE TRIGGER receipts_stamp`)

	// The watermark is the cluster's oldest running transaction, which
	// another package's test may hold below these for a while: wait, as
	// the export tests do, until all four are sealed.
	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, err := f.l.SealSpace(ctx, sp, nil, 2); err != nil {
			t.Fatal(err)
		}
		var sealed int64
		if err := f.pool.QueryRow(ctx, `SELECT COALESCE(max(position), 0) FROM v2.receipt_chain_heads WHERE space_id = $1`, sp).
			Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if sealed >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sealed %d of 4 within 90 s", sealed)
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, err := f.pool.Query(ctx, `SELECT first_receipt_id, last_receipt_id, position_from, position_to FROM v2.receipt_checkpoints
	                                 WHERE space_id = $1 ORDER BY number`, sp)
	if err != nil {
		t.Fatal(err)
	}
	type cp struct {
		first, last uuid.UUID
		from, to    int64
	}
	var got []cp
	for rows.Next() {
		var c cp
		if err := rows.Scan(&c.first, &c.last, &c.from, &c.to); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	rows.Close()
	wantCps := []cp{{want[p-2], want[p-1], 1, 2}, {want[p], want[p+1], 3, 4}}
	if len(got) != len(wantCps) {
		t.Fatalf("checkpoints %+v, want %+v (txids around %d)", got, wantCps, p)
	}
	for i := range wantCps {
		if got[i] != wantCps[i] {
			t.Errorf("checkpoint %d = %+v, want %+v (txids around %d)", i+1, got[i], wantCps[i], p)
		}
	}
}
