package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (s *PostgresStore) CreateTopic(topic *model.Topic) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO topics (id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at, archived_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		topic.ID, topic.OwnerID, topic.HubID, topic.ParentID, topic.Name,
		topic.Description, topic.Icon, topic.Position, topic.Pinned, topic.UserModified,
		topic.CreatedAt, topic.UpdatedAt, topic.ArchivedAt)
	return err
}

func (s *PostgresStore) GetTopic(id string, hubID string) (*model.Topic, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at, archived_at
		FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid`, id, hubID)
	return scanTopic(row)
}

// GetTopicAccessible resolves a topic by id scoped to the viewer's
// VisibilityScope — used by endpoints that take a bare topic id
// (e.g. RecordVisit) and need to derive the hub without trusting the
// caller to pre-resolve it. Mirrors GetAccessibleMemory.
func (s *PostgresStore) GetTopicAccessible(id string, scope VisibilityScope) (*model.Topic, error) {
	ctx := context.Background()
	scopeFilter, scopeArgs := scope.SQLFilter("t", 2)
	args := []any{id}
	args = append(args, scopeArgs...)
	query := `SELECT t.id, t.owner_id, t.hub_id, t.parent_id, t.name, t.description, t.icon, t.position, t.pinned, t.user_modified, t.created_at, t.updated_at, t.archived_at
		FROM topics t WHERE t.id = $1::uuid AND ` + scopeFilter
	row := s.pool.QueryRow(ctx, query, args...)
	return scanTopic(row)
}

func scanTopic(row pgx.Row) (*model.Topic, error) {
	var t model.Topic
	err := row.Scan(&t.ID, &t.OwnerID, &t.HubID, &t.ParentID, &t.Name,
		&t.Description, &t.Icon, &t.Position, &t.Pinned, &t.UserModified, &t.CreatedAt, &t.UpdatedAt, &t.ArchivedAt)
	if err != nil {
		return nil, fmt.Errorf("topic not found: %w", err)
	}
	return &t, nil
}

// ListTopics returns the hub's ACTIVE topics only. Archived topics are
// invisible to every tree consumer (handlers, dreams, inline classification,
// agent tools, MCP) by construction — use ListArchivedTopics for the
// explicit archived browse surface.
func (s *PostgresStore) ListTopics(hubID string) ([]model.Topic, error) {
	ctx := context.Background()
	query := `SELECT id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at, archived_at
		FROM topics WHERE hub_id = $1::uuid AND archived_at IS NULL`
	args := []any{hubID}
	query += " ORDER BY position ASC, created_at ASC"

	return s.queryTopics(ctx, query, args...)
}

// ListArchivedTopics returns the hub's archived topics, most recently
// archived first. Flat list — the web archived section renders rows, not a
// tree (subtrees are archived atomically so hierarchy adds no information).
func (s *PostgresStore) ListArchivedTopics(hubID string) ([]model.Topic, error) {
	ctx := context.Background()
	query := `SELECT id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at, archived_at
		FROM topics WHERE hub_id = $1::uuid AND archived_at IS NOT NULL
		ORDER BY archived_at DESC, created_at ASC`
	return s.queryTopics(ctx, query, hubID)
}

func (s *PostgresStore) queryTopics(ctx context.Context, query string, args ...any) ([]model.Topic, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	topics := make([]model.Topic, 0)
	for rows.Next() {
		var t model.Topic
		if err := rows.Scan(&t.ID, &t.OwnerID, &t.HubID, &t.ParentID, &t.Name,
			&t.Description, &t.Icon, &t.Position, &t.Pinned, &t.UserModified, &t.CreatedAt, &t.UpdatedAt, &t.ArchivedAt); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// ArchiveTopicSubtree archives a topic and all of its descendants in one
// statement. Already-archived rows keep their original archived_at so
// restore ordering stays truthful. Returns the number of rows newly archived.
//
// Memory assignments are deliberately untouched: memories whose only topic
// is archived are PARKED with it — they do not resurface as unassigned
// (inbox) and dreams will not reorganize them, because either would make
// restore lossy. Archive means "out of the way, intact", not "dissolve".
func (s *PostgresStore) ArchiveTopicSubtree(id, hubID string, archivedAt time.Time) (int, error) {
	ctx := context.Background()
	tag, err := s.pool.Exec(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid
			UNION ALL
			SELECT t.id FROM topics t
			JOIN subtree st ON t.parent_id = st.id
			WHERE t.hub_id = $2::uuid
		)
		UPDATE topics SET archived_at = $3, updated_at = $3
		WHERE id IN (SELECT id FROM subtree) AND archived_at IS NULL`,
		id, hubID, archivedAt)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RestoreTopicSubtree clears archived_at on a topic and all of its
