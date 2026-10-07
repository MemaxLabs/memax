-- 042: v2_imports
--
-- `npx memax-cli init` (plan 25 §7.3, Phase 2 epic 2.1): what agents
-- already know, read from their files on the person's machine, arrives as
-- proposals. The CLI splits each file into statements, scans them for
-- secrets and hidden characters, and uploads what is clean in one request;
-- the server writes each statement as its own proposal (its own command,
-- receipt and idempotency key), folds the statements of the batch that
-- repeat each other, skips the ones the space already has, and checks the
-- batch for disagreements: "6 files, 3 conflicts, 1 Brief".
--
--   imports           one upload: who ran it, the files it read (paths and
--                     counts, never contents), what the CLI kept on the
--                     machine and why (a ref and a rule, never the
--                     secret), and how far the server got (uploaded,
--                     checked for conflicts).
--   import_items      one row per statement the CLI sent: its file:line,
--                     where the file lives (the repository or the
--                     person's home), and what became of it: proposed,
--                     folded into another statement of the batch, already
--                     in the record, or refused.
--   import_conflicts  the batch's disagreements: a group of proposals that
--                     can't all be true ("Test command: 3 files
--                     disagree"), settled once, as a group.
--
-- # What is the record here
--
-- The proposals, their sources (file:line), their conflict flags and
-- conflicts_with links are the record, written through the ledger with
-- receipts like any other. imports and import_items are bookkeeping about
-- an upload, like reads (038) and the Ask meter (041): they hold no
-- statement text, only refs, counts and outcomes, so they need no receipt
-- and Forget has nothing to purge in them. import_conflicts is part of the
-- record: a group is written by the import check (as Memax, beside the
-- `flagged` receipts of its members) and settled by a person (beside the
-- receipts of what the settlement did), so it carries a receipt check of
-- its own, as decision gates do (036), leaving v2.require_receipt alone.
-- Its subject, rationale and suggestion are the model's words about the
-- members; the Forget epic purges them with a member's words (it will
-- grant UPDATE on those three columns).
--
-- Isolation: RLS is ENABLEd and FORCEd on all three, keyed on
-- app.space_ids, with the explicit space_id filters in the ledger as
-- defence in depth. memax_v2 may insert and read them, update only the
-- progress columns, and never delete.

-- ---------------------------------------------------------------------
-- Uploads
-- ---------------------------------------------------------------------

CREATE TABLE v2.imports (
    id              uuid PRIMARY KEY,
    tenant_id       uuid NOT NULL,
    space_id        uuid NOT NULL,
    actor_kind      text NOT NULL,
    actor_id        uuid,
    agent           text,
    -- The request's Idempotency-Key and a hash of what it asked for: a
    -- retry with the same key resumes the same import; the same key with
    -- other content is refused.
    idempotency_key text NOT NULL,
    request_sha256  bytea NOT NULL,
    client          text,
    -- [{path, kind, agent, location, trust, statements, skipped, hidden_characters}]
    files           jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- [{ref, reason, detail}]: what the CLI kept on the machine. detail is
    -- the rule that matched ("GitHub token"), never the matched text.
    skipped         jsonb NOT NULL DEFAULT '[]'::jsonb,
    items_total     integer NOT NULL,
    uploaded_at     timestamptz,
    checked_at      timestamptz,
    check_state     text,
    check_tier      text,
    check_model     text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT imports_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT imports_id_space_key UNIQUE (id, space_id),
    CONSTRAINT imports_key UNIQUE NULLS NOT DISTINCT (space_id, actor_kind, actor_id, idempotency_key),
    CONSTRAINT imports_actor_kind_check CHECK (actor_kind IN ('person', 'agent', 'dream', 'memax', 'repository')),
    CONSTRAINT imports_files_check CHECK (jsonb_typeof(files) = 'array'),
    CONSTRAINT imports_skipped_check CHECK (jsonb_typeof(skipped) = 'array'),
    CONSTRAINT imports_items_total_check CHECK (items_total >= 0),
    CONSTRAINT imports_check_state_check CHECK (check_state IS NULL OR check_state IN ('checked', 'no_model', 'failed', 'skipped'))
);

CREATE INDEX imports_space_created_idx ON v2.imports (space_id, created_at DESC, id DESC);

COMMENT ON TABLE v2.imports IS
    'One upload of statements read from agent files (memax init). Bookkeeping, not the record: refs and counts, never statement text, so no receipts.';

CREATE TABLE v2.import_items (
    import_id         uuid NOT NULL,
    position          integer NOT NULL,
    space_id          uuid NOT NULL,
    item_key          text NOT NULL,
    -- What people see: "CLAUDE.md:12", "~/.codex/memories/notes.md:3".
    ref               text NOT NULL,
    location          text NOT NULL,
    outcome           text NOT NULL,
    -- proposed: the proposal it became. folded: the proposal of the item it
    -- folded into. existing: the memory the space already had. refused: null.
    memory_id         uuid,
    folded_into       integer,
    code              text,
    hidden_characters integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (import_id, position),
    CONSTRAINT import_items_import_fkey FOREIGN KEY (import_id, space_id) REFERENCES v2.imports (id, space_id),
    CONSTRAINT import_items_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT import_items_key UNIQUE (import_id, item_key),
    CONSTRAINT import_items_location_check CHECK (location IN ('repository', 'home')),
    CONSTRAINT import_items_outcome_check CHECK (outcome IN ('proposed', 'folded', 'existing', 'refused')),
    CONSTRAINT import_items_memory_check CHECK ((outcome = 'refused') = (memory_id IS NULL)),
    CONSTRAINT import_items_folded_check CHECK ((outcome = 'folded') = (folded_into IS NOT NULL)),
    CONSTRAINT import_items_hidden_check CHECK (hidden_characters >= 0)
);

