package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// memoryColsBasic lists memory columns without author JOINs.
// Use for internal/worker queries that do not display to users.
const memoryColsBasic = `id, hub_id, owner_id, title, content, content_type, content_hash,
	summary, hint, kind, stability, retrieval_weight, access_intents, tags, boundary, state, pinned, source, COALESCE(source_kind, '') AS source_kind, COALESCE(metadata, '{}'::jsonb) AS metadata, source_agent, COALESCE(assisted_by_agent, '') AS assisted_by_agent, source_path, COALESCE(hub_reason, '') AS hub_reason, project_context, event_dates,
	batch_id, version, access_count, shown_count, created_at, updated_at, accessed_at,
	COALESCE(created_by_type, ''), COALESCE(created_by_slug, ''), COALESCE(created_by_display_name, ''), COALESCE(created_via, ''), COALESCE(assisted_by_agent, ''), COALESCE(initiation_type, ''), COALESCE(attribution_source, ''),
	COALESCE(source_fetch_hash, '') AS source_fetch_hash, COALESCE(user_followup_marker, '') AS user_followup_marker`

// memoryCols lists memory columns with display enrichment.
// Always use with memoryFrom.
const memoryCols = `m.id, m.hub_id, m.owner_id, m.title, m.content, m.content_type, m.content_hash,
	m.summary, m.hint, m.kind, m.stability, m.retrieval_weight, m.access_intents, m.tags, m.boundary, m.state, m.pinned, m.source, COALESCE(m.source_kind, '') AS source_kind, COALESCE(m.metadata, '{}'::jsonb) AS metadata, m.source_agent, COALESCE(m.assisted_by_agent, '') AS assisted_by_agent, m.source_path, COALESCE(m.hub_reason, '') AS hub_reason, m.project_context, m.event_dates,
	m.batch_id, m.version, m.access_count, m.shown_count, m.created_at, m.updated_at, m.accessed_at,
	COALESCE(m.created_by_type, ''), COALESCE(m.created_by_slug, ''), COALESCE(m.created_by_display_name, ''), COALESCE(m.created_via, ''), COALESCE(m.assisted_by_agent, ''), COALESCE(m.initiation_type, ''), COALESCE(m.attribution_source, ''),
	COALESCE(m.source_fetch_hash, '') AS source_fetch_hash, COALESCE(m.user_followup_marker, '') AS user_followup_marker,
	COALESCE(u.display_name, u.name, '') AS author_name, COALESCE(u.avatar_url, '') AS author_avatar_url,
	COALESCE(h.name, '') AS hub_name,
	COALESCE(ca.display_name, '') AS agent_display_name, COALESCE(ca.icon, '') AS agent_icon`

const memoryAgentSlugExpr = `COALESCE(NULLIF(m.created_by_slug, ''), NULLIF(m.source_agent, ''))`

// memoryFrom is the FROM clause with LEFT JOINs for display enrichment.
const memoryFrom = `memories m LEFT JOIN users u ON m.owner_id = u.id LEFT JOIN hubs h ON m.hub_id = h.id LEFT JOIN connected_agents ca ON m.owner_id = ca.owner_id AND ` + memoryAgentSlugExpr + ` = ca.agent_name`

const recentActorExpr = `CASE
	WHEN COALESCE(h.hub_type, '') = 'team' AND NULLIF(COALESCE(u.display_name, u.name, ''), '') IS NOT NULL THEN 'author:' || COALESCE(u.display_name, u.name, '')
	WHEN ` + memoryAgentSlugExpr + ` IS NOT NULL THEN 'agent:' || ` + memoryAgentSlugExpr + `
	WHEN NULLIF(COALESCE(u.display_name, u.name, ''), '') IS NOT NULL THEN 'author:' || COALESCE(u.display_name, u.name, '')
	ELSE 'self'
END`

func buildMemoryListWhere(opts ListOptions) ([]string, []any) {
	scope := opts.Scope
	visPred, visArgs := scope.SQLFilter("m", 1)
	where := []string{visPred, "m.state != 'archived'"}
	args := visArgs
	nextParam := func() string { return fmt.Sprintf("$%d", len(args)+1) }

	if opts.HubID != "" {
		p := nextParam()
		where = append(where, "m.hub_id = "+p+"::uuid")
		args = append(args, opts.HubID)
	}
	if opts.TopicID != "" {
		p := nextParam()
		where = append(where, "EXISTS (SELECT 1 FROM memory_topics mt WHERE mt.memory_id = m.id AND mt.topic_id = "+p+"::uuid)")
		args = append(args, opts.TopicID)
	}
	if opts.CreatedAfter != nil {
		p := nextParam()
		where = append(where, "m.created_at >= "+p+"::timestamptz")
		args = append(args, *opts.CreatedAfter)
	}
	if opts.Actor != "" && opts.Actor != "all" {
		p := nextParam()
		where = append(where, recentActorExpr+" = "+p)
		args = append(args, opts.Actor)
	}
	if opts.Kind != "" {
		p := nextParam()
		where = append(where, "m.kind = "+p)
		args = append(args, opts.Kind)
	}

	return where, args
}

