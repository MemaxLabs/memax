-- 065: v2_forget_checks (epic 3.1; the stale-detection design §6.1, rule 7)
--
-- A Forget names the check state members' machines keep (the daemon's
-- results of the memory's conditions: refs, condition ids and, for a
-- failure, the paths its words named), as one more step of its
-- propagation: `checks`, written when the memory had check results.
-- Memax's own daemon drops them on each machine when it hears of the
-- Forget, so the step's status is read, not stored: it is done once every
-- machine that checked the space before the Forget has checked it since
-- (ledger.fillTombstone). No words, and nothing new to purge.

ALTER TABLE v2.propagations DROP CONSTRAINT propagations_kind_check;
ALTER TABLE v2.propagations ADD CONSTRAINT propagations_kind_check
    CHECK (destination_kind IN ('target', 'artifacts', 'caches', 'ledger', 'attachments', 'checks'));
