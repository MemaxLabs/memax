// Standalone migration runner — used as Fly.io release_command.
//
// Runs app migrations (golang-migrate) and River queue migrations
// to completion before any new instances start. This prevents advisory
// lock contention between old and new instances during rolling deploys.
package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbmigrate "github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "/migrations"
	}

	lockTimeout := readDurationFromEnv("MIGRATION_LOCK_TIMEOUT_MS", 15*time.Second)
	statementTimeout := readDurationFromEnv("MIGRATION_STATEMENT_TIMEOUT_MS", 4*time.Minute)
	connectTimeout := readDurationFromEnv("MIGRATION_CONNECT_TIMEOUT_MS", 30*time.Second)
	riverTimeout := readDurationFromEnv("MIGRATION_RIVER_TIMEOUT_MS", 30*time.Second)
	slog.Info(
		"migration timeouts configured",
		"lock_timeout", lockTimeout,
		"statement_timeout", statementTimeout,
		"connect_timeout", connectTimeout,
		"river_timeout", riverTimeout,
	)

	connectCtx, connectCancel := context.WithTimeout(context.Background(), connectTimeout)
	defer connectCancel()

	pool, err := pgxpool.New(connectCtx, dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	if err := pool.Ping(connectCtx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to PostgreSQL")

	// App migrations (golang-migrate) — uses lock_timeout=15s per attempt,
	// 3 attempts with backoff = max ~60s total
	slog.Info("running app migrations", "dir", migrationsDir)
	if err := dbmigrate.RunWithOptions(dbURL, migrationsDir, dbmigrate.Options{
		LockTimeout:      lockTimeout,
		StatementTimeout: statementTimeout,
	}); err != nil {
		slog.Error("app migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("app migrations complete")

	// River queue migrations — uses its own advisory lock with context deadline
	riverCtx, riverCancel := context.WithTimeout(context.Background(), riverTimeout)
	defer riverCancel()
	slog.Info("running River migrations")
	if err := queue.RunMigrations(riverCtx, pool); err != nil {
		slog.Error("River migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("all migrations complete")

	// Intentionally skip pool.Close() in this short-lived release binary.
	// pgxpool.Close blocks until every checked-out connection is returned, and
	// Fly's release_command only cares that the process exits cleanly once the
	// migration work is done. Letting process exit tear down any remaining
	// connections avoids shutdown hangs masking successful migration completion.
}

func readDurationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		slog.Warn("invalid migration timeout env, using fallback", "key", key, "value", raw, "fallback", fallback)
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}
