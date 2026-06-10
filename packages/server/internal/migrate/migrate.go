package migrate

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type Options struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
}

// Run applies all pending migrations from migrationsDir against the database at connStr.
// Uses table-based locking so a concurrent/stale release_command fails fast
// instead of waiting indefinitely on pg_advisory_lock inside golang-migrate's
// pgx/v5 driver. Retries on lock contention before returning an error.
func Run(connStr string, migrationsDir string) error {
	return RunWithOptions(connStr, migrationsDir, Options{
		LockTimeout:      15 * time.Second,
		StatementTimeout: 0,
	})
}

// RunWithOptions applies all pending migrations with explicit PostgreSQL session timeouts.
// statement_timeout bounds the execution time of a single SQL statement, which
// keeps expensive DDL from appearing to hang indefinitely in release_command
// logs. App-level migration locking uses golang-migrate's table lock strategy
// so concurrent release_command instances fail fast with ErrLocked.
func RunWithOptions(connStr string, migrationsDir string, opts Options) error {
	lockTimeout := opts.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = 15 * time.Second
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 5 * time.Second
			slog.Info("retrying migrations after lock timeout", "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		err := runOnce(connStr, migrationsDir, Options{
			LockTimeout:      lockTimeout,
			StatementTimeout: opts.StatementTimeout,
		})
		if err == nil {
			return nil
		}

		// Retry on advisory lock timeout or contention
		if isLockError(err) {
			lastErr = err
			continue
		}

		return err
	}
	return fmt.Errorf("migrations failed after 3 attempts: %w", lastErr)
}

func runOnce(connStr string, migrationsDir string, opts Options) error {
	migrationURL, err := url.Parse(connStr)
	if err != nil {
		return fmt.Errorf("parse migration database URL: %w", err)
	}

	// Use the pgx/v4 migrate driver because it supports x-lock-strategy=table.
	// The pgx/v5 migrate driver always uses pg_advisory_lock on context.Background(),
	// which can hang forever when another release_command is stuck.
	switch migrationURL.Scheme {
	case "postgres":
		migrationURL.Scheme = "pgx"
	case "postgresql":
		migrationURL.Scheme = "pgx"
	case "pgx", "pgx4":
		// already usable
	default:
		return fmt.Errorf("unsupported migration database URL scheme: %s", migrationURL.Scheme)
	}

	q := migrationURL.Query()
	q.Set("lock_timeout", fmt.Sprintf("%d", opts.LockTimeout.Milliseconds()))
	q.Set("x-lock-strategy", "table")
	if opts.StatementTimeout > 0 {
		q.Set("statement_timeout", fmt.Sprintf("%d", opts.StatementTimeout.Milliseconds()))
		q.Set("x-statement-timeout", fmt.Sprintf("%d", opts.StatementTimeout.Milliseconds()))
	}
	migrationURL.RawQuery = q.Encode()

	m, err := migrate.New(
		fmt.Sprintf("file://%s", migrationsDir),
		migrationURL.String(),
	)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("read migration version: %w", err)
	}
	if err == migrate.ErrNilVersion {
		slog.Info("migrations: starting from empty schema")
	} else {
		slog.Info("migrations: current version", "version", version, "dirty", dirty)
	}

	// Auto-recover from dirty state: force the version back so the
	// migrator retries from the failed migration. This handles the
	// case where a previous deploy's migration partially applied and
	// crashed, leaving dirty=true.
	if dirty {
		slog.Warn("migrations: dirty state detected, forcing version to retry",
			"version", version)
		if forceErr := m.Force(int(version) - 1); forceErr != nil {
			return fmt.Errorf("force version after dirty state: %w", forceErr)
		}
		slog.Info("migrations: forced to previous version, retrying up")
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}

	if err == migrate.ErrNoChange {
		slog.Info("migrations: already up to date")
	} else {
		slog.Info("migrations: applied successfully")
	}
	return nil
}

func isLockError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "deadlock") ||
		strings.Contains(msg, "advisory_lock") ||
		strings.Contains(msg, "lock timeout") ||
		strings.Contains(msg, "lock_timeout") ||
		strings.Contains(msg, "canceling statement due to lock timeout") ||
		(strings.Contains(msg, "lock") && strings.Contains(msg, "failed"))
}
