-- Revert 028: v2_ledger
--
-- Drops the V2 record. Run only where losing V2 data is acceptable
-- (local, test, or before any space has switched to V2).
DROP VIEW IF EXISTS v2.notes;
DROP VIEW IF EXISTS v2.spaces;

DROP TABLE IF EXISTS v2.command_keys;
DROP TABLE IF EXISTS v2.id_counters;
DROP TABLE IF EXISTS v2.memory_links;
DROP TABLE IF EXISTS v2.memory_sources;
DROP TABLE IF EXISTS v2.sources;
DROP TABLE IF EXISTS v2.memory_versions;
DROP TABLE IF EXISTS v2.memories;
DROP TABLE IF EXISTS v2.receipts;

DROP FUNCTION IF EXISTS v2.redact_receipt_reasons(uuid);
DROP FUNCTION IF EXISTS v2.require_receipt();
DROP FUNCTION IF EXISTS v2.memories_lifecycle_guard();
DROP FUNCTION IF EXISTS v2.receipts_guard();
DROP FUNCTION IF EXISTS v2.receipts_stamp();
DROP FUNCTION IF EXISTS v2.flags_valid(text[]);
DROP FUNCTION IF EXISTS v2.lifecycle_transition_allowed(text, text);
DROP FUNCTION IF EXISTS v2.display_state(text, text[]);
