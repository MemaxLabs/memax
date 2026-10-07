-- 043 down: v2_imports
--
-- Drops the import bookkeeping and the import conflict groups. The
-- proposals an import wrote, their sources, flags, links and receipts are
-- the record and stay.

DROP TABLE IF EXISTS v2.import_conflicts;
DROP FUNCTION IF EXISTS v2.import_conflicts_guard();
DROP FUNCTION IF EXISTS v2.require_import_conflict_receipt();
DROP TABLE IF EXISTS v2.import_items;
DROP TABLE IF EXISTS v2.imports;
