-- Revert 062: v2_undo_scope
--
-- Scope changes' undo entries go (bookkeeping without words); where a
-- memory applies stays as the person left it, with its receipts.

DELETE FROM v2.undo_entries WHERE command = 'scope';
ALTER TABLE v2.undo_entries DROP CONSTRAINT undo_entries_command_check;
ALTER TABLE v2.undo_entries ADD CONSTRAINT undo_entries_command_check
    CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold', 'remember', 'verify'));
