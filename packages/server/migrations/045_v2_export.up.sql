-- 045: v2_export
--
-- Export (plan 25 §7.2, rule 14; Phase 2 epic 2.4): a person takes a
-- space's whole record as Markdown with frontmatter, its receipts in their
-- canonical form and the signed checkpoints (internal/export,
-- `memax export`, `memax verify-export`).
--
-- An export changes nothing in the record, but it is counted: one receipt
-- per export, `exported`, on the space's own stream (object_kind space,
-- object_ref "space"), by the person who exported it. The receipt holds no
-- words, like every receipt. Reads made to build the export are not R-
-- reads: those measure agents.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains exported: the union of 044's list and the
-- new verb. The list keeps every verb 028, 029, 031, 035, 036, 042 and 044
-- admit; a branch that adds verbs in parallel merges by taking the union
-- of both lists (TestReceiptEnumsMatchTheSchema holds the last migration
-- to the spec's ReceiptAction enum).

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
     'exported'));
