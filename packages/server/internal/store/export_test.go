package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryVectorLaneForTest exposes queryVectorLane to the store_test
// package, which can't import testdb from inside package store.
func QueryVectorLaneForTest(s *PostgresStore, ctx context.Context, limit int, sql string, args []any, scan func(pgx.Rows) error) error {
	return s.queryVectorLane(ctx, limit, sql, args, scan)
}

// PoolForTest exposes the store's pool to store_test.
func PoolForTest(s *PostgresStore) *pgxpool.Pool { return s.pool }
