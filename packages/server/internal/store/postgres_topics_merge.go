package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MergeTopics-related errors. Returned by MergeTopics and MergeTopicsTx
// so callers can map them to user-facing API errors at the handler layer.
var (
	ErrTopicMergeEmptySources   = errors.New("merge requires at least one source topic")
	ErrTopicMergeTargetIsSource = errors.New("merge target cannot be a source topic")
	ErrTopicMergeTargetNotInHub = errors.New("merge target topic is not in the requested hub")
	ErrTopicMergeSourceNotInHub = errors.New("at least one source topic is not in the requested hub")
	ErrTopicMergeCycle          = errors.New("merge would create a topic-tree cycle: target is a descendant of a source")
)

// MergeTopics is the auto-commit entry point for the topic-merge domain
// primitive. It opens its own transaction via WithTx and delegates to
// MergeTopicsTx.
//
// Producers that need to fan the merge into a larger transaction (e.g.
// review resolution that also inserts a notification + audit row in the
// same boundary) should call WithTx themselves and invoke MergeTopicsTx
// inside the closure.
//
// Returns the number of memories rehomed to the target topic. Errors
// follow the package-level Err* sentinels above.
func (s *PostgresStore) MergeTopics(ctx context.Context, hubID, targetID string, sourceIDs []string) (int, error) {
	var moved int
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		n, err := s.MergeTopicsTx(ctx, tx, hubID, targetID, sourceIDs)
		moved = n
		return err
	})
	return moved, err
}

// MergeTopicsTx performs the topic-merge primitive inside an existing
// transaction. The flow:
//
//  1. Validate inputs (non-empty sources, target not in sources).
//  2. Lock and verify the target topic is in hubID.
//  3. Lock and verify every source topic is in hubID.
//  4. Cycle prevention: refuse if the target is a descendant of any
//     source. Walked with a single recursive CTE over the union of the
//     source subtrees.
//  5. Rehome memories: UPDATE memory_topics SET topic_id = target WHERE
//     topic_id = ANY(sources). Safe as a single statement because the
//     unique index idx_memory_topics_memory enforces that each memory is
//     in at most one topic — there are no (memory, target) collisions.
//  6. Reparent children: UPDATE topics SET parent_id = target WHERE
//     parent_id = ANY(sources) AND hub_id = hub.
//  7. Bump target.updated_at.
//  8. Delete the source topics. memory_topics rows for the (now
//     orphaned) source topic_ids are already moved in step 5, and any
//     stragglers cascade away via memory_topics_topic_id_fkey ON DELETE
//     CASCADE.
//
// Returns the count of memories rehomed in step 5.
func (s *PostgresStore) MergeTopicsTx(ctx context.Context, tx pgx.Tx, hubID, targetID string, sourceIDs []string) (int, error) {
	if len(sourceIDs) == 0 {
		return 0, ErrTopicMergeEmptySources
	}

	// Dedupe sources defensively. Same source listed twice is a caller
	// quirk, not a user error worth surfacing — silently collapse.
	dedupedSources := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if id == targetID {
			return 0, ErrTopicMergeTargetIsSource
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dedupedSources = append(dedupedSources, id)
	}

	// Lock target. FOR UPDATE pins the row for the duration of the tx
	// so a concurrent rename / reparent / delete on the target cannot
	// race with the merge.
	var targetExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM topics WHERE id = $1::uuid AND hub_id = $2::uuid FOR UPDATE)`,
		targetID, hubID,
	).Scan(&targetExists); err != nil {
		return 0, fmt.Errorf("lock merge target: %w", err)
	}
	if !targetExists {
		return 0, ErrTopicMergeTargetNotInHub
	}

	// Lock and count sources. If the count differs from the deduped
	// input length, at least one source is not in the hub — refuse.
	var sourceCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM (
			SELECT id FROM topics
			WHERE id = ANY($1::uuid[]) AND hub_id = $2::uuid
			FOR UPDATE
		) t`,
		dedupedSources, hubID,
	).Scan(&sourceCount); err != nil {
		return 0, fmt.Errorf("lock merge sources: %w", err)
	}
	if sourceCount != len(dedupedSources) {
		return 0, ErrTopicMergeSourceNotInHub
	}

	// Cycle prevention. Walk the union of all source subtrees and
	// refuse if the target appears anywhere in it. A single CTE handles
	// every source at once so the cost is O(total subtree size), not
	// O(sources * subtree).
	var cycle bool
	if err := tx.QueryRow(ctx,
		`WITH RECURSIVE union_subtree AS (
			SELECT id FROM topics
			WHERE id = ANY($1::uuid[]) AND hub_id = $2::uuid
			UNION ALL
			SELECT t.id
			FROM topics t
			JOIN union_subtree s ON t.parent_id = s.id
			WHERE t.hub_id = $2::uuid
		)
		SELECT EXISTS(SELECT 1 FROM union_subtree WHERE id = $3::uuid)`,
		dedupedSources, hubID, targetID,
	).Scan(&cycle); err != nil {
		return 0, fmt.Errorf("merge cycle check: %w", err)
	}
	if cycle {
		return 0, ErrTopicMergeCycle
	}

	// Rehome memories. The unique index idx_memory_topics_memory makes
	// this a no-collision single-statement update — each memory is in
	// at most one topic, so moving every memory currently assigned to a
	// source topic over to the target cannot violate the constraint.
	rehome, err := tx.Exec(ctx,
		`UPDATE memory_topics
		SET topic_id = $1::uuid
		WHERE topic_id = ANY($2::uuid[])`,
		targetID, dedupedSources,
	)
	if err != nil {
		return 0, fmt.Errorf("rehome memories: %w", err)
	}
	moved := int(rehome.RowsAffected())

	// Reparent children. Any topic whose parent is being deleted is
	// re-pointed at the merge target so the tree stays connected.
	// Cycle prevention above guarantees the target is not itself in
	// the set being reparented (target ∉ source subtrees ⇒ target's
	// own parent_id is unaffected).
	if _, err := tx.Exec(ctx,
		`UPDATE topics
		SET parent_id = $1::uuid, updated_at = now()
		WHERE parent_id = ANY($2::uuid[]) AND hub_id = $3::uuid`,
		targetID, dedupedSources, hubID,
	); err != nil {
		return 0, fmt.Errorf("reparent children: %w", err)
	}

	// Bump target updated_at so listing UIs sort the merged target to
	// the top of "recently changed."
	if _, err := tx.Exec(ctx,
		`UPDATE topics SET updated_at = now() WHERE id = $1::uuid AND hub_id = $2::uuid`,
		targetID, hubID,
	); err != nil {
		return 0, fmt.Errorf("touch merge target: %w", err)
	}

	// Delete source topics. The earlier in-hub validation guarantees we
	// only delete what the caller is authorized to touch. Any straggler
	// memory_topics rows would cascade via the FK, but step 5 already
	// rehomed them all so this is just a topics-table delete.
	del, err := tx.Exec(ctx,
		`DELETE FROM topics
		WHERE id = ANY($1::uuid[]) AND hub_id = $2::uuid`,
		dedupedSources, hubID,
	)
	if err != nil {
		return 0, fmt.Errorf("delete source topics: %w", err)
	}
	if int(del.RowsAffected()) != len(dedupedSources) {
		// Should be unreachable: we locked sources at the top of the
		// tx and the FOR UPDATE held them. If the count diverges, the
		// invariant is broken — treat as a hard error so the tx rolls
		// back.
		return 0, fmt.Errorf("merge consistency: expected to delete %d source topics, deleted %d", len(dedupedSources), del.RowsAffected())
	}

	return moved, nil
}
