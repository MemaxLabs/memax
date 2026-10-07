package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	gomigrate "github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb/catalock"
)

// lastV1Version is the newest migration before the V2 record
// (025 drops reviews; 026–028 add schema v2). Bump it only if a V1
// migration is inserted before them.
const lastV1Version = 24

// TestV2MigrationsRoundTrip migrates up, rolls the V2 migrations back,
// and migrates up again: every V2 down migration must undo its up
// migration exactly, so the second up succeeds on the same database.
func TestV2MigrationsRoundTrip(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	state := func() (v2Schema, spaceKind, reviews bool) {
		t.Helper()
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'v2'),
			       EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'hubs' AND column_name = 'space_kind'),
			       to_regclass('public.reviews') IS NOT NULL`).Scan(&v2Schema, &spaceKind, &reviews)
		if err != nil {
			t.Fatalf("inspect schema: %v", err)
		}
		return
	}
	if v2, kind, reviews := state(); !v2 || !kind || reviews {
		t.Fatalf("after up: v2=%v space_kind=%v reviews=%v", v2, kind, reviews)
	}
	// Seed a hub so the space columns are dropped and re-backfilled
	// over real rows.
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('11111111-1111-1111-1111-111111111111', 'rt@test', 'rt');
		INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES
			('22222222-2222-2222-2222-222222222222', 'P', 'rt-p', 'personal', '11111111-1111-1111-1111-111111111111')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := newMigrator(t, cs)
	if err := migrateDownTo(t, m, lastV1Version); err != nil {
		t.Fatalf("migrate down to %03d: %v", lastV1Version, err)
	}
	if v2, kind, reviews := state(); v2 || kind || !reviews {
		t.Fatalf("after down: v2=%v space_kind=%v reviews=%v; a down migration left something behind", v2, kind, reviews)
	}
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("migrate up again: %v", err)
	}
	if v2, kind, reviews := state(); !v2 || !kind || reviews {
		t.Fatalf("after second up: v2=%v space_kind=%v reviews=%v", v2, kind, reviews)
	}
	var tenant string
	if err := pool.QueryRow(ctx, `SELECT tenant_id::text FROM hubs WHERE id = '22222222-2222-2222-2222-222222222222'`).Scan(&tenant); err != nil || tenant != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("backfilled tenant = %q, %v", tenant, err)
	}
}

// briefTargetsVersion is migration 031 (the Brief, targets and compile
// runs).
const briefTargetsVersion = 31

// TestBriefTargetsMigrationStepsBack rolls back only 031 while receipts
// written by its commands exist: receipts are append-only, so the down
// migration keeps them, and restores 029's action CHECK as NOT VALID.
func TestBriefTargetsMigrationStepsBack(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('11111111-1111-1111-1111-111111111111', 'rt@test', 'rt');
		INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES
			('22222222-2222-2222-2222-222222222222', 'P', 'rt-p', 'personal', '11111111-1111-1111-1111-111111111111');
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
		VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
		        'brief', '33333333-3333-3333-3333-333333333333', 'B-0001', 'revised', 'memax', 'system', now(),
		        '33333333-3333-3333-3333-333333333333', 1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tables := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'v2'
		    AND tablename IN ('briefs', 'brief_versions', 'targets', 'compile_runs', 'target_observations')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := tables(); n != 5 {
		t.Fatalf("after up: %d of the 031 tables", n)
	}
	m := newMigrator(t, cs)
	if err := migrateDownTo(t, m, briefTargetsVersion-1); err != nil {
		t.Fatalf("migrate down to %03d: %v", briefTargetsVersion-1, err)
	}
	if n := tables(); n != 0 {
		t.Errorf("after down: %d of the 031 tables remain", n)
	}
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts WHERE action = 'revised'`).Scan(&receipts); err != nil || receipts != 1 {
		t.Errorf("the revised receipt: %d, %v; receipts are never dropped", receipts, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
		VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
		        'brief', '33333333-3333-3333-3333-333333333333', 'B-0002', 'revised', 'memax', 'system', now(),
		        '33333333-3333-3333-3333-333333333333', 2)`); err == nil {
		t.Error("029's action CHECK isn't back: a new 'revised' receipt was accepted")
	}
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("migrate up again: %v", err)
	}
	if n := tables(); n != 5 {
		t.Errorf("after second up: %d of the 031 tables", n)
	}
}

// decisionGatesVersion is migration 036 (decision gates).
const decisionGatesVersion = 36

// TestDecisionGatesMigrationStepsBack rolls back only 036 while a gate's
// receipt exists: the table goes, the receipt stays (receipts are
// append-only), and 035's action CHECK comes back NOT VALID, so a new
// `asked` receipt is refused until 036 is applied again.
func TestDecisionGatesMigrationStepsBack(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	asked := func(ref string, version int) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
			        'gate', '33333333-3333-3333-3333-333333333333', $1, 'asked', 'agent', gen_random_uuid(), 'mcp', now(),
			        '33333333-3333-3333-3333-333333333333', $2)`, ref, version)
		return err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('11111111-1111-1111-1111-111111111111', 'rt@test', 'rt');
		INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES
			('22222222-2222-2222-2222-222222222222', 'P', 'rt-p', 'personal', '11111111-1111-1111-1111-111111111111')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := asked("G-0001", 1); err != nil {
		t.Fatalf("an asked receipt after 036: %v", err)
	}
	gates := func() bool {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('v2.decision_gates') IS NOT NULL`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	m := newMigrator(t, cs)
	if err := migrateDownTo(t, m, decisionGatesVersion-1); err != nil {
		t.Fatalf("migrate down to %03d: %v", decisionGatesVersion-1, err)
	}
	if gates() {
		t.Error("after down: v2.decision_gates remains")
	}
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts WHERE action = 'asked'`).Scan(&receipts); err != nil || receipts != 1 {
		t.Errorf("the asked receipt: %d, %v; receipts are never dropped", receipts, err)
	}
	if err := asked("G-0002", 2); err == nil {
		t.Error("035's action CHECK isn't back: a new 'asked' receipt was accepted")
	}
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("migrate up again: %v", err)
	}
	if !gates() {
		t.Error("after second up: no v2.decision_gates")
	}
	if err := asked("G-0002", 2); err != nil {
		t.Errorf("an asked receipt after 036 again: %v", err)
	}
}

