// Phase 3.1 chat foundation — Postgres impls of the chat MVP
// Store methods. Plan 24 §"Feature 2 — Agent Chat" specifies the
// shapes and semantics; this file is the SQL behind the
// interface. Owner-scoping is enforced at the WHERE clause on
// every read AND every write so a buggy handler caller can never
// reach across owners.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// chatSessionCols is the canonical SELECT list for chat_sessions.
// Kept as a const so list/detail queries stay in lockstep with
// the scan helper. Never mix with hand-rolled column lists.
const chatSessionCols = `id, owner_id, scope_type, scope_hub_ids, write_hub_id, title, model, tools,
	toolset_version, status, active_message_id, locked_at, message_count, pinned_at, archived_at,
	last_message_at, created_at, updated_at, deleted_at`

// chatMessageCols is the canonical SELECT list for chat_messages.
// Mirror constraint: stays in lockstep with scanChatMessage.
const chatMessageCols = `id, session_id, sequence, role, author_user_id, status, scope_hub_ids_used,
	content, tool_calls, citations, usage, error, idempotency_key, parent_message_id, toolset_version_used,
	started_at, finished_at, created_at`

// CreateChatSession — see Store interface comments. Caller has
// already validated scope shape + hub membership; the SQL CHECK
// is belt+braces.
func (s *PostgresStore) CreateChatSession(ctx context.Context, sess *model.ChatSession) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tools := sess.Tools
	if tools == nil {
		tools = []string{}
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("marshal tools: %w", err)
	}
	if sess.Status == "" {
		sess.Status = model.ChatSessionStatusIdle
	}
	if sess.ToolsetVersion == 0 {
		sess.ToolsetVersion = 1
	}
	scopeHubs := sess.ScopeHubIDs
	if scopeHubs == nil {
		scopeHubs = []string{}
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO chat_sessions (
			id, owner_id, scope_type, scope_hub_ids, write_hub_id, title, model, tools,
			toolset_version, status, active_message_id, locked_at, message_count, pinned_at,
			archived_at, last_message_at, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid[], $5, $6, $7, $8::jsonb,
			$9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		sess.ID, sess.OwnerID, sess.ScopeType, scopeHubs, nullableString(sess.WriteHubID),
		sess.Title, sess.Model, toolsJSON,
		sess.ToolsetVersion, sess.Status, nullableString(sess.ActiveMessageID), sess.LockedAt,
		sess.MessageCount, sess.PinnedAt, sess.ArchivedAt, sess.LastMessageAt,
		sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create chat session: %w", err)
	}
	return nil
}

// GetChatSession — see Store interface comments.
func (s *PostgresStore) GetChatSession(ctx context.Context, sessionID, ownerID string) (*model.ChatSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+chatSessionCols+` FROM chat_sessions
		 WHERE id = $1::uuid AND owner_id = $2::uuid`,
		sessionID, ownerID)
	sess, err := scanChatSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatSessionNotFound
	}
	return sess, err
}

// ListChatSessionsByOwner — sidebar query. Keyset pagination by
// (last_message_at DESC NULLS LAST, id). The cursor is opaque to
// the handler; internal shape is RFC3339Nano timestamp + id
// joined by `|` so timestamp ties break deterministically.
func (s *PostgresStore) ListChatSessionsByOwner(ctx context.Context, opts ChatSessionListOpts) ([]model.ChatSession, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}

	where := []string{"owner_id = $1::uuid"}
	args := []any{opts.OwnerID}
	if !opts.IncludeDeleted {
		where = append(where, "deleted_at IS NULL")
	}
	// Archive filter precedence: ArchivedOnly wins over
	// IncludeArchived when both are set so callers can't
	// accidentally combine "archived only" with "everything"
	// and get the broader set. ArchivedOnly returns the
	// archived recovery surface; IncludeArchived returns the
	// admin/audit "all sessions" combined view.
	switch {
	case opts.ArchivedOnly:
		where = append(where, "archived_at IS NOT NULL")
	case !opts.IncludeArchived:
		where = append(where, "archived_at IS NULL")
	}

	if opts.Cursor != "" {
		ts, id, ok := parseChatCursor(opts.Cursor)
		if !ok {
			return nil, "", ErrChatInvalidCursor
		}
		// (last_message_at, id) DESC keyset: a row appears AFTER
		// the cursor when (last_message_at < cursor.ts) OR
		// (last_message_at = cursor.ts AND id < cursor.id). NULL
		// last_message_at sorts LAST under "DESC NULLS LAST", so
		// we treat null as "less than every non-null timestamp"
		// by branching on cursor.ts being non-null.
		args = append(args, ts, id)
		where = append(where, fmt.Sprintf(
			"(last_message_at < $%d OR (last_message_at IS NULL AND $%d IS NOT NULL) OR (last_message_at = $%d AND id::text < $%d))",
			len(args)-1, len(args)-1, len(args)-1, len(args),
		))
	}
	args = append(args, limit+1) // fetch one extra to detect more pages

	q := fmt.Sprintf(`SELECT %s FROM chat_sessions WHERE %s
		ORDER BY last_message_at DESC NULLS LAST, id DESC
		LIMIT $%d`, chatSessionCols, strings.Join(where, " AND "), len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list chat sessions: %w", err)
	}
	defer rows.Close()

	out := make([]model.ChatSession, 0, limit)
	for rows.Next() {
		sess, err := scanChatSession(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, *sess)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	var nextCursor string
	if len(out) > limit {
		last := out[limit-1]
		out = out[:limit]
		nextCursor = formatChatCursor(last.LastMessageAt, last.ID)
	}
	return out, nextCursor, nil
}

// UpdateChatSessionMeta — see Store interface. Builds the SET
// clause dynamically from the non-nil patch fields so partial
// PATCHes don't have to round-trip the full row.
func (s *PostgresStore) UpdateChatSessionMeta(ctx context.Context, sessionID, ownerID string, patch ChatSessionMetaPatch) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if patch.Title != nil {
		add("title", *patch.Title)
	}
	if patch.PinnedAt != nil {
		// pointer-to-zero-value clears the field; the handler
		// distinguishes "unpin" (zero time) from "leave alone" (nil)
		// before reaching the store.
		if patch.PinnedAt.IsZero() {
			add("pinned_at", nil)
		} else {
			add("pinned_at", *patch.PinnedAt)
		}
	}
	if patch.ArchivedAt != nil {
		if patch.ArchivedAt.IsZero() {
			add("archived_at", nil)
		} else {
			add("archived_at", *patch.ArchivedAt)
		}
	}
	if patch.Tools != nil {
		toolsJSON, err := json.Marshal(*patch.Tools)
		if err != nil {
			return fmt.Errorf("marshal tools: %w", err)
		}
		add("tools", toolsJSON)
	}
	if patch.ToolsetVersion != nil {
		add("toolset_version", *patch.ToolsetVersion)
	}
	if patch.WriteHubID != nil {
		if *patch.WriteHubID == "" {
			add("write_hub_id", nil)
		} else {
			add("write_hub_id", *patch.WriteHubID)
		}
	}
	if len(sets) == 0 {
		return nil // no-op patch
	}
	add("updated_at", time.Now().UTC())
	args = append(args, sessionID, ownerID)

	// Tool changes need an atomic status='idle' predicate.
	// The handler's pre-check is racy: PATCH reads idle, Send
	// acquires FOR UPDATE and flips to 'running', PATCH waits,
	// Send commits, PATCH then runs an unconditional UPDATE
	// while the message is in-flight. The SQL gate closes the
	// race so a late PATCH gets 0 rows and returns
	// ErrChatSessionLocked instead of silently winning.
	// Codex flagged this on Phase 3.3 v2.
	//
	// We disambiguate "no rows because session doesn't exist"
	// from "no rows because session was non-idle" with a
	// follow-up SELECT — the row's existence answers it.
	// deleted_at IS NULL excludes soft-deleted tombstones from
	// any mutation — a soft-deleted session is read-only by
	// design (the row stays for audit but is "gone" from a
	// user perspective). Codex flagged the gap on Phase 3.3 v3.
	patchTouchesTools := patch.Tools != nil
	whereExtra := " AND deleted_at IS NULL"
	if patchTouchesTools {
		whereExtra += " AND status = 'idle'"
	}
	q := fmt.Sprintf(
		`UPDATE chat_sessions SET %s WHERE id = $%d::uuid AND owner_id = $%d::uuid%s`,
		strings.Join(sets, ", "), len(args)-1, len(args), whereExtra,
	)
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update chat session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if patchTouchesTools {
			// Distinguish locked from missing: read the row.
			// We mirror the UPDATE's deleted_at filter here so a
			// soft-deleted row reads as "not found" — same as
			// the user's mental model. If the row exists AND is
			// not soft-deleted, the only reason the UPDATE missed
			// is the status='idle' predicate.
			var exists int
			row := s.pool.QueryRow(ctx,
				`SELECT 1 FROM chat_sessions WHERE id = $1::uuid AND owner_id = $2::uuid AND deleted_at IS NULL`,
				sessionID, ownerID)
			if scanErr := row.Scan(&exists); errors.Is(scanErr, pgx.ErrNoRows) {
				return ErrChatSessionNotFound
			}
			return ErrChatSessionLocked
		}
		return ErrChatSessionNotFound
	}
	return nil
}

