package netsim_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

func warm(t *testing.T, db *testdb.DB, sqls ...string) {
	t.Helper()
	for range 3 {
		for _, sql := range sqls {
			if _, err := db.Pool.Exec(context.Background(), sql); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
		}
	}
}

// TestProxyDelaysEachRoundTrip: one statement costs one round trip of the
// simulated network, and a pipelined batch costs one too.
func TestProxyDelaysEachRoundTrip(t *testing.T) {
	t.Parallel()
	const rtt = 20 * time.Millisecond
	db := testdb.Open(t, testdb.Options{RTT: rtt})
	ctx := context.Background()
	warm(t, db, "SELECT 1", "SELECT 2")

	const n = 5
	start := time.Now()
	for range n {
		var x int
		if err := db.Pool.QueryRow(ctx, "SELECT 1").Scan(&x); err != nil {
			t.Fatal(err)
		}
	}
	if per := time.Since(start) / n; per < rtt || per > rtt+15*time.Millisecond {
		t.Errorf("a query took %v over a %v network", per, rtt)
	}

	batch := func() *pgx.Batch {
		b := &pgx.Batch{}
		for range 10 {
			b.Queue("SELECT 1")
			b.Queue("SELECT 2")
		}
		return b
	}
	// The first batch prepares its statements (a round trip of its own).
	if err := db.Pool.SendBatch(ctx, batch()).Close(); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if err := db.Pool.SendBatch(ctx, batch()).Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < rtt || took > 2*rtt {
		t.Errorf("a batch of 20 took %v over a %v network; a pipeline is one round trip", took, rtt)
	}
}

// TestTracerCountsWhatTheWireSees: the tracer's count for an operation is
// the number of round trips the proxy sees.
func TestTracerCountsWhatTheWireSees(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t, testdb.Options{Proxy: true})
	ctx := context.Background()
	b := func() *pgx.Batch {
		b := &pgx.Batch{}
		b.Queue("SELECT 1")
		b.Queue("SELECT 2")
		return b
	}
	op := func(ctx context.Context) {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var x int
		if err := tx.QueryRow(ctx, "SELECT 1").Scan(&x); err != nil {
			t.Fatal(err)
		}
		if err := tx.SendBatch(ctx, b()).Close(); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		op(ctx) // warm the connection's statement cache
	}
	wire := db.Proxy.RoundTrips()
	cctx, c := netsim.Track(ctx)
	op(cctx)
	if got := db.Proxy.RoundTrips() - wire; got != 4 || c.RoundTrips() != 4 || c.Count() != 4 {
		t.Errorf("BEGIN, a query, a batch and COMMIT: wire %d, tracer %d (%d with prepares), want 4\n%s",
			got, c.RoundTrips(), c.Count(), c)
	}
	kinds := []netsim.Kind{}
	for _, tr := range c.Trips() {
		kinds = append(kinds, tr.Kind)
	}
	if strings.Join(kindStrings(kinds), ",") != "query,query,batch,query" {
		t.Errorf("trips = %v", kinds)
	}

	// A statement the connection hasn't prepared costs a prepare first.
	cctx, c = netsim.Track(ctx)
	var x int
	if err := db.Pool.QueryRow(cctx, "SELECT 3").Scan(&x); err != nil {
		t.Fatal(err)
	}
	if c.Count(netsim.Prepare) != 1 || c.RoundTrips() != 1 {
		t.Errorf("a new statement: %d prepares, %d round trips\n%s", c.Count(netsim.Prepare), c.RoundTrips(), c)
	}
}

func kindStrings(ks []netsim.Kind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

// TestAuditFindsUnscopedStatements: the audit flags a v2 statement outside
// a transaction, before the scope, and as the login role, and passes one
// that follows the role switch and the scope in its transaction.
func TestAuditFindsUnscopedStatements(t *testing.T) {
	t.Parallel()
	audit := netsim.NewAudit("memax_v2")
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	ctx := context.Background()
	audit.Arm()

	const read = `SELECT count(*) FROM v2.memories`
	scope := `SELECT set_config('role', $1, true), set_config('app.space_ids', $2, true)`
	run := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
	}
	// Outside a transaction.
	if _, err := db.Pool.Exec(ctx, read); err != nil {
		t.Fatal(err)
	}
	// As memax_v2, before the scope.
	run(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE memax_v2`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, read)
		return err
	})
	// Scoped, then back to the login role (the River window).
	run(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, scope, "memax_v2", "{}"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, read); err != nil { // fine
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, "none"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, read)
		return err
	})
	// The scope in the same pipeline as the read, after BEGIN: fine.
	b := &pgx.Batch{}
	b.Queue("BEGIN READ ONLY")
	b.Queue(scope, "memax_v2", "{}")
	b.Queue(read)
	b.Queue("COMMIT")
	if err := db.Pool.SendBatch(ctx, b).Close(); err != nil {
		t.Fatal(err)
	}
	// The scope and the read pipelined without BEGIN: an implicit
	// transaction the audit doesn't accept.
	b = &pgx.Batch{}
	b.Queue(scope, "memax_v2", "{}")
	b.Queue(read)
	if err := db.Pool.SendBatch(ctx, b).Close(); err != nil {
		t.Fatal(err)
	}

	got := audit.Violations()
	want := []string{"outside a transaction", "before the scope was set", `as role "none"`, "outside a transaction"}
	if len(got) != len(want) {
		t.Fatalf("violations = %v, want %d", got, len(want))
	}
	for i, v := range got {
		if v.Reason != want[i] {
			t.Errorf("violation %d = %s, want %s", i, v, want[i])
		}
	}
	if audit.Checked() != 6 {
		t.Errorf("checked %d v2 statements, want 6", audit.Checked())
	}
}
