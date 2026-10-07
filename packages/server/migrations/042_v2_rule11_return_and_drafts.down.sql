-- Revert 042: v2_rule11_return_and_drafts
--
-- Restores 035's lifecycle guard, 035's judge modes and 036's receipt
-- verbs exactly. Receipts (returned, drafted) and verdicts (settling) the
-- new commands wrote stay, because both are append-only, so the restored
-- CHECKs are NOT VALID. Memories the judge returned to Review stay
-- proposals in conflict: a person settles them as before.

-- The lifecycle guard as 035 wrote it.
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
DROP FUNCTION IF EXISTS v2.lifecycle_return_allowed(text, text);

ALTER TABLE v2.judge_verdicts DROP CONSTRAINT judge_verdicts_mode_check;
ALTER TABLE v2.judge_verdicts ADD CONSTRAINT judge_verdicts_mode_check CHECK (mode IN ('proposal', 'kept')) NOT VALID;

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn')) NOT VALID;
