-- 062: v2_undo_scope (personal-layer design §3.3)
--
-- A person's change to where a memory applies (ScopeMemory, a `scoped`
-- receipt) is undoable by the same person within the undo window, as
-- their Keep, Edit and Verify are, so the undo journal gains the command
-- `scope`. Its entries hold the memory's scope before the change (paths,
-- agents and repositories: globs, agent names and owner/name keys, never
-- words).

ALTER TABLE v2.undo_entries DROP CONSTRAINT undo_entries_command_check;
ALTER TABLE v2.undo_entries ADD CONSTRAINT undo_entries_command_check
    CHECK (command IN ('keep', 'reject', 'edit', 'resolve_conflict', 'judge_fold', 'remember', 'verify', 'scope'));