func (s *PostgresStore) CreateMemory(memory *model.Memory) error {
	ctx := context.Background()
	model.NormalizeMemoryProvenanceFields(memory)
	projCtx := memory.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}
	eventDates := memory.EventDates
	if eventDates == nil {
		eventDates = []time.Time{}
	}
	accessIntents := memory.AccessIntents
	if accessIntents == nil {
		accessIntents = map[string]int{}
	}
	hubReason := strings.TrimSpace(memory.HubReason)
	// SourceKind: empty string normalizes to NULL in DB so the partial
	// indexes (`WHERE source_kind IS NOT NULL` / `WHERE source_kind =
	// 'onboarding-seed'`) only cover real entries. Plan 23 §4.1.
	var sourceKindArg any
	if k := strings.TrimSpace(memory.SourceKind); k != "" {
		sourceKindArg = k
	}
	// Metadata: nil map normalizes to NULL in DB. Empty map would still
	// be a valid JSONB value `{}`, but storing NULL keeps the row
	// representationally distinct ("no metadata at all" vs "metadata
	// container present, no keys").
	var metadataArg any
	if len(memory.Metadata) > 0 {
		metadataArg = memory.Metadata
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash,
			summary, hint, kind, stability, retrieval_weight, access_intents, tags, boundary, state, pinned, source, source_kind, metadata, source_agent, assisted_by_agent, source_path, hub_reason, project_context, event_dates,
			batch_id, version, access_count, shown_count, created_at, updated_at, accessed_at,
			created_by_type, created_by_slug, created_by_display_name, created_via, initiation_type, attribution_source)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39)`,
		memory.ID, memory.HubID, memory.OwnerID, memory.Title, memory.Content, memory.ContentType,
		memory.ContentHash, memory.Summary, memory.Hint, model.NormalizeMemoryKind(memory.Kind), model.NormalizeMemoryStability(memory.Stability), model.DefaultRetrievalWeight(memory.RetrievalWeight), accessIntents, memory.Tags, memory.Boundary,
		memory.State, memory.Pinned, memory.Source, sourceKindArg, metadataArg, memory.SourceAgent, memory.AssistedByAgent, memory.SourcePath, hubReason, projCtx, eventDates,
		memory.BatchID, memory.Version, memory.AccessCount, memory.ShownCount, memory.CreatedAt, memory.UpdatedAt, memory.AccessedAt,
		memory.ProvenanceCreatedByType, memory.ProvenanceCreatedBySlug, memory.ProvenanceCreatedByDisplayName, memory.ProvenanceCreatedVia, memory.ProvenanceInitiationType, memory.ProvenanceAttributionSource,
	)
	return err
}

func (s *PostgresStore) GetMemory(id string, ownerID string) (*model.Memory, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE m.id = $1::uuid AND m.owner_id = $2::uuid`, memoryCols, memoryFrom),
		id, ownerID)
	return scanMemoryWithAuthor(row)
}

func (s *PostgresStore) GetMemoryForAdmin(id string) (*model.Memory, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE m.id = $1::uuid`, memoryCols, memoryFrom),
		id)
	return scanMemoryWithAuthor(row)
}

func (s *PostgresStore) FindSuspiciousMetadata(ctx context.Context, limit int) ([]SuspiciousMetadataRow, error) {
	if limit <= 0 {
		limit = 500
	}
	// Match JSON-shaped metadata, JSON field syntax, or label-prefixed artifacts.
	// Uses field syntax regex instead of plain ILIKE to avoid false-positives on
	// valid content that mentions "title" or "summary" as prose.
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, title, summary
		 FROM memories
		 WHERE state != 'archived'
		   AND (
		     summary ~ '^\s*[\{\[]'
		     OR summary ~ '"(?:summary|title)"\s*:'
		     OR summary ~* '^\s*(?:summary|title)\s*:'
		     OR title ~ '^\s*[\{\[]'
		     OR title ~ '"(?:summary|title)"\s*:'
		     OR title ~* '^\s*(?:summary|title)\s*:'
		   )
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("find suspicious metadata: %w", err)
	}
	defer rows.Close()
	var result []SuspiciousMetadataRow
	for rows.Next() {
		var r SuspiciousMetadataRow
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Title, &r.Summary); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ErrMemoryNotFound is returned by memory-detail lookups when no
// row matches the access-checked predicates. Distinguishing this
// sentinel from generic database errors lets handlers map it to
// HTTP 404 cleanly. Used by the scope-aware helper as the
// fail-closed return when a strict-bounded principal has no
// resolvable hubs (the principal MUST observe "memory does not
// exist for me" rather than "memory exists somewhere I can't
// see," which would leak existence).
var ErrMemoryNotFound = errors.New("store: memory not found")

func (s *PostgresStore) GetAccessibleMemory(id string, userID string, hubIDs []string) (*model.Memory, error) {
	ctx := context.Background()
	query := fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.id = $1::uuid
		  AND m.owner_id = $2::uuid`, memoryCols, memoryFrom)
	args := []any{id, userID}
	if len(hubIDs) > 0 {
		query = fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.id = $1::uuid
		  AND (m.owner_id = $2::uuid OR m.hub_id = ANY($3::uuid[]))`, memoryCols, memoryFrom)
		args = append(args, hubIDs)
	}
	row := s.pool.QueryRow(ctx, query, args...)
	return scanMemoryWithAuthor(row)
}

// GetMemoryInHubs is the strict-hub memory-detail lookup used by
// scope-bounded principals (OAuth grant or API key with
// HubScopeAllowlist). Filter is `m.hub_id = ANY($hubs)` EXACTLY
// — owner-OR-hub would let a scoped grant fetch a memory the
// user owns in a non-granted hub by guessing its ID, which makes
// the OAuth scope meaningless. Same security boundary as
// SearchChunksInHubs at the chunk-search layer; together they
// close the scoped-principal leak class.
//
// Returns ErrEmptyHubIDsForStrictSearch on empty hubIDs to keep
// the fail-closed contract uniform across strict-hub store
// methods. The handler-layer scope-aware helper short-circuits
// the empty-hubs case to ErrMemoryNotFound before reaching this
// method; the store-side guard is the belt-and-braces defense
// against a misuse that would otherwise return whatever memory
// matched id (any owner, any hub).
func (s *PostgresStore) GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error) {
	if len(hubIDs) == 0 {
		return nil, ErrEmptyHubIDsForStrictSearch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	query := fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.id = $1::uuid
		  AND m.hub_id = ANY($2::uuid[])`, memoryCols, memoryFrom)
	row := s.pool.QueryRow(ctx, query, id, hubIDs)
	return scanMemoryWithAuthor(row)
}

