-- 052: v2_undo_remember
--
-- Undo for a person's own Remember (Review's ⌘Z and Remember's toast).
-- Remember writes an undo entry like Keep, Reject and Edit do; undoing it
-- withdraws the memory: it ends rejected, with an `undid` receipt whose
-- source is the Remember's receipt.
--
--   undo_entries.command   gains 'remember'
--
-- # The lifecycle guard
--
-- kept → rejected exists only as this undo. v2.lifecycle_transition_allowed
-- still refuses it, so "a kept memory is never rejected" holds for every
-- other write (a kept memory fades, or is forgotten). The guard admits it
-- only beside an `undid` receipt written in the same transaction about the
-- same memory whose source is the receipt that created the memory: the
-- Remember being undone, and nothing later. (proposed → rejected, a
-- person's Remember that went to Review, is an ordinary transition.)

ALTER TABLE v2.undo_entries DROP CONSTRAINT undo_entries_command_check;
ALTER TABLE v2.undo_entries ADD CONSTRAINT undo_entries_command_check
    CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold', 'remember'));

CREATE OR REPLACE FUNCTION v2.lifecycle_undo_allowed(from_state text, to_state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT (from_state, to_state) IN (('kept', 'proposed'), ('rejected', 'proposed'), ('kept', 'rejected')) $$;

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
              AND r.txid = pg_current_xact_id()
              -- 052: kept → rejected undoes the Remember that created it.
              AND (NOT (from_state = 'kept' AND NEW.lifecycle = 'rejected')
                   OR (r.source ->> 'kind' = 'receipt' AND r.source ->> 'ref' = NEW.created_receipt_id::text)))
    THEN
        RETURN NEW;
    END IF;
    -- 042: the judge returns a Write-level agent's write to Review.
    IF TG_OP = 'UPDATE' AND v2.lifecycle_return_allowed(from_state, NEW.lifecycle) THEN
        IF 'conflict' = ANY (NEW.flags)
           AND NEW.current_version = OLD.current_version
           AND NEW.stream_version = OLD.stream_version + 1
           AND EXISTS (
               SELECT 1 FROM v2.receipts r
                WHERE r.id = NEW.last_receipt_id
                  AND r.action = 'returned'
                  AND r.actor_kind = 'memax'
                  AND r.object_id = NEW.id
                  AND r.stream_version = NEW.stream_version
                  AND r.txid = pg_current_xact_id())
           AND EXISTS (
               SELECT 1 FROM v2.receipts p
                 JOIN v2.memory_versions v ON v.memory_id = p.object_id AND v.receipt_id = p.id
                WHERE p.id = OLD.last_receipt_id
                  AND p.object_id = NEW.id
                  AND p.actor_kind = 'agent'
                  AND p.action IN ('kept', 'edited')
                  AND v.version = OLD.current_version)
        THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'memory %: lifecycle % -> % is not allowed', NEW.id, COALESCE(from_state, '(new)'), NEW.lifecycle
        USING ERRCODE = 'MXL01';
END $$;
