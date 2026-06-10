package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ResolveMemoryLifecycleForList batches the pending_dream_action
// resolution for a page of memories.
//
// Semantics:
//   - For each memory, find the most recent dream action touching it
//     (memory.id = ANY(dream_actions.source_memory_ids)) whose
//     created_at is LATER THAN the viewer's last visit to the memory's
//     CURRENT topic (as recorded in memory_topics). If the viewer has
//     never visited that topic, the comparison uses epoch so ANY
//     existing dream action counts as pending.
//   - If a memory's current topic assignment is absent (unassigned),
//     no scoping anchor exists — we use epoch and return the most
//     recent action across all dream runs. This matches the UX
//     expectation that "memax reorganized your unassigned memory" is
//     an always-visible signal until the user files it into a topic.
//   - dream_history is NOT populated by this resolver (empty slice on
//     the returned MemoryLifecycle). Callers that need history hit
//     ResolveMemoryLifecycleForDetail.
//
// Visibility: the query joins memories and filters via VisibilityScope
// so the resolver cannot leak lifecycle for memories outside the
// viewer's scope — defensive even though callers should already have
// filtered at the list level.
func (s *PostgresStore) ResolveMemoryLifecycleForList(
	ctx context.Context,
	scope VisibilityScope,
	viewerID string,
	memoryIDs []string,
) (map[string]*model.MemoryLifecycle, error) {
	out := make(map[string]*model.MemoryLifecycle, len(memoryIDs))
	if len(memoryIDs) == 0 {
		return out, nil
	}

	// scopeArgs flow at fixed positions $3, $4 so we can reuse the same
	// param numbers for three different aliases (m on memories, ft/tt on
	// topics joined for lineage names). See SQLFilterFor.
	scopeArgs := scope.filterArgs()
	args := []any{viewerID, memoryIDs}
	args = append(args, scopeArgs...)
	memScope := scope.SQLFilterFor("m", 3)
	fromScope := scope.SQLFilterFor("ft", 3)
	toScope := scope.SQLFilterFor("tt", 3)

	// Only "user-visible" lifecycle action types drive row lifecycle
	// signals. "contradiction" actions create review notifications but
	// do not count as a "dream moved this" signal — they reflect
	// potential conflicts surfaced for user review, not a completed
	// reorganization. Filtering at the query layer keeps the server
	// authoritative and matches the SDK's DreamActionRef.action_type
	// union ("organize" | "merge" | "archive" | "restructure").
	const lifecycleActionTypes = `('organize', 'merge', 'archive', 'restructure')`

	query := fmt.Sprintf(`
		WITH scoped_mems AS (
			SELECT m.id AS memory_id, mt.topic_id
			FROM memories m
			LEFT JOIN memory_topics mt ON mt.memory_id = m.id
			WHERE m.id = ANY($2::uuid[])
			  AND %s
		),
		mem_visit AS (
			SELECT sm.memory_id,
			       sm.topic_id,
			       COALESCE(tv.last_visited_at, 'epoch'::timestamptz) AS visit_at
			FROM scoped_mems sm
			LEFT JOIN topic_visits tv
			  ON tv.user_id = $1::uuid AND tv.topic_id = sm.topic_id
		),
		latest AS (
			SELECT DISTINCT ON (mv.memory_id)
			       mv.memory_id,
			       da.run_id,
			       da.action_type,
			       da.from_topic_id,
			       da.to_topic_id,
			       da.reason,
			       da.created_at
			FROM mem_visit mv
			JOIN dream_actions da ON mv.memory_id::text = ANY(da.source_memory_ids)
			WHERE da.created_at > mv.visit_at
			  AND da.action_type IN %s
			ORDER BY mv.memory_id, da.created_at DESC
		)
		SELECT l.memory_id,
		       l.run_id,
		       l.action_type,
		       -- Pull id + name + icon from the scoped topic joins (ft / tt)
		       -- rather than from l.from_topic_id / l.to_topic_id so a
		       -- scope miss returns NULL for ALL three columns. This
		       -- prevents topic-id leak AND icon leak: if viewer can't
		       -- see the topic, we return no reference at all — not an
		       -- icon that hints at what the topic looked like.
		       COALESCE(ft.id::text, ''),
		       COALESCE(tt.id::text, ''),
		       COALESCE(ft.name, ''),
		       COALESCE(tt.name, ''),
		       COALESCE(ft.icon, ''),
		       COALESCE(tt.icon, ''),
		       COALESCE(l.reason, ''),
		       l.created_at
		FROM latest l
		-- Scope-safe topic name joins: only include the topic name if
		-- the viewer can see that topic. Out-of-scope topics fall
		-- through to NULL → COALESCE '' → buildDreamActionRef returns
		-- nil for that side. Prevents topic-name leak through stale
		-- dream-action references.
		LEFT JOIN topics ft ON ft.id = l.from_topic_id AND %s
		LEFT JOIN topics tt ON tt.id = l.to_topic_id AND %s
	`, memScope, lifecycleActionTypes, fromScope, toScope)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			memoryID, runID, actionType string
			fromID, toID                string
			fromName, toName            string
			fromIcon, toIcon            string
			reason                      string
			at                          time.Time
		)
		if err := rows.Scan(&memoryID, &runID, &actionType, &fromID, &toID,
			&fromName, &toName, &fromIcon, &toIcon, &reason, &at); err != nil {
			continue
		}
		out[memoryID] = &model.MemoryLifecycle{
			PendingDreamAction: buildDreamActionRef(runID, actionType, at,
				fromID, fromName, fromIcon, toID, toName, toIcon, reason),
			DreamHistory: []model.DreamActionRef{}, // list reads do not fetch history
		}
	}
	return out, rows.Err()
}

