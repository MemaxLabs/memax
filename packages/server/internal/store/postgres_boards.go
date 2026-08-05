package store

import (
	"context"
	"encoding/json"
	"errors"

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
// first access. The no-op DO UPDATE makes the insert race-safe against
// the partial unique index while still RETURNING the surviving row.
func (s *PostgresStore) GetOrCreateSystemBoard(hubID, createdBy string) (*model.Board, error) {
	ctx := context.Background()
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

func (s *PostgresStore) CreateBoardFeedback(f *model.BoardFeedback) error {
	ctx := context.Background()
	cites := f.CiteMemoryIDs
	if cites == nil {
		cites = []string{}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO board_feedback (board_id, slot_key, card_kind, card_title, verdict, user_id, cite_memory_ids)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7::uuid[])
		RETURNING id, created_at`,
		f.BoardID, f.SlotKey, f.CardKind, f.CardTitle, f.Verdict, f.UserID, cites)
	return row.Scan(&f.ID, &f.CreatedAt)
}
