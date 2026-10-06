-- 035: v2_judge_undo
--
-- The judge (plan 25 §5.8, epic 1.3) and Undo (Review's ⌘Z):
--
--   memories.content_sha256,  derived from the current statement, like
--   memories.minhash_bands    `search`: the exact and near-verbatim
--                             duplicate filters of the judge's stage 0
--   memory_links              links can now end (ended_receipt_id), so an
--                             undone fold or a settled conflict leaves its
--                             history instead of a dangling edge
--   judge_verdicts            one row per judged memory version: what the
--                             judge decided, from which stage and tier,
--                             against which candidates
--   undo_entries              the inverse of each undoable command, so Undo
--                             restores exactly what the command changed
--
-- The guarantees of 028 and 031 extend to the new tables and columns: RLS
-- ENABLEd and FORCEd on app.space_ids, memax_v2 gets only the grants the
-- ledger needs (no DELETE, no TRUNCATE), and the deferred receipt trigger
-- refuses any insert or update without a receipt from the same
-- transaction, in the same space, about the same memory.
--
-- # Undo and the lifecycle guard
--
-- Two transitions exist only as an undo: kept → proposed (undo a keep) and
-- rejected → proposed (undo a reject). v2.lifecycle_transition_allowed
-- still refuses both, so "kept things don't become proposals again" holds
-- for every other write; the guard admits them only when the row's receipt
-- is an `undid` receipt written in the same transaction about the same
-- memory (v2.lifecycle_undo_allowed is the Go side's lifecycle.UndoAllowed).
--
-- # Words
--
-- judge_verdicts.rationale and merged_statement may hold the words of the
-- memory and of its candidates; Forget must purge them (with the
-- statement versions, source quotes and receipt reasons). content_sha256
-- and minhash_bands are fingerprints of the words, so a forgotten memory
-- can't keep them either (memories_forgotten_purged_check). undo_entries
-- hold states, versions and ids, never words.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains: judged (the judge recorded a verdict that
-- changed nothing), linked (it linked a proposal to the memory it updates
-- or supersedes) and superseded (a kept decision gave way to a newer one).
-- The list keeps every verb 028, 029 and 031 admit. merged, flagged,
-- resolved and undid were already there.

-- ---------------------------------------------------------------------
-- Receipt actions
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded'));

-- ---------------------------------------------------------------------
-- Fingerprints for the judge's stage 0 (derived, like `search`)
-- ---------------------------------------------------------------------

ALTER TABLE v2.memories
    ADD COLUMN content_sha256 text,
    ADD COLUMN minhash_bands bigint[],
    ADD CONSTRAINT memories_content_sha256_check CHECK (content_sha256 IS NULL OR v2.sha256_valid(content_sha256)),
    ADD CONSTRAINT memories_minhash_bands_check CHECK (minhash_bands IS NULL OR cardinality(minhash_bands) BETWEEN 1 AND 64);

COMMENT ON COLUMN v2.memories.content_sha256 IS
    'Derived: sha256 of the normalised current statement (internal/textsig). Purged at Forget.';
COMMENT ON COLUMN v2.memories.minhash_bands IS
    'Derived: MinHash LSH band keys of the current statement (internal/textsig). Purged at Forget.';

ALTER TABLE v2.memories DROP CONSTRAINT memories_forgotten_purged_check;
ALTER TABLE v2.memories ADD CONSTRAINT memories_forgotten_purged_check CHECK (
    lifecycle <> 'forgotten'
    OR (embedding IS NULL AND search IS NULL AND content_sha256 IS NULL AND minhash_bands IS NULL));

CREATE INDEX memories_space_content_idx ON v2.memories (space_id, content_sha256) WHERE content_sha256 IS NOT NULL;
CREATE INDEX memories_minhash_idx ON v2.memories USING gin (minhash_bands);
-- The keyed decisions in force (TEPA): the judge's second candidate set.
CREATE INDEX memories_space_decisions_idx ON v2.memories (space_id)
    WHERE kind = 'decision' AND lifecycle = 'kept';

GRANT UPDATE (content_sha256, minhash_bands) ON v2.memories TO memax_v2;

-- ---------------------------------------------------------------------
-- The lifecycle guard admits the two undo transitions
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.lifecycle_undo_allowed(from_state text, to_state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT (from_state, to_state) IN (('kept', 'proposed'), ('rejected', 'proposed')) $$;

CREATE OR REPLACE FUNCTION v2.memories_lifecycle_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    from_state text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        from_state := OLD.lifecycle;
    END IF;
    IF v2.lifecycle_transition_allowed(from_state, NEW.lifecycle) THEN
        RETURN NEW;
    END IF;
    IF from_state IS NOT NULL
       AND v2.lifecycle_undo_allowed(from_state, NEW.lifecycle)
       AND EXISTS (
           SELECT 1 FROM v2.receipts r
            WHERE r.id = NEW.last_receipt_id
              AND r.action = 'undid'
              AND r.object_id = NEW.id
              AND r.txid = pg_current_xact_id())
    THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'memory %: lifecycle % -> % is not allowed', NEW.id, COALESCE(from_state, '(new)'), NEW.lifecycle
        USING ERRCODE = 'MXL01';
END $$;

-- ---------------------------------------------------------------------
-- Links end instead of disappearing
-- ---------------------------------------------------------------------

ALTER TABLE v2.memory_links
    ADD COLUMN ended_receipt_id uuid REFERENCES v2.receipts (id),
    ADD COLUMN ended_at timestamptz,
    ADD CONSTRAINT memory_links_ended_check CHECK ((ended_receipt_id IS NULL) = (ended_at IS NULL));

-- One active link per (kind, from, to); ended ones are history, so the
-- same pair can be linked again (a fold undone, then made again).
ALTER TABLE v2.memory_links DROP CONSTRAINT memory_links_unique;
CREATE UNIQUE INDEX memory_links_active_key ON v2.memory_links (kind, from_memory_id, to_memory_id, to_note_id)
    NULLS NOT DISTINCT WHERE ended_receipt_id IS NULL;

-- A link ends once, and nothing else about it changes.
CREATE FUNCTION v2.memory_links_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.ended_receipt_id IS NOT NULL OR NEW.ended_receipt_id IS NULL
       OR (NEW.id, NEW.space_id, NEW.kind, NEW.from_memory_id, NEW.to_memory_id, NEW.to_note_id, NEW.receipt_id, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.space_id, OLD.kind, OLD.from_memory_id, OLD.to_memory_id, OLD.to_note_id, OLD.receipt_id, OLD.created_at)
    THEN
        RAISE EXCEPTION 'memory link %: links only end, once', OLD.id USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER memory_links_guard
    BEFORE UPDATE ON v2.memory_links
    FOR EACH ROW EXECUTE FUNCTION v2.memory_links_guard();

GRANT UPDATE (ended_receipt_id, ended_at) ON v2.memory_links TO memax_v2;

-- ---------------------------------------------------------------------
-- Judge verdicts
-- ---------------------------------------------------------------------

CREATE TABLE v2.judge_verdicts (
    memory_id         uuid NOT NULL,
    version           integer NOT NULL,                     -- the statement version judged
    round             integer NOT NULL DEFAULT 0,           -- a re-judgement after an undone fold is round 1, …
    space_id          uuid NOT NULL,
    mode              text NOT NULL,                        -- proposal | kept (a Write agent's, checked after the fact)
    stage             text NOT NULL,                        -- exact | near | reproposal | llm | none
    verdict           text NOT NULL,                        -- duplicate | updates | extends | contradicts | unrelated | none
    outcome           text NOT NULL,                        -- what it did: folded | suppressed | linked | superseding | flagged | none | failed | skipped
    related_memory_id uuid,                                 -- the memory the verdict is about (the fold target, the decision…)
    confidence        real,
    rationale         text,                                 -- one line; may quote memories, so purged at Forget
    merged_statement  text,                                 -- the model's merged wording, offered in Review; purged at Forget
    tier              text,                                 -- primary | fallback | strong (the tier whose answer counted)
    model             text,
    candidates        jsonb NOT NULL DEFAULT '[]'::jsonb,   -- [{memory_id, ref, sets, relation, confidence, tier}]: never words
    error             text,                                 -- a code, never words (llm_failed, escalation_failed, …)
    timings           jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {queue_ms, snapshot_ms, stage0_ms, candidates_ms, llm_ms, strong_ms, total_ms}
    receipt_id        uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id   uuid NOT NULL REFERENCES v2.receipts (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, version, round),
    CONSTRAINT judge_verdicts_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT judge_verdicts_related_fkey FOREIGN KEY (related_memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT judge_verdicts_version_check CHECK (version >= 1 AND round >= 0),
    CONSTRAINT judge_verdicts_mode_check CHECK (mode IN ('proposal', 'kept')),
    CONSTRAINT judge_verdicts_stage_check CHECK (stage IN ('exact', 'near', 'reproposal', 'llm', 'none')),
    CONSTRAINT judge_verdicts_verdict_check CHECK (verdict IN ('duplicate', 'updates', 'extends', 'contradicts', 'unrelated', 'none')),
    CONSTRAINT judge_verdicts_outcome_check CHECK (outcome IN
        ('folded', 'suppressed', 'linked', 'superseding', 'flagged', 'none', 'failed', 'skipped')),
    CONSTRAINT judge_verdicts_tier_check CHECK (tier IS NULL OR tier IN ('primary', 'fallback', 'strong')),
    CONSTRAINT judge_verdicts_confidence_check CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    CONSTRAINT judge_verdicts_rationale_check CHECK (rationale IS NULL OR char_length(rationale) <= 500),
    CONSTRAINT judge_verdicts_merged_check CHECK (merged_statement IS NULL OR char_length(merged_statement) <= 2000),
    CONSTRAINT judge_verdicts_model_check CHECK (model IS NULL OR char_length(model) <= 200),
    CONSTRAINT judge_verdicts_error_check CHECK (error IS NULL OR error ~ '^[a-z_]{1,64}$'),
    CONSTRAINT judge_verdicts_candidates_check CHECK (jsonb_typeof(candidates) = 'array'),
    CONSTRAINT judge_verdicts_timings_check CHECK (jsonb_typeof(timings) = 'object')
);

CREATE INDEX judge_verdicts_space_idx ON v2.judge_verdicts (space_id);
CREATE INDEX judge_verdicts_related_idx ON v2.judge_verdicts (related_memory_id) WHERE related_memory_id IS NOT NULL;

COMMENT ON TABLE v2.judge_verdicts IS
    'What the judge decided about one memory version: stage, verdict, tier, candidates. Written only by internal/ledger.';

-- ---------------------------------------------------------------------
-- Undo entries
-- ---------------------------------------------------------------------

CREATE TABLE v2.undo_entries (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    space_id          uuid NOT NULL,
    command           text NOT NULL,                        -- keep | reject | edit | resolve_conflict | judge_fold
    actor_kind        text NOT NULL,                        -- who decided: person, or memax for the judge's folds
    actor_id          uuid,
    receipt_ids       uuid[] NOT NULL,                      -- every receipt the command wrote; any of them addresses it
    memory_ids        uuid[] NOT NULL,                      -- every memory the command changed
    inverse           jsonb NOT NULL,                       -- {memories: [{id, ref, before, after_stream_version}], links_created, links_ended}
    expires_at        timestamptz NOT NULL,                 -- the undo window
    undone_receipt_id uuid REFERENCES v2.receipts (id),
    undone_at         timestamptz,
    receipt_id        uuid NOT NULL REFERENCES v2.receipts (id), -- the command's first receipt
    last_receipt_id   uuid NOT NULL REFERENCES v2.receipts (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT undo_entries_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT undo_entries_command_check CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold')),
    CONSTRAINT undo_entries_actor_check CHECK (actor_kind IN ('person', 'memax') AND (actor_kind <> 'person' OR actor_id IS NOT NULL)),
    CONSTRAINT undo_entries_receipts_check CHECK (cardinality(receipt_ids) >= 1 AND cardinality(memory_ids) >= 1),
    CONSTRAINT undo_entries_inverse_check CHECK (jsonb_typeof(inverse) = 'object'),
    CONSTRAINT undo_entries_undone_check CHECK ((undone_receipt_id IS NULL) = (undone_at IS NULL))
);

CREATE INDEX undo_entries_receipts_idx ON v2.undo_entries USING gin (receipt_ids);
CREATE INDEX undo_entries_memories_idx ON v2.undo_entries USING gin (memory_ids);
CREATE INDEX undo_entries_space_idx ON v2.undo_entries (space_id, created_at DESC);

COMMENT ON TABLE v2.undo_entries IS
    'The inverse of each undoable command (states, versions and links; never words), for Undo.';

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1), extended
-- ---------------------------------------------------------------------

CREATE OR REPLACE FUNCTION v2.require_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    sid uuid;
    objects uuid[];
    n v2.memories;
    o v2.memories;
    nt v2.targets;
    ot v2.targets;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'memories' THEN
        IF TG_OP = 'UPDATE' THEN
            -- embedding, search and the stage-0 fingerprints are derived
            -- index data, not the record.
            n := NEW; o := OLD;
            n.embedding := NULL; o.embedding := NULL;
            n.search := NULL; o.search := NULL;
            n.content_sha256 := NULL; o.content_sha256 := NULL;
            n.minhash_bands := NULL; o.minhash_bands := NULL;
            IF n IS NOT DISTINCT FROM o THEN
                RETURN NULL;
            END IF;
        END IF;
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'memory_versions' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    WHEN 'sources' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := NULL;
    WHEN 'memory_sources' THEN
        rid := NEW.receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    WHEN 'memory_links' THEN
        -- Ending a link needs the receipt that ended it.
        rid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.ended_receipt_id ELSE NEW.receipt_id END;
        sid := NEW.space_id; objects := ARRAY[NEW.from_memory_id, NEW.to_memory_id];
    WHEN 'briefs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'brief_versions' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.brief_id];
    WHEN 'targets' THEN
        IF TG_OP = 'UPDATE' THEN
            -- The compile path's bookkeeping (see the header of 031):
            -- generation counters, the quiet-window clock, updated_at, and
            -- sync_state moving into or out of "compiling".
            nt := NEW; ot := OLD;
            nt.dirty_gen := NULL; ot.dirty_gen := NULL;
            nt.dirty_at := NULL; ot.dirty_at := NULL;
            nt.compiled_gen := NULL; ot.compiled_gen := NULL;
            nt.updated_at := NULL; ot.updated_at := NULL;
            IF (OLD.sync_state IN ('in_sync', 'pending_delivery') AND NEW.sync_state = 'compiling')
               OR (OLD.sync_state = 'compiling' AND NEW.sync_state IN ('in_sync', 'pending_delivery')) THEN
                nt.sync_state := NULL; ot.sync_state := NULL;
            END IF;
            IF nt IS NOT DISTINCT FROM ot THEN
                RETURN NULL;
            END IF;
        END IF;
        -- A target changes with a receipt about itself, or (recording a
        -- compile, acknowledging a delivery) about its own compile run.
        rid := NEW.last_receipt_id; sid := NEW.space_id;
        objects := ARRAY[NEW.id, NEW.last_compile_id, NEW.delivered_compile_id];
    WHEN 'compile_runs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'target_observations' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.target_id];
    WHEN 'judge_verdicts' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    WHEN 'undo_entries' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := NEW.memory_ids;
    ELSE
        RAISE EXCEPTION 'v2.require_receipt is not configured for table %', TG_TABLE_NAME;
    END CASE;

    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = rid
           AND r.txid = pg_current_xact_id()
           AND r.space_id = sid
           AND (objects IS NULL OR r.object_id = ANY (objects))
    ) THEN
        RAISE EXCEPTION 'v2.%: % without a receipt written in the same transaction', TG_TABLE_NAME, TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER judge_verdicts_require_receipt
    AFTER INSERT OR UPDATE ON v2.judge_verdicts
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER undo_entries_require_receipt
    AFTER INSERT OR UPDATE ON v2.undo_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.judge_verdicts ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.judge_verdicts FORCE ROW LEVEL SECURITY;
CREATE POLICY judge_verdicts_space ON v2.judge_verdicts
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.undo_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.undo_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY undo_entries_space ON v2.undo_entries
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs
-- ---------------------------------------------------------------------

-- Verdicts are immutable until Forget learns to purge their words
-- (forget-propagation epic), which will grant UPDATE (rationale,
-- merged_statement, last_receipt_id) then.
GRANT SELECT, INSERT ON v2.judge_verdicts TO memax_v2;

GRANT SELECT, INSERT ON v2.undo_entries TO memax_v2;
GRANT UPDATE (undone_receipt_id, undone_at, last_receipt_id) ON v2.undo_entries TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.lifecycle_undo_allowed(text, text) TO memax_v2;
