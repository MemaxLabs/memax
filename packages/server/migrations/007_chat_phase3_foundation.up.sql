-- 005: chat_phase3_foundation
--
-- Plan 24 §"Feature 2 — Agent Chat" + §"Data model". Four new
-- tables for the chat MVP:
--
--   chat_sessions          — one per conversation; owns scope + status
--   chat_messages          — one per turn (user, assistant, system)
--   chat_message_events    — SSE replay buffer (24h TTL)
--   chat_tool_approvals    — mutating-tool approval workflow
--
-- This migration ships the SCHEMA only — endpoints, store CRUD,
-- and SSE handler land in subsequent commits. Schema-first lets
-- the model + store layers compile against a stable contract
-- before any handler code touches them.
--
-- Conventions:
--   - All tables CASCADE on owning-row delete (session → messages →
--     events → approvals). Account-delete cascades through users
--     → chat_sessions → the rest.
--   - status/role/scope_type/etc. use text columns + CHECK
--     constraints (rather than Postgres enums) to keep migrations
--     light and Go-side consts the canonical source.
--   - JSONB for tool_calls / citations / payload / args because
--     the shapes evolve across SDK versions; consumers parse with
--     the in-process model types.

-- chat_sessions: one row per conversation. Scope (operating
-- boundary) + tools + status + cap counter all live here so a
-- session-level read fetches the full context in one row.
CREATE TABLE public.chat_sessions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    -- Scope: the agent's operating boundary. Mirror of
    -- internal/agent.ScopeType. The CHECK below enforces the
    -- shape contract (single_hub=1, hub_subset>=1, user_all_hubs=0)
    -- AT THE DB LAYER so a buggy direct-SQL writer can't smuggle
    -- a malformed scope past the Go validators.
    scope_type          text NOT NULL,
    scope_hub_ids       uuid[] NOT NULL DEFAULT '{}'::uuid[],
    -- write_hub_id: optional default-write target. App layer
    -- re-validates per send (membership may have changed); the
    -- DB CHECK only enforces the static-shape invariant.
    write_hub_id        uuid,
    title               text NOT NULL DEFAULT '',
    model               text NOT NULL,
    -- Tools is JSONB (an array of strings: ["recall_memories",
    -- "list_memories", ...]). PATCH /v1/chat/sessions/{id} with
    -- {tools: [...]} bumps toolset_version; each chat_messages
    -- row records toolset_version_used so audit can reconstruct
    -- the exact tool surface the agent had at any point.
    tools               jsonb NOT NULL DEFAULT '[]'::jsonb,
    toolset_version     int NOT NULL DEFAULT 1,
    -- status lifecycle: idle → running → (canceling →) idle, or
    -- idle → awaiting_approval → running. The lock primitive is
    -- the row's status + active_message_id + locked_at triple,
    -- transitioned via UPDATE ... WHERE status='idle'.
    status              text NOT NULL DEFAULT 'idle',
    active_message_id   uuid, -- forward-declared FK; constraint added at end
    -- locked_at: lease start. Sweeper runs every 60s; lease
    -- expires at locked_at + 5min.
    locked_at           timestamptz,
    -- message_count: denormalized cap-check counter. Soft 200,
    -- hard 250 per plan 24.
    message_count       int NOT NULL DEFAULT 0,
    pinned_at           timestamptz,
    archived_at         timestamptz,
    last_message_at     timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    deleted_at          timestamptz,
    CONSTRAINT chat_sessions_scope_type_check
        CHECK (scope_type IN ('user_all_hubs', 'single_hub', 'hub_subset')),
    CONSTRAINT chat_sessions_status_check
        CHECK (status IN ('idle', 'running', 'awaiting_approval', 'canceling')),
    -- COALESCE on every disjunct: array_length on an empty
    -- array returns NULL, and NULL = 1 evaluates to NULL (which
    -- Postgres CHECK treats as "constraint satisfied"). Without
    -- the COALESCE wrap, scope_type='single_hub' with an empty
    -- scope_hub_ids would slip past the CHECK and silently
    -- create a malformed session row.
    CONSTRAINT chat_sessions_scope_shape_check CHECK (
        (scope_type = 'user_all_hubs' AND COALESCE(array_length(scope_hub_ids, 1), 0) = 0) OR
        (scope_type = 'single_hub'    AND COALESCE(array_length(scope_hub_ids, 1), 0) = 1) OR
        (scope_type = 'hub_subset'    AND COALESCE(array_length(scope_hub_ids, 1), 0) >= 1)
    ),
    CONSTRAINT chat_sessions_write_hub_check CHECK (
        write_hub_id IS NULL OR
        scope_type = 'user_all_hubs' OR
        write_hub_id = ANY(scope_hub_ids)
    )
);

