// Package catalock serialises test databases' DROP DATABASE against
// migrations that drop a role. DROP ROLE scans every database's catalog
// for the role's dependencies and fails with "cache lookup failed for
// database" when another process drops a database mid-scan, which made
// TestV2MigrationsRoundTrip flaky under the full suite.
//
// Drops share the lock, so they never wait on one another; a role drop
// takes it alone. Advisory locks belong to one database, so every caller
// locks on a connection to the admin database (TEST_DATABASE_URL's). The
// package has no dependencies of its own, so both testdb and migrate's
// tests can import it.
package catalock

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// key is the advisory lock's key ("memax" in ASCII).
const key int64 = 0x6d656d6178

// DropDatabase drops name holding the lock shared.
func DropDatabase(ctx context.Context, admin *pgxpool.Pool, name string) error {
	conn, err := admin.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock_shared($1)", key); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock_shared($1)", key) }()
	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q", name))
	return err
}

// Exclusive takes the lock alone, waiting for drops in flight, and keeps
// new ones waiting until the returned function releases it.
func Exclusive(ctx context.Context, admin *pgxpool.Pool) (release func(), err error) {
	conn, err := admin.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		conn.Release()
	}, nil
}