// ResolveMemoryLifecycleForDetail returns the full lifecycle for a
// single memory — pending action scoped to topic_visits plus up to 10
// most recent dream_history entries, durable and unscoped.
func (s *PostgresStore) ResolveMemoryLifecycleForDetail(
	ctx context.Context,
	scope VisibilityScope,
	viewerID, memoryID string,
) (*model.MemoryLifecycle, error) {
	pending, err := s.ResolveMemoryLifecycleForList(ctx, scope, viewerID, []string{memoryID})
	if err != nil {
		return nil, err
	}

	lifecycle := &model.MemoryLifecycle{
		DreamHistory: []model.DreamActionRef{},
	}
	if existing, ok := pending[memoryID]; ok && existing != nil {
		lifecycle.PendingDreamAction = existing.PendingDreamAction
	}

	// History query — unscoped by topic_visits, visibility-scoped at
	// the memories join so the resolver can't return history for a
	// memory the viewer shouldn't see. Topic name joins additionally
	// enforce scope so out-of-scope topics degrade to null (no name
	// leak). action_type filter matches the list resolver —
	// contradictions never appear in lifecycle history.
	scopeArgs := scope.filterArgs()
	args := []any{memoryID}
	args = append(args, scopeArgs...)
	memScope := scope.SQLFilterFor("m", 2)
	fromScope := scope.SQLFilterFor("ft", 2)
	toScope := scope.SQLFilterFor("tt", 2)
	const lifecycleActionTypes = `('organize', 'merge', 'archive', 'restructure')`

	historyQuery := fmt.Sprintf(`
		SELECT da.run_id,
		       da.action_type,
		       -- Same id/name/icon-from-scoped-join trick as the list
		       -- query so an out-of-scope topic never leaks its id,
		       -- name, or icon.
		       COALESCE(ft.id::text, ''),
		       COALESCE(tt.id::text, ''),
		       COALESCE(ft.name, ''),
		       COALESCE(tt.name, ''),
		       COALESCE(ft.icon, ''),
		       COALESCE(tt.icon, ''),
		       COALESCE(da.reason, ''),
		       da.created_at
		FROM dream_actions da
		JOIN memories m ON m.id::text = $1
		LEFT JOIN topics ft ON ft.id = da.from_topic_id AND %s
		LEFT JOIN topics tt ON tt.id = da.to_topic_id AND %s
		WHERE $1 = ANY(da.source_memory_ids)
		  AND da.action_type IN %s
		  AND %s
		ORDER BY da.created_at DESC
		LIMIT 10
	`, fromScope, toScope, lifecycleActionTypes, memScope)

	rows, err := s.pool.Query(ctx, historyQuery, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lifecycle, nil
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			runID, actionType string
			fromID, toID      string
			fromName, toName  string
			fromIcon, toIcon  string
			reason            string
			at                time.Time
		)
		if err := rows.Scan(&runID, &actionType, &fromID, &toID,
			&fromName, &toName, &fromIcon, &toIcon, &reason, &at); err != nil {
			continue
		}
		ref := buildDreamActionRef(runID, actionType, at,
			fromID, fromName, fromIcon, toID, toName, toIcon, reason)
		if ref != nil {
			lifecycle.DreamHistory = append(lifecycle.DreamHistory, *ref)
		}
	}
	return lifecycle, rows.Err()
}