// SoftDeleteChatSession — see Store interface.
func (s *PostgresStore) SoftDeleteChatSession(ctx context.Context, sessionID, ownerID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE chat_sessions SET deleted_at = now(), updated_at = now()
		 WHERE id = $1::uuid AND owner_id = $2::uuid AND deleted_at IS NULL`,
		sessionID, ownerID)
	if err != nil {
		return fmt.Errorf("soft delete chat session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// AppendChatMessage — atomic insert + session-counter bump.
// Caller assigns sequence; idempotency_key collisions surface as
// ErrChatMessageIdempotencyConflict so the send handler can map
// to 202-with-active-message rather than re-running.
//
// Owner-scoped via an upfront SELECT inside the same tx — a
// caller passing another user's session_id sees
// ErrChatSessionNotFound and the insert rolls back, never
// touching the row. The verification + insert + counter bump
// all run in one transaction for atomicity.
func (s *PostgresStore) AppendChatMessage(ctx context.Context, ownerID string, msg *model.ChatMessage) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Verify the session exists AND belongs to ownerID. SELECT
	// FOR UPDATE so concurrent finalize/cancel paths serialize
	// against this insert (the session row's status + counters
	// are about to change, so the read needs the row lock).
	var ok int
	err = tx.QueryRow(ctx,
		`SELECT 1 FROM chat_sessions WHERE id = $1::uuid AND owner_id = $2::uuid AND deleted_at IS NULL FOR UPDATE`,
		msg.SessionID, ownerID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChatSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("verify chat session ownership: %w", err)
	}

	scopeHubs := msg.ScopeHubIDsUsed
	if scopeHubs == nil {
		scopeHubs = []string{}
	}
	if msg.Status == "" {
		// Default status by role: user/system rows are emitted-
		// and-done; assistant rows enter as 'queued' (the agent
		// picks up the message and transitions it to 'running').
		switch msg.Role {
		case model.ChatMessageRoleUser, model.ChatMessageRoleSystem:
			msg.Status = model.ChatMessageStatusCompleted
		default:
			msg.Status = model.ChatMessageStatusQueued
		}
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO chat_messages (
			id, session_id, sequence, role, author_user_id, status, scope_hub_ids_used,
			content, tool_calls, citations, usage, error, idempotency_key, parent_message_id,
			toolset_version_used, started_at, finished_at, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7::uuid[],
			$8, COALESCE($9::jsonb, '[]'::jsonb), COALESCE($10::jsonb, '[]'::jsonb),
			COALESCE($11::jsonb, '{}'::jsonb), $12::jsonb,
			$13, $14, $15, $16, $17, $18)`,
		msg.ID, msg.SessionID, msg.Sequence, msg.Role, msg.AuthorUserID, msg.Status, scopeHubs,
		msg.Content, msg.ToolCalls, msg.Citations, msg.Usage, msg.Error,
		nullableString(msg.IdempotencyKey), nullableString(msg.ParentMessageID),
		msg.ToolsetVersionUsed, msg.StartedAt, msg.FinishedAt, msg.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
			return ErrChatMessageIdempotencyConflict
		}
		return fmt.Errorf("insert chat message: %w", err)
	}

	// Bump session counters atomically. last_message_at = max
	// (current, this row's created_at) so an out-of-order insert
	// (rare but possible during regenerate) doesn't roll the
	// session's recency back.
	_, err = tx.Exec(ctx,
		`UPDATE chat_sessions SET
			message_count = message_count + 1,
			last_message_at = GREATEST(COALESCE(last_message_at, 'epoch'::timestamptz), $1::timestamptz),
			updated_at = now()
		 WHERE id = $2::uuid`,
		msg.CreatedAt, msg.SessionID)
	if err != nil {
		return fmt.Errorf("bump session counters: %w", err)
	}

	return tx.Commit(ctx)
}