// rule11Version is migration 042 (the judge's return to Review, and
// "keep both" drafts). Renumber it with the file if a merge moves it.
const rule11Version = 42

// TestRule11MigrationStepsBack rolls back only 042 while a `returned`
// receipt exists: the receipt stays (receipts are append-only), 036's
// action CHECK and 035's lifecycle guard come back, so a new `returned`
// receipt is refused, and 042 applies again on top.
func TestRule11MigrationStepsBack(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	returned := func(version int) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
			VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
			        'memory', '33333333-3333-3333-3333-333333333333', 'M-0001', 'returned', 'memax', 'system', now(),
			        '33333333-3333-3333-3333-333333333333', $1)`, version)
		return err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('11111111-1111-1111-1111-111111111111', 'rt@test', 'rt');
		INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES
			('22222222-2222-2222-2222-222222222222', 'P', 'rt-p', 'personal', '11111111-1111-1111-1111-111111111111')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := returned(1); err != nil {
		t.Fatalf("a returned receipt after 042: %v", err)
	}
	returnFunc := func() bool {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regproc('v2.lifecycle_return_allowed') IS NOT NULL`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	m := newMigrator(t, cs)
	if err := migrateDownTo(t, m, rule11Version-1); err != nil {
		t.Fatalf("migrate down to %03d: %v", rule11Version-1, err)
	}
	if returnFunc() {
		t.Error("after down: v2.lifecycle_return_allowed remains")
	}
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts WHERE action = 'returned'`).Scan(&receipts); err != nil || receipts != 1 {
		t.Errorf("the returned receipt: %d, %v; receipts are never dropped", receipts, err)
	}
	if err := returned(2); err == nil {
		t.Error("036's action CHECK isn't back: a new 'returned' receipt was accepted")
	}
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("migrate up again: %v", err)
	}
	if !returnFunc() {
		t.Error("after second up: no v2.lifecycle_return_allowed")
	}
	if err := returned(2); err != nil {
		t.Errorf("a returned receipt after 042 again: %v", err)
	}
}

func newMigrator(t *testing.T, cs string) *gomigrate.Migrate {
	t.Helper()
	u, err := url.Parse(cs)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Scheme = "pgx" // the driver runOnce uses
	m, err := gomigrate.New("file://"+migrationsDir(), u.String())
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

// migrateDownTo steps down one migration at a time to version, each
// step holding the catalog lock alone (catalock): a down migration that
// drops a role scans every database's catalog, and another test package
// dropping its database mid-scan made it fail with "cache lookup failed
// for database".
func migrateDownTo(t *testing.T, m *gomigrate.Migrate, version uint) error {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, migrateTestBaseURL())
	if err != nil {
		return fmt.Errorf("admin pool: %w", err)
	}
	defer admin.Close()
	for {
		cur, _, err := m.Version()
		if err != nil {
			return err
		}
		if cur <= version {
			return nil
		}
		release, err := catalock.Exclusive(ctx, admin)
		if err != nil {
			return fmt.Errorf("catalog lock: %w", err)
		}
		err = m.Steps(-1)
		release()
		if err != nil {
			return fmt.Errorf("down from %03d: %w", cur, err)
		}
	}
}