func (s *PostgresStore) GetAccessibleMemories(ids []string, userID string, hubIDs []string) (map[string]*model.Memory, error) {
	result := make(map[string]*model.Memory)
	if len(ids) == 0 {
		return result, nil
	}

	ctx := context.Background()
	query := fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.id = ANY($1::uuid[])
		  AND m.owner_id = $2::uuid`, memoryCols, memoryFrom)
	args := []any{ids, userID}
	if len(hubIDs) > 0 {
		query = fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.id = ANY($1::uuid[])
		  AND (m.owner_id = $2::uuid OR m.hub_id = ANY($3::uuid[]))`, memoryCols, memoryFrom)
		args = append(args, hubIDs)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		memory, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, err
		}
		result[memory.ID] = memory
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMemoryBySourcePath(sourcePath string, ownerID string, hubID string) (*model.Memory, error) {
	if sourcePath == "" {
		return nil, fmt.Errorf("memory not found for source_path: %s", sourcePath)
	}
	ctx := context.Background()
	query := fmt.Sprintf(`SELECT %s FROM memories WHERE source_path = $1 AND owner_id = $2::uuid`, memoryColsBasic)
	args := []any{sourcePath, ownerID}
	if hubID != "" {
		query += ` AND hub_id = $3::uuid`
		args = append(args, hubID)
	}
	query += ` LIMIT 1`
	row := s.pool.QueryRow(ctx, query, args...)
	return scanMemory(row)
}

func (s *PostgresStore) GetMemoryByContentHash(hash string, ownerID string, hubID string) (*model.Memory, error) {
	ctx := context.Background()
	query := fmt.Sprintf(`SELECT %s FROM memories WHERE content_hash = $1 AND owner_id = $2::uuid`, memoryColsBasic)
	args := []any{hash, ownerID}
	if hubID != "" {
		query += ` AND hub_id = $3::uuid`
		args = append(args, hubID)
	}
	query += ` LIMIT 1`
	row := s.pool.QueryRow(ctx, query, args...)
	return scanMemory(row)
}

