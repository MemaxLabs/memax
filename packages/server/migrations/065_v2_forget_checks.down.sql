-- Revert 065: v2_forget_checks
--
-- The machines' check-state steps go (bookkeeping without words: what a
-- tombstone said about them is read from the check runs, which stay).

DELETE FROM v2.propagations WHERE destination_kind = 'checks';
ALTER TABLE v2.propagations DROP CONSTRAINT propagations_kind_check;
ALTER TABLE v2.propagations ADD CONSTRAINT propagations_kind_check
    CHECK (destination_kind IN ('target', 'artifacts', 'caches', 'ledger', 'attachments'));
