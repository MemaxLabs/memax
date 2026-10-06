-- Revert 031: v2_brief_targets_compiles
--
-- Drops the Brief, targets, compile runs and observations. Run only where
-- losing that data is acceptable (local, test, or before any space has a
-- Brief).
--
-- Receipts are append-only, so the receipts these tables' commands wrote
-- (revised, configured, delivered, …) stay. The restored action CHECK (029's
-- list) is therefore NOT VALID: it holds for every new receipt and leaves
-- the history alone.

DROP FUNCTION IF EXISTS v2.dirty_targets(integer);

DROP TABLE IF EXISTS v2.target_observations;
ALTER TABLE IF EXISTS v2.targets
    DROP CONSTRAINT IF EXISTS targets_last_compile_fkey,
    DROP CONSTRAINT IF EXISTS targets_delivered_compile_fkey;
DROP TABLE IF EXISTS v2.compile_runs;
DROP TABLE IF EXISTS v2.targets;
ALTER TABLE IF EXISTS v2.briefs DROP CONSTRAINT IF EXISTS briefs_current_version_fkey;
DROP TABLE IF EXISTS v2.brief_versions;
DROP TABLE IF EXISTS v2.briefs;

DROP FUNCTION IF EXISTS v2.sha256_valid(text);
DROP FUNCTION IF EXISTS v2.repo_path_valid(text);

-- The receipt trigger as 028 wrote it (029 added its own for agent
-- connections, v2.require_agent_receipt).
CREATE OR REPLACE FUNCTION v2.require_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    sid uuid;
    objects uuid[];
    n v2.memories;
    o v2.memories;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'memories' THEN
        IF TG_OP = 'UPDATE' THEN
            -- embedding/search are derived index data, not the record.
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

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected')) NOT VALID;
