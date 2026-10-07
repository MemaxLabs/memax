-- Revert 038: v2_reads
--
-- Reads are usage data, not the record: rolling back drops them (and
-- every monthly partition with the parent).

DROP POLICY IF EXISTS agent_connections_metrics ON v2.agent_connections;
DROP FUNCTION IF EXISTS v2.read_metrics(date);
DROP FUNCTION IF EXISTS v2.read_metrics_as_owner(date);
DROP FUNCTION IF EXISTS v2.prune_reads(timestamptz);
DROP FUNCTION IF EXISTS v2.prune_reads_as_owner(timestamptz);
DROP FUNCTION IF EXISTS v2.ensure_reads_partitions(timestamptz, integer);
DROP INDEX IF EXISTS v2.compile_runs_refs_idx;
DROP TABLE IF EXISTS v2.read_rollups;
DROP TABLE IF EXISTS v2.reads;
