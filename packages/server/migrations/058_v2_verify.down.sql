-- Revert 058: v2_verify
--
-- Verify's undo entries go (bookkeeping without words); what a person
-- verified stays verified, with its receipts.

DELETE FROM v2.undo_entries WHERE command = 'verify';
ALTER TABLE v2.undo_entries DROP CONSTRAINT undo_entries_command_check;
ALTER TABLE v2.undo_entries ADD CONSTRAINT undo_entries_command_check
    CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold', 'remember'));
