// Package testdb provides shared integration-test infrastructure: a
// real PostgreSQL database, migrated, isolated per test.
//
// # Why this package exists
//
// The server's `store` layer is pure SQL against Postgres, and the
// `handler` layer's business logic is tightly coupled to the store.
// Mocking these out loses the things most likely to break in
// production — constraint violations, transaction semantics, JSONB
// merge operators, cascade deletes, cursor pagination. Every bug
// that shipped unit-tested but broke integration was caught by a
// real-DB test, not by a mock.
//
// This package makes "real Postgres" cheap enough to use on every
// package's test run:
//
//   - One shared template database per `go test` process, migrated
//     once at first call (fast: golang-migrate is idempotent)
//   - Each Acquire call clones the template via `CREATE DATABASE
//     ... TEMPLATE` — near-zero cost on Postgres (a few ms), but
//     gives the test a completely isolated DB so it can write and
//     truncate freely without stepping on siblings
//   - Cleanup drops the clone at test end
//
// # Usage
//
//	func TestSomething(t *testing.T) {
//		s, pool := testdb.Acquire(t)
//		defer pool.Close()
//		// Drive store methods against s — real Postgres, real FKs,
//		// real constraints. Whatever you write gets dropped at the
//		// end of the test.
//	}
//
// # Skipping when Postgres isn't available
//
// If the `TEST_DATABASE_URL` env var is unset AND the default
// `postgres://memax:memax@postgres:5432/memax` connection fails,
// `Acquire` calls `t.Skip("...")` — the test is silently skipped
// rather than failing. This keeps `go test ./...` green on
// machines without docker-compose up.
//
// For CI, set TEST_DATABASE_URL so the tests actually run. The
// devcontainer already has a live Postgres at the default URL, so
// no env setup is needed there.
package testdb

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// defaultBaseURL matches the docker-compose service name. Override
// with TEST_DATABASE_URL for CI or a different local setup.
const defaultBaseURL = "postgres://memax:memax@postgres:5432/memax?sslmode=disable"

var (
	// templateOnce guards the one-time template-DB creation +
	// migrations. Every subsequent Acquire clones from the
	// already-migrated template, so migration cost is paid once
	// per `go test` process rather than per test.
	templateOnce sync.Once
	templateName string
	templateErr  error

	// adminPool stays around for CREATE DATABASE / DROP DATABASE
	// commands — those can't run inside a transaction and can't
	// target the connected DB, so we need a persistent admin
	// connection to the default `memax` DB to issue them.
	adminPool     *pgxpool.Pool
	adminPoolOnce sync.Once
	adminPoolErr  error
)

// baseURL returns the connection string for the administrative
// database used to create and drop per-test clones. Prefers
// TEST_DATABASE_URL for explicit override, then DATABASE_URL (so
// existing tests that already look at this env var work
// unchanged), then falls back to the compose default.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	return defaultBaseURL
}

// Acquire returns a migrated PostgresStore backed by a fresh
// per-test database. Registers a cleanup that closes the pool and
// drops the database at test end.
//
// If Postgres isn't reachable, Acquire calls t.Skip — the test
// silently skips rather than failing, matching the existing
// postgres_*_test.go pattern.
func Acquire(t *testing.T) (store.Store, *pgxpool.Pool) {
	t.Helper()
	return AcquireWithContext(t, context.Background())
}

// AcquireWithContext is Acquire with an explicit context for
// connection timeouts. The returned cleanup tears down the test
// database; callers should NOT defer pool.Close separately — the
// cleanup already handles it.
func AcquireWithContext(t *testing.T, ctx context.Context) (store.Store, *pgxpool.Pool) {
	t.Helper()

	template, err := ensureTemplate(ctx)
	if err != nil {
		t.Skipf("testdb: Postgres unavailable (%v) — skipping integration test", err)
	}

	dbName := fmt.Sprintf("memax_test_%d_%d", time.Now().UnixNano(), rand.Int64N(1_000_000))

	admin, err := getAdminPool(ctx)
	if err != nil {
		t.Skipf("testdb: admin pool unavailable (%v)", err)
	}
	// Use CREATE DATABASE ... TEMPLATE to clone the migrated
	// template at near-zero cost. Postgres copies catalog entries
	// rather than re-running DDL, so even with ~20 migrations the
	// clone completes in milliseconds. Needs a connection that
	// isn't using the template (else CREATE DATABASE fails with
	// "source database is being accessed by other users").
	createSQL := fmt.Sprintf("CREATE DATABASE %q TEMPLATE %q", dbName, template)
	if _, err := admin.Exec(ctx, createSQL); err != nil {
		t.Fatalf("testdb: CREATE DATABASE failed: %v", err)
	}

	// Open a pool against the new DB and hand it to the caller.
	pool, err := pgxpool.New(ctx, connStringFor(dbName))
	if err != nil {
		_, _ = admin.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %q", dbName))
		t.Fatalf("testdb: pgxpool.New for clone: %v", err)
	}

	// Register teardown BEFORE returning so a panic/fail later
	// still releases the database.
	t.Cleanup(func() {
		pool.Close()
		// Postgres refuses DROP DATABASE if any session is
		// connected to the target. Our pool is already closed
		// here so the normal case succeeds, but terminate any
		// stragglers defensively before dropping.
		termCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.Exec(termCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName,
		)
		if _, err := admin.Exec(termCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %q", dbName)); err != nil {
			t.Logf("testdb: DROP DATABASE %q failed (non-fatal): %v", dbName, err)
		}
	})

	return store.NewPostgresStore(pool), pool
}

