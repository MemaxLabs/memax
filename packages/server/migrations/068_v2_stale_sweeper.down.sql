DROP INDEX IF EXISTS v2.memories_date_due_idx;

DROP POLICY IF EXISTS memories_stale_sweep ON v2.memories;
DROP POLICY IF EXISTS receipts_stale_sweep ON v2.receipts;
REVOKE ALL ON v2.memories, v2.receipts FROM memax_v2_stale_sweeper;
REVOKE ALL ON public.hubs FROM memax_v2_stale_sweeper;
REVOKE USAGE ON SCHEMA v2 FROM memax_v2_stale_sweeper;
REVOKE USAGE ON SCHEMA public FROM memax_v2_stale_sweeper;
-- The role is cluster-wide (other databases' migrations may use it), so
-- it stays, with no privileges here.