// BeginChatMessageTurn is the send-message critical section.
//
// One transaction wraps:
//
//  1. Verify session exists, is owned, and is not deleted, with
//     SELECT FOR UPDATE so concurrent BeginChatMessageTurn calls
//     against the same session serialize on the row lock. This
//     is what makes the whole "idempotency-check + lock-flip"
//     race-safe across processes.
//  2. Idempotency lookup: SELECT chat_messages WHERE
//     (session_id, idempotency_key) — if hit, find the linked
//     assistant via parent_message_id and return Replay or
//     InFlight based on the assistant's status.
//  3. Hard cap check (message_count + 2 > 250 → reject).
//  4. Status check (status != 'idle' → ErrChatSessionLocked).
//  5. Insert user message (sequence = message_count + 1).
//  6. Insert assistant message (sequence = message_count + 2).
//  7. UPDATE chat_sessions: status='running',
//     active_message_id=<assistant>, locked_at=now(),
//     message_count += 2, last_message_at=now().
//  8. COMMIT — only now is the new state visible to other tx's.
//
// On any failure post-step-1 the deferred Rollback restores
// the prior state. Pairs with FinalizeChatMessage which flips
// the session back to idle and writes the assistant terminal
// state in its own transaction.
func (s *PostgresStore) BeginChatMessageTurn(ctx context.Context, in BeginChatMessageTurnInput) (BeginChatMessageTurnResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BeginChatMessageTurnResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Verify + lock session row. We pull every send-relevant
	// session field under FOR UPDATE so any concurrent PATCH
	// (tools, archived) serializes against this read. The
	// per-turn message stamp uses what's CURRENT at lock time,
	// not what the handler may have pre-fetched. Codex flagged
	// the toolset_version race on Phase 3.3 v1 and the archived
	// race on v2; both close with the same shape.
	var status, scopeType string
	var sessionScopeHubs []string
	var toolsetVersion int
	var messageCount int
	var archivedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT status, scope_type, scope_hub_ids, toolset_version, message_count, archived_at
		   FROM chat_sessions
		  WHERE id = $1::uuid AND owner_id = $2::uuid AND deleted_at IS NULL
		  FOR UPDATE`,
		in.SessionID, in.OwnerID).Scan(&status, &scopeType, &sessionScopeHubs, &toolsetVersion, &messageCount, &archivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BeginChatMessageTurnResult{}, ErrChatSessionNotFound
	}
	if err != nil {
		return BeginChatMessageTurnResult{}, fmt.Errorf("verify chat session: %w", err)
	}
	if archivedAt != nil {
		// Race-edge: handler pre-check saw unarchived, then a
		// PATCH archived the session before this tx acquired
		// FOR UPDATE. Reject with the same error code clients
		// see from the pre-check path.
		return BeginChatMessageTurnResult{}, ErrChatSessionArchived
	}

	// 2. Idempotency lookup. The (session_id, idempotency_key)
	// partial unique index means this is at most one row.
	if in.IdempotencyKey != "" {
		var userID string
		var userStatus string
		err = tx.QueryRow(ctx,
			`SELECT id, status FROM chat_messages
			 WHERE session_id = $1::uuid AND idempotency_key = $2`,
			in.SessionID, in.IdempotencyKey).Scan(&userID, &userStatus)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return BeginChatMessageTurnResult{}, fmt.Errorf("idempotency lookup: %w", err)
		}
		if err == nil {
			// Hit — fetch the linked assistant. The DESC ORDER +
			// LIMIT 1 picks the most recent assistant for this
			// user message even if a regenerate has stacked
			// supersession (Phase 3.5+ feature; harmless today).
			row := tx.QueryRow(ctx, fmt.Sprintf(`
				SELECT %s FROM chat_messages
				WHERE parent_message_id = $1::uuid AND role = 'assistant'
				ORDER BY sequence DESC LIMIT 1`, chatMessageCols),
				userID)
			assistant, scanErr := scanChatMessage(row)
			if errors.Is(scanErr, pgx.ErrNoRows) {
				// Pathological: the user row exists but no
				// assistant has been created yet. This can
				// happen if a prior BeginChatMessageTurn
				// crashed between the user-insert and the
				// assistant-insert — but the whole flow is in
				// one tx, so this is impossible under normal
				// operation. Treat as InFlight with no asst-id;
				// the lease sweeper will eventually rectify.
				_ = assistant
				return BeginChatMessageTurnResult{}, fmt.Errorf("idempotency match has no assistant row (user_msg=%s)", userID)
			}
			if scanErr != nil {
				return BeginChatMessageTurnResult{}, fmt.Errorf("load assistant for replay: %w", scanErr)
			}
			outcome := ChatTurnInFlight
			if IsChatMessageTerminal(assistant.Status) {
				outcome = ChatTurnReplay
			}
			return BeginChatMessageTurnResult{
				Outcome:          outcome,
				AssistantMessage: assistant,
			}, nil
		}
	}

	// 3. Hard cap check. One turn = 2 rows.
	if messageCount+2 > model.ChatMessageHardCap {
		return BeginChatMessageTurnResult{}, ErrChatSessionMsgCapReached
	}

	// 4. Lock check. The status column is the source of truth;
	// active_message_id and locked_at are the lease metadata.
	if status != model.ChatSessionStatusIdle {
		return BeginChatMessageTurnResult{}, ErrChatSessionLocked
	}

	// Sequence allocation: 1-indexed within the session, so the
	// first turn writes (1, 2). message_count is 0-based and
	// reflects the rows already in this session.
	userSeq := messageCount + 1
	assistantSeq := messageCount + 2

	// Compute the per-turn scope_hub_ids_used stamp. This is
	// what Phase 3.4's read-time redaction pivots on — the
	// snapshot of "which hubs the agent could see for THIS
	// turn." Live-membership intersection happens HERE so a
	// caller who's lost access to a hub since session create
	// stops contributing data from that hub onward.
	//
	// user_all_hubs sessions snapshot the caller's live hub
	// set wholesale (the resolver behavior); hub_subset and
	// single_hub intersect against the session's frozen
	// scope_hub_ids. Empty intersection is rejected because
	// "this caller can't see anything in this session" should
	// fail loud, not silently produce a turn with no scope.
	scopeHubs := computeChatTurnScope(scopeType, sessionScopeHubs, in.CallerAccessibleHubs)
	if len(scopeHubs) == 0 {
		return BeginChatMessageTurnResult{}, ErrChatSessionNoAccessibleHubs
	}

	// 5. Insert user row.
	userMsg := &model.ChatMessage{
		ID:                 in.UserMessageID,
		SessionID:          in.SessionID,
		Sequence:           userSeq,
		Role:               model.ChatMessageRoleUser,
		Status:             model.ChatMessageStatusCompleted,
		ScopeHubIDsUsed:    scopeHubs,
		Content:            in.UserContent,
		IdempotencyKey:     in.IdempotencyKey,
		ToolsetVersionUsed: toolsetVersion,
		CreatedAt:          in.Now,
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO chat_messages (
			id, session_id, sequence, role, author_user_id, status, scope_hub_ids_used,
			content, tool_calls, citations, usage, error,
			idempotency_key, parent_message_id,
			toolset_version_used, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7::uuid[],
			$8, '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, NULL,
			$9, NULL,
			$10, $11)`,
		userMsg.ID, userMsg.SessionID, userMsg.Sequence, userMsg.Role,
		in.OwnerID, // author_user_id = owner for v1 (single-user sessions)
		userMsg.Status, scopeHubs, userMsg.Content,
		nullableString(userMsg.IdempotencyKey),
		userMsg.ToolsetVersionUsed, userMsg.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
			// Race-edge: a concurrent caller passed step 2's
			// "no hit" check but committed first. The unique
			// index on (session_id, idempotency_key) catches it.
			return BeginChatMessageTurnResult{}, ErrChatMessageIdempotencyConflict
		}
		return BeginChatMessageTurnResult{}, fmt.Errorf("insert user message: %w", err)
	}

	// 6. Insert assistant placeholder. parent_message_id points
	// at the user row so ListChatMessagesBySession's "exclude
	// superseded" filter can later supersede this assistant on
	// regenerate (a fresh assistant whose parent is THIS row).
	assistantMsg := &model.ChatMessage{
		ID:                 in.AssistantMessageID,
		SessionID:          in.SessionID,
		Sequence:           assistantSeq,
		Role:               model.ChatMessageRoleAssistant,
		Status:             model.ChatMessageStatusQueued,
		ScopeHubIDsUsed:    scopeHubs,
		ParentMessageID:    in.UserMessageID,
		ToolsetVersionUsed: toolsetVersion,
		CreatedAt:          in.Now,
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO chat_messages (
			id, session_id, sequence, role, author_user_id, status, scope_hub_ids_used,
			content, tool_calls, citations, usage, error,
			idempotency_key, parent_message_id,
			toolset_version_used, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7::uuid[],
			'', '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, NULL,
			NULL, $8::uuid,
			$9, $10)`,
		assistantMsg.ID, assistantMsg.SessionID, assistantMsg.Sequence,
		assistantMsg.Role, in.OwnerID, // author_user_id same as owner — v1 single-user sessions
		assistantMsg.Status, scopeHubs,
		assistantMsg.ParentMessageID,
		assistantMsg.ToolsetVersionUsed, assistantMsg.CreatedAt,
	)
	if err != nil {
		return BeginChatMessageTurnResult{}, fmt.Errorf("insert assistant placeholder: %w", err)
	}

	// 7. Flip session lock + counters atomically. If this is the
	// first user message in an untitled session, derive a short
	// deterministic title from the prompt in the same transaction
	// so sidebar rows never stay "Untitled chat" forever.
	autoTitle := chatAutoTitleFromPrompt(in.UserContent)
	_, err = tx.Exec(ctx,
		`UPDATE chat_sessions SET
			status = 'running',
			active_message_id = $1::uuid,
			locked_at = $2,
			message_count = message_count + 2,
			last_message_at = $2,
			title = CASE
				WHEN btrim(COALESCE(title, '')) = '' THEN $4
				ELSE title
			END,
			updated_at = $2
		 WHERE id = $3::uuid`,
		assistantMsg.ID, in.Now, in.SessionID, autoTitle)
	if err != nil {
		return BeginChatMessageTurnResult{}, fmt.Errorf("flip session lock: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return BeginChatMessageTurnResult{}, fmt.Errorf("commit: %w", err)
	}

	return BeginChatMessageTurnResult{
		Outcome:          ChatTurnFresh,
		UserMessage:      userMsg,
		AssistantMessage: assistantMsg,
	}, nil
}

const chatAutoTitleMaxRunes = 64

func chatAutoTitleFromPrompt(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) <= chatAutoTitleMaxRunes {
		return title
	}
	cut := string(runes[:chatAutoTitleMaxRunes])
	if idx := strings.LastIndex(cut, " "); idx >= 24 {
		cut = cut[:idx]
	}
	cut = strings.TrimRight(cut, " \t\r\n.,;:!?")
	if cut == "" {
		cut = string(runes[:chatAutoTitleMaxRunes])
	}
	return cut + "..."
}

// computeChatTurnScope returns the per-turn scope_hub_ids_used
// snapshot. Plan 24 §"Lifecycle policies" — "transcript reads
// after hub membership changes" — pins this stamp as the
// per-turn anchor for read-time redaction. Three cases:
//
//   - user_all_hubs: snapshot the caller's live set wholesale.
//     The resolver expands this to "all hubs the user has now",
//     so the per-turn freeze MUST be the same set.
//   - hub_subset / single_hub: intersect the session's frozen
//     scope_hub_ids with the caller's live accessible set. A
//     hub the user has lost access to drops out — the agent
//     literally couldn't read from it on this turn, so its
//     id has no place on the message row.
//   - default (defensive): treat as the intersection case.
//
// Returns nil when nothing's in scope; the caller decides how
// to surface that (BeginChatMessageTurn → ErrChatSessionNoAccessibleHubs).
func computeChatTurnScope(scopeType string, sessionScopeHubs, callerAccessibleHubs []string) []string {
	if scopeType == model.ChatScopeTypeUserAllHubs {
		// Take the caller's live set; canonicalize to a
		// non-nil empty slice so downstream JSONB serialization
		// produces `[]` not `null`.
		out := make([]string, 0, len(callerAccessibleHubs))
		seen := map[string]struct{}{}
		for _, h := range callerAccessibleHubs {
			if h == "" {
				continue
			}
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = struct{}{}
			out = append(out, h)
		}
		return out
	}
	// Intersection. O(n*m) — both sides bounded to a few dozen
	// in practice, so the simple loop is fine.
	accessible := make(map[string]struct{}, len(callerAccessibleHubs))
	for _, h := range callerAccessibleHubs {
		if h == "" {
			continue
		}
		accessible[h] = struct{}{}
	}
	out := make([]string, 0, len(sessionScopeHubs))
	for _, h := range sessionScopeHubs {
		if _, ok := accessible[h]; ok {
			out = append(out, h)
		}
	}
	return out
}

// IsChatMessageTerminal reports whether a chat_messages.status is
// a terminal lifecycle state. Used to distinguish Replay (terminal)
// from InFlight (queued/running/canceling) on idempotency hits.
func IsChatMessageTerminal(status string) bool {
	switch status {
	case model.ChatMessageStatusCompleted,
		model.ChatMessageStatusPartialFailed,
		model.ChatMessageStatusFailed,
		model.ChatMessageStatusCanceled:
		return true
	}
	return false
}

// GetChatMessage — owner-scoped via session join.
func (s *PostgresStore) GetChatMessage(ctx context.Context, messageID, ownerID string) (*model.ChatMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	row := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM chat_messages m
		WHERE m.id = $1::uuid
		  AND EXISTS (SELECT 1 FROM chat_sessions s WHERE s.id = m.session_id AND s.owner_id = $2::uuid)`,
		prefixCols(chatMessageCols, "m")),
		messageID, ownerID)
	msg, err := scanChatMessage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatMessageNotFound
	}
	return msg, err
}

