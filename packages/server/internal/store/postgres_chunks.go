package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ErrEmptyHubIDsForStrictSearch is returned by SearchChunksInHubs
// when called with an empty hubIDs slice. The strict-hub variant
// requires at least one hub — falling through to owner-only reads
// would be the wrong fail-open behavior for the agent path.
var ErrEmptyHubIDsForStrictSearch = errors.New("store: SearchChunksInHubs requires non-empty hubIDs")

// chunkSearchAccess controls how searchChunks builds its access
// predicate. The three modes are mutually exclusive and reflect
// the three security shapes the store supports:
//
//   - chunkAccessOwnerOnly: WHERE m.owner_id = $user
//     Used by /v1/recall when the caller has no hub scope
//     (e.g., a CLI invocation without X-Hub-ID).
//
//   - chunkAccessOwnerOrHub: WHERE m.owner_id = $user OR
//                                  m.hub_id = ANY($hubs)
//     The default cross-hub recall. Used by /v1/recall, /v1/ask,
//     and MCP tools. Users see their own content regardless of
//     which hub they explicitly scoped to.
//
//   - chunkAccessHubStrict:  WHERE m.hub_id = ANY($hubs)
//     Strict-hub-only. Used by the agent runtime when the chat
//     agent narrows recall to a specific hub or when a dream
//     agent runs against its single team-hub. User-owned content
//     in non-target hubs MUST NOT leak — the OR shape would be
//     wrong here.
type chunkSearchAccess int

const (
	chunkAccessOwnerOnly chunkSearchAccess = iota
	chunkAccessOwnerOrHub
	chunkAccessHubStrict
)

// Reciprocal Rank Fusion (RRF) constants.
//
// Standard RRF: score = Σ 1/(k + rank + 1)
// Weighted RRF: score = Σ weight/(k + rank + 1)
//
// Weights reflect each lane's signal quality for ranking precision:
//   - Vector (1.5): best semantic signal, bridges languages
//   - BM25 (1.0):   exact lexical match, baseline
//   - Trigram (0.6): fuzzy matching, noisiest lane
//   - Temporal (1.0): self-selecting (only fires on temporal queries)
//   - Field (1.2):  structural rescue for short-content memories
const (
	rrfK         = 60
	rrfVectorW   = 1.5
	rrfBM25W     = 1.0
	rrfTrigramW  = 0.6
	rrfTemporalW = 1.0
	rrfFieldW    = 1.2
)

func (s *PostgresStore) CreateChunks(chunks []model.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	ctx := context.Background()
	return insertChunks(ctx, s.pool, chunks)
}

type chunkBatchSender interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