-- "Recent sessions for owner" — the Inbox / sidebar query.
-- Keyset pagination by last_message_at DESC. Excludes deleted.
CREATE INDEX idx_chat_sessions_owner_recent
    ON public.chat_sessions (owner_id, last_message_at DESC NULLS LAST)
    WHERE deleted_at IS NULL;

-- "Sessions touching this hub" — hub-delete cascade audit.
-- See plan 24's note: this index covers single_hub + hub_subset
-- only (user_all_hubs sessions have empty arrays). Hub-delete
-- cascade also UNIONs sessions WHERE scope_type='user_all_hubs'.
CREATE INDEX idx_chat_sessions_scope_hubs
    ON public.chat_sessions USING GIN (scope_hub_ids)
    WHERE deleted_at IS NULL;

-- Lease sweeper: find expired in-flight rows.
CREATE INDEX idx_chat_sessions_lease
    ON public.chat_sessions (locked_at)
    WHERE status != 'idle';

-- chat_messages: one row per turn. User rows go straight to
-- 'completed' at insert; assistant rows transition through
-- queued → running → completed/partial_failed/failed/canceled.
CREATE TABLE public.chat_messages (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            uuid NOT NULL REFERENCES public.chat_sessions(id) ON DELETE CASCADE,
    sequence              int NOT NULL,
    role                  text NOT NULL,
    -- author_user_id: always = session.owner_id for v1 (single-
    -- user sessions). Pre-allocated for hub-shared chat in a
    -- later phase.
    author_user_id        uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    status                text NOT NULL DEFAULT 'completed',
    -- scope_hub_ids_used: snapshot of the resolved scope at run
    -- time. Used for read-time redaction when the caller has
    -- since lost access to a hub the agent operated against.
    scope_hub_ids_used    uuid[] NOT NULL DEFAULT '{}'::uuid[],
    content               text NOT NULL DEFAULT '',
    -- tool_calls: [{id, name, args, results: [{hub_id, ...}]}].
    -- Per-result hub_id enables transcript redaction.
    tool_calls            jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- citations: [{memory_id, hub_id, span_start, span_end}].
    citations             jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- usage: {input_tokens, output_tokens, cache_read, ...}.
    usage                 jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- error: {code, message} when the turn failed.
    error                 jsonb,
    idempotency_key       text,
    -- parent_message_id: non-null when this is a regenerate of a
    -- prior assistant msg; the prior is "superseded" (UI-only
    -- filter; no DB column needed because parent_message_id IS
    -- NOT NULL on the new row implies the parent is superseded).
    --
    -- The composite FK to (id, session_id) below pins parent +
    -- child to the SAME session — a regenerate must point at a
    -- message in its own conversation, never one in another.
    parent_message_id     uuid,
    toolset_version_used  int NOT NULL,
    started_at            timestamptz,
    finished_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, sequence),
    -- Composite uniqueness on (id, session_id) is strictly
    -- redundant with the PK on id alone, but it's required for
    -- the same-session foreign keys below: Postgres' FK target
    -- must be a UNIQUE or PRIMARY KEY column SET. The composite
    -- FK ensures parent_message_id and chat_sessions.active_
    -- message_id can ONLY point at messages in the same session,
    -- closing a leak where a buggy writer could cross sessions.
    UNIQUE (id, session_id),
    CONSTRAINT chat_messages_role_check
        CHECK (role IN ('user', 'assistant', 'system')),
    CONSTRAINT chat_messages_status_check
        CHECK (status IN ('queued', 'running', 'completed', 'partial_failed', 'failed', 'canceled', 'canceling'))
);