// GetChatMessageByIdempotencyKey — returns nil, nil when the key
// hasn't been used in this session.
func (s *PostgresStore) GetChatMessageByIdempotencyKey(ctx context.Context, sessionID, key string) (*model.ChatMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == "" {
		return nil, nil
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+chatMessageCols+` FROM chat_messages
		 WHERE session_id = $1::uuid AND idempotency_key = $2`,
		sessionID, key)
	msg, err := scanChatMessage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return msg, err
}

// ListChatMessagesBySession — transcript reads. Sequence-based
// keyset pagination; the cursor for this query is the last
// sequence the caller saw.
func (s *PostgresStore) ListChatMessagesBySession(ctx context.Context, opts ChatMessageListOpts) ([]model.ChatMessage, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Owner-scope check upfront — saves a join on every row of
	// the main read by validating session ownership once.
	var ownerCheck int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM chat_sessions WHERE id = $1::uuid AND owner_id = $2::uuid`,
		opts.SessionID, opts.OwnerID).Scan(&ownerCheck)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrChatSessionNotFound
	}
	if err != nil {
		return nil, "", err
	}

	where := []string{"session_id = $1::uuid", "sequence > $2"}
	args := []any{opts.SessionID, opts.AfterSequence}
	if !opts.IncludeSuperseded {
		// Exclude rows replaced by a same-role regenerate. The
		// SAME-ROLE qualifier matters: an assistant.parent points
		// at the user it's replying to (normal flow, NOT a
		// supersession), so we'd hide every user row if we just
		// filtered "row pointed to by another row". A regenerate
		// produces a NEW assistant whose parent_message_id is
		// the PRIOR assistant's id — both assistants share role,
		// and the older one is the one we want to hide.
		where = append(where,
			`NOT EXISTS (SELECT 1 FROM chat_messages c WHERE c.parent_message_id = chat_messages.id AND c.role = chat_messages.role)`)
	}
	args = append(args, limit+1)

	q := fmt.Sprintf(`SELECT %s FROM chat_messages WHERE %s ORDER BY sequence ASC LIMIT $%d`,
		chatMessageCols, strings.Join(where, " AND "), len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]model.ChatMessage, 0, limit)
	for rows.Next() {
		m, err := scanChatMessage(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	var nextCursor string
	if len(out) > limit {
		last := out[limit-1]
		out = out[:limit]
		nextCursor = fmt.Sprintf("%d", last.Sequence)
	}
	return out, nextCursor, nil
}

// UpdateChatMessageStatus — owner-scoped.
func (s *PostgresStore) UpdateChatMessageStatus(ctx context.Context, messageID, ownerID, status string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE chat_messages m SET status = $3
		FROM chat_sessions s
		WHERE m.id = $1::uuid
		  AND m.session_id = s.id
		  AND s.owner_id = $2::uuid`,
		messageID, ownerID, status)
	if err != nil {
		return fmt.Errorf("update chat message status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// FinalizeChatMessage — terminal state in one atomic op.
func (s *PostgresStore) FinalizeChatMessage(ctx context.Context, messageID, ownerID string, final ChatMessageFinalize) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Owner-validated UPDATE with a RETURNING session_id so we
	// can reset the parent session in the same tx without a
	// separate ownership check.
	//
	// **CAS gate on m.status NOT IN (terminal set).** Codex's
	// Phase 3.4a review caught a race: if a sweeper or cancel
	// path finalizes the same assistant between the worker's
	// load and its finalize, the worker's finalize would have
	// silently overwritten the real terminal state. The status
	// gate makes the update no-op when the row is already
	// terminal — first-finalize-wins, deterministic. The
	// caller distinguishes "already-finalized" via
	// ErrChatMessageAlreadyFinalized so it can log+skip rather
	// than treating it as a row-not-found.
	// CAS gate logic:
	//   1. Reject every terminal status — first finalize wins,
	//      idempotent re-finalize returns ErrChatMessageAlreadyFinalized.
	//   2. Protect 'canceling': only a 'canceled' target may
	//      overwrite it. Any other target (failed / completed /
	//      partial_failed) is a no-op so a worker bottoming out via
	//      failMessage or the deferred guard can't stomp the user's
	//      cancel intent. Codex's Phase 3.7b review caught the gap.
	//      Callers that hit this path receive ErrChatMessageCancelPending
	//      and must retry with status='canceled'.
	var sessionID string
	row := tx.QueryRow(ctx, `
		UPDATE chat_messages m SET status = $3, content = $4,
			tool_calls = COALESCE($5::jsonb, '[]'::jsonb),
			citations = COALESCE($6::jsonb, '[]'::jsonb),
			usage = COALESCE($7::jsonb, '{}'::jsonb),
			error = $8::jsonb,
			finished_at = $9
		FROM chat_sessions s
		WHERE m.id = $1::uuid AND m.session_id = s.id AND s.owner_id = $2::uuid
		  AND m.status NOT IN ('completed', 'partial_failed', 'failed', 'canceled')
		  AND (m.status <> 'canceling' OR $3 = 'canceled')
		RETURNING m.session_id`,
		messageID, ownerID, final.Status, final.Content,
		final.ToolCalls, final.Citations, final.Usage, final.Error, final.FinishedAt,
	)
	if err := row.Scan(&sessionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Disambiguate three cases:
			//   - row doesn't exist (or not owned) → ErrChatMessageNotFound
			//   - row is already terminal → ErrChatMessageAlreadyFinalized
			//   - row is canceling AND target wasn't 'canceled' → ErrChatMessageCancelPending
			// Use tx.QueryRow (not the pool) so we read on the
			// same connection and don't open a second one while
			// our tx is still held — codex flagged the v1 shape
			// as a small connection-pool footgun under load.
			var status string
			err2 := tx.QueryRow(ctx, `
				SELECT m.status FROM chat_messages m
				JOIN chat_sessions s ON s.id = m.session_id
				WHERE m.id = $1::uuid AND s.owner_id = $2::uuid`,
				messageID, ownerID).Scan(&status)
			if errors.Is(err2, pgx.ErrNoRows) {
				return ErrChatMessageNotFound
			}
			if err2 != nil {
				return fmt.Errorf("disambiguate finalize miss: %w", err2)
			}
			if status == model.ChatMessageStatusCanceling && final.Status != model.ChatMessageStatusCanceled {
				return ErrChatMessageCancelPending
			}
			return ErrChatMessageAlreadyFinalized
		}
		return fmt.Errorf("finalize chat message: %w", err)
	}

	// Reset session lock state. The session may already be in
	// 'idle' if a regenerate fired without the lock primitive;
	// the WHERE clause is permissive — finalizing a message
	// always returns the session to idle regardless of prior
	// state. Lease sweeper relies on this resilience.
	_, err = tx.Exec(ctx, `
		UPDATE chat_sessions SET
			status = 'idle', active_message_id = NULL, locked_at = NULL,
			updated_at = now()
		WHERE id = $1::uuid`, sessionID)
	if err != nil {
		return fmt.Errorf("reset session lock: %w", err)
	}

	// Append the terminal row to chat_message_events INSIDE the
	// same tx. Codex's Phase 3.4b2.b v2 review caught a real
	// race — writing the terminal AFTER FinalizeChatMessage in
	// the worker meant a stream client polling between the two
	// commits would see status=terminal AND no events, and exit
	// without delivering the terminal frame. Worse, the
	// preflight `failMessage` and deferred-cleanup paths
	// finalized without ever writing a terminal row, so SSE
	// clients on those paths got an empty 200 with no
	// `message.failed` frame.
	//
	// Putting the terminal write here makes "the message reached
	// terminal status" and "the terminal SSE row exists" the
	// same atomic event. Every finalization path — success,
	// budget-exceeded, agent-error, preflight-failure, deferred-
	// cleanup — gets the terminal row for free.
	//
	// next_seq is computed from MAX(seq) + 1 for this message's
	// events. Safe vs the observer: per-event writes commit
	// synchronously inside the run goroutine, so by the time
	// finalize lands, all observer writes are durable. There's
	// no concurrent observer + finalize.
	terminalSeq, err := chatNextEventSeq(ctx, tx, messageID)
	if err != nil {
		return fmt.Errorf("compute terminal event seq: %w", err)
	}
	terminalPayload, err := buildChatTerminalPayload(messageID, final)
	if err != nil {
		return fmt.Errorf("build terminal payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO chat_message_events (message_id, seq, event_type, payload, created_at)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5)`,
		messageID, terminalSeq, chatTerminalWireEvent(final.Status), terminalPayload, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert terminal event: %w", err)
	}

	return tx.Commit(ctx)
}

// CancelChatMessage marks an in-flight assistant turn for soft
// cancellation. See Store.CancelChatMessage for the contract.
//
// The transaction:
//
//  1. SELECT FOR UPDATE the message + parent session, validate
//     ownership and role.
//  2. If status is already terminal (or already canceling), return
//     a no-op outcome with CancelRegistered=false + the actual
//     current status. The handler maps this to 200 — re-cancel
//     of a finished message is a UX no-op.
//  3. Otherwise UPDATE chat_messages.status='canceling' and the
//     session's status='canceling' (when its active_message_id
//     points at this row — defense against a stale handler call
//     after the session has moved on).
//  4. Append a `cancel.requested` row to chat_message_events so
//     live SSE clients see the cancel intent within the next
//     250ms poll cycle (or sooner via signaler wake).
//
// We deliberately do NOT directly finalize even when the message
// is in 'queued' state without an active worker — the worker
// (when it eventually picks up the river job) sees status =
// 'canceling' on entry and finalizes via FinalizeChatMessage,
// keeping the terminal-write path single-sourced. The lease
// sweeper covers the rare case where no worker ever picks it up.
func (s *PostgresStore) CancelChatMessage(ctx context.Context, messageID, ownerID string) (ChatMessageCancelOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Lock the message + read its session id and current
	// status. Owner-scoped via the session join.
	var status, role, sessionID string
	err = tx.QueryRow(ctx, `
		SELECT m.status, m.role, m.session_id
		  FROM chat_messages m
		  JOIN chat_sessions s ON s.id = m.session_id
		 WHERE m.id = $1::uuid AND s.owner_id = $2::uuid
		   FOR UPDATE OF m`,
		messageID, ownerID).Scan(&status, &role, &sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatMessageCancelOutcome{}, ErrChatMessageNotFound
	}
	if err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("lock message for cancel: %w", err)
	}

	// 2. Cancel only applies to assistant turns. A user message
	// is durable from the moment it commits; "cancel my message"
	// is a different operation (delete, which doesn't exist for
	// chat_messages today). Reject with NotCancelable so the
	// handler surfaces it distinctly from already-terminal.
	if role != model.ChatMessageRoleAssistant {
		return ChatMessageCancelOutcome{
			AssistantMessageID: messageID,
			SessionID:          sessionID,
			Status:             status,
			CancelRegistered:   false,
		}, ErrChatMessageNotCancelable
	}

	// 3. Already-terminal short-circuit. Returns the actual
	// current status so the handler can shape its 200 response
	// faithfully ("you tried to cancel; here's what the message
	// actually settled at").
	if IsChatMessageTerminal(status) {
		// Roll back the no-op tx (our defer runs the rollback
		// regardless of the explicit Commit path).
		return ChatMessageCancelOutcome{
			AssistantMessageID: messageID,
			SessionID:          sessionID,
			Status:             status,
			CancelRegistered:   false,
		}, nil
	}

	// 4. Already canceling — same no-op shape. Multiple cancel
	// requests against the same in-flight turn collapse to one
	// canceling state; the worker finalizes once.
	if status == model.ChatMessageStatusCanceling {
		return ChatMessageCancelOutcome{
			AssistantMessageID: messageID,
			SessionID:          sessionID,
			Status:             status,
			CancelRegistered:   false,
		}, nil
	}

	// 5. Move the message to 'canceling'. The CAS predicate
	// (status NOT IN terminal) defends against a finalize race
	// landing between our SELECT FOR UPDATE and this UPDATE —
	// FOR UPDATE prevents a concurrent UPDATE from racing, but
	// the gate keeps the call idempotent under any future
	// retry-during-finalize scenario.
	tag, err := tx.Exec(ctx, `
		UPDATE chat_messages
		   SET status = $1
		 WHERE id = $2::uuid
		   AND status NOT IN ('completed', 'partial_failed', 'failed', 'canceled', 'canceling')`,
		model.ChatMessageStatusCanceling, messageID)
	if err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("mark message canceling: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Lost the race to a concurrent finalize. Re-read the
		// terminal status and surface it idempotently.
		var nowStatus string
		_ = tx.QueryRow(ctx,
			`SELECT status FROM chat_messages WHERE id = $1::uuid`, messageID).Scan(&nowStatus)
		return ChatMessageCancelOutcome{
			AssistantMessageID: messageID,
			SessionID:          sessionID,
			Status:             nowStatus,
			CancelRegistered:   false,
		}, nil
	}

	// 6. Move the session to 'canceling' — but only if it's
	// still pointing at this assistant. A session whose
	// active_message_id has moved on (race with another finalize
	// + a regenerate that flipped to running) shouldn't be
	// re-canceled. Status='running' is the lock-state we expect
	// to find; the WHERE filter makes the UPDATE a no-op if not.
	if _, err = tx.Exec(ctx, `
		UPDATE chat_sessions
		   SET status = 'canceling', updated_at = now()
		 WHERE id = $1::uuid
		   AND active_message_id = $2::uuid
		   AND status = 'running'`,
		sessionID, messageID); err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("flip session to canceling: %w", err)
	}

	// 7. Append a non-terminal `cancel.requested` event so live
	// SSE clients see the cancel intent immediately. The
	// terminal `message.canceled` event lands when the worker
	// (or sweeper) calls FinalizeChatMessage.
	seq, err := chatNextEventSeq(ctx, tx, messageID)
	if err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("compute cancel event seq: %w", err)
	}
	payload, err := buildChatCancelRequestedPayload(messageID)
	if err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("build cancel.requested payload: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO chat_message_events (message_id, seq, event_type, payload, created_at)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5)`,
		messageID, seq, "cancel.requested", payload, time.Now().UTC()); err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("insert cancel.requested event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ChatMessageCancelOutcome{}, fmt.Errorf("commit cancel: %w", err)
	}
	return ChatMessageCancelOutcome{
		AssistantMessageID: messageID,
		SessionID:          sessionID,
		Status:             model.ChatMessageStatusCanceling,
		CancelRegistered:   true,
	}, nil
}

