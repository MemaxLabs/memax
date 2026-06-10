package store

import (
	"context"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// GetTopicVisit returns the viewer's visit record for a topic, or the
// row does not exist. Callers treat "not exists" as "never visited"
// and substitute epoch when computing delta-since-visit.
func (s *PostgresStore) GetTopicVisit(userID, topicID string) (*model.TopicVisit, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
		SELECT user_id, topic_id, hub_id, first_visited_at, last_visited_at, created_at, updated_at
		FROM topic_visits
		WHERE user_id = $1::uuid AND topic_id = $2::uuid
	`, userID, topicID)

	var visit model.TopicVisit
	if err := row.Scan(
		&visit.UserID,
		&visit.TopicID,
		&visit.HubID,
		&visit.FirstVisitedAt,
		&visit.LastVisitedAt,
		&visit.CreatedAt,
		&visit.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &visit, nil
}

// UpsertTopicVisit inserts or updates the viewer's visit timestamp for
// a topic. Idempotent on (user_id, topic_id). HubID is captured on
// first insert so the row is denormalized for hub-scoped queries.
//
// Fires only from real topic page mount (guarded client-side in
// useMarkTopicVisit — never from prefetch, hover, or memory detail).
func (s *PostgresStore) UpsertTopicVisit(userID, topicID, hubID string, visitedAt time.Time) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO topic_visits (
			user_id,
			topic_id,
			hub_id,
			first_visited_at,
			last_visited_at,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $4, now(), now())
		ON CONFLICT (user_id, topic_id)
		DO UPDATE SET
			last_visited_at = EXCLUDED.last_visited_at,
			updated_at = now()
	`, userID, topicID, hubID, visitedAt)
	return err
}