func (s *PostgresStore) ListMemories(ownerID string, limit int) ([]model.Memory, error) {
	ctx := context.Background()
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE m.owner_id = $1::uuid AND m.state != 'archived' ORDER BY m.created_at DESC LIMIT $2`, memoryCols, memoryFrom),
		ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []model.Memory
	for rows.Next() {
		m, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	if memories == nil {
		memories = make([]model.Memory, 0)
	}
	return memories, rows.Err()
}

func (s *PostgresStore) CountRecentMemoriesByHub(scope VisibilityScope, hubID string, createdAfter time.Time) (int, error) {
	ctx := context.Background()
	where, args := buildMemoryListWhere(ListOptions{
		Scope:        scope,
		HubID:        hubID,
		CreatedAfter: &createdAfter,
	})

	var count int
	row := s.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM `+memoryFrom+` WHERE `+strings.Join(where, " AND "),
		args...,
	)
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ListArchiveCandidates returns memories that are candidates for archival.
// Pre-filters: active, not pinned, zero access count, older than minAgeDays.
// Ordered by creation date ASC (oldest first — most likely to be stale).
func (s *PostgresStore) ListSeedMemoryTemplates(ctx context.Context) ([]model.Memory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.hub_id = $1::uuid
		  AND m.source_kind = $2
		  AND m.state = 'active'
		ORDER BY m.created_at ASC`, memoryCols, memoryFrom)
	rows, err := s.pool.Query(ctx, query, model.TutorialHubID, model.MemorySourceKindOnboard)
	if err != nil {
		return nil, fmt.Errorf("list seed templates: %w", err)
	}
	defer rows.Close()
	var out []model.Memory
	for rows.Next() {
		m, err := scanMemoryWithAuthor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ListSeedMemoryTemplatesAdmin returns ALL seed templates including
// archived rows — plan 23 §5.7 admin surface. Diverges from
// ListSeedMemoryTemplates only in the state predicate so admins can
// toggle `archived` rows back to `active`.
func (s *PostgresStore) ListSeedMemoryTemplatesAdmin(ctx context.Context) ([]model.Memory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := fmt.Sprintf(`SELECT %s FROM %s
		WHERE m.hub_id = $1::uuid
		  AND m.source_kind = $2
		ORDER BY m.created_at ASC`, memoryCols, memoryFrom)
	rows, err := s.pool.Query(ctx, query, model.TutorialHubID, model.MemorySourceKindOnboard)
	if err != nil {
		return nil, fmt.Errorf("list seed templates (admin): %w", err)
	}
	defer rows.Close()
	var out []model.Memory
	for rows.Next() {
		m, err := scanMemoryWithAuthor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// DeleteSeedMemoryTemplate hard-deletes one onboarding-seed template.
// The WHERE clause is scoped on (id, owner=SystemUserID, hub=TutorialHubID,
// source_kind=onboarding-seed) as defense in depth — even though admin
// auth gates the route, this means a malformed id or path-confusion bug
// can't translate this admin-only delete into a vector for removing
// arbitrary user memories. Existing per-user copies (different
// owner_id) stay (plan 23 principle 4). Chunks on the deleted template
// row cascade via FK, but the worker's per-user copies don't have
// chunks pointing at the template, so user data is untouched.
func (s *PostgresStore) DeleteSeedMemoryTemplate(id string) (bool, error) {
	ctx := context.Background()
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM memories
		 WHERE id = $1::uuid
		   AND owner_id = $2::uuid
		   AND hub_id = $3::uuid
		   AND source_kind = $4`,
		id, model.SystemUserID, model.TutorialHubID, model.MemorySourceKindOnboard)
	if err != nil {
		return false, fmt.Errorf("delete seed memory template: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteSeedCopiesForOwner removes every onboarding-seed copy owned by
// the given user. Used by the admin "sync seeds to my account" path
// to reset the caller's seed-copy set before re-running RunSeedCopy.
// Templates themselves (rows in the system tutorial hub owned by the
// system user) are NOT touched — the partial unique index from
// migration 002 already gates on owner_id, so this delete is safe to
// run without an extra `hub_id <>` clause. Chunks cascade via FK.
func (s *PostgresStore) DeleteSeedCopiesForOwner(ownerID string) (int64, error) {
	ctx := context.Background()
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM memories
		 WHERE owner_id = $1::uuid
		   AND source_kind = $2`,
		ownerID, model.MemorySourceKindOnboard)
	if err != nil {
		return 0, fmt.Errorf("delete seed copies for owner: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PostgresStore) ListArchiveCandidates(hubID string, minAgeDays int, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 500
	}
	ctx := context.Background()
	cutoff := time.Now().AddDate(0, 0, -minAgeDays)
	rows, err := s.pool.Query(ctx,
		`SELECT `+memoryColsBasic+` FROM memories
		WHERE hub_id = $1::uuid
		  AND state = 'active'
		  AND pinned = false
		  AND access_count = 0
		  AND created_at < $2
		ORDER BY created_at ASC
		LIMIT $3`, hubID, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []model.Memory
	for rows.Next() {
		m, err := scanMemoryFromRowsBasic(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	if memories == nil {
		memories = make([]model.Memory, 0)
	}
	return memories, rows.Err()
}

// ListMemoriesPaginated returns memories with cursor-based pagination.
// sort: "newest" (default) or "relevant" (by access_count desc).
// cursor: empty for first page, or the last memory's sort value for next page.
// Returns memories + next cursor (empty if no more pages).
const MaxPageSize = 50

func (s *PostgresStore) ListMemoriesPaginated(opts ListOptions) ([]model.Memory, string, int, error) {
	ctx := context.Background()
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	// ── Dynamic query builder ──
	// Uses memoryCols + memoryFrom for automatic display enrichment.
	where, args := buildMemoryListWhere(opts)
	nextParam := func() string { return fmt.Sprintf("$%d", len(args)+1) }

	whereStr := strings.Join(where, " AND ")

	// Total count (with all filters applied).
	// Uses the same join shape as the page query because actor filtering depends
	// on joined user display names and should never diverge from the row query.
	var totalCount int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM `+memoryFrom+` WHERE `+whereStr,
		args...).Scan(&totalCount)
	if err != nil {
		return nil, "", 0, err
	}

	// Cursor + sort
	sortBy := opts.Sort
	var orderBy string
	if sortBy == "relevant" {
		orderBy = "m.access_count DESC, m.created_at DESC"
		if opts.Cursor != "" {
			parts := strings.SplitN(opts.Cursor, "|", 2)
			if len(parts) == 2 {
				p1, p2 := nextParam(), nextParam()
				where = append(where, fmt.Sprintf("(m.access_count, m.created_at) < (%s::int, %s::timestamptz)", p1, p2))
				args = append(args, parts[0], parts[1])
			}
		}
	} else {
		orderBy = "m.created_at DESC"
		if opts.Cursor != "" {
			p := nextParam()
			where = append(where, "m.created_at < "+p+"::timestamptz")
			args = append(args, opts.Cursor)
		}
	}

	whereStr = strings.Join(where, " AND ")
	limitParam := nextParam()
	args = append(args, limit+1)

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %s",
		memoryCols, memoryFrom, whereStr, orderBy, limitParam)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()

	var memories []model.Memory
	for rows.Next() {
		m, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, "", 0, err
		}
		memories = append(memories, *m)
	}

	// Determine next cursor
	nextCursor := ""
	if len(memories) > limit {
		memories = memories[:limit] // trim the extra one
		last := memories[len(memories)-1]
		switch sortBy {
		case "relevant":
			nextCursor = fmt.Sprintf("%d|%s", last.AccessCount, last.CreatedAt.Format(time.RFC3339Nano))
		default:
			nextCursor = last.CreatedAt.Format(time.RFC3339Nano)
		}
	}

	if memories == nil {
		memories = make([]model.Memory, 0)
	}
	return memories, nextCursor, totalCount, nil
}

