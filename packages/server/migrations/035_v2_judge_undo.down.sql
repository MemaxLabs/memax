-- Revert 035: v2_judge_undo
--
-- Drops verdicts and undo entries, the fingerprints and the link endings.
-- Run only where losing them is acceptable (local, test, or before the
-- judge has run). Receipts these commands wrote (judged, linked,
-- superseded, undid) stay: receipts are append-only, so the restored
-- action CHECK (031's list) is NOT VALID.
--
-- Links that ended are history; with the unique constraint back, an ended
-- link and an active one for the same pair can't both stay, so ended links
-- must be gone first. They can't be deleted as memax_v2, but this runs as
-- the migrating role.

DROP TABLE IF EXISTS v2.undo_entries;
DROP TABLE IF EXISTS v2.judge_verdicts;

-- The receipt trigger as 031 wrote it.
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
            n := NEW; o := OLD;
            n.embedding := NULL; o.embedding := NULL;
            n.search := NULL; o.search := NULL;
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
        rid := NEW.receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.from_memory_id, NEW.to_memory_id];
    WHEN 'briefs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'brief_versions' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.brief_id];
    WHEN 'targets' THEN
        IF TG_OP = 'UPDATE' THEN
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
        rid := NEW.last_receipt_id; sid := NEW.space_id;
        objects := ARRAY[NEW.id, NEW.last_compile_id, NEW.delivered_compile_id];
    WHEN 'compile_runs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'target_observations' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.target_id];
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

DROP TRIGGER IF EXISTS memory_links_guard ON v2.memory_links;
DROP FUNCTION IF EXISTS v2.memory_links_guard();
DROP INDEX IF EXISTS v2.memory_links_active_key;
DELETE FROM v2.memory_links WHERE ended_receipt_id IS NOT NULL;
ALTER TABLE v2.memory_links
    DROP CONSTRAINT IF EXISTS memory_links_ended_check,
    DROP COLUMN IF EXISTS ended_at,
    DROP COLUMN IF EXISTS ended_receipt_id,
    ADD CONSTRAINT memory_links_unique UNIQUE NULLS NOT DISTINCT (kind, from_memory_id, to_memory_id, to_note_id);

-- The lifecycle guard as 028 wrote it.
CREATE OR REPLACE FUNCTION v2.memories_lifecycle_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    from_state text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        from_state := OLD.lifecycle;
    END IF;
    IF NOT v2.lifecycle_transition_allowed(from_state, NEW.lifecycle) THEN
        RAISE EXCEPTION 'memory %: lifecycle % -> % is not allowed', NEW.id, COALESCE(from_state, '(new)'), NEW.lifecycle
            USING ERRCODE = 'MXL01';
    END IF;
    RETURN NEW;
END $$;
DROP FUNCTION IF EXISTS v2.lifecycle_undo_allowed(text, text);

DROP INDEX IF EXISTS v2.memories_space_decisions_idx;
DROP INDEX IF EXISTS v2.memories_minhash_idx;
DROP INDEX IF EXISTS v2.memories_space_content_idx;
ALTER TABLE v2.memories DROP CONSTRAINT memories_forgotten_purged_check;
ALTER TABLE v2.memories
    DROP CONSTRAINT IF EXISTS memories_minhash_bands_check,
    DROP CONSTRAINT IF EXISTS memories_content_sha256_check,
    DROP COLUMN IF EXISTS minhash_bands,
    DROP COLUMN IF EXISTS content_sha256,
    ADD CONSTRAINT memories_forgotten_purged_check CHECK (lifecycle <> 'forgotten' OR (embedding IS NULL AND search IS NULL));

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped')) NOT VALID;