// buildChatCancelRequestedPayload — minimal JSON for the
// `cancel.requested` event. Mirrors the terminal-event payload
// builder pattern but carries no error/usage shape since cancel
// is a non-terminal state-change signal.
func buildChatCancelRequestedPayload(messageID string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"msg_id": messageID,
	})
}

// BeginChatRegenerate — regenerate the assistant reply for a
// prior assistant message. See Store.BeginChatRegenerate for the
// contract.
func (s *PostgresStore) BeginChatRegenerate(ctx context.Context, in BeginChatRegenerateInput) (BeginChatRegenerateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BeginChatRegenerateResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Lock the prior assistant + load the session columns we
	// need. Owner-scoped via the session join. We pull session
	// metadata under FOR UPDATE so a concurrent PATCH
	// (archived/tools) serializes against this read.
	var (
		priorStatus      string
		priorRole        string
		priorParentMsgID *string
		sessionID        string
		sessionStatus    string
		sessionScopeType string
		sessionScopeHubs []string
		sessionToolsetV  int
		messageCount     int
		archivedAt       *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT m.status, m.role, m.parent_message_id::text,
		       s.id::text, s.status, s.scope_type, s.scope_hub_ids,
		       s.toolset_version, s.message_count, s.archived_at
		  FROM chat_messages m
		  JOIN chat_sessions s ON s.id = m.session_id
		 WHERE m.id = $1::uuid AND s.owner_id = $2::uuid AND s.deleted_at IS NULL
		   FOR UPDATE OF m, s`,
		in.PriorAssistantMessageID, in.OwnerID).
		Scan(&priorStatus, &priorRole, &priorParentMsgID,
			&sessionID, &sessionStatus, &sessionScopeType, &sessionScopeHubs,
			&sessionToolsetV, &messageCount, &archivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BeginChatRegenerateResult{}, ErrChatMessageNotFound
	}
	if err != nil {
		return BeginChatRegenerateResult{}, fmt.Errorf("lock prior assistant: %w", err)
	}

	// 2. Validate role first — non-assistant rows can't be
	// regenerated regardless of session state. This check is
	// independent of in-flight semantics so it runs before the
	// session-state gate.
	if priorRole != model.ChatMessageRoleAssistant {
		return BeginChatRegenerateResult{}, ErrChatRegenerateNotEligible
	}

	// 3. Session-state checks BEFORE per-message status. Codex's
	// Phase 3.7b v1 review caught the priority bug: the prior code
	// returned ErrChatRegenerateNotEligible for queued/running/
	// canceling/awaiting_approval messages, which surfaces to the
	// handler as 409 regenerate_not_eligible. The intended
	// behavior is 409 chat_session_locked because a session is
	// busy with another turn, not "this message can never be
	// regenerated." Mapping correctly matters for the UI: locked
	// sessions get a "wait for the in-flight turn or cancel it"
	// affordance and an active_message_id pointer, while
	// not_eligible gets a "send again" CTA. Conflating them
	// would wedge the user into the wrong recovery path.
	if archivedAt != nil {
		return BeginChatRegenerateResult{}, ErrChatSessionArchived
	}
	if sessionStatus != model.ChatSessionStatusIdle {
		// 'running', 'canceling', and any future non-idle session
		// state map to chat_session_locked. The handler's locked
		// branch surfaces active_message_id + resume_url so the
		// client can connect to the in-flight stream rather than
		// retrying the regenerate blindly.
		return BeginChatRegenerateResult{}, ErrChatSessionLocked
	}

	// 4. Per-message status. By the time we reach here the session
	// is idle, so the prior assistant CAN'T be queued/running/
	// canceling — those states require a non-idle session. The
	// remaining non-eligible cases are 'canceled' (user already
	// chose to stop) and 'awaiting_approval' (theoretically idle
	// session + paused message; treated as non-eligible because
	// resuming the original via Decide is the right path, not
	// regenerate).
	switch priorStatus {
	case model.ChatMessageStatusCompleted,
		model.ChatMessageStatusPartialFailed,
		model.ChatMessageStatusFailed:
		// eligible — fall through
	default:
		return BeginChatRegenerateResult{}, ErrChatRegenerateNotEligible
	}

	// 4. Hard-cap check. A regenerate adds 1 row (just the new
	// assistant — the user message it replies to is already
	// counted in message_count from the original turn).
	if messageCount+1 > model.ChatMessageHardCap {
		return BeginChatRegenerateResult{}, ErrChatSessionMsgCapReached
	}

	// 5. Re-stamp the per-turn scope_hub_ids_used. MUST NOT
	// inherit the prior assistant's snapshot — a hub the caller
	// has lost access to since the original turn must be
	// redacted from the new turn's reads. Same shape
	// BeginChatMessageTurn uses.
	scopeHubs := computeChatTurnScope(sessionScopeType, sessionScopeHubs, in.CallerAccessibleHubs)
	if len(scopeHubs) == 0 {
		return BeginChatRegenerateResult{}, ErrChatSessionNoAccessibleHubs
	}

	// 6. Insert the new assistant. parent_message_id points at
	// the PRIOR ASSISTANT (not the user message) — this is the
	// supersession link the list query's
	//
	//    NOT EXISTS (SELECT 1 FROM chat_messages c
	//                WHERE c.parent_message_id = chat_messages.id
	//                  AND c.role = chat_messages.role)
	//
	// filter uses to hide the older assistant from conversation
	// views while keeping it for audit. The user message it
	// originally replied to is found by walking the prior
	// assistant's parent — preserved for replay/audit lookups.
	newAssistantSeq := messageCount + 1
	newAssistant := &model.ChatMessage{
		ID:                 in.NewAssistantMessageID,
		SessionID:          sessionID,
		Sequence:           newAssistantSeq,
		Role:               model.ChatMessageRoleAssistant,
		Status:             model.ChatMessageStatusQueued,
		ScopeHubIDsUsed:    scopeHubs,
		ParentMessageID:    in.PriorAssistantMessageID,
		ToolsetVersionUsed: sessionToolsetV,
		CreatedAt:          in.Now,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO chat_messages (
			id, session_id, sequence, role, author_user_id, status, scope_hub_ids_used,
			content, tool_calls, citations, usage, error,
			idempotency_key, parent_message_id,
			toolset_version_used, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7::uuid[],
			'', '[]'::jsonb, '[]'::jsonb, '{}'::jsonb, NULL,
			NULL, $8::uuid,
			$9, $10)`,
		newAssistant.ID, newAssistant.SessionID, newAssistant.Sequence,
		newAssistant.Role, in.OwnerID, // author_user_id same as owner — v1 single-user sessions
		newAssistant.Status, scopeHubs,
		newAssistant.ParentMessageID,
		newAssistant.ToolsetVersionUsed, newAssistant.CreatedAt,
	)
	if err != nil {
		return BeginChatRegenerateResult{}, fmt.Errorf("insert regenerate assistant: %w", err)
	}

	// 7. Flip session lock + counter. message_count increments
	// by 1 (the new assistant). last_message_at also advances.
	if _, err = tx.Exec(ctx, `
		UPDATE chat_sessions SET
			status            = 'running',
			active_message_id = $1::uuid,
			locked_at         = $2,
			message_count     = message_count + 1,
			last_message_at   = $2,
			updated_at        = $2
		 WHERE id = $3::uuid`,
		newAssistant.ID, in.Now, sessionID); err != nil {
		return BeginChatRegenerateResult{}, fmt.Errorf("flip session lock for regenerate: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return BeginChatRegenerateResult{}, fmt.Errorf("commit regenerate: %w", err)
	}
	return BeginChatRegenerateResult{
		NewAssistantMessage:  newAssistant,
		SessionID:            sessionID,
		PriorAssistantID:     in.PriorAssistantMessageID,
		PriorAssistantStatus: priorStatus,
	}, nil
}

// chatNextEventSeq returns the next monotonic seq for a message's
// chat_message_events. COALESCE handles the no-events case (the
// MAX over zero rows returns NULL); seq starts at 1 for a message
// that finalized without writing any per-event rows (preflight
// reject or empty agent run).
func chatNextEventSeq(ctx context.Context, tx pgx.Tx, messageID string) (int, error) {
	var next int
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM chat_message_events WHERE message_id = $1::uuid`,
		messageID).Scan(&next)
	return next, err
}

// chatTerminalWireEvent maps a terminal ChatMessageStatus to the
// public SSE wire event name. Stored on the row as event_type so
// the SSE handler passes it through verbatim.
func chatTerminalWireEvent(status string) string {
	switch status {
	case model.ChatMessageStatusPartialFailed:
		return "message.partial_failed"
	case model.ChatMessageStatusFailed:
		return "message.failed"
	case model.ChatMessageStatusCanceled:
		return "message.canceled"
	default:
		return "message.completed"
	}
}

// buildChatTerminalPayload constructs the JSON body the SSE
// handler emits for the terminal frame. Carries the canonical
// content + tool_calls + usage from FinalizeChatMessage's
// payload, NOT a snapshot of the message row — at the moment we
// build this payload, the row UPDATE hasn't been committed yet
// from the perspective of any reader, so we use the values the
// caller is about to write.
func buildChatTerminalPayload(messageID string, final ChatMessageFinalize) ([]byte, error) {
	out := map[string]any{
		"message_id": messageID,
		"status":     final.Status,
		"content":    final.Content,
	}
	if len(final.ToolCalls) > 0 {
		out["tool_calls"] = json.RawMessage(final.ToolCalls)
	}
	if len(final.Usage) > 0 {
		out["usage"] = json.RawMessage(final.Usage)
	}
	if len(final.Error) > 0 {
		out["error"] = json.RawMessage(final.Error)
	}
	return json.Marshal(out)
}

// AppendChatMessageEvent — single SSE replay row, owner-scoped
// via INSERT...SELECT against chat_messages → chat_sessions.
// Rejects with ErrChatMessageNotFound when the message doesn't
// belong to ownerID; returns ErrChatMessageEventSeqConflict on
// the (message_id, seq) UNIQUE collision (allocated 0 rows OR
// PgError 23505 — both signal "you tried to write a non-owned
// or duplicate event").
func (s *PostgresStore) AppendChatMessageEvent(ctx context.Context, ownerID string, ev *model.ChatMessageEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO chat_message_events (message_id, seq, event_type, payload, created_at)
		SELECT $1::uuid, $2, $3, $4::jsonb, $5
		WHERE EXISTS (
			SELECT 1 FROM chat_messages m
			JOIN chat_sessions s ON s.id = m.session_id
			WHERE m.id = $1::uuid AND s.owner_id = $6::uuid
		)`,
		ev.MessageID, ev.Seq, ev.EventType, ev.Payload, ev.CreatedAt, ownerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrChatMessageEventSeqConflict
		}
		return fmt.Errorf("append chat message event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the message doesn't exist, or it doesn't
		// belong to this owner. Either way the caller should
		// not have been allowed to write here.
		return ErrChatMessageNotFound
	}
	return nil
}

