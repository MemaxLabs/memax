package migrate

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb/catalock"
)

// v2Roles are the NOLOGIN roles the V2 migrations create and switch to.
var v2Roles = []string{"memax_v2", "memax_v2_sealer", "memax_v2_compile_sweeper", "memax_v2_dream_sweeper", "memax_v2_metrics"}

// Staging and production run every migration as Neon's owner role, which
// isn't a superuser: it has CREATEROLE, CREATEDB and BYPASSRLS; it owns
// schema public, which grants nothing to PUBLIC (where the extensions
// live); and a role it creates comes with ADMIN only, not SET or INHERIT
// (Postgres 16). Three things passed as a local superuser and failed there
// (a function's own SET of app.sweep, SET ROLE memax_v2, and memax_v2
// resolving the extensions' names in public), so this runs every migration
// as a role set up the same way and then uses what they made.
func TestMigrationsRunAsNeonsOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	base := migrateTestBaseURL()
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Skipf("Postgres unavailable (%v)", err)
	}
	t.Cleanup(admin.Close)

	owner := fmt.Sprintf("memax_neonlike_%d", rand.Int64N(1_000_000_000))
	const password = "neonlike"
	// Registered before the database's cleanup, so it runs after it: the
	// role owns the database's objects until the database is gone. DROP
	// ROLE reads every database's catalog, so it holds catalock alone.
	t.Cleanup(func() {
		dctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		release, err := catalock.Exclusive(dctx, admin)
		if err != nil {
			t.Logf("drop %s: %v", owner, err)
			return
		}
		defer release()
		if _, err := admin.Exec(dctx, fmt.Sprintf("DROP ROLE IF EXISTS %q", owner)); err != nil {
			t.Logf("drop %s: %v", owner, err)
		}
	})
	if _, err := admin.Exec(ctx, fmt.Sprintf(
		"CREATE ROLE %q LOGIN PASSWORD '%s' NOSUPERUSER CREATEROLE CREATEDB BYPASSRLS", owner, password)); err != nil {
		t.Fatal(err)
	}
	cs := withFreshDB(t)

	// The database as Neon makes it: the owner role owns it and schema
	// public, PUBLIC may not use public, and the extensions are there.
	dbAdmin, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dbAdmin.Close)
	dbName := cs[strings.LastIndex(cs, "/")+1:]
	if i := strings.Index(dbName, "?"); i >= 0 {
		dbName = dbName[:i]
	}
	for _, stmt := range []string{
		fmt.Sprintf("ALTER DATABASE %q OWNER TO %q", dbName, owner),
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"CREATE EXTENSION IF NOT EXISTS pgcrypto",
		"CREATE EXTENSION IF NOT EXISTS unaccent",
		fmt.Sprintf("ALTER SCHEMA public OWNER TO %q", owner),
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
	} {
		if _, err := dbAdmin.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// Roles are cluster-wide and other tests' migrations made them; give
	// the owner what Neon gives the role that creates one: ADMIN alone.
	for _, r := range v2Roles {
		if _, err := admin.Exec(ctx, fmt.Sprintf(`DO $$ BEGIN
			CREATE ROLE %[1]q NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
		EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$`, r)); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, fmt.Sprintf("GRANT %q TO %q WITH ADMIN TRUE, INHERIT FALSE, SET FALSE", r, owner)); err != nil {
			t.Fatal(err)
		}
	}

	u, err := url.Parse(cs)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(owner, password)
	ownerURL := u.String()
	if err := RunWithOptions(ownerURL, migrationsDir(), Options{LockTimeout: 30 * time.Second, StatementTimeout: 2 * time.Minute}); err != nil {
		t.Fatalf("migrations as a Neon-like owner: %v", err)
	}

	pool, err := pgxpool.New(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, c := range []struct{ name, role, sql string }{
		{"memax_v2 resolves the extensions in public", "memax_v2",
			`SELECT similarity('pnpm', 'npm')::text || unaccent('é') || ('[1,2]'::halfvec)::text`},
		{"the compile sweeper reads through v2.dirty_targets", "memax_v2_compile_sweeper",
			`SELECT count(*)::text FROM v2.dirty_targets(10)`},
		{"the sealer lists through v2.receipt_spaces", "memax_v2_sealer",
			`SELECT count(*)::text FROM v2.receipt_spaces()`},
		{"memax_v2 calls a definer function that sets app.sweep", "memax_v2",
			`SELECT coalesce(v2.passkey_owner('\x00'::bytea)::text, 'none') || coalesce(current_setting('app.sweep', true), '')`},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out string
		if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, c.role); err != nil {
			t.Errorf("%s: SET ROLE %s: %v", c.name, c.role, err)
		} else if err := tx.QueryRow(ctx, c.sql).Scan(&out); err != nil {
			t.Errorf("%s: %v", c.name, err)
		} else if c.role == "memax_v2" && strings.HasPrefix(out, "none") && out != "none" {
			t.Errorf("%s: app.sweep left set to %q after the call", c.name, strings.TrimPrefix(out, "none"))
		}
		_ = tx.Rollback(ctx)
	}
}