// ListMemoriesInHubs is the strict-hub memory list used by the
// agent runtime. Unlike ListMemoriesPaginated it never broadens to
// owner_id; the caller has already resolved the session's live
// hub allowlist and this query must honor that list exactly.
func (s *PostgresStore) ListMemoriesInHubs(ctx context.Context, opts StrictHubListOptions) ([]model.Memory, string, int, error) {
	if len(opts.HubIDs) == 0 {
		return nil, "", 0, ErrEmptyHubIDsForStrictSearch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	args := []any{opts.HubIDs}
	where := []string{"m.hub_id = ANY($1::uuid[])", "m.state != 'archived'"}
	nextParam := func() string { return fmt.Sprintf("$%d", len(args)+1) }
	if opts.HubID != "" {
		p := nextParam()
		where = append(where, "m.hub_id = "+p+"::uuid")
		args = append(args, opts.HubID)
	}
	if opts.TopicID != "" {
		p := nextParam()
		where = append(where, "EXISTS (SELECT 1 FROM memory_topics mt WHERE mt.memory_id = m.id AND mt.topic_id = "+p+"::uuid)")
		args = append(args, opts.TopicID)
	}
	if !opts.Since.IsZero() {
		p := nextParam()
		where = append(where, "m.updated_at >= "+p+"::timestamptz")
		args = append(args, opts.Since.UTC())
	}

	whereStr := strings.Join(where, " AND ")
	var totalCount int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM `+memoryFrom+` WHERE `+whereStr,
		args...).Scan(&totalCount); err != nil {
		return nil, "", 0, err
	}

	sortBy := opts.Sort
	var orderBy string
	if sortBy == "relevant" {
		orderBy = "m.access_count DESC, m.created_at DESC"
		if opts.Cursor != "" {
			parts := strings.SplitN(opts.Cursor, "|", 2)
			if len(parts) == 2 {
				p1, p2 := nextParam(), nextParam()
				where = append(where, fmt.Sprintf("(m.access_count, m.created_at) < (%s::int, %s::timestamptz)", p1, p2))
				args = append(args, parts[0], parts[1])
			}
		}
	} else {
		orderBy = "m.created_at DESC"
		if opts.Cursor != "" {
			p := nextParam()
			where = append(where, "m.created_at < "+p+"::timestamptz")
			args = append(args, opts.Cursor)
		}
	}

	whereStr = strings.Join(where, " AND ")
	limitParam := nextParam()
	args = append(args, limit+1)
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %s",
		memoryCols, memoryFrom, whereStr, orderBy, limitParam)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()

	memories := make([]model.Memory, 0, limit)
	for rows.Next() {
		m, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, "", 0, err
		}
		memories = append(memories, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}

	nextCursor := ""
	if len(memories) > limit {
		memories = memories[:limit]
		last := memories[len(memories)-1]
		switch sortBy {
		case "relevant":
			nextCursor = fmt.Sprintf("%d|%s", last.AccessCount, last.CreatedAt.Format(time.RFC3339Nano))
		default:
			nextCursor = last.CreatedAt.Format(time.RFC3339Nano)
		}
	}
	return memories, nextCursor, totalCount, nil
}

func (s *PostgresStore) ListActorCounts(opts ListOptions) (map[string]int, error) {
	ctx := context.Background()
	facetOpts := opts
	facetOpts.Actor = ""
	where, args := buildMemoryListWhere(facetOpts)

	rows, err := s.pool.Query(ctx,
		`SELECT actor_value, COUNT(*) FROM (
			SELECT `+recentActorExpr+` AS actor_value
			FROM `+memoryFrom+`
			WHERE `+strings.Join(where, " AND ")+`
		) actor_values
		GROUP BY actor_value`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var actor string
		var count int
		if err := rows.Scan(&actor, &count); err != nil {
			return nil, err
		}
		counts[actor] = count
	}
	return counts, rows.Err()
}

func (s *PostgresStore) UpdateMemory(memory *model.Memory) error {
	ctx := context.Background()
	model.NormalizeMemoryProvenanceFields(memory)
	projCtx := memory.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}
	eventDates := memory.EventDates
	if eventDates == nil {
		eventDates = []time.Time{}
	}
	accessIntents := memory.AccessIntents
	if accessIntents == nil {
		accessIntents = map[string]int{}
	}
	hubReason := strings.TrimSpace(memory.HubReason)
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET title=$1, content=$2, content_type=$3, content_hash=$4,
			summary=$5, hint=$6, kind=$7, stability=$8, retrieval_weight=$9, access_intents=$10, tags=$11, boundary=$12, state=$13, pinned=$14,
			source=$15, source_agent=$16, assisted_by_agent=$17, source_path=$18, hub_reason=$19, project_context=$20, event_dates=$21, batch_id=$22, version=$23, access_count=$24,
			updated_at=$25, accessed_at=$26, created_by_type=$27, created_by_slug=$28, created_by_display_name=$29, created_via=$30, initiation_type=$31, attribution_source=$32
		WHERE id = $33`,
		memory.Title, memory.Content, memory.ContentType, memory.ContentHash,
		memory.Summary, memory.Hint, model.NormalizeMemoryKind(memory.Kind), model.NormalizeMemoryStability(memory.Stability), model.DefaultRetrievalWeight(memory.RetrievalWeight), accessIntents, memory.Tags, memory.Boundary,
		memory.State, memory.Pinned, memory.Source, memory.SourceAgent, memory.AssistedByAgent, memory.SourcePath, hubReason, projCtx, eventDates,
		memory.BatchID, memory.Version, memory.AccessCount, memory.UpdatedAt, memory.AccessedAt,
		memory.ProvenanceCreatedByType, memory.ProvenanceCreatedBySlug, memory.ProvenanceCreatedByDisplayName, memory.ProvenanceCreatedVia, memory.ProvenanceInitiationType, memory.ProvenanceAttributionSource,
		memory.ID,
	)
	return err
}

