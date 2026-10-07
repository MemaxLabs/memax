package migrate

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb/catalock"
)

// baseURL mirrors internal/testdb's resolution so this test file
// can stand up its own template DB without importing testdb (which
// would introduce a package cycle at the server level).
func migrateTestBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	return "postgres://memax:memax@postgres:5432/memax?sslmode=disable"
}

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// withFreshDB creates a uniquely named empty database, returns its
// connection string, and registers cleanup to drop it.
func withFreshDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, migrateTestBaseURL())
	if err != nil {
		t.Skipf("migrate_run_test: Postgres unavailable (%v)", err)
	}
	t.Cleanup(admin.Close)

	dbName := fmt.Sprintf("memax_migtest_%d_%d", time.Now().UnixNano(), rand.Int64N(1_000_000))
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", dbName)); err != nil {
		t.Skipf("migrate_run_test: CREATE DATABASE failed (%v)", err)
	}
	t.Cleanup(func() {
		termCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.Exec(termCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName,
		)
		_ = catalock.DropDatabase(termCtx, admin, dbName)
	})

	// Build the connection string for the new DB by replacing the
	// path segment of the base URL.
	cs := migrateTestBaseURL()
	if idx := strings.LastIndex(cs, "/"); idx > 0 {
		// Preserve query string if any.
		pathEnd := strings.Index(cs[idx:], "?")
		if pathEnd < 0 {
			return cs[:idx+1] + dbName
		}
		return cs[:idx+1] + dbName + cs[idx+pathEnd:]
	}
	return cs
}

func TestRunAppliesAllMigrations(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify: schema_migrations exists and plans table is seeded.
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM plans").Scan(&count); err != nil {
		t.Fatalf("select plans: %v", err)
	}
	if count == 0 {
		t.Error("plans table should be seeded by migration 001")
	}
}

func TestRunIdempotent(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Second run must succeed with "already up to date" (migrate.ErrNoChange
	// is mapped to a non-error return in runOnce).
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatalf("second Run should be no-op, got %v", err)
	}
}

func TestRunRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()
	// mysql:// scheme should bounce — this package only supports
	// pgx/postgres.
	err := Run("mysql://u:p@host/db", migrationsDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("want unsupported-scheme error, got %v", err)
	}
}

func TestRunRejectsUnparseableURL(t *testing.T) {
	t.Parallel()
	// Bytes below 0x20 break url.Parse.
	err := Run(string([]byte{0x01}), migrationsDir())
	if err == nil {
		t.Error("unparseable URL should error")
	}
}

func TestRunRejectsBadMigrationsDir(t *testing.T) {
	cs := withFreshDB(t)
	err := Run(cs, "/nonexistent/path")
	if err == nil {
		t.Error("bad migrations dir should error")
	}
}

func TestIsLockError(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"deadlock detected":                       true,
		"pg_advisory_lock failed":                 true,
		"canceling statement due to lock timeout": true,
		"lock_timeout exceeded":                   true,
		"some lock failed to acquire":             true,
		"completely unrelated error":              false,
		"":                                        false,
	}
	for msg, want := range cases {
		got := isLockError(errors.New(msg))
		if got != want {
			t.Errorf("isLockError(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestRunWithOptionsClampsLockTimeout(t *testing.T) {
	// Zero LockTimeout should fall through to the 15s default.
	// Can't easily observe the default propagation, but we CAN
	// verify the function runs cleanly with the zero-value path.
	cs := withFreshDB(t)
	if err := RunWithOptions(cs, migrationsDir(), Options{}); err != nil {
		t.Fatalf("zero-value options should default cleanly, got %v", err)
	}
}

// A release deployed back over a database a newer one migrated (a
// rollback) finds the database ahead of its files: its release step must
// succeed and change nothing, or the rollback can't deploy. Dirty and
// ahead, it refuses: only the newer release can repair that.
func TestRunAcceptsADatabaseAheadOfItsFiles(t *testing.T) {
	cs := withFreshDB(t)
	if err := Run(cs, migrationsDir()); err != nil {
		t.Fatal(err)
	}
	older := t.TempDir()
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0") && e.Name() < "024_" {
			b, err := os.ReadFile(filepath.Join(migrationsDir(), e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(older, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var before int
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Run(cs, older); err != nil {
		t.Fatalf("an older release's migrations over a newer database: %v", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || before <= 23 {
		t.Fatalf("version %d, then %d; want it unchanged and above 23", before, after)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET dirty = true`); err != nil {
		t.Fatal(err)
	}
	if err := Run(cs, older); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty and ahead: %v, want a refusal", err)
	}
}
