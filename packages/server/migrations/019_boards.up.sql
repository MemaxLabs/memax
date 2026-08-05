-- 019: boards
--
-- Pulse boards (plan 25): per-hub intelligence boards. A board is a
-- container of named slots; producers (deterministic Lane A, agentic
-- Lane B) upsert card content into slots with replace semantics — the
-- board never grows unboundedly, it stays a fixed-size surface that
-- refreshes. `instruction` is the board-as-instruction contract from
-- day one: for system boards it is empty (behavior is code-defined);
-- for custom boards it is the user's natural-language brief consumed
-- by the dream synthesis phase.
--
-- Exactly one system board per hub (partial unique index); custom
-- boards (P4) share the table. `status` lifecycle: 'active' — normal;
-- 'cooking' — a custom board configured but awaiting its first dream
-- run (酝酿中); 'paused' — excluded from producer runs.
CREATE TABLE boards (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    hub_id uuid NOT NULL REFERENCES hubs(id) ON DELETE CASCADE,
    created_by uuid NOT NULL,
    kind text DEFAULT 'system' NOT NULL,
    title text DEFAULT '' NOT NULL,
    instruction text DEFAULT '' NOT NULL,
    status text DEFAULT 'active' NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX boards_hub_id_idx ON boards (hub_id);
CREATE UNIQUE INDEX boards_hub_system_uniq ON boards (hub_id) WHERE kind = 'system';

-- One row per occupied slot. (board_id, slot_key) is the replace unit:
-- producers UPSERT on it and the previous card is gone — scarcity is
-- structural, not moderated. `payload` follows the plan-18 item
-- contract: `title` (mirrored into the column for querying) and any
-- text fields must be plain user-facing strings so the unknown-kind
-- fallback renderer can print them literally when a producer ships
-- ahead of its renderer. `cite_memory_ids` carries the receipts that
-- make every card auditable; the ≥3-citation floor for Lane B kinds is
-- enforced at the producer/validator layer (P2), not here.
CREATE TABLE board_slots (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    board_id uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    slot_key text NOT NULL,
    kind text NOT NULL,
    title text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    cite_memory_ids uuid[] DEFAULT '{}' NOT NULL,
    state text DEFAULT 'fresh' NOT NULL,
    resolution jsonb,
    dream_run_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    UNIQUE (board_id, slot_key)
);

CREATE INDEX board_slots_board_id_idx ON board_slots (board_id);

-- 准/不准 feedback is a first-class signal consumed by later synthesis
-- runs, so it outlives the slot content it judged (slots are replaced
-- in place). One row per member per slot — repeat verdicts update in
-- place ("latest opinion wins"), which both allows every hub member to
-- weigh in on a shared card and caps table growth per board.
CREATE TABLE board_feedback (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    board_id uuid NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    slot_key text NOT NULL,
    card_kind text NOT NULL,
    card_title text NOT NULL,
    verdict text NOT NULL,
    user_id uuid NOT NULL,
    cite_memory_ids uuid[] DEFAULT '{}' NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    UNIQUE (board_id, slot_key, user_id)
);

CREATE INDEX board_feedback_board_id_idx ON board_feedback (board_id);