// ensureTemplate creates and migrates the shared template DB
// exactly once per process. Returns the template DB name (or an
// error if Postgres isn't reachable).
func ensureTemplate(ctx context.Context) (string, error) {
	templateOnce.Do(func() {
		admin, err := getAdminPool(ctx)
		if err != nil {
			templateErr = err
			return
		}
		// Per-process unique name so parallel `go test` invocations
		// (rare but possible with `-run ...` over multiple packages
		// in parallel) don't clobber each other's templates.
		templateName = fmt.Sprintf("memax_tpl_%d_%d", time.Now().UnixNano(), rand.Int64N(1_000_000))

		if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", templateName)); err != nil {
			templateErr = fmt.Errorf("create template DB: %w", err)
			return
		}

		// Run migrations against the template. Subsequent clones
		// inherit the fully-migrated schema.
		if err := migrate.Run(connStringFor(templateName), findMigrationsDir()); err != nil {
			// Best-effort cleanup if migrations fail.
			_, _ = admin.Exec(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %q", templateName))
			templateErr = fmt.Errorf("migrate template: %w", err)
			return
		}

		// Also run River's own migrations so river_job /
		// river_client / river_leader / etc. exist in the template.
		// Tests that query these tables (admin ops, queue readiness
		// probes) would otherwise see "relation does not exist"
		// errors. River migrations are idempotent + advisory-
		// locked.
		if err := runRiverMigrations(ctx, connStringFor(templateName)); err != nil {
			_, _ = admin.Exec(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %q", templateName))
			templateErr = fmt.Errorf("migrate river template: %w", err)
			return
		}

		// Mark as template so accidental connections during test
		// runs (which should only happen via CREATE DATABASE ...
		// TEMPLATE) get rejected cleanly. Not strictly required
		// but makes misuse loud.
		if _, err := admin.Exec(ctx, fmt.Sprintf("ALTER DATABASE %q IS_TEMPLATE true", templateName)); err != nil {
			templateErr = fmt.Errorf("mark template: %w", err)
			return
		}
	})
	return templateName, templateErr
}

// getAdminPool returns a lazily-initialized pool against the default
// Postgres database, used for CREATE/DROP DATABASE statements. Kept
// across all tests in the process so we pay the connect cost once.
func getAdminPool(ctx context.Context) (*pgxpool.Pool, error) {
	adminPoolOnce.Do(func() {
		connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		pool, err := pgxpool.New(connCtx, baseURL())
		if err != nil {
			adminPoolErr = fmt.Errorf("connect admin pool: %w", err)
			return
		}
		if err := pool.Ping(connCtx); err != nil {
			pool.Close()
			adminPoolErr = fmt.Errorf("ping admin pool: %w", err)
			return
		}
		adminPool = pool
	})
	return adminPool, adminPoolErr
}

// connStringFor returns a copy of the base URL with the path
// segment (database name) replaced. Keeps all other connection
// params (sslmode, etc.) exactly as configured in TEST_DATABASE_URL
// / DATABASE_URL / the default.
func connStringFor(dbName string) string {
	u, err := url.Parse(baseURL())
	if err != nil {
		// baseURL is either from the env (user-controlled and we
		// already connected with it successfully) or the hardcoded
		// default — parse failure here is "can't happen" but we
		// degrade to a best-effort replace rather than panic.
		return strings.Replace(baseURL(), "/memax", "/"+dbName, 1)
	}
	u.Path = "/" + dbName
	return u.String()
}

// findMigrationsDir locates the server package's migrations/
// directory relative to this file. Tests running from any
// sub-package need an absolute path because golang-migrate's file
// source interprets relative paths against os.Getwd, which varies
// by test package.
func findMigrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = .../packages/server/internal/testdb/testdb.go
	// migrations dir = .../packages/server/migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// runRiverMigrations executes River's schema migrations against the
// template DB. Uses rivermigrate directly rather than the queue
// package to avoid an import cycle (queue imports store, store would
// import testdb for integration fixtures).
func runRiverMigrations(ctx context.Context, dsn string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("river pool: %w", err)
	}
	defer pool.Close()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("rivermigrate.New: %w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}
