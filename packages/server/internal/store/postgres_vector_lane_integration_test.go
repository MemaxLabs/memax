package store_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// TestVectorLaneIsNotCutShortByHNSW reproduces the pgvector behaviour
// behind queryVectorLane: when the planner uses the HNSW index, a
// LIMIT above hnsw.ef_search (40) returns only 40 rows. The test
// forces the index path (as the planner chooses on single-owner
// databases) and checks that the vector lane returns the full LIMIT.
func TestVectorLaneIsNotCutShortByHNSW(t *testing.T) {
	t.Parallel()
	s, ownerID, memID := setupChunksFixture(t)
	_ = ownerID
	ctx := context.Background()

	const rows = 100
	rng := rand.New(rand.NewPCG(1, 2))
	now := time.Now().UTC().Truncate(time.Microsecond)
	chunks := make([]model.Chunk, rows)
	for i := range chunks {
		chunks[i] = model.Chunk{
			ID: uuid.NewString(), MemoryID: memID,
			Content: fmt.Sprintf("chunk %d", i), ChunkIndex: i, TokenCount: 2,
			Language: "en", SearchConfig: "english",
			Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0, CreatedAt: now,
			Embedding: randomUnitVector(rng, 1024),
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("CreateChunks: %v", err)
	}

	// Force the HNSW index path on a fresh pool: new sessions inherit
	// the database-level planner settings.
	pg, ok := s.(*store.PostgresStore)
	if !ok {
		t.Fatalf("testdb store is %T, want *store.PostgresStore", s)
	}
	basePool := store.PoolForTest(pg)
	dbName := basePool.Config().ConnConfig.Database
	for _, stmt := range []string{
		fmt.Sprintf(`ALTER DATABASE %q SET enable_seqscan = off`, dbName),
		fmt.Sprintf(`ALTER DATABASE %q SET enable_bitmapscan = off`, dbName),
	} {
		if _, err := basePool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	forcedPool, err := pgxpool.New(ctx, basePool.Config().ConnString())
	if err != nil {
		t.Fatalf("open forced pool: %v", err)
	}
	t.Cleanup(forcedPool.Close)

	query := randomUnitVector(rng, 1024)
	sql := `SELECT c.id FROM chunks c WHERE c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector LIMIT $2`
	args := []any{vectorLiteral(query), 60}

	// Confirm the forced plan really is the HNSW index, then show the
	// unpatched query is capped at ef_search.
	var plan strings.Builder
	planRows, err := forcedPool.Query(ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	planRows.Close()
	if !strings.Contains(plan.String(), "idx_chunks_embedding") {
		t.Fatalf("expected the HNSW index in the plan, got:\n%s", plan.String())
	}
	if got := countRows(t, forcedPool, sql, args); got != 40 {
		t.Fatalf("unpatched HNSW query returned %d rows; expected pgvector's ef_search cap of 40 (has pgvector changed its default?)", got)
	}

	// The vector lane returns the full LIMIT.
	got := 0
	err = store.QueryVectorLaneForTest(store.NewPostgresStore(forcedPool), ctx, 60, sql, args, func(pgx.Rows) error {
		got++
		return nil
	})
	if err != nil {
		t.Fatalf("queryVectorLane: %v", err)
	}
	if got != 60 {
		t.Fatalf("vector lane returned %d rows, want 60", got)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args []any) int {
	t.Helper()
	rows, err := pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return n
}

func randomUnitVector(rng *rand.Rand, dims int) []float64 {
	v := make([]float64, dims)
	for i := range v {
		v[i] = rng.Float64() - 0.5
	}
	return v
}

func vectorLiteral(v []float64) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