CREATE INDEX import_items_memory_idx ON v2.import_items (memory_id) WHERE memory_id IS NOT NULL;

COMMENT ON TABLE v2.import_items IS
    'What became of each statement an import sent: proposed, folded into another, already in the record, or refused. Refs only, never statement text.';

-- ---------------------------------------------------------------------
-- Disagreements
-- ---------------------------------------------------------------------

CREATE TABLE v2.import_conflicts (
    id                 uuid PRIMARY KEY,
    import_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    -- 1, 2, 3 within the import ("1 of 3").
    n                  integer NOT NULL,
    -- The model's words (purged at Forget with any member's words).
    subject            text,
    rationale          text,
    suggestion         text,
    confidence         real,
    -- The proposals that disagree, in the import's order; the first is the
    -- one every other member's conflicts_with link points at.
    members            uuid[] NOT NULL,
    state              text NOT NULL DEFAULT 'open',
    choice             text,
    chosen_memory_id   uuid,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    settled_at         timestamptz,
    CONSTRAINT import_conflicts_import_fkey FOREIGN KEY (import_id, space_id) REFERENCES v2.imports (id, space_id),
    CONSTRAINT import_conflicts_n_key UNIQUE (import_id, n),
    CONSTRAINT import_conflicts_n_check CHECK (n >= 1),
    CONSTRAINT import_conflicts_members_check CHECK (cardinality(members) >= 2),
    CONSTRAINT import_conflicts_state_check CHECK (state IN ('open', 'settled')),
    CONSTRAINT import_conflicts_choice_check CHECK (choice IS NULL OR choice IN ('keep_one', 'keep_all', 'leave_open', 'keep_suggestion')),
    CONSTRAINT import_conflicts_settled_check CHECK ((state = 'settled') = (settled_at IS NOT NULL AND choice IS NOT NULL)),
    CONSTRAINT import_conflicts_confidence_check CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1))
);

CREATE INDEX import_conflicts_members_idx ON v2.import_conflicts USING gin (members);
CREATE INDEX import_conflicts_space_idx ON v2.import_conflicts (space_id);

COMMENT ON TABLE v2.import_conflicts IS
    'An import''s disagreements: proposals from the person''s agent files that can''t all be true, settled once as a group. Part of the record, receipted.';

-- A group changes only with a receipt, written in the same transaction,
-- about one of its members: the import check's flagged receipts, or what
-- settling it did. Its own function, like 036's v2.require_gate_receipt,
-- so v2.require_receipt is untouched.
CREATE FUNCTION v2.require_import_conflict_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = NEW.last_receipt_id
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_kind = 'memory'
           AND r.object_id = ANY (NEW.members)
    ) THEN
        RAISE EXCEPTION 'v2.import_conflicts: % without a receipt written in the same transaction', TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER import_conflicts_require_receipt
    AFTER INSERT OR UPDATE ON v2.import_conflicts
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_import_conflict_receipt();

-- Settled is final: a group never reopens, and its members never change.
CREATE FUNCTION v2.import_conflicts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.import_id IS DISTINCT FROM OLD.import_id
       OR NEW.space_id IS DISTINCT FROM OLD.space_id OR NEW.n IS DISTINCT FROM OLD.n
       OR NEW.members IS DISTINCT FROM OLD.members OR NEW.created_receipt_id IS DISTINCT FROM OLD.created_receipt_id THEN
        RAISE EXCEPTION 'an import conflict''s members are fixed when it is found (conflict %)', OLD.id;
    END IF;
    IF OLD.state = 'settled' THEN
        RAISE EXCEPTION 'import conflict % is already settled', OLD.id;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER import_conflicts_guard
    BEFORE UPDATE ON v2.import_conflicts
    FOR EACH ROW EXECUTE FUNCTION v2.import_conflicts_guard();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.imports ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.imports FORCE ROW LEVEL SECURITY;
CREATE POLICY imports_space ON v2.imports
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.import_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.import_items FORCE ROW LEVEL SECURITY;
CREATE POLICY import_items_space ON v2.import_items
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.import_conflicts ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.import_conflicts FORCE ROW LEVEL SECURITY;
CREATE POLICY import_conflicts_space ON v2.import_conflicts
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.imports TO memax_v2;
GRANT UPDATE (uploaded_at, checked_at, check_state, check_tier, check_model) ON v2.imports TO memax_v2;
GRANT SELECT, INSERT ON v2.import_items TO memax_v2;
GRANT SELECT, INSERT ON v2.import_conflicts TO memax_v2;
GRANT UPDATE (state, choice, chosen_memory_id, settled_at, last_receipt_id) ON v2.import_conflicts TO memax_v2;
