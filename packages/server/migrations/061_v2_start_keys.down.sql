-- Revert 061: v2_start_keys
--
-- The functions go. Start keys already made stay in api_keys (V1 reads
-- compile:read only once this release knows it; an older one refuses a key
-- with no permission it knows). Receipts are append-only, so any rekeyed
-- receipts stay, and the restored CHECK is NOT VALID.

DROP FUNCTION v2.rekey_start_key(uuid, text, text, timestamptz);
DROP FUNCTION v2.mint_start_key(uuid, text, text, text, timestamptz);

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
     'scoped',
     'conditions_changed')) NOT VALID;