// ResolveTopicLifecycle batches delta_since_visit aggregates for a set
// of topics.
//
// Counts are non-overlapping:
//   - Added: dream actions with to_topic_id == topic AND from_topic_id IS NULL
//     (new inbound from unassigned — a true "arrival" in this topic).
//   - Reorganized: inter-topic moves involving this topic
//     (from_topic_id == topic OR (to_topic_id == topic AND from_topic_id IS NOT NULL)).
//
// This split lets the UI render "+3 · 2 reorganized" without overlap
// confusion on the topic card chip.
func (s *PostgresStore) ResolveTopicLifecycle(
	ctx context.Context,
	scope VisibilityScope,
	viewerID string,
	topicIDs []string,
) (map[string]*model.TopicLifecycle, error) {
	out := make(map[string]*model.TopicLifecycle, len(topicIDs))
	if len(topicIDs) == 0 {
		return out, nil
	}

	scopeFilter, scopeArgs := scope.SQLFilter("t", 3)
	args := []any{viewerID, topicIDs}
	args = append(args, scopeArgs...)

	query := fmt.Sprintf(`
		WITH scoped_topics AS (
			SELECT t.id AS topic_id,
			       COALESCE(tv.last_visited_at, 'epoch'::timestamptz) AS visit_at
			FROM topics t
			LEFT JOIN topic_visits tv
			  ON tv.user_id = $1::uuid AND tv.topic_id = t.id
			WHERE t.id = ANY($2::uuid[])
			  AND %s
		),
		matched AS (
			SELECT st.topic_id,
			       st.visit_at,
			       da.run_id,
			       da.from_topic_id,
			       da.to_topic_id,
			       da.created_at
			FROM scoped_topics st
			JOIN dream_actions da
			  ON (da.from_topic_id = st.topic_id OR da.to_topic_id = st.topic_id)
			WHERE da.created_at > st.visit_at
			  AND da.action_type IN ('organize', 'merge', 'archive', 'restructure')
		)
		SELECT topic_id,
		       MIN(visit_at) AS since_visit_at,
		       COUNT(*) FILTER (WHERE to_topic_id = topic_id AND from_topic_id IS NULL) AS added,
		       COUNT(*) FILTER (WHERE from_topic_id = topic_id OR (to_topic_id = topic_id AND from_topic_id IS NOT NULL)) AS reorganized,
		       ARRAY_AGG(DISTINCT run_id) AS run_ids
		FROM matched
		GROUP BY topic_id
	`, scopeFilter)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			topicID      string
			sinceVisitAt time.Time
			added        int
			reorganized  int
			runIDs       []string
		)
		if err := rows.Scan(&topicID, &sinceVisitAt, &added, &reorganized, &runIDs); err != nil {
			continue
		}
		if added == 0 && reorganized == 0 {
			continue
		}
		out[topicID] = &model.TopicLifecycle{
			DeltaSinceVisit: &model.TopicDeltaSummary{
				SinceVisitAt: sinceVisitAt,
				RunIDs:       runIDs,
				Added:        added,
				Reorganized:  reorganized,
			},
		}
	}
	return out, rows.Err()
}

// buildDreamActionRef maps the scanned columns to a DreamActionRef,
// converting empty strings (SQL NULL via COALESCE) to nil for the
// from/to topic refs. Returns nil if runID is empty (no action).
//
// fromIcon / toIcon are the topic icon names resolved via the scoped
// join at the query layer — empty string when the topic is out of
// scope or has no icon. Kept on the DreamTopicRef so UI surfaces
// (dream-history, hover-popover) can render full topic identity
// matching the row breadcrumb.
func buildDreamActionRef(
	runID, actionType string,
	at time.Time,
	fromID, fromName, fromIcon string,
	toID, toName, toIcon string,
	reason string,
) *model.DreamActionRef {
	if runID == "" {
		return nil
	}
	ref := &model.DreamActionRef{
		RunID:      runID,
		ActionType: actionType,
		At:         at,
		Reason:     reason,
	}
	if fromID != "" {
		ref.FromTopic = &model.DreamTopicRef{ID: fromID, Name: fromName, Icon: fromIcon}
	}
	if toID != "" {
		ref.ToTopic = &model.DreamTopicRef{ID: toID, Name: toName, Icon: toIcon}
	}
	return ref
}
