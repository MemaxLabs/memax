package migrate

import (
	"context"
	"errors"
	"net/url"
	"testing"

	gomigrate "github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if err := m.Migrate(lastV1Version); err != nil {
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
