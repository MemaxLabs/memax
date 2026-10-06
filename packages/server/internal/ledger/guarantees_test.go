package ledger_test

// These tests talk to Postgres directly, bypassing the Go write path,
// to prove the guarantees live in the database: a buggy or malicious
// caller with a connection still can't write without a receipt, rewrite
// a receipt, or see another space.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func sqlstate(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// insertReceiptSQL writes a receipt about object in the transaction.
func insertReceiptSQL(tx pgx.Tx, m *ledger.Memory, object uuid.UUID, version int) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	_, err := tx.Exec(context.Background(), `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
		VALUES ($1, $2, $3, 'memory', $4, 'M-9999', 'edited', 'memax', 'system', now(), $4, $5)`,
		id, m.TenantID, m.SpaceID, object, version)
	return id, err
}

// Rule 1: a projection write without a same-transaction receipt fails
// at commit.
func TestReceiptRequiredAtCommit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "Receipts are enforced by the database.")
	other := f.remember(zz, space, "A second memory.")
	ctx := context.Background()
	spaces, tenants := []uuid.UUID{space}, []uuid.UUID{m.TenantID}
	oldReceipt := m.LastReceiptID // committed in an earlier transaction

	cases := []struct {
		name string
		fn   func(tx pgx.Tx) error
		want string // "" = commits
	}{
		{"update without a receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.memories SET section = 'preferences' WHERE id = $1`, m.ID)
			return err
		}, "MXR01"},
		{"update pointing at an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'faded', last_receipt_id = $2 WHERE id = $1`, m.ID, oldReceipt)
			return err
		}, "MXR01"},
		{"insert a memory with an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.memories (id, tenant_id, space_id, seq, section, lifecycle, trust, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, 900, 'conventions', 'kept', 'person', $4, $4)`, uuid.New(), m.TenantID, space, oldReceipt)
			return err
		}, "MXR01"},
		{"insert a version with an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.memory_versions (memory_id, version, space_id, statement, receipt_id, last_receipt_id)
				VALUES ($1, 9, $2, 'smuggled words', $3, $3)`, m.ID, space, oldReceipt)
			return err
		}, "MXR01"},
		{"insert a source with an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.sources (id, space_id, kind, ref, trust_class, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, 'file', 'a.go:1', 'repository', $3, $3)`, uuid.New(), space, oldReceipt)
			return err
		}, "MXR01"},
		{"insert a link with an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.memory_links (id, space_id, kind, from_memory_id, to_memory_id, receipt_id)
				VALUES ($1, $2, 'closes', $3, $4, $5)`, uuid.New(), space, m.ID, other.ID, oldReceipt)
			return err
		}, "MXR01"},
		{"a same-transaction receipt about another memory", func(tx pgx.Tx) error {
			rid, err := insertReceiptSQL(tx, m, other.ID, 2)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.memories SET section = 'preferences', last_receipt_id = $2 WHERE id = $1`, m.ID, rid)
			return err
		}, "MXR01"},
		{"a same-transaction receipt about this memory commits", func(tx pgx.Tx) error {
			rid, err := insertReceiptSQL(tx, m, m.ID, 2)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.memories SET section = 'preferences', stream_version = 2, last_receipt_id = $2 WHERE id = $1`, m.ID, rid)
			return err
		}, ""},
		{"derived index columns need no receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.memories SET search = to_tsvector('simple', 'river'), embedding = NULL WHERE id = $1`, m.ID)
			return err
		}, ""},
	}
	for _, c := range cases {
		var stmtErr error
		err := f.asV2(spaces, tenants, func(tx pgx.Tx) error {
			stmtErr = c.fn(tx)
			return stmtErr
		})
		if stmtErr != nil {
			t.Errorf("%s: the statement itself failed (%v); the check must be deferred to commit", c.name, stmtErr)
			continue
		}
		if got := sqlstate(err); got != c.want {
			t.Errorf("%s: commit error %v (SQLSTATE %q), want %q", c.name, err, got, c.want)
		}
	}

	// The trigger binds everyone, not only memax_v2: a superuser
	// connection without the role switch is refused too.
	_, err := f.pool.Exec(ctx, `UPDATE v2.memories SET kind = 'fact', section = 'decisions' WHERE id = $1`, other.ID)
	if sqlstate(err) != "MXR01" {
		t.Errorf("superuser write without a receipt: %v, want MXR01", err)
	}

	// txid and recorded_at are stamped by the database; a forged txid is
	// overwritten.
	err = f.asV2(spaces, tenants, func(tx pgx.Tx) error {
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version, txid, recorded_at)
			VALUES ($1, $2, $3, 'memory', $4, 'M-0001', 'edited', 'memax', 'system', now(), $4, 99, '1'::xid8, '2001-01-01')`,
			id, m.TenantID, space, uuid.New()); err != nil {
			return err
		}
		var stamped bool
		if err := tx.QueryRow(ctx, `SELECT txid = pg_current_xact_id() AND recorded_at > '2020-01-01' FROM v2.receipts WHERE id = $1`, id).Scan(&stamped); err != nil {
			return err
		}
		if !stamped {
			return fmt.Errorf("forged txid/recorded_at kept")
		}
		return nil
	})
	if err != nil {
		t.Errorf("receipt stamping: %v", err)
	}
}

func TestReceiptsAreAppendOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()
	m := meta(person(zz), scope, policy.ViaWeb)
	m.Reason = "mentions the customer by name"
	mem := f.apply(&ledger.Remember{Meta: m, NewMemory: fact(space, "Acme's deploys go out on Tuesdays.")}).Memory
	spaces, tenants := []uuid.UUID{space}, []uuid.UUID{mem.TenantID}

	for _, c := range []struct {
		name, sql string
	}{
		{"update", `UPDATE v2.receipts SET reason = NULL`},
		{"delete", `DELETE FROM v2.receipts`},
		{"truncate", `TRUNCATE v2.receipts`},
		{"delete a memory", `DELETE FROM v2.memories`},
	} {
		err := f.asV2(spaces, tenants, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.sql); return err })
		if sqlstate(err) != "42501" {
			t.Errorf("memax_v2 %s: %v, want permission denied", c.name, err)
		}
	}
	// Even the owner (superuser here) can't rewrite or remove receipts.
	for _, sql := range []string{
		`DELETE FROM v2.receipts`,
		`TRUNCATE v2.receipts CASCADE`,
		`UPDATE v2.receipts SET action = 'rejected'`,
		`UPDATE v2.receipts SET reason = 'something else'`,
	} {
		if _, err := f.pool.Exec(ctx, sql); sqlstate(err) != "MXR02" {
			t.Errorf("owner %q: %v, want MXR02", sql, err)
		}
	}

	// Redaction is only for Forget, inside the same transaction.
	err := f.asV2(spaces, tenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT v2.redact_receipt_reasons($1)`, mem.ID)
		return err
	})
	if sqlstate(err) != "MXR02" {
		t.Errorf("redaction without a forget receipt: %v, want MXR02", err)
	}
	err = f.asV2(spaces, tenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'forgot', 'person', $6, 'web', now(), $4, 2)`,
			uuid.Must(uuid.NewV7()), mem.TenantID, space, mem.ID, mem.Ref, zz); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT v2.redact_receipt_reasons($1)`, mem.ID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("redacted %d reasons, want 1", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("redaction during forget: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND reason IS NOT NULL`, mem.ID); n != 0 {
		t.Errorf("%d reasons survived redaction", n)
	}
}

