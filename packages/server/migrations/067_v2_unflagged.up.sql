-- 067: v2_unflagged
--
-- Memax clears its own stale flag when the change is undone (the
-- stale-detection design §3.4, decision 8): a check run whose results show
-- every condition that failed holding again, on a later commit, writes an
-- `unflagged` receipt by Memax beside the flag's removal, citing the run.
-- Memax never clears a flag Dream or a date set (ledger/stale.Decide).
--
-- receipts_action_check gains unflagged. The list keeps every verb 028
-- through 066 admit; a branch that adds verbs in parallel merges by taking
-- the union of both lists (TestReceiptEnumsMatchTheSchema holds the last
-- migration to the spec's ReceiptAction enum).

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
     'conditions_changed',
     'rekeyed',
     'unflagged'));
