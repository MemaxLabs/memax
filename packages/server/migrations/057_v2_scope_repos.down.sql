-- Revert 057: v2_scope_repos
--
-- Restores 056's receipt verbs. Receipts are append-only, so `scoped`
-- receipts written meanwhile stay, and the restored CHECK is NOT VALID.
-- Memories keep their scope.repos: it is a key in a jsonb object, which
-- the previous code reads past.

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
     'unlinked')) NOT VALID;