// Receipts (and idempotency records, and logs) never contain the words.
func TestReceiptsNeverHoldTheStatement(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	words := []string{"zephyrquokka", "marmalade-lighthouse", "obsidianfjord", "quietvelocity"}
	nm := fact(space, "The "+words[0]+" service retries three times.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "retry.go:12", Quote: "const " + words[1] + " = 3"}}
	m := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}).Memory
	f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1, Statement: "The " + words[2] + " service retries five times."})
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, words[3]+" is the default.")}).Memory
	f.apply(&ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: p.Ref})
	f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), scope, policy.ViaMCP), Memory: m.Ref, ExpectedVersion: 2, Statement: words[3] + " retries forever."})

	ctx := context.Background()
	for _, table := range []string{"v2.receipts", "v2.command_keys", "v2.id_counters", "v2.memory_links"} {
		rows, err := f.pool.Query(ctx, `SELECT t::text FROM `+table+` t`)
		if err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		texts, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		if len(texts) == 0 && table == "v2.receipts" {
			t.Fatal("no receipts written")
		}
		for _, row := range texts {
			for _, w := range words {
				if strings.Contains(strings.ToLower(row), w) {
					t.Errorf("%s holds the words %q: %s", table, w, row)
				}
			}
		}
	}
	logs := strings.ToLower(f.logs.String())
	if !strings.Contains(logs, "ledger: applied") {
		t.Error("expected the ledger to log applied commands")
	}
	for _, w := range words {
		if strings.Contains(logs, w) {
			t.Errorf("logs hold the words %q", w)
		}
	}
	// The words are where Forget will purge them, and only there.
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE statement ILIKE '%' || $1 || '%'`, words[0]); n != 1 {
		t.Errorf("statement versions holding %q = %d, want 1", words[0], n)
	}
}

// Rule 13: an actor scoped to space A sees nothing of space B, and no
// scope sees nothing, even on a superuser connection.
func TestRowLevelSecurity(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	jy := f.user("jy")
	zzSpace := f.space(zz, policy.SpaceProject, "memax-v2")
	jySpace := f.space(jy, policy.SpaceProject, "side-project")
	zzScope, jyScope := f.scope(zz), f.scope(jy)

	mine := f.remember(zz, zzSpace, "Our space.")
	nm := fact(jySpace, "Their space.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "x.go:1"}}
	theirs := f.apply(&ledger.Remember{Meta: meta(person(jy), jyScope, policy.ViaWeb), NewMemory: nm}).Memory
	f.apply(&ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), jyScope, policy.ViaMCP), Memory: theirs.Ref, ExpectedVersion: 1, Statement: "Their space, edited."})
	// A V1 memory (a note) in each space.
	for _, s := range []struct{ owner, hub uuid.UUID }{{zz, zzSpace}, {jy, jySpace}} {
		f.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content) VALUES ($1, $2, $3, 'note', 'v1 body')`, uuid.New(), s.owner, s.hub)
	}

	tables := []string{"v2.receipts", "v2.memories", "v2.memory_versions", "v2.sources", "v2.memory_sources",
		"v2.memory_links", "v2.command_keys", "v2.id_counters", "v2.spaces", "v2.notes"}
	countAll := map[string]int{}
	for _, tbl := range tables {
		countAll[tbl] = f.count(`SELECT count(*) FROM ` + tbl)
	}
	// Without the role switch a superuser sees every row: that is exactly
	// why every ledger transaction switches to memax_v2. (The views filter
	// by scope themselves, so they show nothing without one.)
	if countAll["v2.memories"] != 3 || countAll["v2.receipts"] != 3 || countAll["v2.memory_links"] != 1 {
		t.Fatalf("superuser sees %v; fixture broken", countAll)
	}
	// What each space should see: its own rows, counted as superuser.
	own := func(tbl string, space, tenant uuid.UUID) int {
		switch tbl {
		case "v2.id_counters":
			return f.count(`SELECT count(*) FROM v2.id_counters WHERE tenant_id = $1`, tenant)
		case "v2.spaces":
			return 1
		case "v2.notes":
			return f.count(`SELECT count(*) FROM public.memories WHERE hub_id = $1`, space)
		}
		return f.count(`SELECT count(*) FROM `+tbl+` WHERE space_id = $1`, space)
	}

	countAs := func(spaces, tenants []uuid.UUID, tbl string) int {
		var n int
		if err := f.asV2(spaces, tenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n)
		}); err != nil {
			t.Fatalf("count %s as memax_v2: %v", tbl, err)
		}
		return n
	}
	for _, tbl := range tables {
		if n := countAs(nil, nil, tbl); n != 0 {
			t.Errorf("%s: unset scope sees %d rows, want 0", tbl, n)
		}
		if n := countAs([]uuid.UUID{}, []uuid.UUID{}, tbl); n != 0 {
			t.Errorf("%s: empty scope sees %d rows, want 0", tbl, n)
		}
		wantA, wantB := own(tbl, zzSpace, mine.TenantID), own(tbl, jySpace, theirs.TenantID)
		if wantB == 0 {
			t.Fatalf("%s: the fixture gives space B no rows, so the check would prove nothing", tbl)
		}
		if a := countAs([]uuid.UUID{zzSpace}, []uuid.UUID{mine.TenantID}, tbl); a != wantA {
			t.Errorf("%s: scope A sees %d rows, want its own %d", tbl, a, wantA)
		}
		if b := countAs([]uuid.UUID{jySpace}, []uuid.UUID{theirs.TenantID}, tbl); b != wantB {
			t.Errorf("%s: scope B sees %d rows, want its own %d", tbl, b, wantB)
		}
	}
	// One space's scope never sees the other's specific rows.
	err := f.asV2([]uuid.UUID{zzSpace}, []uuid.UUID{mine.TenantID}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.memories WHERE id = $1`, theirs.ID).Scan(&n); err != nil || n != 0 {
			return fmt.Errorf("A sees B's memory: %d %v", n, err)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.receipts WHERE space_id = $1`, jySpace).Scan(&n); err != nil || n != 0 {
			return fmt.Errorf("A sees B's receipts: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	// …nor write into it: RLS WITH CHECK refuses the row.
	err = f.asV2([]uuid.UUID{zzSpace}, []uuid.UUID{mine.TenantID}, func(tx pgx.Tx) error {
		_, err := insertReceiptSQL(tx, theirs, theirs.ID, 50)
		return err
	})
	if sqlstate(err) != "42501" {
		t.Errorf("A writing a receipt into B: %v, want an RLS violation", err)
	}
	// memax_v2 can't reach V1 tables directly; it sees spaces and notes
	// only through the scoped views.
	for _, tbl := range []string{"public.memories", "public.hubs", "public.hub_members", "public.users"} {
		err := f.asV2([]uuid.UUID{zzSpace}, nil, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT 1 FROM `+tbl+` LIMIT 1`)
			return err
		})
		if sqlstate(err) != "42501" {
			t.Errorf("memax_v2 reading %s: %v, want permission denied", tbl, err)
		}
	}

	// Through the ledger API.
	if _, err := f.l.GetMemory(ctx, zzScope, theirs.ID.String()); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("GetMemory across spaces: %v", err)
	}
	if _, err := f.l.GetMemory(ctx, zzScope, mine.ID.String()); err != nil {
		t.Errorf("GetMemory in scope: %v", err)
	}
	if _, err := f.l.ListMemories(ctx, zzScope, ledger.MemoryQuery{SpaceID: jySpace}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("ListMemories across spaces: %v", err)
	}
	if _, err := f.l.ListReceipts(ctx, zzScope, ledger.ReceiptQuery{SpaceID: jySpace}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("ListReceipts across spaces: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Edit{Meta: meta(person(zz), zzScope, policy.ViaWeb), Memory: theirs.ID.String(), ExpectedVersion: 2, Statement: "Mine now."}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("Edit across spaces: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Remember{Meta: meta(person(zz), zzScope, policy.ViaWeb), NewMemory: fact(jySpace, "Sneaky.")}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("Remember into a space outside the scope: %v", err)
	}
	// Read with an empty scope sees nothing.
	if err := f.l.Read(ctx, ledger.Scope{}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.memories`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("empty scope read %d memories", n)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
	// Read transactions are read-only.
	if err := f.l.Read(ctx, zzScope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO v2.id_counters (tenant_id, prefix, next) VALUES ($1, 'H', 1)`, mine.TenantID)
		return err
	}); sqlstate(err) != "25006" {
		t.Errorf("write inside Read: %v, want read-only transaction", err)
	}
}

// Every table in schema v2 must have RLS enabled and forced, at least
// one policy, and no DELETE or TRUNCATE for memax_v2. New tables are
// caught here.
func TestEveryV2TableIsLockedDown(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rows, err := f.pool.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid),
		       has_table_privilege('memax_v2', c.oid, 'DELETE'),
		       has_table_privilege('memax_v2', c.oid, 'TRUNCATE')
		  FROM pg_class c
		 WHERE c.relnamespace = 'v2'::regnamespace AND c.relkind = 'r'
		 ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name string
		var rls, force, del, trunc bool
		var policies int
		if err := rows.Scan(&name, &rls, &force, &policies, &del, &trunc); err != nil {
			t.Fatal(err)
		}
		n++
		if !rls || !force || policies == 0 {
			t.Errorf("v2.%s: rls=%v force=%v policies=%d; every v2 table needs ENABLE + FORCE ROW LEVEL SECURITY and a policy", name, rls, force, policies)
		}
		if del || trunc {
			t.Errorf("v2.%s: memax_v2 may DELETE=%v TRUNCATE=%v; the record is never deleted", name, del, trunc)
		}
	}
	if n < 8 {
		t.Errorf("found %d v2 tables, want at least 8", n)
	}
	var super, bypass, login bool
	if err := f.pool.QueryRow(context.Background(), `SELECT rolsuper, rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname = 'memax_v2'`).Scan(&super, &bypass, &login); err != nil {
		t.Fatal(err)
	}
	if super || bypass || login {
		t.Errorf("memax_v2: super=%v bypassrls=%v login=%v", super, bypass, login)
	}
}

// The lifecycle table and the displayed state are defined twice, in Go
// and in SQL; they must agree.
func TestLifecycleMatchesSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	from := append([]lifecycle.Lifecycle{lifecycle.None}, lifecycle.Lifecycles...)
	for _, a := range from {
		for _, b := range lifecycle.Lifecycles {
			var sqlFrom any = string(a)
			if a == lifecycle.None {
				sqlFrom = nil
			}
			var got bool
			if err := f.pool.QueryRow(ctx, `SELECT v2.lifecycle_transition_allowed($1::text, $2)`, sqlFrom, string(b)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := lifecycle.Allowed(a, b); got != want {
				t.Errorf("transition %q → %q: SQL %v, Go %v", a, b, got, want)
			}
		}
	}
	flagSets := []lifecycle.Flags{{}, {lifecycle.Conflict}, {lifecycle.Stale}, {lifecycle.Conflict, lifecycle.Stale}}
	for _, l := range lifecycle.Lifecycles {
		for _, fs := range flagSets {
			var got string
			if err := f.pool.QueryRow(ctx, `SELECT v2.display_state($1, $2)`, string(l), fs.Strings()).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := lifecycle.DisplayState(l, fs); got != string(want) {
				t.Errorf("display_state(%s, %v): SQL %s, Go %s", l, fs, got, want)
			}
		}
	}
}

// The SQL guard refuses a disallowed transition even with a receipt,
// and a forgotten memory can't change at all.
func TestLifecycleGuardInSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "Kept things don't become proposals again.")
	ctx := context.Background()
	move := func(sql string, version int) error {
		return f.asV2([]uuid.UUID{space}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
			rid, err := insertReceiptSQL(tx, m, m.ID, version)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, sql, m.ID, rid, version)
			return err
		})
	}
	if err := move(`UPDATE v2.memories SET lifecycle = 'proposed', last_receipt_id = $2, stream_version = $3 WHERE id = $1`, 2); sqlstate(err) != "MXL01" {
		t.Errorf("kept → proposed: %v, want MXL01", err)
	}
	if err := move(`UPDATE v2.memories SET lifecycle = 'forgotten', last_receipt_id = $2, stream_version = $3, search = NULL WHERE id = $1`, 2); err != nil {
		t.Fatalf("kept → forgotten: %v", err)
	}
	if err := move(`UPDATE v2.memories SET section = 'decisions', last_receipt_id = $2, stream_version = $3 WHERE id = $1`, 3); sqlstate(err) != "MXL01" {
		t.Errorf("changing a forgotten memory: %v, want MXL01", err)
	}
	// Forgotten rows can't carry derived words either.
	if _, err := f.pool.Exec(ctx, `UPDATE v2.memories SET search = to_tsvector('simple', 'words') WHERE id = $1`, m.ID); sqlstate(err) != "23514" && sqlstate(err) != "MXL01" {
		t.Errorf("re-indexing a forgotten memory: %v, want refused", err)
	}
}