// SetUserFollowupMarker writes the marker text to the
// user_followup_marker column on the given memory. The owner_id
// predicate guards against cross-owner writes if a future caller
// passes the wrong id; the canonical caller is the ingest
// pipeline, which already has the trusted ownerID from the push
// flow.
//
// Empty marker is written as the empty string (NOT NULL) so a
// trigger evaluator can distinguish "scan ran and found nothing"
// from "scan never ran." See the Store interface comment for the
// three-state semantics.
func (s *PostgresStore) SetUserFollowupMarker(ctx context.Context, memoryID string, ownerID string, marker string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET user_followup_marker = $1
		 WHERE id = $2::uuid AND owner_id = $3::uuid`,
		marker, memoryID, ownerID)
	if err != nil {
		return fmt.Errorf("set user_followup_marker: %w", err)
	}
	return nil
}

// IncrementMemoryShownBatch atomically increments shown_count for memories
// that appeared in recall results. Enforces visibility scope at the data layer.
// Used by reinforceResults — does NOT affect access_count or retrieval scoring.
func (s *PostgresStore) IncrementMemoryShownBatch(ctx context.Context, memoryIDs []string, ownerID string, hubIDs []string) error {
	if len(memoryIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET shown_count = shown_count + 1
		 WHERE id = ANY($1::uuid[])
		   AND (owner_id = $2::uuid OR hub_id = ANY($3::uuid[]))`,
		memoryIDs, ownerID, hubIDs)
	return err
}

// IncrementMemoryAccessed atomically increments access_count and updates
// accessed_at for a single memory. Enforces visibility scope at the data layer.
// Used by GET /v1/memories/:id — this IS the signal that feeds decay.Multiplier.
func (s *PostgresStore) IncrementMemoryAccessed(ctx context.Context, memoryID string, ownerID string, hubIDs []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET access_count = access_count + 1, accessed_at = now()
		 WHERE id = $1::uuid
		   AND (owner_id = $2::uuid OR hub_id = ANY($3::uuid[]))`,
		memoryID, ownerID, hubIDs)
	return err
}

// IncrementMemoryAccessedBatch atomically increments access_count and updates
// accessed_at for multiple memories. Enforces visibility scope at the data layer.
// Used by Ask synthesis to record that source memories were used in an answer.
func (s *PostgresStore) IncrementMemoryAccessedBatch(ctx context.Context, memoryIDs []string, ownerID string, hubIDs []string) error {
	if len(memoryIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE memories SET access_count = access_count + 1, accessed_at = now()
		 WHERE id = ANY($1::uuid[])
		   AND (owner_id = $2::uuid OR hub_id = ANY($3::uuid[]))`,
		memoryIDs, ownerID, hubIDs)
	return err
}

func (s *PostgresStore) DeleteMemory(id string, ownerID string) error {
	ctx := context.Background()
	tag, err := s.pool.Exec(ctx, `DELETE FROM memories WHERE id = $1::uuid AND owner_id = $2::uuid`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory not found or not owned by user")
	}
	return nil
}

