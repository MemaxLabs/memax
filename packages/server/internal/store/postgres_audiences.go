package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// CreateAudience inserts a saved audience. Validation of rule_type vs
// rule fields is the handler's job; the store enforces the CHECK
// constraint on rule_type and otherwise persists the rule blob as-is.
func (s *PostgresStore) CreateAudience(ctx context.Context, a *model.Audience) error {
	ruleJSON, err := json.Marshal(a.Rule)
	if err != nil {
		return fmt.Errorf("marshal audience rule: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO audiences (
			id, name, description, rule_type, rule_json,
			created_by, created_at, updated_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5::jsonb,
			$6::uuid, now(), now()
		)
	`, a.ID, a.Name, a.Description, a.RuleType, ruleJSON, a.CreatedBy)
	return err
}

// GetAudience loads one audience by id.
func (s *PostgresStore) GetAudience(ctx context.Context, id string) (*model.Audience, error) {
	row := s.pool.QueryRow(ctx, selectAudienceSQL+` WHERE id = $1::uuid`, id)
	a, err := scanAudience(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAudienceNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAudiences returns all saved audiences ordered newest-first. The
// volume is low (admin-authored), so pagination isn't required yet.
func (s *PostgresStore) ListAudiences(ctx context.Context) ([]model.Audience, error) {
	rows, err := s.pool.Query(ctx, selectAudienceSQL+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]model.Audience, 0, 8)
	for rows.Next() {
		a, scanErr := scanAudience(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *a)
	}
	return result, rows.Err()
}

// UpdateAudience rewrites the editable fields.
func (s *PostgresStore) UpdateAudience(ctx context.Context, a *model.Audience) error {
	ruleJSON, err := json.Marshal(a.Rule)
	if err != nil {
		return fmt.Errorf("marshal audience rule: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE audiences
		SET name = $2,
		    description = $3,
		    rule_type = $4,
		    rule_json = $5::jsonb,
		    updated_at = now()
		WHERE id = $1::uuid
	`, a.ID, a.Name, a.Description, a.RuleType, ruleJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAudienceNotFound
	}
	return nil
}

// DeleteAudience removes the audience. Campaigns that reference it keep
// their audience_snapshot (the execution contract) but audience_id is
// set to NULL by the FK's ON DELETE SET NULL.
func (s *PostgresStore) DeleteAudience(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audiences WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAudienceNotFound
	}
	return nil
}

const selectAudienceSQL = `
	SELECT id, name, description, rule_type, rule_json,
	       created_by, created_at, updated_at
	FROM audiences
`

type audienceRowScanner interface {
	Scan(dest ...any) error
}

func scanAudience(row audienceRowScanner) (*model.Audience, error) {
	var a model.Audience
	var ruleJSON []byte
	if err := row.Scan(
		&a.ID, &a.Name, &a.Description, &a.RuleType, &ruleJSON,
		&a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(ruleJSON) > 0 && string(ruleJSON) != "null" {
		if err := json.Unmarshal(ruleJSON, &a.Rule); err != nil {
			return nil, fmt.Errorf("decode audience rule: %w", err)
		}
	}
	return &a, nil
}
