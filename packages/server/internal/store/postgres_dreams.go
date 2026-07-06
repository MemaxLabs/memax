package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *PostgresStore) ExecuteDreamMerge(keeper *model.Memory, absorbedID string, ownerID string, chunks []model.Chunk) error {
	hubID := keeper.HubID
	if hubID == "" {
		var err error
		hubID, err = s.personalHubIDForOwner(ownerID)
		if err != nil {
			return err
		}
	}
	return s.ExecuteDreamMergeInHub(keeper, absorbedID, hubID, chunks)
}

func (s *PostgresStore) ExecuteDreamMergeInHub(keeper *model.Memory, absorbedID string, hubID string, chunks []model.Chunk) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dream merge tx: %w", err)
	}
	defer tx.Rollback(ctx)

	projCtx := keeper.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}
	accessIntents := keeper.AccessIntents
	if accessIntents == nil {
		accessIntents = map[string]int{}
	}
	_, err = tx.Exec(ctx,
		`UPDATE memories SET title=$1, content=$2, content_type=$3, content_hash=$4,
			summary=$5, hint=$6, kind=$7, stability=$8, retrieval_weight=$9, access_intents=$10, tags=$11, boundary=$12, state=$13, pinned=$14,
			source=$15, source_path=$16, project_context=$17, batch_id=$18, version=$19, access_count=$20,
			updated_at=$21, accessed_at=$22
		WHERE id = $23::uuid AND hub_id = $24::uuid`,
		keeper.Title, keeper.Content, keeper.ContentType, keeper.ContentHash,
		keeper.Summary, keeper.Hint, model.NormalizeMemoryKind(keeper.Kind), model.NormalizeMemoryStability(keeper.Stability), model.DefaultRetrievalWeight(keeper.RetrievalWeight), accessIntents, keeper.Tags, keeper.Boundary,
		keeper.State, keeper.Pinned, keeper.Source, keeper.SourcePath, projCtx,
		keeper.BatchID, keeper.Version, keeper.AccessCount, keeper.UpdatedAt, keeper.AccessedAt,
		keeper.ID, hubID,
	)
	if err != nil {
		return fmt.Errorf("update keeper memory: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE memory_id = $1`, keeper.ID); err != nil {
		return fmt.Errorf("delete old chunks: %w", err)
	}
	if err := insertChunks(ctx, tx, chunks); err != nil {
		return fmt.Errorf("insert merged chunks: %w", err)
	}

	result, err := tx.Exec(ctx,
		`UPDATE memories SET state = 'archived', updated_at = now() WHERE id = $1::uuid AND hub_id = $2::uuid`,
		absorbedID, hubID)
	if err != nil {
		return fmt.Errorf("archive absorbed memory: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("absorbed memory not found: %s", absorbedID)
	}

	return tx.Commit(ctx)
}

// ListDistinctOwners returns all unique owner IDs that have active memories.

func (s *PostgresStore) ListDistinctOwners() ([]string, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT owner_id::text FROM memories WHERE state = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var owners []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		owners = append(owners, id)
	}
	return owners, nil
}

// --- Dream Engine Store Methods ---

func (s *PostgresStore) personalHubIDForOwner(ownerID string) (string, error) {
	hub, err := s.GetPersonalHub(ownerID)
	if err != nil {
		return "", err
	}
	return hub.ID, nil
}

// FindRelevantTopicIDs finds topics most relevant to a batch of unassigned memories
// by embedding similarity. For each batch memory's representative chunk, finds nearest
// assigned chunks via HNSW index, then returns the distinct topic IDs those chunks belong to.
func (s *PostgresStore) FindRelevantTopicIDs(ownerID string, memoryIDs []string, limit int) ([]string, error) {
	hubID, err := s.personalHubIDForOwner(ownerID)
	if err != nil {
		return nil, err
	}
	return s.FindRelevantTopicIDsByHub(hubID, memoryIDs, limit)
}

