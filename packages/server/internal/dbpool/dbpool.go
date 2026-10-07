// Package dbpool opens the process's Postgres pool with a size that fits
// its concurrency. pgxpool's default is max(4, NumCPU), so a 2-CPU Fly
// machine got 4 connections: the API's parallel recall lanes and the
// worker's 23 River workers queued for them. Production connects through
// Neon's pooler (PgBouncer, transaction mode), which takes thousands of
// client connections, so a larger pool costs little.
package dbpool

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Defaults when neither the connection string (pool_max_conns) nor
// DB_MAX_CONNS says otherwise.
const (
	// APIMaxConns covers concurrent requests, each of which may run a few
	// statements side by side (recall's lanes, a principal and its hubs).
	APIMaxConns = 20
	// WorkerMaxConns covers River's workers (20 default + 3 dreams) plus
	// its producer, leader election and periodic jobs.
	WorkerMaxConns = 32
)

// Open connects a pool. The size comes from the connection string's
// pool_max_conns if present, else DB_MAX_CONNS, else defaultMax.
func Open(ctx context.Context, url string, defaultMax int32) (*pgxpool.Pool, error) {
	cfg, err := Config(url, defaultMax)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Config is Open's pool configuration, for callers and tests that need
// it before connecting.
func Config(url string, defaultMax int32) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("dbpool: parse DATABASE_URL: %w", err)
	}
	if strings.Contains(url, "pool_max_conns=") {
		return cfg, nil
	}
	size := defaultMax
	if raw := strings.TrimSpace(os.Getenv("DB_MAX_CONNS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("dbpool: DB_MAX_CONNS=%q: want a positive integer", raw)
		}
		size = int32(n)
	}
	cfg.MaxConns = size
	return cfg, nil
}
