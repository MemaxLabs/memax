package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// CreateCampaign inserts a new draft campaign. Callers are expected to
// have validated content against kind and to have materialized
// audience_snapshot at write time — the store does not re-validate.
func (s *PostgresStore) CreateCampaign(ctx context.Context, c *model.Campaign) error {
	snapshotJSON, err := marshalAudienceSnapshot(c.AudienceSnapshot)
	if err != nil {
		return fmt.Errorf("marshal audience snapshot: %w", err)
	}
	contentBytes := []byte(c.Content)
	if len(contentBytes) == 0 {
		contentBytes = []byte(`{}`)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO campaigns (
			id, name, channel, kind, status,
			audience_id, audience_snapshot, content,
			scheduled_at, sent_count, failed_count,
			created_by, created_at, updated_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5,
			$6, $7::jsonb, $8::jsonb,
			$9, $10, $11,
			$12::uuid, now(), now()
		)
	`,
		c.ID, c.Name, c.Channel, c.Kind, c.Status,
		nilableUUID(c.AudienceID), snapshotJSON, contentBytes,
		c.ScheduledAt, c.SentCount, c.FailedCount,
		c.CreatedBy,
	)
	return err
}

// GetCampaign loads one campaign by id.
func (s *PostgresStore) GetCampaign(ctx context.Context, id string) (*model.Campaign, error) {
	row := s.pool.QueryRow(ctx, selectCampaignSQL+` WHERE id = $1::uuid`, id)
	c, err := scanCampaign(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCampaignNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListCampaigns returns campaigns ordered newest-first, with an optional
// status filter and cursor-based pagination on created_at.
func (s *PostgresStore) ListCampaigns(ctx context.Context, opts model.CampaignListOpts) ([]model.Campaign, string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{}
	where := ""
	if opts.Status != "" {
		args = append(args, opts.Status)
		where = fmt.Sprintf(" WHERE status = $%d", len(args))
	}
	if opts.Cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, opts.Cursor)
		if err == nil {
			args = append(args, t)
			if where == "" {
				where = fmt.Sprintf(" WHERE created_at < $%d", len(args))
			} else {
				where += fmt.Sprintf(" AND created_at < $%d", len(args))
			}
		}
	}
	args = append(args, limit+1)

	query := selectCampaignSQL + where +
		" ORDER BY created_at DESC" +
		fmt.Sprintf(" LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	result := make([]model.Campaign, 0, limit)
	for rows.Next() {
		c, scanErr := scanCampaign(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(result) > limit {
		nextCursor = result[limit-1].CreatedAt.UTC().Format(time.RFC3339Nano)
		result = result[:limit]
	}
	return result, nextCursor, nil
}

// UpdateCampaignDraft mutates editable fields. Rejects non-draft
// campaigns with ErrCampaignStateMismatch.
func (s *PostgresStore) UpdateCampaignDraft(ctx context.Context, id string, patch CampaignDraftPatch) error {
	current, err := s.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != model.CampaignStatusDraft {
		return ErrCampaignStateMismatch
	}

	name := current.Name
	if patch.Name != nil {
		name = *patch.Name
	}
	audienceID := current.AudienceID
	if patch.AudienceID != nil {
		if *patch.AudienceID == "" {
			audienceID = nil
		} else {
			v := *patch.AudienceID
			audienceID = &v
		}
	}
	snapshot := current.AudienceSnapshot
	if patch.AudienceSnapshot != nil {
		snapshot = patch.AudienceSnapshot
	}
	content := current.Content
	if patch.Content != nil {
		content = patch.Content
	}
	snapshotJSON, err := marshalAudienceSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("marshal audience snapshot: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET name = $2,
		    audience_id = $3,
		    audience_snapshot = $4::jsonb,
		    content = $5::jsonb,
		    updated_by = $6::uuid,
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'draft'
	`, id, name, nilableUUID(audienceID), snapshotJSON, []byte(content), patch.UpdatedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCampaignStateMismatch
	}
	return nil
}

// DeleteCampaign hard-deletes a draft or cancelled campaign. Rejects
// any other state with ErrCampaignStateMismatch. Returns
// ErrCampaignNotFound if the row is missing.
func (s *PostgresStore) DeleteCampaign(ctx context.Context, id string, actorID string) error {
	_ = actorID
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM campaigns
		WHERE id = $1::uuid AND status IN ('draft', 'cancelled')
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Distinguish missing vs wrong-state for the handler.
		var status string
		row := s.pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1::uuid`, id)
		if scanErr := row.Scan(&status); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return ErrCampaignNotFound
			}
			return scanErr
		}
		return ErrCampaignStateMismatch
	}
	return nil
}

// ScheduleCampaign moves a draft campaign to scheduled.
func (s *PostgresStore) ScheduleCampaign(ctx context.Context, id string, scheduledAt time.Time, actorID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET status = 'scheduled',
		    scheduled_at = $2,
		    updated_by = $3::uuid,
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'draft'
	`, id, scheduledAt, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return stateOrNotFound(ctx, s, id)
	}
	return nil
}

