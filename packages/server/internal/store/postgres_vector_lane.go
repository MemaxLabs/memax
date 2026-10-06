package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

// An HNSW index scan in pgvector returns at most hnsw.ef_search rows
// (default 40), however large the query's LIMIT is. Whenever the
// planner chooses the HNSW index for the vector lane, a candidate pool
// of 60–120 is silently cut to 40 — and post-filtering by owner or hub
// can cut it further. The planner picks the index when the access
// filter looks unselective, which is the normal case for single-owner
// databases: the eval harness, the LongMemEval and LoCoMo benchmarks,
// early staging and self-hosted installs. Multi-tenant production
// usually gets an exact owner-index plan instead.
//
// vectorLaneSettings raises ef_search to at least the LIMIT and, on
// pgvector >= 0.8, turns on iterative scans in strict order so the
// index keeps searching until the LIMIT is filled after filtering.
// Both are SET LOCAL, so they only live for the lane's transaction.
const (
	hnswDefaultEfSearch = 40
	hnswMaxEfSearch     = 1000 // pgvector's upper bound for ef_search
)

// hnswEfSearchFor returns the ef_search needed for a LIMIT of limit.
func hnswEfSearchFor(limit int) int {
	switch {
	case limit < hnswDefaultEfSearch:
		return hnswDefaultEfSearch
	case limit > hnswMaxEfSearch:
		return hnswMaxEfSearch
	default:
		return limit
	}
}

// vectorLaneSettings returns the SET LOCAL statements for a vector
// lane query with the given LIMIT.
func vectorLaneSettings(limit int, iterative bool) []string {
	stmts := []string{"SET LOCAL hnsw.ef_search = " + strconv.Itoa(hnswEfSearchFor(limit))}
	if iterative {
		stmts = append(stmts, "SET LOCAL hnsw.iterative_scan = strict_order")
	}
	return stmts
}

// pgvectorSupportsIterativeScan reports whether a pgvector version
// string (e.g. "0.8.0") has hnsw.iterative_scan, added in 0.8.0.
func pgvectorSupportsIterativeScan(version string) bool {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 0 || minor >= 8
}

var (
	iterativeScanOnce      sync.Once
	iterativeScanSupported bool
)

// vectorIterativeScan reports whether the database's pgvector supports
// iterative index scans. Checked once per process; on error it assumes
// not, which keeps the ef_search fix and loses nothing else.
func (s *PostgresStore) vectorIterativeScan(ctx context.Context) bool {
	iterativeScanOnce.Do(func() {
		var version string
		err := s.pool.QueryRow(ctx,
			`SELECT extversion FROM pg_extension WHERE extname = 'vector'`,
		).Scan(&version)
		iterativeScanSupported = err == nil && pgvectorSupportsIterativeScan(version)
	})
	return iterativeScanSupported
}

// queryVectorLane runs a vector-lane query (ORDER BY embedding <=> …
// LIMIT limit) inside a read-only transaction with vectorLaneSettings
// applied, and hands each row to scan. The settings and the query go
// to the server in one batch, so the extra cost is the BEGIN and
// COMMIT round trips.
func (s *PostgresStore) queryVectorLane(ctx context.Context, limit int, sql string, args []any, scan func(pgx.Rows) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("vector lane: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	settings := vectorLaneSettings(limit, s.vectorIterativeScan(ctx))
	for _, stmt := range settings {
		batch.Queue(stmt)
	}
	batch.Queue(sql, args...)

	results := tx.SendBatch(ctx, batch)
	for range settings {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("vector lane: settings: %w", err)
		}
	}
	rows, err := results.Query()
	if err != nil {
		_ = results.Close()
		return fmt.Errorf("vector lane: query: %w", err)
	}
	for rows.Next() {
		if err := scan(rows); err != nil {
			rows.Close()
			_ = results.Close()
			return err
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		_ = results.Close()
		return fmt.Errorf("vector lane: rows: %w", err)
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("vector lane: batch: %w", err)
	}
	return tx.Commit(ctx)
}