func (s *PostgresStore) BatchDeleteMemories(ids []string, ownerID string) ([]string, error) {
	ctx := context.Background()

	// Single atomic DELETE ... RETURNING — Postgres guarantees statement
	// atomicity. Chunks cascade via FK ON DELETE CASCADE. Attachment
	// objects cleaned up by caller (handler). RETURNING id::text lets
	// the caller build the skipped-not_found list by set difference
	// against the input slice — a row removed concurrently between
	// accessibility load and this delete surfaces as an id missing from
	// `deleted`, which the handler reports as BatchDeleteSkipNotFound.
	rows, err := s.pool.Query(ctx,
		`DELETE FROM memories WHERE id = ANY($1::uuid[]) AND owner_id = $2::uuid RETURNING id::text`,
		ids, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deleted := make([]string, 0, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		deleted = append(deleted, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deleted, nil
}

func (s *PostgresStore) DeleteHubMemory(id string, hubID string) error {
	ctx := context.Background()
	tag, err := s.pool.Exec(ctx, `DELETE FROM memories WHERE id = $1::uuid AND hub_id = $2::uuid`, id, hubID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory not found in hub")
	}
	return nil
}

func (s *PostgresStore) BatchDeleteHubMemories(ids []string, hubID string) ([]string, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`DELETE FROM memories WHERE id = ANY($1::uuid[]) AND hub_id = $2::uuid RETURNING id::text`,
		ids, hubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deleted := make([]string, 0, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		deleted = append(deleted, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deleted, nil
}

func (s *PostgresStore) BatchMoveToTopic(ids []string, topicID string, hubID string, confidence float64) (int, error) {
	ctx := context.Background()

	// Atomic transaction: delete old assignments, then insert new.
	// Enforces 1:1 relationship — each memory belongs to exactly one topic.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Remove existing topic assignments for these memories.
	_, err = tx.Exec(ctx,
		`DELETE FROM memory_topics WHERE memory_id = ANY($1::uuid[])`,
		ids)
	if err != nil {
		return 0, err
	}

	// Insert new assignments.
	tag, err := tx.Exec(ctx, `
		INSERT INTO memory_topics (memory_id, topic_id, confidence, created_at)
		SELECT unnest($1::uuid[]), $2::uuid, $3, now()`,
		ids, topicID, confidence)
	if err != nil {
		return 0, err
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *PostgresStore) BatchMoveMemories(ids []string, targetHubID string, targetTopicID string, ownerID string) (*model.BatchMoveResult, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Load every row the caller referenced so we can diff against the request
	// and report per-id skipped reasons. Joined against memory_topics so we
	// also know each memory's current topic assignment for already-at-target
	// detection.
	rows, err := tx.Query(ctx,
		`SELECT m.id::text, m.hub_id::text, m.owner_id::text,
		        COALESCE(mt.topic_id::text, '')
		 FROM memories m
		 LEFT JOIN memory_topics mt ON mt.memory_id = m.id
		 WHERE m.id = ANY($1::uuid[])`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	type rowState struct {
		hubID   string
		ownerID string
		topicID string
	}
	found := make(map[string]rowState, len(ids))
	for rows.Next() {
		var id, hubID, owner, topic string
		if err := rows.Scan(&id, &hubID, &owner, &topic); err != nil {
			rows.Close()
			return nil, err
		}
		found[id] = rowState{hubID: hubID, ownerID: owner, topicID: topic}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Partition the request: movable ids get UPDATEd, skipped ids get
	// reported with a reason.
	result := &model.BatchMoveResult{Skipped: []model.SkippedMemory{}}
	movable := make([]string, 0, len(ids))
	for _, id := range ids {
		state, ok := found[id]
		if !ok {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipNotFound,
			})
			continue
		}
		if state.ownerID != ownerID {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipNotOwned,
			})
			continue
		}
		if state.hubID == targetHubID && state.topicID == targetTopicID {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipAlreadyAtTarget,
			})
			continue
		}
		movable = append(movable, id)
	}

	if len(movable) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return result, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE memories
		 SET hub_id = $1::uuid, updated_at = now()
		 WHERE id = ANY($2::uuid[]) AND owner_id = $3::uuid`,
		targetHubID, movable, ownerID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM memory_topics WHERE memory_id = ANY($1::uuid[])`,
		movable,
	); err != nil {
		return nil, err
	}

	if targetTopicID != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO memory_topics (memory_id, topic_id, confidence, created_at)
			 SELECT id, $2::uuid, $3, now()
			 FROM memories
			 WHERE id = ANY($1::uuid[]) AND owner_id = $4::uuid`,
			movable, targetTopicID, model.ConfidenceUserMove, ownerID,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result.Moved = len(movable)
	return result, nil
}

func (s *PostgresStore) BatchMoveToHub(ids []string, targetHubID string, ownerID string) (int, error) {
	ctx := context.Background()

	// Single atomic UPDATE — only moves memories owned by this user.
	// Hub membership verified by the handler before calling this.
	tag, err := s.pool.Exec(ctx,
		`UPDATE memories SET hub_id = $1::uuid, updated_at = now()
		 WHERE id = ANY($2::uuid[]) AND owner_id = $3::uuid`,
		targetHubID, ids, ownerID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *PostgresStore) CreateMemoryAttachment(attachment *model.MemoryAttachment) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO memory_attachments (
			id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (memory_id, sha256)
		WHERE sha256 <> ''
		DO NOTHING`,
		attachment.ID, attachment.MemoryID, attachment.OwnerID, attachment.Kind, attachment.Filename,
		attachment.ContentType, attachment.SizeBytes, attachment.SHA256, attachment.StorageKey,
		attachment.Width, attachment.Height, attachment.InlineEligible, attachment.CreatedAt,
	)
	return err
}

