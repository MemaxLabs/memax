package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Restructure-related errors.
var (
	ErrTopicRestructureChildNotInHub  = errors.New("restructure child topic is not in the requested hub")
	ErrTopicRestructureParentNotInHub = errors.New("restructure parent topic is not in the requested hub")
	ErrTopicRestructureSelfParent     = errors.New("a topic cannot be its own parent")
	ErrTopicRestructureCycle          = errors.New("restructure would create a topic-tree cycle: new parent is a descendant of child")
	ErrTopicRestructureArchived       = errors.New("restructure cannot involve archived topics — restore them first")
)

// ApplyTopicRestructure is the auto-commit entry point for the
// reparent domain primitive.
//
// newParentID may be the empty string to promote the child to a root
// topic in the hub.
//
// Returns nil on success, or one of the Err* sentinels above. Producers
// that need a shared transaction boundary (notification resolver fan-out)
// should call WithTx + ApplyTopicRestructureTx instead.
func (s *PostgresStore) ApplyTopicRestructure(ctx context.Context, hubID, childID, newParentID string) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		return s.ApplyTopicRestructureTx(ctx, tx, hubID, childID, newParentID)
	})
}

// ApplyTopicRestructureTx reparents childID under newParentID inside an
// existing transaction. Flow:
//
//  1. Refuse self-parent (childID == newParentID).
//  2. Lock the child row and verify it lives in hubID.
//  3. If newParentID is non-empty: lock the parent row and verify it
//     also lives in hubID.
//  4. Cycle prevention: refuse if the new parent is a descendant of
//     the child. Walked with the same hub-scoped recursive CTE the
//     merge primitive uses.
//  5. Single UPDATE on the child's parent_id + updated_at.
//
// Depth-cap enforcement (the 5-level topic tree limit) lives in the
// topics handler today — the store primitive deliberately stays focused
// on cycle / cross-hub / self-parent invariants and lets the handler
// own UX-shaped bounds. Callers that need depth enforcement should run
// GetSubtreeMaxDepth before invoking this primitive.
func (s *PostgresStore) ApplyTopicRestructureTx(ctx context.Context, tx pgx.Tx, hubID, childID, newParentID string) error {
	if newParentID != "" && childID == newParentID {
		return ErrTopicRestructureSelfParent
	}

	// Lock and verify the child. Archived children are refused — a
	// stale dream/review proposal must not silently move a topic the
	// user has parked in the archive.
	var childExists, childArchived bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid FOR UPDATE),
			COALESCE((SELECT archived_at IS NOT NULL FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid), false)`,
		childID, hubID,
	).Scan(&childExists, &childArchived); err != nil {
		return fmt.Errorf("lock restructure child: %w", err)
	}
	if !childExists {
		return ErrTopicRestructureChildNotInHub
	}
	if childArchived {
		return ErrTopicRestructureArchived
	}

	if newParentID != "" {
		// Lock and verify the new parent. Archived parents are refused
		// so an active topic can never be re-planted under a hidden node.
		var parentExists, parentArchived bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid FOR UPDATE),
				COALESCE((SELECT archived_at IS NOT NULL FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid), false)`,
			newParentID, hubID,
		).Scan(&parentExists, &parentArchived); err != nil {
			return fmt.Errorf("lock restructure parent: %w", err)
		}
		if !parentExists {
			return ErrTopicRestructureParentNotInHub
		}
		if parentArchived {
			return ErrTopicRestructureArchived
		}

		// Cycle prevention. The new parent must not live inside the
		// subtree rooted at the child — otherwise reparenting would
		// create a loop (child → newParent → … → child).
		var cycle bool
		if err := tx.QueryRow(ctx,
			`WITH RECURSIVE child_subtree AS (
				SELECT id FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid
				UNION ALL
				SELECT t.id
				FROM topics t
				JOIN child_subtree s ON t.parent_id = s.id
				WHERE t.hub_id = $2::uuid
			)
			SELECT EXISTS(SELECT 1 FROM child_subtree WHERE id = $3::uuid)`,
			childID, hubID, newParentID,
		).Scan(&cycle); err != nil {
			return fmt.Errorf("restructure cycle check: %w", err)
		}
		if cycle {
			return ErrTopicRestructureCycle
		}
	}

	// Apply the reparent. NULLIF turns an empty newParentID into a real
	// SQL NULL so the child becomes a root topic.
	tag, err := tx.Exec(ctx,
		`UPDATE topics
		SET parent_id = NULLIF($1, '')::uuid, updated_at = now()
		WHERE id = $2::uuid AND hub_id = $3::uuid`,
		newParentID, childID, hubID,
	)
	if err != nil {
		return fmt.Errorf("apply restructure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		// Defensive: the FOR UPDATE earlier should make this
		// unreachable. If it fires, the tx rolls back and we surface
		// a hard error so the caller can investigate.
		return fmt.Errorf("restructure consistency: expected 1 row updated, got %d", tag.RowsAffected())
	}
	return nil
}