// ListChatMessageEvents — replay reads.
func (s *PostgresStore) ListChatMessageEvents(ctx context.Context, messageID, ownerID string, sinceSeq int) ([]model.ChatMessageEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.message_id::text, e.seq, e.event_type, e.payload, e.created_at
		FROM chat_message_events e
		JOIN chat_messages m ON m.id = e.message_id
		JOIN chat_sessions s ON s.id = m.session_id
		WHERE e.message_id = $1::uuid AND s.owner_id = $2::uuid AND e.seq > $3
		ORDER BY e.seq ASC`,
		messageID, ownerID, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("list chat message events: %w", err)
	}
	defer rows.Close()
	var out []model.ChatMessageEvent
	for rows.Next() {
		var ev model.ChatMessageEvent
		if err := rows.Scan(&ev.ID, &ev.MessageID, &ev.Seq, &ev.EventType, &ev.Payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ListExpiredChatLeases returns sessions whose lease has expired.
// One row per stuck session; the active_message_id is the
// orphaned assistant the lease sweeper finalizes as
// failed{server_crashed}.
//
// `lockedBefore` is the absolute cutoff: sessions with
// `locked_at < lockedBefore` AND a non-idle status are returned.
// Phase 3.5a wires this with `now() - 5min`. The query is
// owner-agnostic — sweeper is a system-level worker and
// authorizes through the chat_sessions.owner_id column when it
// calls FinalizeChatMessage.
//
// Status filter excludes terminal states (idle for completed
// turns; deleted_at IS NULL guards against soft-deleted
// sessions whose row is just a tombstone). The seek into
// idx_chat_sessions_lease (locked_at) makes this O(stuck) not
// O(all sessions).
func (s *PostgresStore) ListExpiredChatLeases(ctx context.Context, lockedBefore time.Time, limit int) ([]ExpiredChatLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, owner_id::text, active_message_id::text, locked_at, status
		FROM chat_sessions
		WHERE status IN ('running', 'canceling')
		  AND locked_at IS NOT NULL
		  AND locked_at < $1
		  AND active_message_id IS NOT NULL
		  AND deleted_at IS NULL
		ORDER BY locked_at ASC
		LIMIT $2`, lockedBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired chat leases: %w", err)
	}
	defer rows.Close()
	var out []ExpiredChatLease
	for rows.Next() {
		var lease ExpiredChatLease
		if err := rows.Scan(&lease.SessionID, &lease.OwnerID, &lease.ActiveMessageID, &lease.LockedAt, &lease.Status); err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

// DeleteChatMessageEventsOlderThan — 24h sweeper.
func (s *PostgresStore) DeleteChatMessageEventsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM chat_message_events WHERE created_at < $1`,
		cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old chat message events: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CreateChatToolApproval — owner-scoped via INSERT...SELECT.
// Rejects with ErrChatMessageNotFound when the message doesn't
// belong to ownerID. The (message_id, args_hash) idempotency
// the interface comment used to claim is NOT enforced at the
// create step; idempotency is at the DECIDE step where
// status='pending' guards against double-application. The
// args_hash is stored for future use.
func (s *PostgresStore) CreateChatToolApproval(ctx context.Context, ownerID string, ap *model.ChatToolApproval) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ap.ExpiresAt.IsZero() {
		ap.ExpiresAt = ap.RequestedAt.Add(model.ChatToolApprovalTTL)
	}
	if ap.Status == "" {
		ap.Status = model.ChatToolApprovalStatusPending
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO chat_tool_approvals (
			id, message_id, tool_name, args, args_hash, status,
			decided_by, decided_at, requested_at, expires_at, created_at)
		SELECT $1::uuid, $2::uuid, $3, $4::jsonb, $5, $6,
			$7, $8, $9, $10, $11
		WHERE EXISTS (
			SELECT 1 FROM chat_messages m
			JOIN chat_sessions s ON s.id = m.session_id
			WHERE m.id = $2::uuid AND s.owner_id = $12::uuid
		)`,
		ap.ID, ap.MessageID, ap.ToolName, ap.Args, ap.ArgsHash, ap.Status,
		nullableString(ap.DecidedBy), ap.DecidedAt, ap.RequestedAt, ap.ExpiresAt, ap.CreatedAt,
		ownerID,
	)
	if err != nil {
		return fmt.Errorf("create chat tool approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// GetChatToolApproval — owner-scoped via the message → session join.
func (s *PostgresStore) GetChatToolApproval(ctx context.Context, approvalID, ownerID string) (*model.ChatToolApproval, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	row := s.pool.QueryRow(ctx, `
		SELECT a.id::text, a.message_id::text, a.tool_name, a.args, a.args_hash, a.status,
			COALESCE(a.decided_by::text, ''), a.decided_at, a.requested_at, a.expires_at, a.created_at
		FROM chat_tool_approvals a
		JOIN chat_messages m ON m.id = a.message_id
		JOIN chat_sessions s ON s.id = m.session_id
		WHERE a.id = $1::uuid AND s.owner_id = $2::uuid`,
		approvalID, ownerID)
	ap, err := scanChatToolApproval(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatApprovalNotFound
	}
	return ap, err
}

// DecideChatToolApproval — atomic pending → approved/denied.
func (s *PostgresStore) DecideChatToolApproval(ctx context.Context, approvalID, ownerID, sessionID, decision string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if decision != model.ChatToolApprovalStatusApproved && decision != model.ChatToolApprovalStatusDenied {
		return fmt.Errorf("decision must be approved or denied, got %q", decision)
	}
	// Optional session-id filter — codex's Phase 3.6b1 v1
	// review flagged that without it, a same-owner request
	// could POST `/sessions/A/approvals/{approvalFromB}` and
	// successfully decide approvalFromB. The HTTP route's
	// {id} path param now flows here; mismatch reads as
	// ErrChatApprovalNotFound (no existence leak across
	// the user's own sessions).
	sessionPredicate := ""
	args := []any{approvalID, ownerID, decision}
	if sessionID != "" {
		sessionPredicate = " AND m.session_id = $4::uuid"
		args = append(args, sessionID)
	}
	// Expiry predicate — codex's Phase 3.6b2 v2 review flagged
	// that gating only on status='pending' lets a late user
	// click approve a row whose expires_at has already passed
	// (e.g., approver crashed, or the click landed in the
	// vanishingly small window before the local timer fires).
	// With the predicate, the boundary is enforced at the SQL
	// layer and the disambiguation read distinguishes
	// expired-pending from already-decided so the handler can
	// surface chat_approval_timeout (408) instead of a stale
	// 200 approved.
	q := `
		UPDATE chat_tool_approvals a SET status = $3, decided_by = $2::uuid, decided_at = now()
		FROM chat_messages m, chat_sessions s
		WHERE a.id = $1::uuid AND a.message_id = m.id AND m.session_id = s.id
		  AND s.owner_id = $2::uuid
		  AND a.status = 'pending'
		  AND a.expires_at > now()` + sessionPredicate
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("decide chat tool approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either not found, not owned, wrong session, no longer
		// pending, or pending-but-expired. Disambiguate so the
		// handler returns the right HTTP code. The
		// disambiguation reads BOTH status and `expires_at >
		// now()` so an expired-pending row is distinguishable
		// from an already-decided one. Pending+expired surfaces
		// as ErrChatApprovalExpired (handler → 408); any other
		// non-pending status is ErrChatApprovalAlreadyDecided
		// (handler → 409).
		var status string
		var stillPending bool
		dq := `
			SELECT a.status, (a.status = 'pending' AND a.expires_at > now())
			FROM chat_tool_approvals a
			JOIN chat_messages m ON m.id = a.message_id
			JOIN chat_sessions s ON s.id = m.session_id
			WHERE a.id = $1::uuid AND s.owner_id = $2::uuid`
		dargs := []any{approvalID, ownerID}
		if sessionID != "" {
			dq += " AND m.session_id = $3::uuid"
			dargs = append(dargs, sessionID)
		}
		err := s.pool.QueryRow(ctx, dq, dargs...).Scan(&status, &stillPending)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChatApprovalNotFound
		}
		if err != nil {
			return err
		}
		// Row is pending but expired — the SQL UPDATE above
		// rejected it because of the expires_at predicate. The
		// approver's local timer or the periodic sweeper will
		// flip it to timed_out shortly; the user just clicked
		// too late.
		if status == model.ChatToolApprovalStatusPending && !stillPending {
			return ErrChatApprovalExpired
		}
		return ErrChatApprovalAlreadyDecided
	}
	return nil
}

// ExpireChatToolApproval — single-row expiry from the SDK-host
// approver's local deadline timer. Codex's Phase 3.6b2 review
// flagged that the original approver wrote expires_at but didn't
// enforce it: an unanswered 90s approval would only flip when
// the 1m periodic sweeper happened to run, leaving up to 60s of
// "expired-but-still-pending" time during which the agent run's
// 120s ctx could hit deadline first (returning context-deadline-
// exceeded instead of a clean timed_out result).
//
// CAS-guarded on status='pending' so a concurrent user-decide
// that committed first wins — the approver re-fetches and
// returns the user's decision, not a synthetic timed_out. Pairs
// with the existing DecideChatToolApproval CAS: both ends
// transition only from pending, both contend on the same row
// lock, MVCC gives "winner stands" without explicit retry.
//
// Owner-scoped via the chat_messages → chat_sessions join, the
// same shape as Decide. Returns:
//   - nil on success (we won, row is now timed_out)
//   - ErrChatApprovalAlreadyDecided if the row is already
//     non-pending (user beat us to it; caller re-fetches)
//   - ErrChatApprovalNotFound if no row matches owner+id
func (s *PostgresStore) ExpireChatToolApproval(ctx context.Context, approvalID, ownerID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE chat_tool_approvals a SET status = 'timed_out'
		FROM chat_messages m, chat_sessions s
		WHERE a.id = $1::uuid AND a.message_id = m.id AND m.session_id = s.id
		  AND s.owner_id = $2::uuid
		  AND a.status = 'pending'`,
		approvalID, ownerID)
	if err != nil {
		return fmt.Errorf("expire chat tool approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Same disambiguation pattern as Decide: existence-by-
		// owner check separates "not yours" from "already
		// decided" without leaking existence across owners.
		var status string
		err := s.pool.QueryRow(ctx, `
			SELECT a.status FROM chat_tool_approvals a
			JOIN chat_messages m ON m.id = a.message_id
			JOIN chat_sessions s ON s.id = m.session_id
			WHERE a.id = $1::uuid AND s.owner_id = $2::uuid`,
			approvalID, ownerID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChatApprovalNotFound
		}
		if err != nil {
			return err
		}
		return ErrChatApprovalAlreadyDecided
	}
	return nil
}

// ListPendingChatToolApprovals — UI surface for pending decisions.
func (s *PostgresStore) ListPendingChatToolApprovals(ctx context.Context, messageID, ownerID string) ([]model.ChatToolApproval, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id::text, a.message_id::text, a.tool_name, a.args, a.args_hash, a.status,
			COALESCE(a.decided_by::text, ''), a.decided_at, a.requested_at, a.expires_at, a.created_at
		FROM chat_tool_approvals a
		JOIN chat_messages m ON m.id = a.message_id
		JOIN chat_sessions s ON s.id = m.session_id
		WHERE a.message_id = $1::uuid AND s.owner_id = $2::uuid AND a.status = 'pending'
		ORDER BY a.requested_at ASC`,
		messageID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list pending chat tool approvals: %w", err)
	}
	defer rows.Close()
	var out []model.ChatToolApproval
	for rows.Next() {
		ap, err := scanChatToolApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ap)
	}
	return out, rows.Err()
}

