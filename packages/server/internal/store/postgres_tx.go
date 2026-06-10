package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// WithTx begins a transaction, runs fn against it, and commits on
// success. The deferred Rollback is a no-op after a successful Commit
// (pgx returns ErrTxClosed which we deliberately swallow), so this is
// safe to call in the success path.
//
// Producers that need to fan a domain event into multiple writes —
// e.g. "create a dream action AND create a notification AND record
// an audit row" — should wrap their writes in WithTx and call the
// *Tx-suffixed sibling helpers so all writes commit or roll back
// together.
func (s *PostgresStore) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateDreamActionTx writes a dream_actions row inside an existing
// transaction.
func (s *PostgresStore) CreateDreamActionTx(ctx context.Context, tx pgx.Tx, action *model.DreamAction) error {
	return createDreamActionExec(ctx, tx, action)
}

// UpdateDreamRunTx updates a dream_runs row inside an existing transaction.
func (s *PostgresStore) UpdateDreamRunTx(ctx context.Context, tx pgx.Tx, run *model.DreamRun) error {
	return updateDreamRunExec(ctx, tx, run)
}

// createDreamActionExec is the single source of truth for the dream_actions insert.
// from_topic_id / to_topic_id are nullable — empty strings map to SQL NULL
// via nullableUUID so non-organize/restructure actions and pre-migration-069
// historical rows degrade gracefully at the read layer.
//
// agent_path defaults to 'single_call' (DB-side default) when the
// caller leaves it empty — the existing single-call code paths
// don't need to set it. Phase 2b's agent path explicitly sets
// AgentPath = "lucid_active" plus the session id + call counts
// for cost/audit attribution.
func createDreamActionExec(ctx context.Context, q pgQuerier, action *model.DreamAction) error {
	agentPath := action.AgentPath
	if agentPath == "" {
		agentPath = model.DreamActionAgentPathSingleCall
	}
	_, err := q.Exec(ctx,
		`INSERT INTO dream_actions (id, run_id, action_type, source_memory_ids,
			result_memory_id, from_topic_id, to_topic_id, reason, similarity, created_at,
			agent_path, agent_session_id, agent_model_calls, agent_tool_calls)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		action.ID, action.RunID, action.ActionType, action.SourceMemoryIDs,
		action.ResultMemoryID,
		nullableUUID(action.FromTopicID), nullableUUID(action.ToTopicID),
		action.Reason, action.Similarity, action.CreatedAt,
		agentPath, nullableString(action.AgentSessionID),
		action.AgentModelCalls, action.AgentToolCalls)
	return err
}

// nullableString returns the SQL NULL sentinel when s is empty.
// agent_session_id is nullable text so single-call rows can leave
// it blank rather than carrying a meaningless empty string.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableUUID converts an empty string to SQL NULL; any non-empty value
// is passed through as-is. The dream_actions.from_topic_id / to_topic_id
// columns are nullable uuid references to topics(id), so this is the
// bridge between our stringly-typed Go model and the DB's nullable type.
func nullableUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
