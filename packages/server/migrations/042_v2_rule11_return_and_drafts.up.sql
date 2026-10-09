-- 042: v2_rule11_return_and_drafts
--
-- Rule 11 ("a proposal that contradicts a decision in force is flagged
-- before anyone keeps it") closes its two remaining holes (plan 25 §5.6
-- downgrade (b), the Phase 1 gate's decision):
--
--   returned  A Write-level agent's write is kept at once when the inline
--             check finds nothing; when the judge later finds that it
--             contradicts a decision in force (strong tier confirmed), it
--             goes back to Review: kept → proposed, with the conflict
--             flag and its conflicts_with links. Only Memax does this,
--             through RecordVerdict, within a bounded window of the
--             agent's keep, and only while nothing has changed or built on
--             the write since (internal/ledger/judge.go).
--   drafted   Settling a conflict with "keep both" can narrow each side.
--             New words for a kept memory that touch another decision in
--             force wait for the judge as a draft: a version above the
--             memory's current_version, so the words in force stay as they
--             are until the resolution is applied (internal/ledger/conflict.go).
--
-- # The lifecycle guard
--
-- v2.lifecycle_transition_allowed still refuses kept → proposed, and 035
-- admits it only beside an `undid` receipt. This adds the return, which
-- the guard admits only when all of these hold in the same row change:
--
--   * the row's receipt is a `returned` receipt by Memax (actor_kind
--     memax), about this memory, written in this transaction, and the next
--     on its stream;
--   * the memory carries the conflict flag and keeps its version;
--   * the change it undoes is an agent's own: the receipt the row had
--     before is an agent's `kept` or `edited`, and it wrote the current
--     version. A person's keep or edit (or anything later) is never
--     returned.
--
-- v2.lifecycle_return_allowed is the Go side's lifecycle.ReturnAllowed.
--
-- # Judge modes
--
-- judge_verdicts.mode gains `settling`: words written to settle a conflict
-- (a proposal's narrowed version, or a kept memory's draft), checked only
-- for a contradiction with a decision in force other than the conflict's
-- other side.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains returned and drafted. The list keeps every
-- verb 028, 029, 031, 035 and 036 admit.

-- ---------------------------------------------------------------------
-- Receipt actions
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'returned', 'drafted'));

-- ---------------------------------------------------------------------
-- Judge modes
-- ---------------------------------------------------------------------

ALTER TABLE v2.judge_verdicts DROP CONSTRAINT judge_verdicts_mode_check;
ALTER TABLE v2.judge_verdicts ADD CONSTRAINT judge_verdicts_mode_check CHECK (mode IN ('proposal', 'kept', 'settling'));

-- ---------------------------------------------------------------------
-- The lifecycle guard admits the judge's return
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.lifecycle_return_allowed(from_state text, to_state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT COALESCE(from_state = 'kept' AND to_state = 'proposed', false) $$;

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

GRANT EXECUTE ON FUNCTION v2.lifecycle_return_allowed(text, text) TO memax_v2;
