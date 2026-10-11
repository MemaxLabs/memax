-- 060: v2_checks, reverted: the check state goes, and retiring a space
-- and Forget's check at commit are 056's and 044's again.

DROP TABLE v2.condition_checks;
DROP FUNCTION v2.condition_checks_forgotten_guard();
DROP TABLE v2.check_runs;

CREATE OR REPLACE FUNCTION v2.memories_forgotten_words() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM v2.memories m WHERE m.id = NEW.id AND m.lifecycle = 'forgotten') THEN
        RETURN NULL; -- deleted since (a retired space)
    END IF;
    IF EXISTS (SELECT 1 FROM v2.memory_versions v WHERE v.memory_id = NEW.id AND v.statement IS NOT NULL) THEN
        RAISE EXCEPTION 'memory %: forgotten, but a version still holds its words', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget purges every version (internal/ledger/forget.go).';
    END IF;
    IF EXISTS (
        SELECT 1 FROM v2.memory_sources ms JOIN v2.sources s ON s.id = ms.source_id
         WHERE ms.memory_id = NEW.id AND (s.quote IS NOT NULL OR s.uri IS NOT NULL OR s.content_hash IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'memory %: forgotten, but a source still holds a quote, a URI or a hash', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget purges every source (internal/ledger/forget.go).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM v2.tombstones t WHERE t.object_id = NEW.id) THEN
        RAISE EXCEPTION 'memory %: forgotten without a tombstone', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget writes the tombstone beside the forgot receipt.';
    END IF;
    RETURN NULL;
END $$;

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
    DELETE FROM v2.machines WHERE space_id = p_space;
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