func (s *PostgresStore) FindRelevantTopicIDsByHub(hubID string, memoryIDs []string, limit int) ([]string, error) {
	if len(memoryIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 40
	}
	ctx := context.Background()

	rows, err := s.pool.Query(ctx, `
		WITH batch_chunks AS (
			SELECT DISTINCT ON (c.memory_id) c.memory_id, c.embedding
			FROM chunks c
			WHERE c.memory_id = ANY($1)
			  AND c.embedding IS NOT NULL
			ORDER BY c.memory_id, c.chunk_index ASC
		),
		nearest_topics AS (
			SELECT mt.topic_id, MAX(1 - (bc.embedding <=> c2.embedding)) AS max_sim
			FROM batch_chunks bc
			CROSS JOIN LATERAL (
				SELECT c2.memory_id, c2.embedding
				FROM chunks c2
				JOIN memories m ON c2.memory_id = m.id
				WHERE m.hub_id = $2::uuid
				  AND m.state = 'active'
				  AND c2.embedding IS NOT NULL
				  AND c2.memory_id != bc.memory_id
				  AND EXISTS (SELECT 1 FROM memory_topics WHERE memory_id = c2.memory_id)
				ORDER BY c2.embedding <=> bc.embedding
				LIMIT 5
			) c2
			JOIN memory_topics mt ON c2.memory_id = mt.memory_id
			JOIN topics t ON mt.topic_id = t.id AND t.archived_at IS NULL
			GROUP BY mt.topic_id
		)
		SELECT topic_id
		FROM nearest_topics
		WHERE max_sim >= 0.4
		ORDER BY max_sim DESC
		LIMIT $3
	`, memoryIDs, hubID, limit)
	if err != nil {
		return nil, fmt.Errorf("find relevant topic IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// FindRelatedMemories returns the nearest-neighbor memories for a given memory
// using pgvector HNSW index on chunk embeddings. Uses the memory's first chunk
// as the query vector. Single SQL, no external API calls, sub-50ms.
//
// sourceHubID is the hub of the source memory — callers MUST authorize the
// caller's access to the source memory before calling this (see the Related
// handler, which uses GetAccessibleMemory). The query_vec CTE additionally
// joins memories to ensure the source memory is in sourceHubID; this is
// defense-in-depth against future callers that forget to authorize, and it
// closes the similarity-oracle surface where a caller could read any
// memory's embedding by ID.
//
// Uses CROSS JOIN LATERAL + ORDER BY <=> LIMIT to trigger HNSW index scan
// (same pattern as FindSimilarMemories). Oversamples 5x then deduplicates
// per memory (best chunk wins) to ensure limit distinct memories.
func (s *PostgresStore) FindRelatedMemories(memoryID string, sourceHubID string, limit int) ([]model.RelatedMemory, error) {
	if limit <= 0 {
		limit = 3
	}
	ctx := context.Background()

	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		WITH query_vec AS (
			SELECT c.embedding
			FROM chunks c
			JOIN memories m ON c.memory_id = m.id
			WHERE c.memory_id = $1::uuid
			  AND m.hub_id = $2::uuid
			  AND c.embedding IS NOT NULL
			ORDER BY c.chunk_index LIMIT 1
		),
		raw_neighbors AS (
			SELECT nb.memory_id, 1 - (nb.embedding <=> qv.embedding) AS similarity
			FROM query_vec qv
			CROSS JOIN LATERAL (
				SELECT c2.memory_id, c2.embedding
				FROM chunks c2
				JOIN memories m2 ON c2.memory_id = m2.id
				WHERE m2.hub_id = $2::uuid
				  AND m2.state = 'active'
				  AND c2.embedding IS NOT NULL
				  AND c2.memory_id != $1::uuid
				ORDER BY c2.embedding <=> qv.embedding
				LIMIT $3 * 5
			) nb
		),
		neighbors AS (
			SELECT memory_id, MAX(similarity) AS similarity
			FROM raw_neighbors
			GROUP BY memory_id
			ORDER BY similarity DESC
			LIMIT $3
		)
		SELECT %s, n.similarity
		FROM neighbors n
		JOIN (%s) ON m.id = n.memory_id
		ORDER BY n.similarity DESC
	`, memoryCols, memoryFrom), memoryID, sourceHubID, limit)
	if err != nil {
		return nil, fmt.Errorf("find related memories: %w", err)
	}
	defer rows.Close()

	var results []model.RelatedMemory
	for rows.Next() {
		var m model.Memory
		var similarity float64
		// Column order MUST match memoryCols exactly. scanMemoryWithAuthor
		// in postgres_chunks.go is the canonical reference — keep in sync.
		err := rows.Scan(
			&m.ID, &m.HubID, &m.OwnerID, &m.Title, &m.Content, &m.ContentType,
			&m.ContentHash, &m.Summary, &m.Hint, &m.Kind, &m.Stability, &m.RetrievalWeight, &m.AccessIntents, &m.Tags, &m.Boundary,
			&m.State, &m.Pinned, &m.Source, &m.SourceKind, &m.Metadata, &m.SourceAgent, &m.AssistedByAgent, &m.SourcePath, &m.HubReason, &m.ProjectContext, &m.EventDates,
			&m.BatchID, &m.Version, &m.AccessCount, &m.ShownCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt,
			&m.ProvenanceCreatedByType, &m.ProvenanceCreatedBySlug, &m.ProvenanceCreatedByDisplayName, &m.ProvenanceCreatedVia, &m.ProvenanceAssistedByAgent, &m.ProvenanceInitiationType, &m.ProvenanceAttributionSource,
			&m.SourceFetchHash, &m.UserFollowupMarker,
			&m.AuthorName, &m.AuthorAvatarURL, &m.HubName, &m.AgentDisplayName, &m.AgentIcon,
			&similarity,
		)
		if err != nil {
			return nil, fmt.Errorf("scan related memory: %w", err)
		}
		if m.ProjectContext == nil {
			m.ProjectContext = map[string]string{}
		}
		normalizeScannedMemory(&m)
		results = append(results, model.RelatedMemory{Memory: m, Similarity: similarity})
	}
	return results, rows.Err()
}

// FindRelatedForEnrichment searches for memories in the given hubs that are
// semantically similar to the provided embedding. Returns lightweight candidates
// (title + summary + raw cosine similarity) for use as summarizer context during
// ingestion. This is a dedicated path that does NOT use RRF fusion or recall
// scoring — it returns raw vector similarity for quality gating.
func (s *PostgresStore) FindRelatedForEnrichment(ctx context.Context, queryEmbedding []float64, _ string, readHubIDs []string, excludeMemoryID string, limit int) ([]model.EnrichmentCandidate, error) {
	if len(queryEmbedding) == 0 || len(readHubIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	// Retrieve more chunk candidates than the final limit so per-memory
	// dedup still leaves enough results.
	chunkCandidateLimit := limit * 6

	// Convert to pgvector literal format ([1,0,...]) — pgx serializes
	// []float64 as a Postgres array {1,0,...} which pgvector rejects.
	embLiteral := pgvectorLiteral(queryEmbedding)

	// Strictly hub-scoped: only search memories in the pre-authorized hubs.
	// Do NOT include m.owner_id — that would pull personal/private context
	// from other hubs into a team-hub summary.
	rows, err := s.pool.Query(ctx, `
		WITH nearest AS (
			SELECT c.memory_id,
			       1 - (c.embedding <=> $1::vector) AS similarity
			FROM chunks c
			JOIN memories m ON c.memory_id = m.id
			WHERE c.embedding IS NOT NULL
			  AND m.hub_id = ANY($2::uuid[])
			  AND m.id != $3::uuid
			  AND m.state = 'active'
			ORDER BY c.embedding <=> $1::vector
			LIMIT $4
		),
		best_per_memory AS (
			SELECT DISTINCT ON (memory_id)
			       memory_id, similarity
			FROM nearest
			ORDER BY memory_id, similarity DESC
		)
		SELECT m.id, m.title, COALESCE(m.summary, ''), b.similarity
		FROM best_per_memory b
		JOIN memories m ON m.id = b.memory_id
		ORDER BY b.similarity DESC
		LIMIT $5
	`, embLiteral, readHubIDs, excludeMemoryID, chunkCandidateLimit, limit)
	if err != nil {
		return nil, fmt.Errorf("find related for enrichment: %w", err)
	}
	defer rows.Close()

	var candidates []model.EnrichmentCandidate
	for rows.Next() {
		var c model.EnrichmentCandidate
		if err := rows.Scan(&c.MemoryID, &c.Title, &c.Summary, &c.Similarity); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// FindSimilarMemories finds pairs of memories with high cosine similarity between their chunks.
// Uses pgvector to compute pairwise similarity, returns pairs above threshold.
func (s *PostgresStore) FindSimilarMemories(ownerID string, threshold float64, limit int) ([]model.SimilarMemoryPair, error) {
	hubID, err := s.personalHubIDForOwner(ownerID)
	if err != nil {
		return nil, err
	}
	return s.FindSimilarMemoriesByHub(hubID, threshold, limit)
}

func (s *PostgresStore) FindSimilarMemoriesByHub(hubID string, threshold float64, limit int) ([]model.SimilarMemoryPair, error) {
	if limit <= 0 {
		limit = 50
	}
	ctx := context.Background()

	// Uses up to 3 representative chunks per memory + CROSS JOIN LATERAL
	// to find nearest neighbors via HNSW index. Using multiple chunks per memory
	// improves recall for long documents where chunk-0 may be a generic intro.
	// O(n * R * K * log n) where R=3 representative chunks, K=5 neighbors each.
	rows, err := s.pool.Query(ctx, `
		WITH representatives AS (
			SELECT c.memory_id, c.embedding
			FROM chunks c
			JOIN memories m ON c.memory_id = m.id
			WHERE m.hub_id = $1::uuid
			  AND m.state = 'active'
			  AND c.embedding IS NOT NULL
			  AND c.chunk_index < 3
		),
		raw_pairs AS (
			SELECT
				r.memory_id AS memory_a_id,
				nb.memory_id AS memory_b_id,
				1 - (r.embedding <=> nb.embedding) AS similarity
			FROM representatives r
			CROSS JOIN LATERAL (
				SELECT c2.memory_id, c2.embedding
				FROM chunks c2
				JOIN memories m2 ON c2.memory_id = m2.id
				WHERE m2.hub_id = $1::uuid
				  AND m2.state = 'active'
				  AND c2.embedding IS NOT NULL
				  AND c2.memory_id != r.memory_id
				ORDER BY c2.embedding <=> r.embedding
				LIMIT 5
			) nb
			WHERE 1 - (r.embedding <=> nb.embedding) >= $2
		),
		deduped AS (
			SELECT
				LEAST(memory_a_id, memory_b_id) AS memory_a_id,
				GREATEST(memory_a_id, memory_b_id) AS memory_b_id,
				MAX(similarity) AS similarity
			FROM raw_pairs
			GROUP BY LEAST(memory_a_id, memory_b_id), GREATEST(memory_a_id, memory_b_id)
		)
		SELECT
			d.similarity,
			ma.id, ma.hub_id, ma.owner_id, ma.title, ma.content, ma.content_type, ma.content_hash,
			ma.summary, ma.hint, ma.kind, ma.stability, ma.retrieval_weight, ma.access_intents, ma.tags, ma.boundary, ma.state, ma.pinned,
			ma.source, ma.source_path, ma.project_context,
			ma.batch_id, ma.version, ma.access_count, ma.shown_count, ma.created_at, ma.updated_at, ma.accessed_at,
			mb.id, mb.hub_id, mb.owner_id, mb.title, mb.content, mb.content_type, mb.content_hash,
			mb.summary, mb.hint, mb.kind, mb.stability, mb.retrieval_weight, mb.access_intents, mb.tags, mb.boundary, mb.state, mb.pinned,
			mb.source, mb.source_path, mb.project_context,
			mb.batch_id, mb.version, mb.access_count, mb.shown_count, mb.created_at, mb.updated_at, mb.accessed_at
		FROM deduped d
		JOIN memories ma ON d.memory_a_id = ma.id
		JOIN memories mb ON d.memory_b_id = mb.id
		ORDER BY d.similarity DESC
		LIMIT $3
	`, hubID, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar memories: %w", err)
	}
	defer rows.Close()

	var pairs []model.SimilarMemoryPair
	for rows.Next() {
		var sim float64
		var memA, memB model.Memory
		if err := rows.Scan(
			&sim,
			&memA.ID, &memA.HubID, &memA.OwnerID, &memA.Title, &memA.Content,
			&memA.ContentType, &memA.ContentHash, &memA.Summary, &memA.Hint,
			&memA.Kind, &memA.Stability, &memA.RetrievalWeight, &memA.AccessIntents, &memA.Tags, &memA.Boundary, &memA.State, &memA.Pinned,
			&memA.Source, &memA.SourcePath, &memA.ProjectContext,
			&memA.BatchID, &memA.Version, &memA.AccessCount, &memA.ShownCount, &memA.CreatedAt, &memA.UpdatedAt, &memA.AccessedAt,
			&memB.ID, &memB.HubID, &memB.OwnerID, &memB.Title, &memB.Content,
			&memB.ContentType, &memB.ContentHash, &memB.Summary, &memB.Hint,
			&memB.Kind, &memB.Stability, &memB.RetrievalWeight, &memB.AccessIntents, &memB.Tags, &memB.Boundary, &memB.State, &memB.Pinned,
			&memB.Source, &memB.SourcePath, &memB.ProjectContext,
			&memB.BatchID, &memB.Version, &memB.AccessCount, &memB.ShownCount, &memB.CreatedAt, &memB.UpdatedAt, &memB.AccessedAt,
		); err != nil {
			slog.Warn("find similar memories: scan error", "error", err)
			continue
		}
		if memA.ProjectContext == nil {
			memA.ProjectContext = map[string]string{}
		}
		if memB.ProjectContext == nil {
			memB.ProjectContext = map[string]string{}
		}
		normalizeScannedMemory(&memA)
		normalizeScannedMemory(&memB)
		pairs = append(pairs, model.SimilarMemoryPair{
			MemoryA:    memA,
			MemoryB:    memB,
			Similarity: sim,
		})
	}
	return pairs, nil
}

// ArchiveMemory sets a memory's state to "archived".
func (s *PostgresStore) ArchiveMemory(id string, ownerID string) error {
	hubID, err := s.personalHubIDForOwner(ownerID)
	if err != nil {
		return err
	}
	return s.ArchiveMemoryInHub(id, hubID)
}

func (s *PostgresStore) ArchiveMemoryInHub(id string, hubID string) error {
	ctx := context.Background()
	result, err := s.pool.Exec(ctx,
		`UPDATE memories SET state = 'archived', updated_at = now() WHERE id = $1::uuid AND hub_id = $2::uuid`,
		id, hubID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

// CreateDreamRun inserts a new dream run record.
//
// Translates the partial unique index on dream_runs(hub_id) WHERE
// status = 'running' (migration 012) into store.ErrDreamRunConcurrent
// so callers can branch on the concurrent-run case without sniffing
// pg error codes themselves.
func (s *PostgresStore) CreateDreamRun(run *model.DreamRun) error {
	ctx := context.Background()
	phaseMetrics, err := json.Marshal(run.PhaseMetrics)
	if err != nil {
		return fmt.Errorf("marshal dream phase metrics: %w", err)
	}
	phaseBudgets, err := json.Marshal(run.PhaseBudgets)
	if err != nil {
		return fmt.Errorf("marshal dream phase budgets: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO dream_runs (id, owner_id, hub_id, mode, status, started_at, memories_scanned,
			duplicates_merged, contradictions_found, memories_archived, memories_organized,
			topics_restructured, phase_metrics, phase_budgets, report)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		run.ID, run.OwnerID, run.HubID, run.Mode, run.Status, run.StartedAt,
		run.MemoriesScanned, run.DuplicatesMerged, run.ContradictionsFound,
		run.MemoriesArchived, run.MemoriesOrganized, run.TopicsRestructured, phaseMetrics, phaseBudgets, run.Report)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_dream_runs_one_active_per_hub" {
			return ErrDreamRunConcurrent
		}
	}
	return err
}

// UpdateDreamRun updates a dream run record.
func (s *PostgresStore) UpdateDreamRun(run *model.DreamRun) error {
	return updateDreamRunExec(context.Background(), s.pool, run)
}

// ClaimStaleDreamRun flips any `running` dream_run row for this hub
// that started before (now - olderThan) into status='failed' with a
// "stale run" report. Single atomic statement so concurrent callers
// serialize at the row level; only one of them sees rows_affected > 0.
//
// Called from the engine's ErrDreamRunConcurrent handler to recover
// from the pathological case where a prior cycle committed phase
// mutations but the terminal UpdateDreamRun failed. Without this
// safety valve, migration 012's partial unique index would block
// all future cycles for the hub.
//
// Staleness compares COALESCE(last_heartbeat_at, started_at) to the
// threshold so a slow-but-alive cycle that still bumps its heartbeat
// between phases stays safe from reclaim. A cycle that has stopped
// calling home (worker died, pgbouncer blip, etc.) falls past the
// threshold and is reclaimed.
//
// Threshold must be wider than the longest possible quiet interval
// between heartbeats inside a phase, not the worker timeout — the
// engine bumps last_heartbeat_at after each phase boundary, so any
// interval longer than a single phase's LLM budget is safe.
func (s *PostgresStore) ClaimStaleDreamRun(ctx context.Context, hubID string, olderThan time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE dream_runs
		    SET status = 'failed',
		        finished_at = now(),
		        report = 'Stale run reclaimed — previous cycle went quiet past the heartbeat threshold.'
		  WHERE hub_id = $1
		    AND status = 'running'
		    AND COALESCE(last_heartbeat_at, started_at) < now() - $2::interval`,
		hubID, fmt.Sprintf("%d milliseconds", olderThan.Milliseconds()),
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// HeartbeatDreamRun bumps last_heartbeat_at = now() for a running
// row. Called by the engine after each phase boundary. Idempotent
// and lock-free — a single short UPDATE that NO-OPs if the row has
// already terminated.
func (s *PostgresStore) HeartbeatDreamRun(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE dream_runs SET last_heartbeat_at = now()
		 WHERE id = $1::uuid AND status = 'running'`, runID)
	return err
}

func updateDreamRunExec(ctx context.Context, q pgQuerier, run *model.DreamRun) error {
	phaseMetrics, err := json.Marshal(run.PhaseMetrics)
	if err != nil {
		return fmt.Errorf("marshal dream phase metrics: %w", err)
	}
	phaseBudgets, err := json.Marshal(run.PhaseBudgets)
	if err != nil {
		return fmt.Errorf("marshal dream phase budgets: %w", err)
	}
	_, err = q.Exec(ctx,
		`UPDATE dream_runs SET mode = $2, status = $3, finished_at = $4, memories_scanned = $5,
			duplicates_merged = $6, contradictions_found = $7, memories_archived = $8,
			memories_organized = $9, topics_restructured = $10, phase_metrics = $11, phase_budgets = $12, report = $13
		WHERE id = $1`,
		run.ID, run.Mode, run.Status, run.FinishedAt, run.MemoriesScanned,
		run.DuplicatesMerged, run.ContradictionsFound, run.MemoriesArchived,
		run.MemoriesOrganized, run.TopicsRestructured, phaseMetrics, phaseBudgets, run.Report)
	return err
}

// CreateDreamAction is the auto-commit entry point for dream_actions
// writes. Producers that need a shared tx boundary should call WithTx +
// CreateDreamActionTx instead; both paths go through createDreamActionExec.
func (s *PostgresStore) CreateDreamAction(action *model.DreamAction) error {
	return createDreamActionExec(context.Background(), s.pool, action)
}

// GetLatestDreamRun returns the most recent dream run for an owner.
func (s *PostgresStore) GetLatestDreamRun(ownerID string) (*model.DreamRun, error) {
	hubID, err := s.personalHubIDForOwner(ownerID)
	if err != nil {
		return nil, err
	}
	return s.GetLatestDreamRunByHub(hubID)
}

func (s *PostgresStore) GetLatestDreamRunByHub(hubID string) (*model.DreamRun, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, hub_id, COALESCE(mode, 'maintenance'), status, started_at, COALESCE(finished_at, '0001-01-01'),
			memories_scanned, duplicates_merged, contradictions_found, memories_archived,
			COALESCE(memories_organized, 0), COALESCE(topics_restructured, 0), phase_metrics, phase_budgets, report, last_heartbeat_at
		FROM dream_runs WHERE hub_id = $1::uuid
		ORDER BY started_at DESC LIMIT 1`, hubID)
	var r model.DreamRun
	var phaseMetrics, phaseBudgets []byte
	err := row.Scan(&r.ID, &r.OwnerID, &r.HubID, &r.Mode, &r.Status, &r.StartedAt, &r.FinishedAt,
		&r.MemoriesScanned, &r.DuplicatesMerged, &r.ContradictionsFound,
		&r.MemoriesArchived, &r.MemoriesOrganized, &r.TopicsRestructured, &phaseMetrics, &phaseBudgets, &r.Report, &r.LastHeartbeatAt)
	if err != nil {
		return nil, fmt.Errorf("no dream runs found: %w", err)
	}
	_ = json.Unmarshal(phaseMetrics, &r.PhaseMetrics)
	_ = json.Unmarshal(phaseBudgets, &r.PhaseBudgets)
	return &r, nil
}

// ListDreamRuns returns recent dream runs for an owner.
func (s *PostgresStore) ListDreamRuns(ownerID string, limit int) ([]model.DreamRun, error) {
	hubID, err := s.personalHubIDForOwner(ownerID)
	if err != nil {
		return nil, err
	}
	return s.ListDreamRunsByHub(hubID, limit)
}

func (s *PostgresStore) ListDreamRunsByHub(hubID string, limit int) ([]model.DreamRun, error) {
	if limit <= 0 {
		limit = 10
	}
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, hub_id, COALESCE(mode, 'maintenance'), status, started_at, COALESCE(finished_at, '0001-01-01'),
			memories_scanned, duplicates_merged, contradictions_found, memories_archived,
			COALESCE(memories_organized, 0), COALESCE(topics_restructured, 0), phase_metrics, phase_budgets, report, last_heartbeat_at
		FROM dream_runs WHERE hub_id = $1::uuid
		ORDER BY started_at DESC LIMIT $2`, hubID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []model.DreamRun
	for rows.Next() {
		var r model.DreamRun
		var phaseMetrics, phaseBudgets []byte
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.HubID, &r.Mode, &r.Status, &r.StartedAt, &r.FinishedAt,
			&r.MemoriesScanned, &r.DuplicatesMerged, &r.ContradictionsFound,
			&r.MemoriesArchived, &r.MemoriesOrganized, &r.TopicsRestructured, &phaseMetrics, &phaseBudgets, &r.Report, &r.LastHeartbeatAt); err != nil {
			continue
		}
		_ = json.Unmarshal(phaseMetrics, &r.PhaseMetrics)
		_ = json.Unmarshal(phaseBudgets, &r.PhaseBudgets)
		runs = append(runs, r)
	}
	return runs, nil
}

// GetDreamRunByID reads a single dream_runs row. Used by the admin
// detail endpoint. Returns ErrDreamRunNotFound when the row does
// not exist (so handlers can return 404) and a wrapped error for
// any other failure (so handlers can return 500). Scanner mirrors
// GetLatestDreamRunByHub for column consistency.
func (s *PostgresStore) GetDreamRunByID(ctx context.Context, runID string) (*model.DreamRun, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, hub_id, COALESCE(mode, 'maintenance'), status, started_at, COALESCE(finished_at, '0001-01-01'),
			memories_scanned, duplicates_merged, contradictions_found, memories_archived,
			COALESCE(memories_organized, 0), COALESCE(topics_restructured, 0), phase_metrics, phase_budgets, report, last_heartbeat_at
		FROM dream_runs WHERE id = $1::uuid`, runID)
	var r model.DreamRun
	var phaseMetrics, phaseBudgets []byte
	err := row.Scan(&r.ID, &r.OwnerID, &r.HubID, &r.Mode, &r.Status, &r.StartedAt, &r.FinishedAt,
		&r.MemoriesScanned, &r.DuplicatesMerged, &r.ContradictionsFound,
		&r.MemoriesArchived, &r.MemoriesOrganized, &r.TopicsRestructured, &phaseMetrics, &phaseBudgets, &r.Report, &r.LastHeartbeatAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDreamRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get dream run: %w", err)
	}
	_ = json.Unmarshal(phaseMetrics, &r.PhaseMetrics)
	_ = json.Unmarshal(phaseBudgets, &r.PhaseBudgets)
	return &r, nil
}

// ListDreamRunsForUser returns dream runs across every hub the user
// participates in — personal hub (owner) plus any team hub where
// they're a hub_member. Keyset paginated by started_at DESC; cursor
// is the last row's started_at. Optional single-hub scope via
// opts.HubID.
//
// The admin UI hits this to render "dream activity" for a given
// user without N per-hub queries. Pagination is keyset (not offset)
// because run rows are append-only per hub and started_at is
// indexed; offset pagination would get slow after ~50 hubs.
func (s *PostgresStore) ListDreamRunsForUser(ctx context.Context, opts DreamRunListForUserOpts) ([]model.DreamRun, string, error) {
	if opts.UserID == "" {
		return nil, "", fmt.Errorf("dream runs list: user id is required")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Accessible hub set: personal hub (owner) + team hubs where
	// the user is a hub_member. Subquery keeps the join off the
	// hot path; dream_runs index on hub_id handles the IN-list.
	args := []any{opts.UserID}
	where := `(
		hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid)
		OR hub_id IN (SELECT hub_id FROM hub_members WHERE user_id = $1::uuid)
	)`
	if opts.HubID != "" {
		args = append(args, opts.HubID)
		where += fmt.Sprintf(" AND hub_id = $%d::uuid", len(args))
	}
	if opts.Cursor != "" {
		// Composite cursor (started_at|id) — a started_at-only
		// cursor would skip rows that share the same timestamp.
		// Compare as a row tuple to match ORDER BY (started_at
		// DESC, id DESC): the standard keyset-pagination shape.
		startedAt, cursorID, err := parseDreamRunCursor(opts.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("dream runs list: invalid cursor: %w", err)
		}
		args = append(args, startedAt)
		stAtIdx := len(args)
		args = append(args, cursorID)
		idIdx := len(args)
		where += fmt.Sprintf(" AND (started_at, id) < ($%d, $%d::uuid)", stAtIdx, idIdx)
	}
	args = append(args, limit+1) // fetch +1 to detect next page
	limitParam := len(args)

	query := fmt.Sprintf(
		`SELECT id, owner_id, hub_id, COALESCE(mode, 'maintenance'), status, started_at, COALESCE(finished_at, '0001-01-01'),
			memories_scanned, duplicates_merged, contradictions_found, memories_archived,
			COALESCE(memories_organized, 0), COALESCE(topics_restructured, 0), phase_metrics, phase_budgets, report, last_heartbeat_at
		 FROM dream_runs
		 WHERE %s
		 ORDER BY started_at DESC, id DESC
		 LIMIT $%d`, where, limitParam)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list dream runs for user: %w", err)
	}
	defer rows.Close()

	runs := make([]model.DreamRun, 0, limit)
	for rows.Next() {
		var r model.DreamRun
		var phaseMetrics, phaseBudgets []byte
		// Admin audit view: a scan error is worse surfaced as
		// truncated history than as an honest 500. Bail rather
		// than `continue` silently — we're not the memory list
		// where missing rows are acceptable.
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.HubID, &r.Mode, &r.Status, &r.StartedAt, &r.FinishedAt,
			&r.MemoriesScanned, &r.DuplicatesMerged, &r.ContradictionsFound,
			&r.MemoriesArchived, &r.MemoriesOrganized, &r.TopicsRestructured, &phaseMetrics, &phaseBudgets, &r.Report, &r.LastHeartbeatAt); err != nil {
			return nil, "", fmt.Errorf("scan dream run row: %w", err)
		}
		_ = json.Unmarshal(phaseMetrics, &r.PhaseMetrics)
		_ = json.Unmarshal(phaseBudgets, &r.PhaseBudgets)
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(runs) > limit {
		last := runs[limit-1]
		nextCursor = encodeDreamRunCursor(last.StartedAt, last.ID)
		runs = runs[:limit]
	}
	return runs, nextCursor, nil
}

// Dream-run cursor format: RFC3339Nano timestamp + "|" + uuid.
// Opaque to clients; no backwards-compat concerns yet because the
// previous version was unreleased. Worth keeping the separator
// simple so the value can travel through URL query strings without
// escaping surprises.
const dreamRunCursorSep = "|"

func encodeDreamRunCursor(startedAt time.Time, id string) string {
	return startedAt.Format(time.RFC3339Nano) + dreamRunCursorSep + id
}

func parseDreamRunCursor(raw string) (time.Time, string, error) {
	parts := splitDreamRunCursor(raw)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("cursor must be <rfc3339nano>%s<uuid>", dreamRunCursorSep)
	}
	startedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursor started_at: %w", err)
	}
	if parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("cursor id is empty")
	}
	return startedAt, parts[1], nil
}

