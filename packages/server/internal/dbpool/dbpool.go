// Package dbpool opens the process's Postgres pool with a size that fits
// its concurrency. pgxpool's default is max(4, NumCPU), so a 2-CPU Fly
// machine got 4 connections: the API's parallel recall lanes and the
// worker's 23 River workers queued for them. Production connects through
// Neon's pooler (PgBouncer, transaction mode), which takes thousands of
// client connections, so a larger pool costs little.
//
// The pooler links a client to a server connection only for a
// transaction, so an idle LISTEN through it never hears a NOTIFY. The
// worker's River client listens on a direct connection instead
// (ListenURL, OpenListen), or every job waits for River's 1 s poll.
package dbpool

import (
	"context"
	"fmt"
	"net/url"
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
	// ListenMaxConns is the listener's pool: River holds one connection
	// for as long as it listens, and the second covers a reconnect.
	ListenMaxConns = 2
)

// DirectEnv names a direct (unpooled) connection string to the database
// DATABASE_URL reaches, for the one session that LISTENs.
const DirectEnv = "DATABASE_DIRECT_URL"

// ListenApplicationName is the listener's application_name, so its
// session is easy to find in pg_stat_activity.
const ListenApplicationName = "memax-river-listener"

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

// ListenURL is where to LISTEN when databaseURL goes through a pooler:
// DATABASE_DIRECT_URL if set; else, for a Neon pooled host (its endpoint
// ID ends in -pooler), the same URL without the suffix, which is the
// endpoint's direct connection with the same credentials; else "", and
// the pool can listen itself.
func ListenURL(databaseURL string) string {
	if v := strings.TrimSpace(os.Getenv(DirectEnv)); v != "" {
		return v
	}
	u, err := url.Parse(databaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return ""
	}
	endpoint, rest, ok := strings.Cut(u.Hostname(), ".")
	if !ok || !strings.HasSuffix(endpoint, "-pooler") || !strings.HasSuffix(rest, ".neon.tech") {
		return ""
	}
	port := u.Port()
	u.Host = strings.TrimSuffix(endpoint, "-pooler") + "." + rest
	if port != "" {
		u.Host += ":" + port
	}
	return u.String()
}

// OpenListen opens the listener's pool on url (ListenURL). Like any
// pgxpool it connects lazily, so an unreachable host fails a Ping, not
// the open.
func OpenListen(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("dbpool: parse %s: %w", DirectEnv, err)
	}
	cfg.MaxConns = ListenMaxConns
	cfg.ConnConfig.RuntimeParams["application_name"] = ListenApplicationName
	return pgxpool.NewWithConfig(ctx, cfg)
}