// descendants. If the restored topic's parent is still archived (it was
// archived as part of a larger subtree and only this branch is being
// restored), the topic is re-planted at the root with the next free
// position so it never dangles under an invisible parent.
// Returns the number of rows restored.
func (s *PostgresStore) RestoreTopicSubtree(id, hubID string, restoredAt time.Time) (int, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid
			UNION ALL
			SELECT t.id FROM topics t
			JOIN subtree st ON t.parent_id = st.id
			WHERE t.hub_id = $2::uuid
		)
		UPDATE topics SET archived_at = NULL, updated_at = $3
		WHERE id IN (SELECT id FROM subtree) AND archived_at IS NOT NULL`,
		id, hubID, restoredAt)
	if err != nil {
		return 0, err
	}

	// Re-plant at root if the parent is still archived: the restored branch
	// must not hang under a hidden node.
	_, err = tx.Exec(ctx, `
		UPDATE topics SET
			parent_id = NULL,
			position = COALESCE((SELECT MAX(position) + 1 FROM topics
				WHERE hub_id = $2::uuid AND parent_id IS NULL AND archived_at IS NULL AND id != $1::uuid), 0),
			updated_at = $3
		WHERE id = $1::uuid AND hub_id = $2::uuid
		AND parent_id IS NOT NULL
		AND EXISTS (SELECT 1 FROM topics p WHERE p.id = topics.parent_id AND p.archived_at IS NOT NULL)`,
		id, hubID, restoredAt)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// SearchAccessibleTopics returns up to `limit` topics whose hub is in
// `hubIDs` and whose lowercase name or description contains every
// whitespace-separated token in `query` (case-insensitive AND).
// Used by the bar quick-match endpoint to surface jump-to-topic
// rows next to memory matches.
func (s *PostgresStore) SearchAccessibleTopics(ctx context.Context, query string, hubIDs []string, limit int) ([]model.Topic, error) {
	if len(hubIDs) == 0 || strings.TrimSpace(query) == "" || limit <= 0 {
		return []model.Topic{}, nil
	}
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return []model.Topic{}, nil
	}

	// $1 is the hub-id array. Each token adds one $N placeholder
	// re-used twice (once for name, once for description).
	args := []any{hubIDs}
	clauses := make([]string, 0, len(tokens))
	for _, token := range tokens {
		args = append(args, "%"+token+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(lower(name) LIKE $%d OR lower(COALESCE(description, '')) LIKE $%d)", n, n))
	}
	args = append(args, limit)

	sqlQuery := fmt.Sprintf(`SELECT id, owner_id, hub_id, parent_id, name, description, icon, position, pinned, user_modified, created_at, updated_at, archived_at
		FROM topics
		WHERE hub_id = ANY($1::uuid[]) AND archived_at IS NULL AND %s
		ORDER BY position ASC, created_at DESC
		LIMIT $%d`, strings.Join(clauses, " AND "), len(args))

	rows, err := s.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search accessible topics: %w", err)
	}
	defer rows.Close()

	topics := make([]model.Topic, 0)
	for rows.Next() {
		var t model.Topic
		if err := rows.Scan(&t.ID, &t.OwnerID, &t.HubID, &t.ParentID, &t.Name,
			&t.Description, &t.Icon, &t.Position, &t.Pinned, &t.UserModified, &t.CreatedAt, &t.UpdatedAt, &t.ArchivedAt); err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func (s *PostgresStore) GetTopicActivitySummary(scope VisibilityScope, topicID, hubID string, createdAfter time.Time, previewLimit int) (*model.TopicActivitySummary, error) {
	ctx := context.Background()
	if previewLimit <= 0 {
		previewLimit = 3
	}

	visPred, visArgs := scope.SQLFilter("m", 3)
	summaryArgs := []any{topicID, hubID}
	summaryArgs = append(summaryArgs, visArgs...)
	summaryArgs = append(summaryArgs, createdAfter)
	summaryQuery := `WITH RECURSIVE subtree AS (
		SELECT id
		FROM topics
		WHERE id = $1::uuid AND hub_id = $2::uuid
		UNION ALL
		SELECT t.id
		FROM topics t
		JOIN subtree s ON t.parent_id = s.id
		WHERE t.hub_id = $2::uuid
	)
	SELECT COUNT(DISTINCT m.id), COUNT(DISTINCT m.owner_id)
	FROM memories m
	JOIN memory_topics mt ON mt.memory_id = m.id
	JOIN subtree st ON st.id = mt.topic_id
	WHERE ` + visPred + fmt.Sprintf(` AND m.hub_id = $2::uuid AND m.state != 'archived' AND m.created_at >= $%d`, len(summaryArgs))

	var memoryCount int
	var contributorCount int
	if err := s.pool.QueryRow(ctx, summaryQuery, summaryArgs...).Scan(&memoryCount, &contributorCount); err != nil {
		return nil, err
	}
	if memoryCount == 0 {
		return nil, nil
	}

	previewArgs := []any{topicID, hubID}
	previewArgs = append(previewArgs, visArgs...)
	previewArgs = append(previewArgs, createdAfter, previewLimit)
	previewQuery := `WITH RECURSIVE subtree AS (
		SELECT id
		FROM topics
		WHERE id = $1::uuid AND hub_id = $2::uuid
		UNION ALL
		SELECT t.id
		FROM topics t
		JOIN subtree s ON t.parent_id = s.id
		WHERE t.hub_id = $2::uuid
	), contributor_activity AS (
		SELECT m.owner_id, COUNT(DISTINCT m.id) AS memory_count, MAX(m.created_at) AS last_created_at
		FROM memories m
		JOIN memory_topics mt ON mt.memory_id = m.id
		JOIN subtree st ON st.id = mt.topic_id
		WHERE ` + visPred + fmt.Sprintf(` AND m.hub_id = $2::uuid AND m.state != 'archived' AND m.created_at >= $%d
		GROUP BY m.owner_id
	)
	SELECT u.id::text,
		COALESCE(NULLIF(u.display_name, ''), NULLIF(u.name, ''), 'Someone') AS user_name,
		COALESCE(u.avatar_url, '') AS user_avatar_url
	FROM contributor_activity ca
	JOIN users u ON u.id = ca.owner_id
	ORDER BY ca.memory_count DESC, ca.last_created_at DESC
	LIMIT $%d`, len(previewArgs)-1, len(previewArgs))

	rows, err := s.pool.Query(ctx, previewQuery, previewArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	preview := make([]model.TopicActivityContributor, 0, previewLimit)
	for rows.Next() {
		var contributor model.TopicActivityContributor
		if err := rows.Scan(&contributor.UserID, &contributor.UserName, &contributor.UserAvatarURL); err != nil {
			return nil, err
		}
		preview = append(preview, contributor)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &model.TopicActivitySummary{
		WindowDays:          int(time.Since(createdAfter).Hours() / 24),
		MemoryCount:         memoryCount,
		ContributorCount:    contributorCount,
		ContributorsPreview: preview,
	}, nil
}

func (s *PostgresStore) UpdateTopic(topic *model.Topic) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE topics SET name=$1, description=$2, icon=$3, position=$4, pinned=$5,
			user_modified=$6, parent_id=$7, hub_id=$8::uuid, updated_at=$9
		WHERE id = $10::uuid AND hub_id = $11::uuid`,
		topic.Name, topic.Description, topic.Icon, topic.Position, topic.Pinned,
		topic.UserModified, topic.ParentID, topic.HubID, topic.UpdatedAt,
		topic.ID, topic.HubID)
	return err
}

