-- 058: v2_verify (plan 25 §5.9, epic 3.1; the stale-detection design §3.3)
--
-- A person answers a stale flag with Verify: "still true" writes a
-- `verified` receipt (in 028's list) and the flag clears; "update" writes
-- `edited` (the new words) and then `verified`. Either is undoable by the
-- same person within the undo window, as one command, so the undo journal
-- gains the command `verify`. Its entries also hold the memory's date to
-- check again before the command (stale_after), which a Verify moves or
-- clears; a timestamp, never words.

ALTER TABLE v2.undo_entries DROP CONSTRAINT undo_entries_command_check;
ALTER TABLE v2.undo_entries ADD CONSTRAINT undo_entries_command_check
    CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold', 'remember', 'verify'));