func (s *PostgresStore) ListMemoryAttachments(memoryID string, ownerID string) ([]model.MemoryAttachment, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		FROM memory_attachments
		WHERE memory_id = $1::uuid AND owner_id = $2::uuid
		ORDER BY created_at ASC`,
		memoryID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []model.MemoryAttachment
	for rows.Next() {
		var attachment model.MemoryAttachment
		if err := rows.Scan(
			&attachment.ID, &attachment.MemoryID, &attachment.OwnerID, &attachment.Kind, &attachment.Filename,
			&attachment.ContentType, &attachment.SizeBytes, &attachment.SHA256, &attachment.StorageKey,
			&attachment.Width, &attachment.Height, &attachment.InlineEligible, &attachment.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	if attachments == nil {
		attachments = []model.MemoryAttachment{}
	}
	return attachments, rows.Err()
}

func (s *PostgresStore) ListMemoryAttachmentsByIDs(memoryIDs []string, ownerID string) ([]model.MemoryAttachment, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		FROM memory_attachments
		WHERE memory_id = ANY($1::uuid[]) AND owner_id = $2::uuid`,
		memoryIDs, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []model.MemoryAttachment
	for rows.Next() {
		var a model.MemoryAttachment
		if err := rows.Scan(
			&a.ID, &a.MemoryID, &a.OwnerID, &a.Kind, &a.Filename,
			&a.ContentType, &a.SizeBytes, &a.SHA256, &a.StorageKey,
			&a.Width, &a.Height, &a.InlineEligible, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, rows.Err()
}

func (s *PostgresStore) GetMemoryAttachment(id string, memoryID string, ownerID string) (*model.MemoryAttachment, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		FROM memory_attachments
		WHERE id = $1::uuid AND memory_id = $2::uuid AND owner_id = $3::uuid`,
		id, memoryID, ownerID)
	return scanMemoryAttachment(row)
}

// GetAttachmentByID is the signature-gated lookup used by the view
// endpoint. No owner/memory filter — the HMAC on the URL is what
// authorizes access. Every other call site MUST use
// GetMemoryAttachment.
func (s *PostgresStore) GetAttachmentByID(id string) (*model.MemoryAttachment, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		FROM memory_attachments
		WHERE id = $1::uuid`,
		id)
	return scanMemoryAttachment(row)
}

func (s *PostgresStore) ListOwnerMemoryAttachments(ownerID string) ([]model.MemoryAttachment, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, memory_id, owner_id, kind, filename, content_type, size_bytes, sha256, storage_key, width, height, inline_eligible, created_at
		FROM memory_attachments
		WHERE owner_id = $1::uuid
		ORDER BY created_at ASC`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []model.MemoryAttachment
	for rows.Next() {
		var attachment model.MemoryAttachment
		if err := rows.Scan(
			&attachment.ID, &attachment.MemoryID, &attachment.OwnerID, &attachment.Kind, &attachment.Filename,
			&attachment.ContentType, &attachment.SizeBytes, &attachment.SHA256, &attachment.StorageKey,
			&attachment.Width, &attachment.Height, &attachment.InlineEligible, &attachment.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	if attachments == nil {
		attachments = []model.MemoryAttachment{}
	}
	return attachments, rows.Err()
}

// DeleteAllUserData purges all user data in a single transaction: memories (cascades to
// chunks + memory_topics), topics, agent configs, dream runs + actions, and reviews.

func (s *PostgresStore) DeleteAllUserData(ownerID string) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Order matters: delete children before parents to respect FK constraints.
	// chunks + memory_topics cascade from memories, dream_actions cascade from dream_runs.
	queries := []string{
		`DELETE FROM reviews WHERE hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal')`,
		`DELETE FROM dream_actions WHERE run_id IN (SELECT id FROM dream_runs WHERE hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal'))`,
		`DELETE FROM dream_runs WHERE hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal')`,
		`DELETE FROM agent_config_tombstones WHERE owner_id = $1::uuid`,
		`DELETE FROM agent_config_sync_states WHERE owner_id = $1::uuid`,
		`DELETE FROM agent_configs WHERE owner_id = $1::uuid`,
		`DELETE FROM memory_topics WHERE topic_id IN (
			SELECT id FROM topics WHERE hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal')
		)`,
		`DELETE FROM topics WHERE hub_id IN (SELECT id FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal')`,
		`DELETE FROM memories WHERE owner_id = $1::uuid`, // cascades to chunks
	}
	for _, q := range queries {
		if _, err := tx.Exec(ctx, q, ownerID); err != nil {
			return fmt.Errorf("delete user data: %w", err)
		}
	}

	return tx.Commit(ctx)
}
