package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// AppendCommsAudit writes one audit entry. Metadata defaults to {}.
func (s *PostgresStore) AppendCommsAudit(ctx context.Context, entry *model.CommsAuditEntry) error {
	metadata := []byte(entry.Metadata)
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	var actor any
	if entry.ActorID != nil && *entry.ActorID != "" {
		actor = *entry.ActorID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO comms_audit (
			id, resource_type, resource_id, action, actor_id, metadata, created_at
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, $6::jsonb, now()
		)
	`, entry.ID, entry.ResourceType, entry.ResourceID, entry.Action, actor, metadata)
	if err != nil {
		return fmt.Errorf("insert comms_audit: %w", err)
	}
	return nil
}

// ListCommsAudit returns audit rows for one resource, newest-first.
func (s *PostgresStore) ListCommsAudit(ctx context.Context, resourceType, resourceID string) ([]model.CommsAuditEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, resource_type, resource_id, action, actor_id, metadata, created_at
		FROM comms_audit
		WHERE resource_type = $1 AND resource_id = $2::uuid
		ORDER BY created_at DESC
		LIMIT 200
	`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]model.CommsAuditEntry, 0, 16)
	for rows.Next() {
		var (
			e        model.CommsAuditEntry
			actor    *string
			metadata []byte
		)
		if err := rows.Scan(
			&e.ID, &e.ResourceType, &e.ResourceID, &e.Action,
			&actor, &metadata, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.ActorID = actor
		if len(metadata) > 0 {
			e.Metadata = json.RawMessage(metadata)
		} else {
			e.Metadata = json.RawMessage(`{}`)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