// ExpirePendingChatToolApprovals — periodic sweeper.
//
// RETURNING the joined session.owner_id lets the worker publish
// approval.decided{state=timed_out} events per row so the
// SDK-host approver wakes immediately at expiry rather than
// waiting for its own poll cycle to detect the status change.
// Codex's Phase 3.6b1 v1 review pinned this as a real gap.
//
// Concurrency contract — "winner stands". Codex's v2 review
// caught a race in v1's CTE: the original shape used
// `a.id IN (SELECT id ... WHERE status='pending')` to pick
// candidates, then UPDATE'd matching rows by message_id/session
// join. That left the `status='pending'` check on a SNAPSHOT
// taken before the row was locked — between the snapshot and
// the UPDATE acquiring its row lock, a user's decide could
// commit, and the sweeper would still overwrite the row to
// `timed_out`, publishing a spurious decided{state=timed_out}.
//
// The fix moves the `a.status='pending' AND a.expires_at<now()`
// predicates onto the UPDATE itself. PG's MVCC under READ
// COMMITTED re-evaluates the WHERE clause AFTER taking the row
// lock; if a concurrent decide flipped the row, the predicate
// fails on re-check and the UPDATE skips the row. The user's
// decision wins, the sweeper publishes nothing for that
// approval, and the contract holds.
func (s *PostgresStore) ExpirePendingChatToolApprovals(ctx context.Context) ([]ExpiredChatToolApproval, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.pool.Query(ctx, `
		WITH expired AS (
			UPDATE chat_tool_approvals a
			SET status = 'timed_out'
			FROM chat_messages m, chat_sessions s
			WHERE a.message_id = m.id
			  AND m.session_id = s.id
			  AND a.status = 'pending'
			  AND a.expires_at < now()
			RETURNING a.id, s.owner_id
		)
		SELECT id::text, owner_id::text FROM expired`)
	if err != nil {
		return nil, fmt.Errorf("expire chat tool approvals: %w", err)
	}
	defer rows.Close()
	var out []ExpiredChatToolApproval
	for rows.Next() {
		var ea ExpiredChatToolApproval
		if err := rows.Scan(&ea.ApprovalID, &ea.OwnerID); err != nil {
			return nil, err
		}
		out = append(out, ea)
	}
	return out, rows.Err()
}

