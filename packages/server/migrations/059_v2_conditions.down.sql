-- Revert 059: v2_conditions
--
-- Conditions go back to any JSON array, which typed conditions still are,
-- so nothing is rewritten. Receipts are append-only, so any
-- conditions_changed receipts stay, and the restored CHECK is NOT VALID.

ALTER TABLE v2.memories DROP CONSTRAINT memories_conditions_check;
ALTER TABLE v2.memories ADD CONSTRAINT memories_conditions_check CHECK (jsonb_typeof(conditions) = 'array');

DROP FUNCTION v2.conditions_valid(jsonb);
DROP FUNCTION v2.condition_line_ok(text, integer);
DROP FUNCTION v2.condition_glob_ok(text);
DROP FUNCTION v2.condition_path_ok(text);

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
     'noted', 'switched', 'switched_back',
     'unlinked',
     'scoped')) NOT VALID;