-- Idempotency: only enforced when idempotency_key IS NOT NULL.
-- User messages set it (preventing double-send on client retry);
-- assistant rows leave it null.
CREATE UNIQUE INDEX idx_chat_messages_idempotency
    ON public.chat_messages (session_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Per-session ordered scan (transcript reads).
CREATE INDEX idx_chat_messages_session_seq
    ON public.chat_messages (session_id, sequence);

-- "In-flight messages for this session" — cancel + sweeper paths.
CREATE INDEX idx_chat_messages_session_status
    ON public.chat_messages (session_id, status)
    WHERE status IN ('queued', 'running', 'canceling');

-- Regenerate audit: find children of a superseded assistant
-- message. Index on (parent_message_id, session_id) so the
-- "is this row superseded?" query stays in-index even with the
-- same-session check.
CREATE INDEX idx_chat_messages_parent
    ON public.chat_messages (parent_message_id, session_id)
    WHERE parent_message_id IS NOT NULL;

-- Same-session parent FK. Composite (parent_message_id,
-- session_id) → (id, session_id) so a regenerate row's parent
-- MUST live in the same conversation.
--
-- Column-targeted ON DELETE SET NULL: if we let the cascade
-- NULL the entire (parent_message_id, session_id) pair, it
-- would try to NULL session_id too — and session_id is NOT
-- NULL on chat_messages, so the cascade would fail and leave
-- the row stuck. The `(parent_message_id)` qualifier (Postgres
-- 15+ syntax) targets ONLY the parent column for nullification,
-- leaving session_id intact. The composite FK still pins same-
-- session for INSERTs and UPDATEs; only the cascade behavior
-- is column-bounded.
ALTER TABLE public.chat_messages
    ADD CONSTRAINT chat_messages_parent_same_session_fkey
        FOREIGN KEY (parent_message_id, session_id)
        REFERENCES public.chat_messages (id, session_id)
        ON DELETE SET NULL (parent_message_id) DEFERRABLE INITIALLY DEFERRED;

-- chat_sessions.active_message_id same-session FK. Composite
-- (active_message_id, id) → (chat_messages.id, session_id) so
-- a session can ONLY mark a message active if that message
-- belongs to the same session — closes a cross-session leak
-- where session A could otherwise point at session B's
-- message.
--
-- Column-targeted ON DELETE SET NULL (active_message_id):
-- when the referenced message is deleted, ONLY null
-- active_message_id; leave chat_sessions.id alone. The id
-- column is the primary key — a default `ON DELETE SET NULL`
-- would try to null it and fail. Postgres 15+ syntax.
--
-- DEFERRABLE so the create-message + update-session pair can
-- run in one tx without ordering pain.
ALTER TABLE public.chat_sessions
    ADD CONSTRAINT chat_sessions_active_message_same_session_fkey
        FOREIGN KEY (active_message_id, id)
        REFERENCES public.chat_messages (id, session_id)
        ON DELETE SET NULL (active_message_id) DEFERRABLE INITIALLY DEFERRED;

-- chat_message_events: SSE replay buffer. One row per emitted
-- wire event. Resume reads WHERE message_id = $1 AND seq >
-- $last_event_id ORDER BY seq.
CREATE TABLE public.chat_message_events (
    id          bigserial PRIMARY KEY,
    message_id  uuid NOT NULL REFERENCES public.chat_messages(id) ON DELETE CASCADE,
    seq         int NOT NULL,
    event_type  text NOT NULL,
    payload     jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (message_id, seq)
);

CREATE INDEX idx_chat_message_events_msg_seq
    ON public.chat_message_events (message_id, seq);

-- 24h replay-window sweeper.
CREATE INDEX idx_chat_message_events_created
    ON public.chat_message_events (created_at);

-- chat_tool_approvals: mutating-tool approval workflow. A
-- pending row blocks the agent goroutine until the user decides
-- (or until expires_at = requested_at + 10min). args_hash makes
-- duplicate decision clicks idempotent.
CREATE TABLE public.chat_tool_approvals (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id    uuid NOT NULL REFERENCES public.chat_messages(id) ON DELETE CASCADE,
    tool_name     text NOT NULL,
    args          jsonb NOT NULL,
    args_hash     text NOT NULL,
    status        text NOT NULL DEFAULT 'pending',
    decided_by    uuid REFERENCES public.users(id),
    decided_at    timestamptz,
    requested_at  timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chat_tool_approvals_status_check
        CHECK (status IN ('pending', 'approved', 'denied', 'timed_out'))
);

-- Pending-approval expiry sweeper.
CREATE INDEX idx_chat_tool_approvals_pending
    ON public.chat_tool_approvals (expires_at)
    WHERE status = 'pending';

-- "Pending approvals for this message" — UI surface.
CREATE INDEX idx_chat_tool_approvals_message
    ON public.chat_tool_approvals (message_id)
    WHERE status = 'pending';