func insertChunks(ctx context.Context, sender chunkBatchSender, chunks []model.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, c := range chunks {
		searchConfig := c.SearchConfig
		if searchConfig == "" {
			searchConfig = "simple"
		}
		if len(c.Embedding) > 0 {
			embStr := pgvectorLiteral(c.Embedding)
			batch.Queue(
				`INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, language, search_config, kind, stability, retrieval_weight, hint, tags_text, metadata_text, project_repo, created_at, embedding)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::vector)`,
				c.ID, c.MemoryID, c.Content, c.HeadingChain, c.ChunkIndex, c.TokenCount, c.Language, searchConfig, model.NormalizeMemoryKind(c.Kind), model.NormalizeMemoryStability(c.Stability), model.DefaultRetrievalWeight(c.RetrievalWeight), c.Hint, c.TagsText, c.MetadataText, c.ProjectRepo, c.CreatedAt, embStr,
			)
		} else {
			batch.Queue(
				`INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, language, search_config, kind, stability, retrieval_weight, hint, tags_text, metadata_text, project_repo, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
				c.ID, c.MemoryID, c.Content, c.HeadingChain, c.ChunkIndex, c.TokenCount, c.Language, searchConfig, model.NormalizeMemoryKind(c.Kind), model.NormalizeMemoryStability(c.Stability), model.DefaultRetrievalWeight(c.RetrievalWeight), c.Hint, c.TagsText, c.MetadataText, c.ProjectRepo, c.CreatedAt,
			)
		}
	}
	br := sender.SendBatch(ctx, batch)
	defer br.Close()
	for range chunks {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) UpdateChunk(chunk *model.Chunk) error {
	ctx := context.Background()
	searchConfig := chunk.SearchConfig
	if searchConfig == "" {
		searchConfig = "simple"
	}
	if len(chunk.Embedding) > 0 {
		embStr := pgvectorLiteral(chunk.Embedding)
		_, err := s.pool.Exec(ctx,
			`UPDATE chunks
			SET content=$1, heading_chain=$2, chunk_index=$3, token_count=$4, language=$5, search_config=$6, kind=$7, stability=$8, retrieval_weight=$9, hint=$10, tags_text=$11, metadata_text=$12, project_repo=$13, embedding=$14::vector
			WHERE id = $15`,
			chunk.Content, chunk.HeadingChain, chunk.ChunkIndex, chunk.TokenCount, chunk.Language, searchConfig, model.NormalizeMemoryKind(chunk.Kind), model.NormalizeMemoryStability(chunk.Stability), model.DefaultRetrievalWeight(chunk.RetrievalWeight), chunk.Hint, chunk.TagsText, chunk.MetadataText, chunk.ProjectRepo, embStr, chunk.ID,
		)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE chunks
		SET content=$1, heading_chain=$2, chunk_index=$3, token_count=$4, language=$5, search_config=$6, kind=$7, stability=$8, retrieval_weight=$9, hint=$10, tags_text=$11, metadata_text=$12, project_repo=$13
		WHERE id = $14`,
		chunk.Content, chunk.HeadingChain, chunk.ChunkIndex, chunk.TokenCount, chunk.Language, searchConfig, model.NormalizeMemoryKind(chunk.Kind), model.NormalizeMemoryStability(chunk.Stability), model.DefaultRetrievalWeight(chunk.RetrievalWeight), chunk.Hint, chunk.TagsText, chunk.MetadataText, chunk.ProjectRepo, chunk.ID,
	)
	return err
}

func (s *PostgresStore) GetChunksByMemory(memoryID string, ownerID string) ([]model.Chunk, error) {
	if _, err := s.GetMemory(memoryID, ownerID); err != nil {
		return nil, err
	}
	return s.getChunksByMemory(memoryID)
}

func (s *PostgresStore) GetAccessibleChunksByMemory(memoryID string, userID string, hubIDs []string) ([]model.Chunk, error) {
	if _, err := s.GetAccessibleMemory(memoryID, userID, hubIDs); err != nil {
		return nil, err
	}
	return s.getChunksByMemory(memoryID)
}

func (s *PostgresStore) getChunksByMemory(memoryID string) ([]model.Chunk, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, memory_id, content, heading_chain, chunk_index, token_count, COALESCE(language, 'und'), COALESCE(search_config, 'simple'), kind, stability, retrieval_weight, COALESCE(hint, ''), COALESCE(tags_text, ''), COALESCE(metadata_text, ''), COALESCE(project_repo, ''), created_at, embedding
		FROM chunks WHERE memory_id = $1 ORDER BY chunk_index ASC`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []model.Chunk
	for rows.Next() {
		var c model.Chunk
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
			&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &c.Embedding); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, nil
}

func (s *PostgresStore) DeleteChunksByMemory(memoryID string, ownerID string) error {
	if _, err := s.GetMemory(memoryID, ownerID); err != nil {
		return err
	}
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM chunks WHERE memory_id = $1`, memoryID)
	return err
}

func (s *PostgresStore) AllChunks() []model.Chunk {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count, COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'), c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at, c.embedding
		FROM chunks c JOIN memories m ON c.memory_id = m.id WHERE m.state NOT IN ('archived', 'processing')`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var all []model.Chunk
	for rows.Next() {
		var c model.Chunk
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
			&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &c.Embedding); err != nil {
			continue
		}
		all = append(all, c)
	}
	return all
}

func (s *PostgresStore) SearchChunks(ctx context.Context, query string, queryEmbedding []float64, ownerID string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error) {
	return s.searchChunks(ctx, query, queryEmbedding, ownerID, nil, limit, filters, opts, chunkAccessOwnerOnly)
}

func pgvectorLiteral(v []float64) string {
	buf := make([]byte, 0, len(v)*12)
	buf = append(buf, '[')
	for i, f := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, []byte(fmt.Sprintf("%g", f))...)
	}
	buf = append(buf, ']')
	return string(buf)
}

// scan helpers

// scanMemoryWithAuthor scans a memory row that includes display enrichment columns.
// Column order MUST match memoryCols (postgres_memories.go) exactly.
func scanMemoryWithAuthor(row pgx.Row) (*model.Memory, error) {
	var m model.Memory
	err := row.Scan(
		&m.ID, &m.HubID, &m.OwnerID, &m.Title, &m.Content, &m.ContentType,
		&m.ContentHash, &m.Summary, &m.Hint, &m.Kind, &m.Stability, &m.RetrievalWeight, &m.AccessIntents, &m.Tags, &m.Boundary,
		&m.State, &m.Pinned, &m.Source, &m.SourceKind, &m.Metadata, &m.SourceAgent, &m.AssistedByAgent, &m.SourcePath, &m.HubReason, &m.ProjectContext, &m.EventDates,
		&m.BatchID, &m.Version, &m.AccessCount, &m.ShownCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt,
		&m.ProvenanceCreatedByType, &m.ProvenanceCreatedBySlug, &m.ProvenanceCreatedByDisplayName, &m.ProvenanceCreatedVia, &m.ProvenanceAssistedByAgent, &m.ProvenanceInitiationType, &m.ProvenanceAttributionSource,
		&m.SourceFetchHash, &m.UserFollowupMarker,
		&m.AuthorName, &m.AuthorAvatarURL, &m.HubName, &m.AgentDisplayName, &m.AgentIcon,
	)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}
	if m.ProjectContext == nil {
		m.ProjectContext = map[string]string{}
	}
	normalizeScannedMemory(&m)
	return &m, nil
}

// scanMemory scans a memory row WITHOUT author columns (for INSERT/UPDATE results).
// Column order MUST match memoryColsBasic (postgres_memories.go) exactly.
func scanMemory(row pgx.Row) (*model.Memory, error) {
	var m model.Memory
	err := row.Scan(
		&m.ID, &m.HubID, &m.OwnerID, &m.Title, &m.Content, &m.ContentType,
		&m.ContentHash, &m.Summary, &m.Hint, &m.Kind, &m.Stability, &m.RetrievalWeight, &m.AccessIntents, &m.Tags, &m.Boundary,
		&m.State, &m.Pinned, &m.Source, &m.SourceKind, &m.Metadata, &m.SourceAgent, &m.AssistedByAgent, &m.SourcePath, &m.HubReason, &m.ProjectContext, &m.EventDates,
		&m.BatchID, &m.Version, &m.AccessCount, &m.ShownCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt,
		&m.ProvenanceCreatedByType, &m.ProvenanceCreatedBySlug, &m.ProvenanceCreatedByDisplayName, &m.ProvenanceCreatedVia, &m.ProvenanceAssistedByAgent, &m.ProvenanceInitiationType, &m.ProvenanceAttributionSource,
		&m.SourceFetchHash, &m.UserFollowupMarker,
	)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}
	if m.ProjectContext == nil {
		m.ProjectContext = map[string]string{}
	}
	normalizeScannedMemory(&m)
	return &m, nil
}

// --- Hub-scoped chunk search ---

func (s *PostgresStore) SearchChunksForHubs(ctx context.Context, query string, queryEmbedding []float64, ownerID string, hubIDs []string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error) {
	if len(hubIDs) == 0 {
		return s.SearchChunks(ctx, query, queryEmbedding, ownerID, limit, filters, opts)
	}
	return s.searchChunks(ctx, query, queryEmbedding, ownerID, hubIDs, limit, filters, opts, chunkAccessOwnerOrHub)
}

// SearchChunksInHubs is the strict-hub variant. Required for the
// agent runtime's narrow-to-hub paths (chat with hub_id arg, dream
// per-hub) where user-owned memories in non-target hubs must NOT
// leak. The caller is responsible for hub-access authorization;
// this method trusts the hubIDs list it receives.
func (s *PostgresStore) SearchChunksInHubs(ctx context.Context, query string, queryEmbedding []float64, hubIDs []string, limit int, filters *model.SearchFilters, opts *SearchOptions) ([]model.Chunk, error) {
	if len(hubIDs) == 0 {
		return nil, ErrEmptyHubIDsForStrictSearch
	}
	// ownerID is unused by the strict access predicate but searchChunks
	// expects a non-empty value for the args slot at $3 (the predicate
	// SQL doesn't reference $3 in strict mode but the slot is still
	// bound to satisfy pgx's parameter accounting). Pass an empty
	// string — the SQL will not consult it.
	return s.searchChunks(ctx, query, queryEmbedding, "", hubIDs, limit, filters, opts, chunkAccessHubStrict)
}

// searchChunks uses hybrid search: pgvector cosine + tsvector full-text +
// pg_trgm fuzzy matching, fused with Reciprocal Rank Fusion (RRF, k=60).
//
// access controls the visibility predicate; see chunkSearchAccess docs.
func (s *PostgresStore) searchChunks(ctx context.Context, query string, queryEmbedding []float64, ownerID string, hubIDs []string, limit int, filters *model.SearchFilters, opts *SearchOptions, access chunkSearchAccess) ([]model.Chunk, error) {
	if limit <= 0 {
		limit = 50
	}
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedQuery := strings.TrimSpace(query)
	perMemoryCap := 3

	// Build the access predicate per mode. The baseArgs layout shifts
	// per mode — in particular, strict-hub does NOT bind ownerID:
	// passing an empty string would force Postgres to fall back to
	// type inference on a placeholder it can't infer, producing
	// "could not determine data type of parameter" errors. The
	// downstream topic-filter + opts param numbering uses
	// len(baseArgs) at the point of insertion so it adapts naturally.
	//
	// Layout per mode (all prefixed by $1=embedding when present;
	// these positions are AFTER that prepend):
	//   owner-only:    [limit, ownerID]            $2=limit $3=ownerID
	//   owner-or-hub:  [limit, ownerID, hubIDs]    $2=limit $3=ownerID $4=hubIDs
	//   hub-strict:    [limit, hubIDs]             $2=limit $3=hubIDs
	var accessPredicate string
	baseArgs := []any{limit}
	switch access {
	case chunkAccessOwnerOnly:
		accessPredicate = `m.owner_id = $3::uuid`
		baseArgs = append(baseArgs, ownerID)
	case chunkAccessOwnerOrHub:
		accessPredicate = `(m.owner_id = $3::uuid OR m.hub_id = ANY($4::uuid[]))`
		baseArgs = append(baseArgs, ownerID, hubIDs)
	case chunkAccessHubStrict:
		// ownerID is intentionally not bound; the predicate is
		// hub-only. Skipping the bind avoids Postgres type-inference
		// errors when callers pass an empty ownerID for this mode.
		accessPredicate = `m.hub_id = ANY($3::uuid[])`
		baseArgs = append(baseArgs, hubIDs)
	default:
		return nil, fmt.Errorf("store: searchChunks: unknown access mode %d", access)
	}

	// Topic filter: when set, restrict all search lanes to memories
	// assigned to this topic. Uses an EXISTS subquery so we don't need
	// to change JOINs or column lists in each lane's SQL.
	topicPredicate := ""
	if filters != nil && filters.TopicID != "" {
		topicParamN := len(baseArgs) + 2 // +1 for ownerID offset, +1 for next
		baseArgs = append(baseArgs, filters.TopicID)
		topicPredicate = fmt.Sprintf(` AND EXISTS (SELECT 1 FROM memory_topics mt WHERE mt.memory_id = m.id AND mt.topic_id = $%d::uuid)`, topicParamN)
	}

	type rankedChunk struct {
		chunk     model.Chunk
		vecRank   int
		txtRank   int
		trgRank   int
		trlRank   int // temporal lane rank
		fieldRank int // field-aware lane rank (title match)
	}
	chunkMap := make(map[string]*rankedChunk)
	var (
		vecChunks   []model.Chunk
		txtChunks   []model.Chunk
		trgChunks   []model.Chunk
		trlChunks   []model.Chunk // temporal lane results
		fieldChunks []model.Chunk // field-aware lane results (title match)
		wg          sync.WaitGroup
	)

	if len(queryEmbedding) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			embStr := pgvectorLiteral(queryEmbedding)
			querySQL := `
					SELECT
						c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count,
						COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'),
						c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at,
						1 - (c.embedding <=> $1::vector) AS similarity
					FROM chunks c
					JOIN memories m ON c.memory_id = m.id
					WHERE m.state NOT IN ('archived', 'processing')
						AND c.embedding IS NOT NULL`
			args := append([]any{embStr}, baseArgs...)
			querySQL += ` AND ` + accessPredicate + topicPredicate + `
					ORDER BY c.embedding <=> $1::vector
				LIMIT $2`
			rows, err := s.pool.Query(ctx, querySQL, args...)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("vector search failed", "error", err)
				}
				return
			}
			defer rows.Close()
			for rows.Next() {
				var c model.Chunk
				if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
					&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &c.RelevanceScore); err != nil {
					continue
				}
				vecChunks = append(vecChunks, c)
			}
		}()
	}

	if normalizedQuery != "" {
		wg.Add(2)
		go func() {
			defer wg.Done()
			textSQL := `
					SELECT
						c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count,
						COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'),
						c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at,
						ts_rank_cd(
							COALESCE(
								c.search_vector,
								to_tsvector(
									COALESCE(c.search_config, 'simple')::regconfig,
									COALESCE(
										NULLIF(c.search_text, ''),
										immutable_unaccent(lower(trim(concat_ws(' ',
											COALESCE(c.heading_chain, ''),
											COALESCE(c.hint, ''),
											COALESCE(c.tags_text, ''),
											COALESCE(c.metadata_text, ''),
											COALESCE(c.content, '')
										))))
									)
								)
							),
							websearch_to_tsquery(COALESCE(c.search_config, 'simple')::regconfig, $1)
						) AS rank
					FROM chunks c
					JOIN memories m ON c.memory_id = m.id
					WHERE m.state NOT IN ('archived', 'processing')
						AND ` + accessPredicate + topicPredicate + `
						AND COALESCE(
							c.search_vector,
							to_tsvector(
								COALESCE(c.search_config, 'simple')::regconfig,
								COALESCE(
									NULLIF(c.search_text, ''),
									immutable_unaccent(lower(trim(concat_ws(' ',
										COALESCE(c.heading_chain, ''),
										COALESCE(c.hint, ''),
										COALESCE(c.tags_text, ''),
										COALESCE(c.metadata_text, ''),
										COALESCE(c.content, '')
									))))
								)
							)
						) @@ websearch_to_tsquery(COALESCE(c.search_config, 'simple')::regconfig, $1)
					ORDER BY rank DESC
					LIMIT $2`
			args := append([]any{normalizedQuery}, baseArgs...)
			rows, err := s.pool.Query(ctx, textSQL, args...)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("full-text search failed", "error", err)
				}
				return
			}
			defer rows.Close()
			for rows.Next() {
				var c model.Chunk
				var textScore float64
				if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
					&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &textScore); err != nil {
					continue
				}
				txtChunks = append(txtChunks, c)
			}
		}()

		go func() {
			defer wg.Done()
			trigramQuery := strings.ToLower(normalizedQuery)
			trgSQL := `
					SELECT
						c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count,
						COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'),
						c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at,
						similarity(
							COALESCE(
								NULLIF(c.search_text, ''),
								immutable_unaccent(lower(trim(concat_ws(' ',
									COALESCE(c.heading_chain, ''),
									COALESCE(c.hint, ''),
									COALESCE(c.tags_text, ''),
									COALESCE(c.metadata_text, ''),
									COALESCE(c.content, '')
								))))
							),
							$1
						) AS sim
					FROM chunks c
					JOIN memories m ON c.memory_id = m.id
					WHERE m.state NOT IN ('archived', 'processing')
						AND ` + accessPredicate + topicPredicate + `
						AND COALESCE(
							NULLIF(c.search_text, ''),
							immutable_unaccent(lower(trim(concat_ws(' ',
								COALESCE(c.heading_chain, ''),
								COALESCE(c.hint, ''),
								COALESCE(c.tags_text, ''),
								COALESCE(c.metadata_text, ''),
								COALESCE(c.content, '')
							))))
						) % $1
					ORDER BY sim DESC
					LIMIT $2`
			args := append([]any{trigramQuery}, baseArgs...)
			rows, err := s.pool.Query(ctx, trgSQL, args...)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("trigram search failed", "error", err)
				}
				return
			}
			defer rows.Close()
			for rows.Next() {
				var c model.Chunk
				var sim float64
				if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
					&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &sim); err != nil {
					continue
				}
				trgChunks = append(trgChunks, c)
			}
		}()
	}

	// Temporal search lane — only launched when temporal bounds are provided
	if filters != nil && filters.TemporalStart != nil && filters.TemporalEnd != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Build dedicated args with stable parameter numbering
			// (same pattern as the field lane). Every parameter is
			// referenced exactly once — no unused placeholders.
			// Layout: $1 = ownerID (when used), $2 = hubIDs (when
			// used), then temporal params.
			var trlArgs []any
			var trlAccessPred string
			var nextParam int
			switch access {
			case chunkAccessOwnerOnly:
				trlAccessPred = `m.owner_id = $1::uuid`
				trlArgs = []any{ownerID}
				nextParam = 2
			case chunkAccessOwnerOrHub:
				trlAccessPred = `(m.owner_id = $1::uuid OR m.hub_id = ANY($2::uuid[]))`
				trlArgs = []any{ownerID, hubIDs}
				nextParam = 3
			case chunkAccessHubStrict:
				// Strict: hub-only predicate; ownerID not bound.
				trlAccessPred = `m.hub_id = ANY($1::uuid[])`
				trlArgs = []any{hubIDs}
				nextParam = 2
			}
			limitParam := nextParam
			startParam := nextParam + 1
			endParam := nextParam + 2
			trlArgs = append(trlArgs, limit, *filters.TemporalStart, *filters.TemporalEnd)

			trlTopicPred := ""
			if filters.TopicID != "" {
				topicParam := nextParam + 3
				trlArgs = append(trlArgs, filters.TopicID)
				trlTopicPred = fmt.Sprintf(` AND EXISTS (SELECT 1 FROM memory_topics mt WHERE mt.memory_id = m.id AND mt.topic_id = $%d::uuid)`, topicParam)
			}

			trlSQL := fmt.Sprintf(`
					SELECT
						c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count,
						COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'),
						c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at,
						1.0 AS score
					FROM chunks c
					JOIN memories m ON c.memory_id = m.id
					WHERE m.state NOT IN ('archived', 'processing')
						AND %s%s
						AND (
							(m.created_at >= $%d::timestamptz AND m.created_at < $%d::timestamptz)
							OR EXISTS (SELECT 1 FROM unnest(m.event_dates) d WHERE d >= $%d::timestamptz AND d < $%d::timestamptz)
						)
					ORDER BY m.created_at DESC
					LIMIT $%d`, trlAccessPred, trlTopicPred, startParam, endParam, startParam, endParam, limitParam)
			rows, err := s.pool.Query(ctx, trlSQL, trlArgs...)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("temporal search failed", "error", err)
				}
				return
			}
			defer rows.Close()
			for rows.Next() {
				var c model.Chunk
				var score float64
				if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
					&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &score); err != nil {
					continue
				}
				trlChunks = append(trlChunks, c)
			}
		}()
	}

	// Field-aware lane: search memory titles for discriminating terms.
	// Only chunk 0 per matched memory. Runs in parallel with other lanes.
	// Skipped when opts is nil or FieldTerms is empty.
	if opts != nil && len(opts.FieldTerms) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Build LIKE patterns: '%term%' for each discriminating term.
			// Terms are pre-filtered by the caller (no single CJK chars, no hub names).
			patterns := make([]string, len(opts.FieldTerms))
			for i, term := range opts.FieldTerms {
				// Escape SQL LIKE special chars, then wrap in %...%
				escaped := strings.ReplaceAll(strings.ReplaceAll(term, "%", "\\%"), "_", "\\_")
				patterns[i] = "%" + strings.ToLower(escaped) + "%"
			}

			// Field lane: title-only, chunk 0, ranked by title match strength.
			// Uses immutable_unaccent(lower(m.title)) to match the trigram index expression.
			// match_count = how many of the field patterns match the title (more = better).
			// Ties broken by pg_trgm similarity to the full query, then by recency.
			//
			// Build dedicated args with stable parameter numbering:
			// Layout per mode:
			//   owner-only:    $1=ownerID;            $N = patterns, $N+1 = query
			//   owner-or-hub:  $1=ownerID, $2=hubIDs; $N = patterns, $N+1 = query
			//   hub-strict:    $1=hubIDs;             $N = patterns, $N+1 = query
			var fieldArgs []any
			var fieldAccessPred string
			var nextParam int
			switch access {
			case chunkAccessOwnerOnly:
				fieldAccessPred = `m.owner_id = $1::uuid`
				fieldArgs = []any{ownerID}
				nextParam = 2
			case chunkAccessOwnerOrHub:
				fieldAccessPred = `(m.owner_id = $1::uuid OR m.hub_id = ANY($2::uuid[]))`
				fieldArgs = []any{ownerID, hubIDs}
				nextParam = 3
			case chunkAccessHubStrict:
				fieldAccessPred = `m.hub_id = ANY($1::uuid[])`
				fieldArgs = []any{hubIDs}
				nextParam = 2
			}
			patternsParam := nextParam
			queryParam := nextParam + 1
			fieldArgs = append(fieldArgs, patterns, normalizedQuery)

			// When project context is available, exclude memories from other
			// projects. Memories without project_context are allowed (general
			// knowledge). This prevents the field lane from rescuing wrong-
			// project memories just because their title matches a technology term.
			projectFilter := ""
			nextExtraParam := nextParam + 2
			if opts.ProjectRepo != "" {
				fieldArgs = append(fieldArgs, opts.ProjectRepo)
				projectFilter = fmt.Sprintf(`
					AND (m.project_context->>'repo' IS NULL
						OR m.project_context->>'repo' = ''
						OR m.project_context->>'repo' = $%d)`, nextExtraParam)
				nextExtraParam++
			}

			fieldTopicFilter := ""
			if filters != nil && filters.TopicID != "" {
				fieldArgs = append(fieldArgs, filters.TopicID)
				fieldTopicFilter = fmt.Sprintf(
					` AND EXISTS (SELECT 1 FROM memory_topics mt WHERE mt.memory_id = m.id AND mt.topic_id = $%d::uuid)`, nextExtraParam)
			}

			fieldSQL := fmt.Sprintf(`
				SELECT c.id, c.memory_id, c.content, c.heading_chain, c.chunk_index, c.token_count,
					COALESCE(c.language, 'und'), COALESCE(c.search_config, 'simple'),
					c.kind, c.stability, c.retrieval_weight, COALESCE(c.hint, ''), COALESCE(c.tags_text, ''), COALESCE(c.metadata_text, ''), COALESCE(c.project_repo, ''), c.created_at,
					(SELECT count(*) FROM unnest($%d::text[]) AS p WHERE immutable_unaccent(lower(m.title)) LIKE p)::float8 AS score
				FROM chunks c
				JOIN memories m ON c.memory_id = m.id
				WHERE c.chunk_index = 0
					AND m.state NOT IN ('archived', 'processing')
					AND %s
					AND immutable_unaccent(lower(m.title)) LIKE ANY($%d::text[])
					%s%s
				ORDER BY score DESC, similarity(immutable_unaccent(lower(m.title)), $%d) DESC, c.created_at DESC
				LIMIT 20`, patternsParam, fieldAccessPred, patternsParam, projectFilter, fieldTopicFilter, queryParam)

			rows, err := s.pool.Query(ctx, fieldSQL, fieldArgs...)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("field lane query failed", "error", err)
				}
				return
			}
			defer rows.Close()
			for rows.Next() {
				var c model.Chunk
				var score float64
				if err := rows.Scan(&c.ID, &c.MemoryID, &c.Content, &c.HeadingChain,
					&c.ChunkIndex, &c.TokenCount, &c.Language, &c.SearchConfig, &c.Kind, &c.Stability, &c.RetrievalWeight, &c.Hint, &c.TagsText, &c.MetadataText, &c.ProjectRepo, &c.CreatedAt, &score); err != nil {
					continue
				}
				fieldChunks = append(fieldChunks, c)
			}
		}()
	}

	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	for rank, c := range vecChunks {
		chunkMap[c.ID] = &rankedChunk{chunk: c, vecRank: rank, txtRank: -1, trgRank: -1, trlRank: -1, fieldRank: -1}
	}
	for rank, c := range txtChunks {
		if existing, ok := chunkMap[c.ID]; ok {
			existing.txtRank = rank
		} else {
			chunkMap[c.ID] = &rankedChunk{chunk: c, vecRank: -1, txtRank: rank, trgRank: -1, trlRank: -1, fieldRank: -1}
		}
	}
	for rank, c := range trgChunks {
		if existing, ok := chunkMap[c.ID]; ok {
			existing.trgRank = rank
		} else {
			chunkMap[c.ID] = &rankedChunk{chunk: c, vecRank: -1, txtRank: -1, trgRank: rank, trlRank: -1, fieldRank: -1}
		}
	}
	for rank, c := range trlChunks {
		if existing, ok := chunkMap[c.ID]; ok {
			existing.trlRank = rank
		} else {
			chunkMap[c.ID] = &rankedChunk{chunk: c, vecRank: -1, txtRank: -1, trgRank: -1, trlRank: rank, fieldRank: -1}
		}
	}
	for rank, c := range fieldChunks {
		if existing, ok := chunkMap[c.ID]; ok {
			existing.fieldRank = rank
		} else {
			chunkMap[c.ID] = &rankedChunk{chunk: c, vecRank: -1, txtRank: -1, trgRank: -1, trlRank: -1, fieldRank: rank}
		}
	}

	// Weighted RRF fusion — see package-level constants.
	type fusedResult struct {
		chunk model.Chunk
		score float64
	}
	var fused []fusedResult
	for _, rc := range chunkMap {
		score := 0.0
		if rc.vecRank >= 0 {
			score += rrfVectorW / float64(rrfK+rc.vecRank+1)
		}
		if rc.txtRank >= 0 {
			score += rrfBM25W / float64(rrfK+rc.txtRank+1)
		}
		if rc.trgRank >= 0 {
			score += rrfTrigramW / float64(rrfK+rc.trgRank+1)
		}
		if rc.trlRank >= 0 {
			score += rrfTemporalW / float64(rrfK+rc.trlRank+1)
		}
		if rc.fieldRank >= 0 {
			score += rrfFieldW / float64(rrfK+rc.fieldRank+1)
		}
		rc.chunk.RelevanceScore = score
		fused = append(fused, fusedResult{chunk: rc.chunk, score: score})
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].score > fused[j].score
	})

	filtered := make([]fusedResult, 0, minInt(len(fused), limit))
	perMemoryCounts := make(map[string]int)
	for _, item := range fused {
		if perMemoryCounts[item.chunk.MemoryID] >= perMemoryCap {
			continue
		}
		filtered = append(filtered, item)
		perMemoryCounts[item.chunk.MemoryID]++
		if len(filtered) >= limit {
			break
		}
	}

	out := make([]model.Chunk, len(filtered))
	for i, f := range filtered {
		out[i] = f.chunk
	}
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// scanMemoryFromRowsBasic scans from rows WITHOUT author columns (internal/worker use).
// Column order MUST match memoryColsBasic (postgres_memories.go) exactly.
func scanMemoryFromRowsBasic(rows pgx.Rows) (*model.Memory, error) {
	var m model.Memory
	err := rows.Scan(
		&m.ID, &m.HubID, &m.OwnerID, &m.Title, &m.Content, &m.ContentType,
		&m.ContentHash, &m.Summary, &m.Hint, &m.Kind, &m.Stability, &m.RetrievalWeight, &m.AccessIntents, &m.Tags, &m.Boundary,
		&m.State, &m.Pinned, &m.Source, &m.SourceKind, &m.Metadata, &m.SourceAgent, &m.AssistedByAgent, &m.SourcePath, &m.HubReason, &m.ProjectContext, &m.EventDates,
		&m.BatchID, &m.Version, &m.AccessCount, &m.ShownCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt,
		&m.ProvenanceCreatedByType, &m.ProvenanceCreatedBySlug, &m.ProvenanceCreatedByDisplayName, &m.ProvenanceCreatedVia, &m.ProvenanceAssistedByAgent, &m.ProvenanceInitiationType, &m.ProvenanceAttributionSource,
		&m.SourceFetchHash, &m.UserFollowupMarker,
	)
	if err != nil {
		return nil, err
	}
	if m.ProjectContext == nil {
		m.ProjectContext = map[string]string{}
	}
	normalizeScannedMemory(&m)
	return &m, nil
}

// scanMemoryFromRows scans from rows including display enrichment columns.
// Column order MUST match memoryCols (postgres_memories.go) exactly.
func scanMemoryFromRows(rows pgx.Rows) (*model.Memory, error) {
	var m model.Memory
	err := rows.Scan(
		&m.ID, &m.HubID, &m.OwnerID, &m.Title, &m.Content, &m.ContentType,
		&m.ContentHash, &m.Summary, &m.Hint, &m.Kind, &m.Stability, &m.RetrievalWeight, &m.AccessIntents, &m.Tags, &m.Boundary,
		&m.State, &m.Pinned, &m.Source, &m.SourceKind, &m.Metadata, &m.SourceAgent, &m.AssistedByAgent, &m.SourcePath, &m.HubReason, &m.ProjectContext, &m.EventDates,
		&m.BatchID, &m.Version, &m.AccessCount, &m.ShownCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt,
		&m.ProvenanceCreatedByType, &m.ProvenanceCreatedBySlug, &m.ProvenanceCreatedByDisplayName, &m.ProvenanceCreatedVia, &m.ProvenanceAssistedByAgent, &m.ProvenanceInitiationType, &m.ProvenanceAttributionSource,
		&m.SourceFetchHash, &m.UserFollowupMarker,
		&m.AuthorName, &m.AuthorAvatarURL, &m.HubName, &m.AgentDisplayName, &m.AgentIcon,
	)
	if err != nil {
		return nil, err
	}
	if m.ProjectContext == nil {
		m.ProjectContext = map[string]string{}
	}
	normalizeScannedMemory(&m)
	return &m, nil
}

func normalizeScannedMemory(m *model.Memory) {
	m.Kind = model.NormalizeMemoryKind(m.Kind)
	m.Stability = model.NormalizeMemoryStability(m.Stability)
	m.RetrievalWeight = model.DefaultRetrievalWeight(m.RetrievalWeight)
	if m.AccessIntents == nil {
		m.AccessIntents = map[string]int{}
	}
	m.Provenance = model.BuildMemoryProvenance(m)
}