// CancelCampaign moves draft/scheduled → cancelled. Sending/sent/failed
// campaigns cannot be cancelled (the caller gets ErrCampaignStateMismatch).
func (s *PostgresStore) CancelCampaign(ctx context.Context, id string, actorID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET status = 'cancelled',
		    cancelled_by = $2::uuid,
		    updated_by = $2::uuid,
		    updated_at = now()
		WHERE id = $1::uuid AND status IN ('draft', 'scheduled')
	`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return stateOrNotFound(ctx, s, id)
	}
	return nil
}

// ClaimCampaignForSend is a compare-and-swap used by the send worker.
// Transitions expectedStatus → "sending" atomically.
func (s *PostgresStore) ClaimCampaignForSend(ctx context.Context, id, expectedStatus string, startedAt time.Time, batchID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET status = 'sending',
		    send_started_at = $3,
		    batch_id = $4,
		    updated_at = now()
		WHERE id = $1::uuid AND status = $2
	`, id, expectedStatus, startedAt, batchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return stateOrNotFound(ctx, s, id)
	}
	return nil
}

// RevertCampaignToDraft reverses an in-flight transition when the
// subsequent enqueue fails. Only safe when the River job was never
// accepted — if the job is enqueued, call CancelCampaign instead.
// Used by the handler's send/schedule flows to avoid stranding a
// campaign in sending/scheduled without a worker behind it.
func (s *PostgresStore) RevertCampaignToDraft(ctx context.Context, id, fromStatus string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET status = 'draft',
		    scheduled_at = NULL,
		    send_started_at = NULL,
		    batch_id = NULL,
		    updated_at = now()
		WHERE id = $1::uuid AND status = $2
	`, id, fromStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return stateOrNotFound(ctx, s, id)
	}
	return nil
}

// FinalizeCampaignSend records the terminal state of a run. If errorMsg
// is non-empty the status becomes "failed" (preserving partial counts);
// otherwise it becomes "sent".
func (s *PostgresStore) FinalizeCampaignSend(ctx context.Context, id string, sentCount, failedCount int, finishedAt time.Time, errorMsg string) error {
	status := model.CampaignStatusSent
	var errPtr *string
	if errorMsg != "" {
		status = model.CampaignStatusFailed
		errPtr = &errorMsg
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE campaigns
		SET status = $2,
		    sent_count = $3,
		    failed_count = $4,
		    send_finished_at = $5,
		    error_message = $6,
		    updated_at = now()
		WHERE id = $1::uuid AND status = 'sending'
	`, id, status, sentCount, failedCount, finishedAt, errPtr)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return stateOrNotFound(ctx, s, id)
	}
	return nil
}

// --- helpers ---

const selectCampaignSQL = `
	SELECT id, name, channel, kind, status,
	       audience_id, audience_snapshot, content,
	       scheduled_at, send_started_at, send_finished_at,
	       sent_count, failed_count, batch_id, error_message,
	       created_by, updated_by, cancelled_by,
	       created_at, updated_at
	FROM campaigns
`

type campaignRowScanner interface {
	Scan(dest ...any) error
}

func scanCampaign(row campaignRowScanner) (*model.Campaign, error) {
	var (
		c                model.Campaign
		audienceID       *string
		audienceSnapshot []byte
		content          []byte
		scheduledAt      *time.Time
		sendStartedAt    *time.Time
		sendFinishedAt   *time.Time
		batchID          *string
		errorMessage     *string
		updatedBy        *string
		cancelledBy      *string
	)
	err := row.Scan(
		&c.ID, &c.Name, &c.Channel, &c.Kind, &c.Status,
		&audienceID, &audienceSnapshot, &content,
		&scheduledAt, &sendStartedAt, &sendFinishedAt,
		&c.SentCount, &c.FailedCount, &batchID, &errorMessage,
		&c.CreatedBy, &updatedBy, &cancelledBy,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.AudienceID = audienceID
	c.ScheduledAt = scheduledAt
	c.SendStartedAt = sendStartedAt
	c.SendFinishedAt = sendFinishedAt
	c.BatchID = batchID
	c.ErrorMessage = errorMessage
	c.UpdatedBy = updatedBy
	c.CancelledBy = cancelledBy
	if len(content) > 0 {
		c.Content = json.RawMessage(content)
	} else {
		c.Content = json.RawMessage("{}")
	}
	if len(audienceSnapshot) > 0 && string(audienceSnapshot) != "null" {
		var snap model.AudienceSnapshot
		if err := json.Unmarshal(audienceSnapshot, &snap); err != nil {
			return nil, fmt.Errorf("decode audience_snapshot: %w", err)
		}
		c.AudienceSnapshot = &snap
	}
	return &c, nil
}

func marshalAudienceSnapshot(s *model.AudienceSnapshot) ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	return json.Marshal(s)
}

func nilableUUID(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

// stateOrNotFound inspects the campaign row to distinguish
// "no such campaign" from "campaign is in the wrong state for this
// transition." The caller uses this to translate to HTTP 404 vs 409.
func stateOrNotFound(ctx context.Context, s *PostgresStore, id string) error {
	var status string
	row := s.pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1::uuid`, id)
	if err := row.Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCampaignNotFound
		}
		return err
	}
	return ErrCampaignStateMismatch
}