func (s *PostgresStore) DeleteTopic(id string, hubID string) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Get the topic's parent so we can re-parent children
	var parentID *string
	err = tx.QueryRow(ctx,
		`SELECT parent_id FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid`, id, hubID).Scan(&parentID)
	if err != nil {
		return fmt.Errorf("topic not found: %w", err)
	}

	// Re-parent children to the deleted topic's parent
	_, err = tx.Exec(ctx,
		`UPDATE topics SET parent_id = $1, updated_at = now() WHERE parent_id = $2::uuid AND hub_id = $3::uuid`,
		parentID, id, hubID)
	if err != nil {
		return err
	}

	// Delete the topic (memory_topics cascade automatically)
	_, err = tx.Exec(ctx,
		`DELETE FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid`, id, hubID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) AssignMemoryToTopic(memoryID, topicID, hubID string, confidence float64) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var memHub string
	err = tx.QueryRow(ctx, `SELECT hub_id::text FROM memories WHERE id = $1::uuid`, memoryID).Scan(&memHub)
	if err != nil {
		return fmt.Errorf("memory not found: %w", err)
	}
	if memHub != hubID {
		return fmt.Errorf("memory does not belong to this hub")
	}

	var topicHub string
	err = tx.QueryRow(ctx, `SELECT hub_id::text FROM topics WHERE id = $1::uuid`, topicID).Scan(&topicHub)
	if err != nil {
		return fmt.Errorf("topic not found: %w", err)
	}
	if topicHub != hubID {
		return fmt.Errorf("topic does not belong to this hub")
	}

	var existingTopicID string
	var existingConfidence float64
	err = tx.QueryRow(ctx,
		`SELECT topic_id::text, confidence
		FROM memory_topics
		WHERE memory_id = $1::uuid
		FOR UPDATE`,
		memoryID,
	).Scan(&existingTopicID, &existingConfidence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err = tx.Exec(ctx,
			`INSERT INTO memory_topics (memory_id, topic_id, confidence, created_at)
			VALUES ($1::uuid, $2::uuid, $3, now())`,
			memoryID, topicID, confidence)
		if err != nil {
			return err
		}
	case existingTopicID == topicID:
		_, err = tx.Exec(ctx,
			`UPDATE memory_topics
			SET confidence = GREATEST(confidence, $3)
			WHERE memory_id = $1::uuid AND topic_id = $2::uuid`,
			memoryID, topicID, confidence)
		if err != nil {
			return err
		}
	case confidence > existingConfidence:
		_, err = tx.Exec(ctx,
			`UPDATE memory_topics
			SET topic_id = $2::uuid, confidence = $3, created_at = now()
			WHERE memory_id = $1::uuid`,
			memoryID, topicID, confidence)
		if err != nil {
			return err
		}
	default:
		return tx.Commit(ctx)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) UnassignMemoryFromTopic(memoryID, topicID, hubID string) error {
	ctx := context.Background()
	var topicHub string
	err := s.pool.QueryRow(ctx, `SELECT hub_id::text FROM topics WHERE id = $1::uuid`, topicID).Scan(&topicHub)
	if err != nil {
		return fmt.Errorf("topic not found: %w", err)
	}
	if topicHub != hubID {
		return fmt.Errorf("topic does not belong to this hub")
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM memory_topics WHERE memory_id = $1::uuid AND topic_id = $2::uuid`, memoryID, topicID)
	return err
}

func (s *PostgresStore) CountMemoriesByTopic(scope VisibilityScope, hubID string) (map[string]int, error) {
	ctx := context.Background()
	// Count memories per topic, using visibility scope on the memories table
	// so team hub members see each other's memory counts.
	visPred, visArgs := scope.SQLFilter("m", 1)
	query := `SELECT mt.topic_id, COUNT(*) FROM memory_topics mt
		JOIN memories m ON mt.memory_id = m.id
		WHERE ` + visPred + ` AND m.state != 'archived'`
	args := visArgs
	if hubID != "" {
		args = append(args, hubID)
		query += fmt.Sprintf(" AND m.hub_id = $%d::uuid", len(args))
	}
	query += " GROUP BY mt.topic_id"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var topicID string
		var count int
		if err := rows.Scan(&topicID, &count); err != nil {
			return nil, err
		}
		counts[topicID] = count
	}
	return counts, rows.Err()
}

func (s *PostgresStore) CountTopicMemories(scope VisibilityScope, hubID string) (map[string]int, int, error) {
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("m", 1)
	query := `SELECT COALESCE(mt.topic_id::text, '') AS tid, COUNT(DISTINCT m.id)
		FROM memories m
		LEFT JOIN memory_topics mt ON mt.memory_id = m.id
		WHERE ` + visPred + ` AND m.state != 'archived'`
	args := visArgs
	if hubID != "" {
		args = append(args, hubID)
		query += fmt.Sprintf(" AND m.hub_id = $%d::uuid", len(args))
	}
	query += " GROUP BY COALESCE(mt.topic_id::text, '')"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	unassigned := 0
	for rows.Next() {
		var tid string
		var count int
		if err := rows.Scan(&tid, &count); err != nil {
			return nil, 0, fmt.Errorf("scan topic count: %w", err)
		}
		if tid == "" {
			unassigned = count
		} else {
			counts[tid] = count
		}
	}
	return counts, unassigned, rows.Err()
}

func (s *PostgresStore) CountUnassignedMemories(scope VisibilityScope, hubID string) (int, error) {
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("memories", 1)
	query := `SELECT COUNT(*) FROM memories
		WHERE ` + visPred + ` AND state != 'archived'
		AND id NOT IN (SELECT memory_id FROM memory_topics)`
	args := visArgs
	if hubID != "" {
		args = append(args, hubID)
		query += fmt.Sprintf(" AND hub_id = $%d::uuid", len(args))
	}

	var count int
	err := s.pool.QueryRow(ctx, query, args...).Scan(&count)
	return count, err
}

func (s *PostgresStore) ReorderTopics(hubID string, ops []model.ReorderOperation) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, op := range ops {
		_, err := tx.Exec(ctx,
			`UPDATE topics SET position = $1, parent_id = $2, updated_at = now()
			WHERE id = $3::uuid AND hub_id = $4::uuid`,
			op.Position, op.ParentID, op.TopicID, hubID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) GetTopicDepth(topicID, hubID string) (int, error) {
	ctx := context.Background()
	depth := 0
	currentID := topicID
	for depth < 5 { // safety bound
		var parentID *string
		// hub_id filter clamps the walk. A row with a cross-hub
		// parent_id (schema-level invariant violated; possible via
		// bad admin input since topics.parent_id has no same-hub FK
		// constraint) returns ErrNoRows and we stop — safer than
		// happily traversing into another hub's tree.
		err := s.pool.QueryRow(ctx,
			`SELECT parent_id FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid`,
			currentID, hubID).Scan(&parentID)
		if err != nil {
			break
		}
		if parentID == nil {
			break
		}
		currentID = *parentID
		depth++
	}
	return depth, nil
}

// IsTopicDescendant walks the subtree rooted at ancestorID and returns true
// if candidateID is the ancestor itself or any descendant. Hub-scoped so a
// leaked cross-hub parent_id cannot masquerade as an ancestor.
func (s *PostgresStore) IsTopicDescendant(hubID, ancestorID, candidateID string) (bool, error) {
	if ancestorID == candidateID {
		return true, nil
	}
	ctx := context.Background()
	var found bool
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE subtree AS (
			SELECT id FROM topics WHERE id = $1::uuid AND hub_id = $3::uuid
			UNION ALL
			SELECT t.id FROM topics t
			JOIN subtree s ON t.parent_id = s.id
			WHERE t.hub_id = $3::uuid
		)
		SELECT EXISTS(SELECT 1 FROM subtree WHERE id = $2::uuid)`,
		ancestorID, candidateID, hubID).Scan(&found)
	if err != nil {
		return false, err
	}
	return found, nil
}

