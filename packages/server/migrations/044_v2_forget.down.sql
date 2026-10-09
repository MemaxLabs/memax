-- 044 down: v2_forget. Puts back 042's receipt vocabulary (043 kept it),
-- 035's receipt check and purge constraint, 036's gate guard, 043's import
-- conflict guard, and the foreign keys from receipts and seals to
-- public.hubs. It fails if a retired space left receipts behind (their hub
-- is gone), which is the point: those receipts can't be re-attached to a
-- hub.

REVOKE UPDATE (subject, rationale, suggestion) ON v2.import_conflicts FROM memax_v2;

CREATE OR REPLACE FUNCTION v2.import_conflicts_guard() RETURNS trigger
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

REVOKE EXECUTE ON FUNCTION v2.retire_space(uuid), v2.redact_space_receipt_reasons(uuid) FROM memax_v2;
REVOKE UPDATE (status, decided_by, decided_at, last_receipt_id, updated_at) ON v2.forget_requests FROM memax_v2;
REVOKE UPDATE (delivered_at, delivered_via) ON v2.agent_notices FROM memax_v2;
REVOKE UPDATE (label, status, detail, done_at, updated_at) ON v2.propagations FROM memax_v2;
REVOKE UPDATE (gone, status, propagation, completed_at, reapplied_at, updated_at) ON v2.tombstones FROM memax_v2;

DROP FUNCTION v2.forgotten_spaces();
DROP FUNCTION IF EXISTS v2.forgotten_spaces_as_owner();
DROP FUNCTION v2.retire_space(uuid);
DROP FUNCTION v2.redact_space_receipt_reasons(uuid);

ALTER TABLE v2.targets ALTER CONSTRAINT targets_last_compile_fkey NOT DEFERRABLE;
ALTER TABLE v2.targets ALTER CONSTRAINT targets_delivered_compile_fkey NOT DEFERRABLE;

DROP TRIGGER forget_requests_require_receipt ON v2.forget_requests;

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
        rid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.ended_receipt_id ELSE NEW.receipt_id END;
        sid := NEW.space_id; objects := ARRAY[NEW.from_memory_id, NEW.to_memory_id];
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

CREATE OR REPLACE FUNCTION v2.decision_gates_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.decision_gates;
    o v2.decision_gates;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT v2.gate_status_transition_allowed(NULL, NEW.status) THEN
            RAISE EXCEPTION 'gate %: a gate starts waiting, not %', NEW.id, NEW.status
                USING ERRCODE = 'MXL03';
        END IF;
        RETURN NEW;
    END IF;
    n := NEW; o := OLD;
    n.delivered_at := NULL; o.delivered_at := NULL;
    IF n IS NOT DISTINCT FROM o THEN
        RETURN NEW;
    END IF;
    IF NOT v2.gate_status_transition_allowed(OLD.status, NEW.status) THEN
        RAISE EXCEPTION 'gate %: status % -> % is not allowed; a gate ends once', NEW.id, OLD.status, NEW.status
            USING ERRCODE = 'MXL03';
    END IF;
    RETURN NEW;
END $$;

REVOKE UPDATE (question, context, options) ON v2.decision_gates FROM memax_v2;

DROP TRIGGER compile_runs_forgotten_guard ON v2.compile_runs;
DROP FUNCTION v2.compile_runs_forgotten_guard();
DROP TRIGGER judge_verdicts_forgotten_guard ON v2.judge_verdicts;
DROP FUNCTION v2.judge_verdicts_forgotten_guard();
REVOKE UPDATE (rationale, merged_statement, last_receipt_id) ON v2.judge_verdicts FROM memax_v2;

REVOKE UPDATE (request_hash) ON v2.command_keys FROM memax_v2;
UPDATE v2.command_keys SET request_hash = '\x'::bytea WHERE request_hash IS NULL;
ALTER TABLE v2.command_keys ALTER COLUMN request_hash SET NOT NULL;

REVOKE UPDATE (changeset) ON v2.target_observations FROM memax_v2;
REVOKE UPDATE (title, summary, structure, last_receipt_id) ON v2.brief_versions FROM memax_v2;
DROP TRIGGER brief_versions_purge_guard ON v2.brief_versions;
DROP FUNCTION v2.brief_versions_purge_guard();
REVOKE UPDATE (ref, uri, locator, content_hash) ON v2.sources FROM memax_v2;
DROP TRIGGER sources_purge_guard ON v2.sources;
DROP FUNCTION v2.sources_purge_guard();

DROP TRIGGER memories_forgotten_words ON v2.memories;
DROP FUNCTION v2.memories_forgotten_words();
ALTER TABLE v2.memories DROP CONSTRAINT memories_forgotten_purged_check;
ALTER TABLE v2.memories ADD CONSTRAINT memories_forgotten_purged_check CHECK (
    lifecycle <> 'forgotten'
    OR (embedding IS NULL AND search IS NULL AND content_sha256 IS NULL AND minhash_bands IS NULL));

DROP TABLE v2.forget_requests;
DROP FUNCTION v2.forget_requests_guard();
DROP TABLE v2.agent_notices;
DROP FUNCTION v2.agent_notices_guard();
DROP TABLE v2.propagations;
DROP TABLE v2.tombstones;
DROP FUNCTION v2.require_tombstone_receipt();

ALTER TABLE v2.receipt_checkpoints DROP CONSTRAINT receipt_checkpoints_space_fkey;
ALTER TABLE v2.receipt_checkpoints ADD CONSTRAINT receipt_checkpoints_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id);
ALTER TABLE v2.receipt_chain_heads DROP CONSTRAINT receipt_chain_heads_space_fkey;
ALTER TABLE v2.receipt_chain_heads ADD CONSTRAINT receipt_chain_heads_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id);
ALTER TABLE v2.receipts DROP CONSTRAINT receipts_space_fkey;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id);

DROP FUNCTION v2.space_ledger(uuid);
DROP FUNCTION IF EXISTS v2.space_ledger_as_owner(uuid);
DROP TRIGGER hubs_space_ledger ON public.hubs;
DROP FUNCTION v2.hubs_space_ledger();
DROP TABLE v2.space_ledgers;

DROP INDEX v2.receipts_space_forgot_idx;

-- Receipts that use the new verbs block this, on purpose.
ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'returned', 'drafted'));
