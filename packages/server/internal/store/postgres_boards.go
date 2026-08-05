package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

var (
	ErrBoardSlotNotFound        = errors.New("board slot not found")
	ErrBoardSlotAlreadyResolved = errors.New("board slot already resolved")
)

const boardColumns = `id, hub_id, created_by, kind, title, instruction, status, created_at, updated_at`

func scanBoard(row pgx.Row) (*model.Board, error) {
	var b model.Board
	if err := row.Scan(&b.ID, &b.HubID, &b.CreatedBy, &b.Kind, &b.Title, &b.Instruction,
		&b.Status, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// GetOrCreateSystemBoard returns the hub's system board, creating it on
// first access. Reads take the SELECT fast path — the board GET is a
// hot read and must not write a row version per view. Only a miss
// falls through to the insert, where the no-op DO UPDATE keeps the
// create race-safe against the partial unique index while still
// RETURNING the surviving row.
func (s *PostgresStore) GetOrCreateSystemBoard(hubID, createdBy string) (*model.Board, error) {
	ctx := context.Background()
	board, err := scanBoard(s.pool.QueryRow(ctx,
		`SELECT `+boardColumns+` FROM boards WHERE hub_id = $1::uuid AND kind = 'system'`, hubID))
	if err == nil {
		return board, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO boards (hub_id, created_by, kind, status)
		VALUES ($1::uuid, $2::uuid, 'system', 'active')
		ON CONFLICT (hub_id) WHERE kind = 'system'
		DO UPDATE SET hub_id = EXCLUDED.hub_id
		RETURNING `+boardColumns,
		hubID, createdBy)
	return scanBoard(row)
}

const boardSlotColumns = `id, board_id, slot_key, kind, title, payload, cite_memory_ids, state, resolution, dream_run_id, created_at, updated_at`

func scanBoardSlot(row pgx.Row) (*model.BoardSlot, error) {
	var slot model.BoardSlot
	var resolution []byte
	var dreamRunID *string
	if err := row.Scan(&slot.ID, &slot.BoardID, &slot.SlotKey, &slot.Kind, &slot.Title,
		&slot.Payload, &slot.CiteMemoryIDs, &slot.State, &resolution, &dreamRunID,
		&slot.CreatedAt, &slot.UpdatedAt); err != nil {
		return nil, err
	}
	if len(resolution) > 0 {
		var r model.BoardSlotResolution
		if err := json.Unmarshal(resolution, &r); err != nil {
			return nil, err
		}
		slot.Resolution = &r
	}
	if dreamRunID != nil {
		slot.DreamRunID = *dreamRunID
	}
	return &slot, nil
}

// GetBoardSlot returns one slot or ErrBoardSlotNotFound. Used by the
// handler's idempotent resolve path to report the current state of a
// slot that is already terminal.
func (s *PostgresStore) GetBoardSlot(boardID, slotKey string) (*model.BoardSlot, error) {
	ctx := context.Background()
	slot, err := scanBoardSlot(s.pool.QueryRow(ctx,
		`SELECT `+boardSlotColumns+` FROM board_slots
		WHERE board_id = $1::uuid AND slot_key = $2`, boardID, slotKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBoardSlotNotFound
	}
	return slot, err
}

// ListBoardSlots returns every occupied slot ordered by slot_key so the
// client renders a deterministic layout regardless of write order.
func (s *PostgresStore) ListBoardSlots(boardID string) ([]model.BoardSlot, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT `+boardSlotColumns+` FROM board_slots
		WHERE board_id = $1::uuid ORDER BY slot_key ASC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []model.BoardSlot
	for rows.Next() {
		slot, err := scanBoardSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, *slot)
	}
	return slots, rows.Err()
}

// UpsertBoardSlot is the producer write path: replace semantics on
// (board_id, slot_key). Replacing a slot resets it to fresh and clears
// any prior resolution — the old card is gone, feedback rows are the
// only surviving trace of it.
func (s *PostgresStore) UpsertBoardSlot(slot *model.BoardSlot) error {
	if err := model.ValidateBoardSlot(slot); err != nil {
		return err
	}
	ctx := context.Background()
	payload := slot.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	cites := slot.CiteMemoryIDs
	if cites == nil {
		cites = []string{}
	}
	var dreamRunID *string
	if slot.DreamRunID != "" {
		dreamRunID = &slot.DreamRunID
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO board_slots (board_id, slot_key, kind, title, payload, cite_memory_ids, state, dream_run_id)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6::uuid[], 'fresh', $7)
		ON CONFLICT (board_id, slot_key)
		DO UPDATE SET kind = $3, title = $4, payload = $5::jsonb, cite_memory_ids = $6::uuid[],
			state = 'fresh', resolution = NULL, dream_run_id = $7, updated_at = now()
		RETURNING id, state, created_at, updated_at`,
		slot.BoardID, slot.SlotKey, slot.Kind, slot.Title, payload, cites, dreamRunID)
	return row.Scan(&slot.ID, &slot.State, &slot.CreatedAt, &slot.UpdatedAt)
}

// ResolveBoardSlot transitions a slot out of fresh/seen. Terminal slots
// are not re-resolvable — the WHERE clause enforces the transition at
// the data layer; a zero-row result is disambiguated with a follow-up
// existence probe.
func (s *PostgresStore) ResolveBoardSlot(boardID, slotKey, newState string, resolution model.BoardSlotResolution) (*model.BoardSlot, error) {
	ctx := context.Background()
	resolutionJSON, err := json.Marshal(resolution)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE board_slots
		SET state = $3, resolution = $4::jsonb, updated_at = now()
		WHERE board_id = $1::uuid AND slot_key = $2 AND state IN ('fresh', 'seen')
		RETURNING `+boardSlotColumns,
		boardID, slotKey, newState, resolutionJSON)
	slot, err := scanBoardSlot(row)
	if err == nil {
		return slot, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var exists bool
	if probeErr := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM board_slots WHERE board_id = $1::uuid AND slot_key = $2)`,
		boardID, slotKey).Scan(&exists); probeErr != nil {
		return nil, probeErr
	}
	if exists {
		return nil, ErrBoardSlotAlreadyResolved
	}
	return nil, ErrBoardSlotNotFound
}

// CreateBoardFeedback records a member's 准/不准 verdict. One row per
// member per slot — a repeat verdict (retry, changed mind, or the slot
// now holding a new card) replaces the previous one, so every hub
// member can weigh in on a shared card and retries after a failed
// resolve+feedback sequence stay safe.
func (s *PostgresStore) CreateBoardFeedback(f *model.BoardFeedback) error {
	ctx := context.Background()
	cites := f.CiteMemoryIDs
	if cites == nil {
		cites = []string{}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO board_feedback (board_id, slot_key, card_kind, card_title, verdict, user_id, cite_memory_ids)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7::uuid[])
		ON CONFLICT (board_id, slot_key, user_id)
		DO UPDATE SET card_kind = $3, card_title = $4, verdict = $5, cite_memory_ids = $7::uuid[], created_at = now()
		RETURNING id, created_at`,
		f.BoardID, f.SlotKey, f.CardKind, f.CardTitle, f.Verdict, f.UserID, cites)
	return row.Scan(&f.ID, &f.CreatedAt)
}

func (s *PostgresStore) DeleteBoardSlot(boardID, slotKey string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`DELETE FROM board_slots WHERE board_id = $1::uuid AND slot_key = $2`, boardID, slotKey)
	return err
}

// boardMemoryFilter is the shared predicate for Lane A queries: hub
// content only, no archived rows, no onboarding seeds (counting seeded
// demo memories as "activity" would make every fresh board lie).
const boardMemoryFilter = `m.hub_id = $1::uuid AND m.state != 'archived'
	AND COALESCE(m.source_kind, '') != 'onboarding-seed'`

// ListRecentAgentActivityByHub groups the window's memories by
// effective agent slug (created_by_slug else source_agent), newest
// title per group, busiest first. Rows with no agent attribution
// (manual web captures) come back with slug "" — the producer decides
// how to label them.
func (s *PostgresStore) ListRecentAgentActivityByHub(hubID string, since time.Time) ([]model.BoardAgentActivity, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT COALESCE(`+memoryAgentSlugExpr+`, '') AS slug,
			COALESCE(MAX(ca.display_name), '') AS display_name,
			COALESCE(MAX(ca.icon), '') AS icon,
			COUNT(*) AS cnt,
			(array_agg(m.title ORDER BY m.created_at DESC))[1] AS latest_title,
			(array_agg(m.id::text ORDER BY m.created_at DESC))[1:3] AS item_ids,
			(array_agg(m.title ORDER BY m.created_at DESC))[1:3] AS item_titles
		FROM memories m
		LEFT JOIN connected_agents ca ON m.owner_id = ca.owner_id AND `+memoryAgentSlugExpr+` = ca.agent_name
		WHERE `+boardMemoryFilter+` AND m.created_at > $2
		GROUP BY 1
		ORDER BY cnt DESC, slug ASC`,
		hubID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activity []model.BoardAgentActivity
	for rows.Next() {
		var a model.BoardAgentActivity
		var itemIDs, itemTitles []string
		if err := rows.Scan(&a.Slug, &a.DisplayName, &a.Icon, &a.Count, &a.LatestTitle,
			&itemIDs, &itemTitles); err != nil {
			return nil, err
		}
		for i := range itemIDs {
			if i < len(itemTitles) && itemTitles[i] != "" {
				a.Items = append(a.Items, model.BoardAgentItem{MemoryID: itemIDs[i], Title: itemTitles[i]})
			}
		}
		activity = append(activity, a)
	}
	return activity, rows.Err()
}

func (s *PostgresStore) ListTopicActivityByHub(hubID string, since time.Time, limit int) ([]model.BoardTopicActivity, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.name, COALESCE(t.icon, ''),
			COUNT(*) AS recent, COUNT(DISTINCT m.owner_id) AS contributors
		FROM memory_topics mt
		JOIN memories m ON m.id = mt.memory_id
		JOIN topics t ON t.id = mt.topic_id
		WHERE `+boardMemoryFilter+` AND m.created_at > $2
			AND t.hub_id = $1::uuid AND t.archived_at IS NULL
		GROUP BY t.id, t.name, t.icon
		ORDER BY recent DESC, t.name ASC
		LIMIT $3`,
		hubID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []model.BoardTopicActivity
	for rows.Next() {
		var t model.BoardTopicActivity
		if err := rows.Scan(&t.TopicID, &t.Name, &t.Icon, &t.RecentCount, &t.Contributors); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func (s *PostgresStore) CountMemoriesInHubRange(hubID string, from, to time.Time) (int, error) {
	ctx := context.Background()
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM memories m
		WHERE `+boardMemoryFilter+` AND m.created_at > $2 AND m.created_at <= $3`,
		hubID, from, to).Scan(&count)
	return count, err
}

func (s *PostgresStore) GetMemoryNear(hubID string, target time.Time, tolerance time.Duration) (*model.Memory, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT m.id, m.title, COALESCE(m.summary, ''), m.created_at
		FROM memories m
		WHERE `+boardMemoryFilter+` AND m.created_at BETWEEN $2 AND $3
			AND (m.title != '' OR COALESCE(m.summary, '') != '')
		ORDER BY ABS(EXTRACT(EPOCH FROM (m.created_at - $4::timestamptz))) ASC
		LIMIT 1`,
		hubID, target.Add(-tolerance), target.Add(tolerance), target)
	var m model.Memory
	if err := row.Scan(&m.ID, &m.Title, &m.Summary, &m.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	m.HubID = hubID
	return &m, nil
}