// GetSubtreeMaxDepth returns the depth of the deepest descendant relative to
// topicID (0 means topicID has no children). Capped at 5 as a safety bound
// since the tree is architecturally limited to 5 total levels.
func (s *PostgresStore) GetSubtreeMaxDepth(hubID, topicID string) (int, error) {
	ctx := context.Background()
	var maxDepth int
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE subtree(id, depth) AS (
			SELECT id, 0 FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid
			UNION ALL
			SELECT t.id, s.depth + 1
			FROM topics t
			JOIN subtree s ON t.parent_id = s.id
			WHERE t.hub_id = $2::uuid AND s.depth < 5
		)
		SELECT COALESCE(MAX(depth), 0) FROM subtree`,
		topicID, hubID).Scan(&maxDepth)
	if err != nil {
		return 0, err
	}
	return maxDepth, nil
}

func (s *PostgresStore) ListUnassignedMemories(scope VisibilityScope, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 100
	}
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("m", 1)
	limitParam := fmt.Sprintf("$%d", len(visArgs)+1)
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE %s AND m.state != 'archived'
		AND m.id NOT IN (SELECT memory_id FROM memory_topics)
		ORDER BY m.created_at DESC LIMIT %s`, memoryCols, memoryFrom, visPred, limitParam),
		append(visArgs, limit)...)
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