func splitDreamRunCursor(raw string) []string {
	// IndexOf rather than strings.Split so a stray separator in
	// the UUID tail (impossible today, but defensive) doesn't
	// truncate the id half.
	for i := 0; i < len(raw); i++ {
		if raw[i] == dreamRunCursorSep[0] {
			return []string{raw[:i], raw[i+1:]}
		}
	}
	return []string{raw}
}

// GetDreamActions returns all actions for a dream run.
func (s *PostgresStore) GetDreamActions(runID string) ([]model.DreamAction, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, run_id, action_type, source_memory_ids, COALESCE(result_memory_id, ''),
			COALESCE(from_topic_id::text, ''), COALESCE(to_topic_id::text, ''),
			reason, COALESCE(similarity, 0), created_at,
			agent_path, COALESCE(agent_session_id, ''), agent_model_calls, agent_tool_calls
		FROM dream_actions WHERE run_id = $1
		ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []model.DreamAction
	for rows.Next() {
		var a model.DreamAction
		if err := rows.Scan(&a.ID, &a.RunID, &a.ActionType, &a.SourceMemoryIDs,
			&a.ResultMemoryID, &a.FromTopicID, &a.ToTopicID,
			&a.Reason, &a.Similarity, &a.CreatedAt,
			&a.AgentPath, &a.AgentSessionID, &a.AgentModelCalls, &a.AgentToolCalls); err != nil {
			continue
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// defaultContradictionLookback is the time window
// LoadDreamTriggerInputs uses when the caller passes 0. 30 days
// matches plan 23's calibration window — long enough to catch
// recurring contradictions across multiple weekly dream cycles,
// short enough that stale runs from quarter-old states don't
// dominate the count.
const defaultContradictionLookback = 30 * 24 * time.Hour

// LoadDreamTriggerInputs computes the four Phase 2a trigger
// signals for every active memory in hubID. Single CTE-backed
// query; no N+1.
//
// Implementation notes:
//   - source_memory_ids is text[] in dream_actions, so the
//     contradiction count CTE casts to uuid via unnest before
//     joining against memory ids.
//   - When a memory belongs to multiple topics (allowed by
//     memory_topics), we pick the highest-confidence topic via
//     DISTINCT ON to compute its sibling count. Tiebreaker on
//     memory_id keeps the choice deterministic.
//   - Topic sibling count INCLUDES the focal memory (counts
//     "active memories sharing this topic"). The trigger
//     evaluator's threshold (default 12) is interpreted as
//     "topic has >= 12 active members", with the focal counted.
func (s *PostgresStore) LoadDreamTriggerInputs(ctx context.Context, hubID string, contradictionLookback time.Duration) ([]DreamTriggerInputRow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if contradictionLookback <= 0 {
		contradictionLookback = defaultContradictionLookback
	}
	cutoff := time.Now().UTC().Add(-contradictionLookback)

	rows, err := s.pool.Query(ctx, `
WITH active_memories AS (
    SELECT m.id,
           m.content_hash,
           COALESCE(m.source_fetch_hash, '')    AS source_fetch_hash,
           (m.user_followup_marker IS NOT NULL) AS user_followup_scanned,
           COALESCE(m.user_followup_marker, '') AS user_followup_marker
    FROM memories m
    WHERE m.hub_id = $1::uuid
      AND m.state = 'active'
),
contradiction_counts AS (
    SELECT mid::uuid AS memory_id, COUNT(*) AS cnt
    FROM (
        SELECT unnest(da.source_memory_ids) AS mid
        FROM dream_actions da
        JOIN dream_runs dr ON da.run_id = dr.id
        WHERE dr.hub_id = $1::uuid
          AND da.action_type = 'contradiction'
          AND da.created_at >= $2
    ) sub
    WHERE mid <> ''
    GROUP BY mid
),
focal_topic AS (
    SELECT DISTINCT ON (mt.memory_id)
        mt.memory_id, mt.topic_id
    FROM memory_topics mt
    JOIN active_memories am ON am.id = mt.memory_id
    ORDER BY mt.memory_id, mt.confidence DESC, mt.created_at DESC
),
topic_sibling_counts AS (
    SELECT mt.topic_id, COUNT(DISTINCT mt.memory_id) AS cnt
    FROM memory_topics mt
    JOIN memories m2 ON mt.memory_id = m2.id
    WHERE m2.hub_id = $1::uuid AND m2.state = 'active'
    GROUP BY mt.topic_id
)
SELECT am.id::text,
       am.content_hash,
       am.source_fetch_hash,
       am.user_followup_scanned,
       am.user_followup_marker,
       COALESCE(cc.cnt, 0)::int  AS contradiction_count,
       COALESCE(tsc.cnt, 0)::int AS topic_sibling_count
FROM active_memories am
LEFT JOIN contradiction_counts cc ON cc.memory_id = am.id
LEFT JOIN focal_topic ft           ON ft.memory_id = am.id
LEFT JOIN topic_sibling_counts tsc ON tsc.topic_id = ft.topic_id`, hubID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("load dream trigger inputs: %w", err)
	}
	defer rows.Close()

	var out []DreamTriggerInputRow
	for rows.Next() {
		var r DreamTriggerInputRow
		if err := rows.Scan(
			&r.MemoryID,
			&r.ContentHash,
			&r.SourceFetchHash,
			&r.UserFollowupScanned,
			&r.UserFollowupMarker,
			&r.ContradictionCount,
			&r.TopicSiblingCount,
		); err != nil {
			return nil, fmt.Errorf("scan dream trigger input row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LogTriggerDecisions batch-inserts dream trigger decision rows
// for runID + hubID. Empty rows is a no-op (no-memory hubs
// should not even reach this path, but defensive).
//
// Implementation: single multi-row INSERT via unnest of parallel
// arrays. Faster than per-row Exec at scale (a hub with 1000
// memories writes 1000 rows in one round-trip) and inherits the
// dream_trigger_decisions CHECK constraint on trigger_kind so a
// bad Kind in any row aborts the whole batch — easier to debug
// than a partial write where some rows landed and others didn't.
func (s *PostgresStore) LogTriggerDecisions(ctx context.Context, runID, hubID string, rows []DreamTriggerDecisionRow) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(rows) == 0 {
		return nil
	}

	memoryIDs := make([]string, len(rows))
	fired := make([]bool, len(rows))
	kinds := make([]string, len(rows))
	signals := make([][]byte, len(rows))
	for i, r := range rows {
		memoryIDs[i] = r.MemoryID
		fired[i] = r.Fired
		kinds[i] = r.Kind
		// Empty signals → "{}" so the JSONB column gets a valid
		// (if empty) object rather than NULL. The DB has a NOT
		// NULL constraint on the column with default '{}'::jsonb,
		// but unnest doesn't re-apply the default for an explicit
		// NULL, so we materialize it here.
		if len(r.SignalsJSON) == 0 {
			signals[i] = []byte("{}")
		} else {
			signals[i] = r.SignalsJSON
		}
	}

	_, err := s.pool.Exec(ctx, `
INSERT INTO dream_trigger_decisions (dream_run_id, hub_id, memory_id, trigger_fired, trigger_kind, signals)
SELECT $1::uuid, $2::uuid, mid::uuid, fired, kind, signals::jsonb
FROM unnest($3::text[], $4::bool[], $5::text[], $6::text[]) AS t(mid, fired, kind, signals)`,
		runID, hubID, memoryIDs, fired, kinds, byteSlicesToText(signals))
	if err != nil {
		return fmt.Errorf("log trigger decisions: %w", err)
	}
	return nil
}

// byteSlicesToText converts [][]byte to []string for pgx's
// text[] parameter binding. JSONB roundtripping through text is
// fine because JSON is a subset of text.
func byteSlicesToText(in [][]byte) []string {
	out := make([]string, len(in))
	for i, b := range in {
		out[i] = string(b)
	}
	return out
}
