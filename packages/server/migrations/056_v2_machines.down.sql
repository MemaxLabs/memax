-- Revert 056: v2_machines
--
-- Restores 044's v2.retire_space, 031's target checks and 048's receipt
-- vocabulary. Home targets go with their runs and drift evidence (files on
-- machines this schema can no longer name; the daemon there keeps what it
-- wrote), then the machines. Receipts are append-only, so the machines'
-- and home targets' receipts stay, and the restored CHECKs are NOT VALID.

SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey DEFERRED;
DELETE FROM v2.target_observations o USING v2.targets t WHERE o.target_id = t.id AND t.root = 'home';
DELETE FROM v2.compile_runs r USING v2.targets t WHERE r.target_id = t.id AND t.root = 'home';
DELETE FROM v2.targets WHERE root = 'home';
SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey IMMEDIATE;

CREATE OR REPLACE FUNCTION v2.retire_space(p_space uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    IF NOT (p_space = ANY ((SELECT v2.current_space_ids())::uuid[])) THEN
        RAISE EXCEPTION 'space %: not in this transaction''s scope', p_space USING ERRCODE = 'MXR02';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.object_id = p_space AND r.object_kind = 'space' AND r.action = 'forgot'
           AND r.txid = pg_current_xact_id() AND r.space_id = p_space
    ) THEN
        RAISE EXCEPTION 'space %: a space is retired only by a Forget of the whole space in the same transaction', p_space
            USING ERRCODE = 'MXR02';
    END IF;
    IF EXISTS (SELECT 1 FROM v2.memories m WHERE m.space_id = p_space AND m.lifecycle <> 'forgotten') THEN
        RAISE EXCEPTION 'space %: forget every memory before retiring it', p_space USING ERRCODE = 'MXR02';
    END IF;
    SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey DEFERRED;
    -- memax init's uploads (043): bookkeeping that refers to the hub and
    -- the memories, so it goes first.
    DELETE FROM v2.import_conflicts WHERE space_id = p_space;
    DELETE FROM v2.import_items WHERE space_id = p_space;
    DELETE FROM v2.imports WHERE space_id = p_space;
    DELETE FROM v2.memory_embeddings WHERE space_id = p_space;
    DELETE FROM v2.judge_verdicts WHERE space_id = p_space;
    DELETE FROM v2.memory_links WHERE space_id = p_space;
    DELETE FROM v2.memory_sources WHERE space_id = p_space;
    DELETE FROM v2.sources WHERE space_id = p_space;
    DELETE FROM v2.forget_requests WHERE space_id = p_space;
    DELETE FROM v2.decision_gates WHERE space_id = p_space;
    DELETE FROM v2.undo_entries WHERE space_id = p_space;
    DELETE FROM v2.target_observations WHERE space_id = p_space;
    DELETE FROM v2.compile_runs WHERE space_id = p_space;
    DELETE FROM v2.targets WHERE space_id = p_space;
    DELETE FROM v2.brief_versions WHERE space_id = p_space;
    DELETE FROM v2.briefs WHERE space_id = p_space;
    DELETE FROM v2.memory_versions WHERE space_id = p_space;
    DELETE FROM v2.memories WHERE space_id = p_space;
    DELETE FROM v2.command_keys WHERE space_id = p_space;
    DELETE FROM v2.agent_connection_spaces WHERE space_id = p_space;
    DELETE FROM v2.reads WHERE space_id = p_space;
    DELETE FROM v2.read_rollups WHERE space_id = p_space;
    UPDATE v2.space_ledgers SET retired_at = now() WHERE space_id = p_space;
END $$;
REVOKE ALL ON FUNCTION v2.retire_space(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.retire_space(uuid) TO memax_v2;

DROP TRIGGER targets_personal_space ON v2.targets;
DROP INDEX v2.targets_machine_idx;
ALTER TABLE v2.targets DROP CONSTRAINT targets_path_key;
ALTER TABLE v2.targets ADD CONSTRAINT targets_path_key UNIQUE NULLS NOT DISTINCT (space_id, kind, path);
ALTER TABLE v2.targets DROP CONSTRAINT targets_root_kind_check;
ALTER TABLE v2.targets DROP CONSTRAINT targets_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_kind_check CHECK (kind IN
    ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules'));
ALTER TABLE v2.targets DROP CONSTRAINT targets_machine_fkey;
ALTER TABLE v2.targets DROP CONSTRAINT targets_machine_check;
ALTER TABLE v2.targets DROP CONSTRAINT targets_root_check;
ALTER TABLE v2.targets DROP COLUMN machine_id;
ALTER TABLE v2.targets DROP COLUMN root;

DROP TABLE v2.machines;
DROP FUNCTION v2.require_machine_receipt();
DROP FUNCTION v2.require_personal_space();
DROP FUNCTION v2.home_path_valid(text, text);

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'returned', 'drafted',
     'purged', 'forget_requested', 'forget_declined',
     'exported',
     'published', 'folded',
     'noted', 'switched', 'switched_back')) NOT VALID;

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_object_kind_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_object_kind_check CHECK (object_kind IN
    ('memory', 'note', 'brief', 'target', 'compile', 'handoff', 'gate', 'dream', 'agent', 'space')) NOT VALID;