func (s *PostgresStore) ListUnassignedMemoriesByHub(hubID string, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 100
	}
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE m.hub_id = $1::uuid AND m.state != 'archived'
		AND m.id NOT IN (SELECT memory_id FROM memory_topics)
		ORDER BY m.created_at DESC LIMIT $2`, memoryCols, memoryFrom),
		hubID, limit)
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

func (s *PostgresStore) GetMemoryTopicNameMap(scope VisibilityScope) (map[string]string, error) {
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("t", 1)
	rows, err := s.pool.Query(ctx,
		`SELECT mt.memory_id, t.name FROM memory_topics mt
		JOIN topics t ON mt.topic_id = t.id
		WHERE t.archived_at IS NULL AND `+visPred, visArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var memID, topicName string
		if err := rows.Scan(&memID, &topicName); err != nil {
			continue
		}
		result[memID] = topicName
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMemoryTopicNameMapForMemories(scope VisibilityScope, memoryIDs []string) (map[string]string, error) {
	if len(memoryIDs) == 0 {
		return map[string]string{}, nil
	}
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("t", 1)
	memoryParam := scope.ParamCount() + 1
	args := append(visArgs, memoryIDs)
	rows, err := s.pool.Query(ctx,
		`SELECT mt.memory_id, t.name FROM memory_topics mt
		JOIN topics t ON mt.topic_id = t.id
		WHERE t.archived_at IS NULL AND `+visPred+` AND mt.memory_id = ANY($`+fmt.Sprintf("%d", memoryParam)+`::uuid[])`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var memID, topicName string
		if err := rows.Scan(&memID, &topicName); err != nil {
			continue
		}
		result[memID] = topicName
	}
	return result, rows.Err()
}

// GetTopicKinds returns the top memory kinds per topic for lightweight visual summaries.
func (s *PostgresStore) GetTopicKinds(hubID string) (map[string][]string, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT mt.topic_id, m.kind, COUNT(*) as cnt
		FROM memory_topics mt
		JOIN memories m ON mt.memory_id = m.id
		JOIN topics t ON mt.topic_id = t.id
		WHERE t.hub_id = $1::uuid AND m.state != 'archived'
		GROUP BY mt.topic_id, m.kind
		ORDER BY mt.topic_id, cnt DESC`, hubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var topicID, kind string
		var cnt int
		if err := rows.Scan(&topicID, &kind, &cnt); err != nil {
			continue
		}
		if kinds := result[topicID]; len(kinds) < 3 {
			result[topicID] = append(kinds, kind)
		}
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMemoryTopicIDMap(scope VisibilityScope) (map[string]string, error) {
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("t", 1)
	rows, err := s.pool.Query(ctx,
		`SELECT mt.memory_id, mt.topic_id FROM memory_topics mt
		JOIN topics t ON mt.topic_id = t.id
		WHERE `+visPred, visArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var memID, topicID string
		if err := rows.Scan(&memID, &topicID); err != nil {
			continue
		}
		result[memID] = topicID
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMemoryTopicIDMapForMemories(scope VisibilityScope, memoryIDs []string) (map[string]string, error) {
	if len(memoryIDs) == 0 {
		return map[string]string{}, nil
	}
	ctx := context.Background()
	visPred, visArgs := scope.SQLFilter("t", 1)
	memoryParam := scope.ParamCount() + 1
	args := append(visArgs, memoryIDs)
	rows, err := s.pool.Query(ctx,
		`SELECT mt.memory_id, mt.topic_id FROM memory_topics mt
		JOIN topics t ON mt.topic_id = t.id
		WHERE `+visPred+` AND mt.memory_id = ANY($`+fmt.Sprintf("%d", memoryParam)+`::uuid[])`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var memID, topicID string
		if err := rows.Scan(&memID, &topicID); err != nil {
			continue
		}
		result[memID] = topicID
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListMemoriesByTopic(scope VisibilityScope, topicID string, limit int, cursor string) ([]model.Memory, string, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx := context.Background()

	// Include memories from this topic AND all descendant topics
	// Uses recursive CTE to find all child topic IDs
	topicFilter := `mt.topic_id IN (
		WITH RECURSIVE descendants AS (
			SELECT id FROM topics WHERE id = $1
			UNION ALL
			SELECT t.id FROM topics t JOIN descendants d ON t.parent_id = d.id
		) SELECT id FROM descendants
	)`

	// Visibility scope on memories — team members see each other's memories in shared topics
	visPred, visArgs := scope.SQLFilter("m", 2)
	n := 2 + scope.ParamCount()

	var query string
	var args []any
	if cursor != "" {
		cursorParam := fmt.Sprintf("$%d", n+1)
		limitParam := fmt.Sprintf("$%d", n)
		query = `SELECT ` + memoryCols + ` FROM ` + memoryFrom + `
				JOIN memory_topics mt ON m.id = mt.memory_id
				WHERE ` + topicFilter + ` AND ` + visPred + ` AND m.state != 'archived'
				AND m.created_at < ` + cursorParam + `::timestamptz
				ORDER BY m.created_at DESC LIMIT ` + limitParam
		args = append([]any{topicID}, visArgs...)
		args = append(args, limit+1, cursor)
	} else {
		limitParam := fmt.Sprintf("$%d", n)
		query = `SELECT ` + memoryCols + ` FROM ` + memoryFrom + `
				JOIN memory_topics mt ON m.id = mt.memory_id
				WHERE ` + topicFilter + ` AND ` + visPred + ` AND m.state != 'archived'
				ORDER BY m.created_at DESC LIMIT ` + limitParam
		args = append([]any{topicID}, visArgs...)
		args = append(args, limit+1)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var memories []model.Memory
	for rows.Next() {
		m, err := scanMemoryFromRows(rows)
		if err != nil {
			return nil, "", err
		}
		memories = append(memories, *m)
	}

	nextCursor := ""
	if len(memories) > limit {
		memories = memories[:limit]
		nextCursor = memories[len(memories)-1].CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	if memories == nil {
		memories = make([]model.Memory, 0)
	}
	return memories, nextCursor, rows.Err()
}
