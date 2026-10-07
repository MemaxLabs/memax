package ledger_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// TestScopeComesFirst: however a transaction's first statement is sent
// (a query, a row, an exec, a batch with or without callbacks, a text of
// several statements, a savepoint, the raw connection), Postgres receives
// BEGIN and the scope ahead of it, in the same transaction. netsim.Audit
// reads the wire and flags any statement on a v2 table that runs outside
// a transaction, before the scope, or as another role than memax_v2: a
// change that let one through fails here.
func TestScopeComesFirst(t *testing.T) {
	t.Parallel()
	audit := netsim.NewAudit(ledger.DBRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f := newFixtureOn(t, db, ledger.WithJobs(client), ledger.WithIndexJobs())
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "River is our queue.")
	f.apply(&ledger.ReviseBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, Title: "memax-v2",
		Sections: []ledger.BriefSection{{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: m.Ref}}}}})
	f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, Kind: ledger.TargetAgentsMD})
	scope := f.scope(zz)
	ctx := context.Background()
	audit.Arm()

	const count = `SELECT count(*) FROM v2.memories WHERE space_id = ANY($1)`
	ids := scope.SpaceIDs()
	scan := func(row pgx.Row) error { var n int; return row.Scan(&n) }
	firsts := []struct {
		name string
		fn   func(tx pgx.Tx) error
	}{
		{"Query", func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, count, ids)
			if err != nil {
				return err
			}
			_, err = pgx.CollectRows(rows, pgx.RowTo[int])
			return err
		}},
		{"QueryRow", func(tx pgx.Tx) error { return scan(tx.QueryRow(ctx, count, ids)) }},
		{"Exec", func(tx pgx.Tx) error { _, err := tx.Exec(ctx, count, ids); return err }},
		{"Exec without arguments", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT count(*) FROM v2.memories`)
			return err
		}},
		{"Exec of several statements", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT count(*) FROM v2.memories; SELECT count(*) FROM v2.receipts`)
			return err
		}},
		{"SendBatch", func(tx pgx.Tx) error {
			b := &pgx.Batch{}
			b.Queue(count, ids)
			b.Queue(`SELECT count(*) FROM v2.receipts`)
			br := tx.SendBatch(ctx, b)
			if err := scan(br.QueryRow()); err != nil {
				return err
			}
			if _, err := br.Exec(); err != nil {
				return err
			}
			return br.Close()
		}},
		{"SendBatch with callbacks", func(tx pgx.Tx) error {
			b := &pgx.Batch{}
			b.Queue(count, ids).QueryRow(scan)
			b.Queue(`SELECT count(*) FROM v2.receipts`).Exec(func(pgconn.CommandTag) error { return nil })
			return tx.SendBatch(ctx, b).Close()
		}},
		{"a savepoint", func(tx pgx.Tx) error {
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			if err := scan(sp.QueryRow(ctx, count, ids)); err != nil {
				return err
			}
			return sp.Commit(ctx)
		}},
		{"the raw connection", func(tx pgx.Tx) error { return scan(tx.Conn().QueryRow(ctx, count, ids)) }},
		{"Prepare", func(tx pgx.Tx) error {
			if _, err := tx.Prepare(ctx, "count_memories", count); err != nil {
				return err
			}
			return scan(tx.QueryRow(ctx, "count_memories", ids))
		}},
	}
	for _, first := range firsts {
		if err := f.l.Read(ctx, scope, first.fn); err != nil {
			t.Fatalf("%s: %v", first.name, err)
		}
	}

	// A batch read: BEGIN, the scope, its statements and COMMIT together.
	b := &pgx.Batch{}
	b.Queue(count, ids).QueryRow(scan)
	if err := f.l.ReadBatch(ctx, scope, b); err != nil {
		t.Fatal(err)
	}
	// Commands: a Remember and a Keep, with their River jobs inserted as
	// the login role in between (the audit flags any v2 statement there).
	f.remember(zz, space, "Deploys go through Fly.")
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "Use pnpm.")})
	f.apply(&ledger.Keep{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: p.Memory.Ref, ExpectedVersion: 1})
	// A metered write, whose COMMIT goes with its one statement.
	if _, err := f.l.CountAsk(ctx, scope, time.Now()); err != nil {
		t.Fatal(err)
	}

	if !netsim.Settle(db.Pool, 5*time.Second) {
		t.Fatal("connections still checked out")
	}
	audit.Disarm()
	if v := audit.Violations(); len(v) > 0 {
		for _, x := range v {
			t.Errorf("unscoped: %s", x)
		}
	}
	if n := audit.Checked(); n < 30 {
		t.Errorf("the audit checked only %d v2 statements", n)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'compile_target'`); n == 0 {
		t.Error("no compile job was enqueued: the River window wasn't exercised")
	}
}

// TestPipelinedRoundTrips: a read of one statement costs one round trip
// before it returns (its COMMIT follows), a batch read costs one in all,
// and a transaction that sends nothing costs none.
func TestPipelinedRoundTrips(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t, testdb.Options{})
	f := newFixtureOn(t, db)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.remember(zz, space, "River is our queue.")
	scope := f.scope(zz)
	const count = `SELECT count(*) FROM v2.memories WHERE space_id = ANY($1)`
	read := func(ctx context.Context) error {
		return f.l.Read(ctx, scope, func(tx pgx.Tx) error {
			var n int
			return tx.QueryRow(ctx, count, scope.SpaceIDs()).Scan(&n)
		})
	}
	batch := func(ctx context.Context) error {
		b := &pgx.Batch{}
		var n, m int
		b.Queue(count, scope.SpaceIDs()).QueryRow(func(r pgx.Row) error { return r.Scan(&n) })
		b.Queue(`SELECT count(*) FROM v2.receipts`).QueryRow(func(r pgx.Row) error { return r.Scan(&m) })
		return f.l.ReadBatch(ctx, scope, b)
	}
	asks := 0
	meter := func(ctx context.Context) error {
		n, err := f.l.CountAsk(ctx, scope, time.Now())
		if err == nil && n != asks+1 {
			t.Errorf("CountAsk = %d, want %d: the count must be committed", n, asks+1)
		}
		asks = n
		return err
	}
	nothing := func(ctx context.Context) error {
		return f.l.Read(ctx, scope, func(pgx.Tx) error { return nil })
	}
	for _, c := range []struct {
		name          string
		op            func(context.Context) error
		before, total int
	}{
		{"a one-statement read", read, 1, 2},
		{"a batch read", batch, 1, 1},
		{"a read that sends nothing", nothing, 0, 0},
		{"a metered write, its COMMIT with its statement", meter, 1, 1},
	} {
		for range 2 {
			if err := c.op(context.Background()); err != nil { // warm the statement cache
				t.Fatal(err)
			}
		}
		netsim.Settle(db.Pool, 2*time.Second)
		ctx, cnt := netsim.Track(context.Background())
		if err := c.op(ctx); err != nil {
			t.Fatal(err)
		}
		before := cnt.RoundTrips()
		netsim.Settle(db.Pool, 2*time.Second)
		if before > c.before || cnt.RoundTrips() != c.total {
			t.Errorf("%s: %d round trips before it returned, %d in all; want at most %d and %d\n%s",
				c.name, before, cnt.RoundTrips(), c.before, c.total, cnt)
		}
	}
}

// TestReadsSeeWhatCommandsCommitted: a read that follows a command sees its
// write, and a command that follows a read whose COMMIT is still on its
// way isn't held up by it.
func TestReadsSeeWhatCommandsCommitted(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t, testdb.Options{RTT: 10 * time.Millisecond})
	f := newFixtureOn(t, db)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()
	for i := range 5 {
		m := f.remember(zz, space, "Fact "+uuid.NewString())
		got, err := f.l.GetMemory(ctx, scope, m.Ref)
		if err != nil || got.ID != m.ID {
			t.Fatalf("read %d after remember: %v", i, err)
		}
	}
	page, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space})
	if err != nil || len(page.Memories) != 5 {
		t.Fatalf("list: %d, %v", len(page.Memories), err)
	}
}