// scanChatSession reads from a Row or Rows-shaped scanner.
func scanChatSession(r interface{ Scan(...any) error }) (*model.ChatSession, error) {
	var (
		s              model.ChatSession
		writeHubID     *string
		activeMsgID    *string
		toolsJSON      []byte
		hubIDsScanText []string
	)
	if err := r.Scan(
		&s.ID, &s.OwnerID, &s.ScopeType, &hubIDsScanText, &writeHubID,
		&s.Title, &s.Model, &toolsJSON,
		&s.ToolsetVersion, &s.Status, &activeMsgID, &s.LockedAt,
		&s.MessageCount, &s.PinnedAt, &s.ArchivedAt, &s.LastMessageAt,
		&s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
	); err != nil {
		return nil, err
	}
	s.ScopeHubIDs = append([]string(nil), hubIDsScanText...)
	if writeHubID != nil {
		s.WriteHubID = *writeHubID
	}
	if activeMsgID != nil {
		s.ActiveMessageID = *activeMsgID
	}
	if len(toolsJSON) > 0 {
		if err := json.Unmarshal(toolsJSON, &s.Tools); err != nil {
			return nil, fmt.Errorf("decode tools: %w", err)
		}
	}
	if s.Tools == nil {
		s.Tools = []string{}
	}
	return &s, nil
}

// scanChatMessage reads from a Row or Rows.
func scanChatMessage(r interface{ Scan(...any) error }) (*model.ChatMessage, error) {
	var (
		m               model.ChatMessage
		idemKey         *string
		parentMessageID *string
		hubIDsScanText  []string
	)
	if err := r.Scan(
		&m.ID, &m.SessionID, &m.Sequence, &m.Role, &m.AuthorUserID, &m.Status,
		&hubIDsScanText, &m.Content, &m.ToolCalls, &m.Citations, &m.Usage, &m.Error,
		&idemKey, &parentMessageID, &m.ToolsetVersionUsed,
		&m.StartedAt, &m.FinishedAt, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	m.ScopeHubIDsUsed = append([]string(nil), hubIDsScanText...)
	if idemKey != nil {
		m.IdempotencyKey = *idemKey
	}
	if parentMessageID != nil {
		m.ParentMessageID = *parentMessageID
	}
	return &m, nil
}

// scanChatToolApproval reads from a Row or Rows. Decided_by comes
// back as text (COALESCE'd to empty string) from the SELECT lists above.
func scanChatToolApproval(r interface{ Scan(...any) error }) (*model.ChatToolApproval, error) {
	var ap model.ChatToolApproval
	if err := r.Scan(
		&ap.ID, &ap.MessageID, &ap.ToolName, &ap.Args, &ap.ArgsHash, &ap.Status,
		&ap.DecidedBy, &ap.DecidedAt, &ap.RequestedAt, &ap.ExpiresAt, &ap.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &ap, nil
}

// prefixCols rewrites a comma+newline-separated column list to
// prefix every column with `<alias>.`, used for queries that join
// chat_messages onto chat_sessions for owner-scoping.
func prefixCols(cols, alias string) string {
	parts := strings.Split(cols, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(out, ", ")
}

// parseChatCursor / formatChatCursor — opaque keyset cursor for
// the sessions list. RFC3339Nano timestamp + id joined by `|`.
// Empty timestamp ("|<id>") means the cursor's row had a NULL
// last_message_at.
func parseChatCursor(c string) (*time.Time, string, bool) {
	idx := strings.Index(c, "|")
	if idx < 0 {
		return nil, "", false
	}
	tsStr := c[:idx]
	id := c[idx+1:]
	if id == "" {
		return nil, "", false
	}
	if tsStr == "" {
		return nil, id, true
	}
	ts, err := time.Parse(time.RFC3339Nano, tsStr)
	if err != nil {
		return nil, "", false
	}
	return &ts, id, true
}

func formatChatCursor(ts *time.Time, id string) string {
	if ts == nil {
		return "|" + id
	}
	return ts.UTC().Format(time.RFC3339Nano) + "|" + id
}
