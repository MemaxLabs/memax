package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- User Preferences ---

// GetUserPreferences returns the user's stored preferences, merged
// lazily with defaults by the caller (p.MergedSettings).
//
// Error policy: a missing row is NOT an error — users without any
// stored preferences get an empty Settings map and fall through to
// DefaultSettings when merged. Any OTHER scan error (DB down, type
// mismatch, schema drift) is surfaced so callers can decide what
// to do. This previously returned defaults for every error,
// silently masking real DB problems from every caller —
// notification gates, admin panels, and dream engine pref reads
// all swallowed the same blind fallback.
func (s *PostgresStore) GetUserPreferences(userID string) (*model.UserPreferences, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT user_id, settings, updated_at FROM user_preferences WHERE user_id = $1`, userID)
	var p model.UserPreferences
	err := row.Scan(&p.UserID, &p.Settings, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &model.UserPreferences{
			UserID:   userID,
			Settings: map[string]any{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) UpsertUserPreferences(userID string, settings map[string]any) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_preferences (user_id, settings, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id) DO UPDATE SET
			settings = user_preferences.settings || $2::jsonb,
			updated_at = now()`,
		userID, settings)
	return err
}
