-- Revert 053: v2_product_metrics. Nothing is stored: the metrics are
-- computed when asked, so rolling back drops the two functions, their
-- policies and the role that runs them.

DROP POLICY IF EXISTS reads_product_metrics ON v2.reads;
DROP POLICY IF EXISTS imports_product_metrics ON v2.imports;
DROP POLICY IF EXISTS agent_connections_product_metrics ON v2.agent_connections;
DROP POLICY IF EXISTS receipts_product_metrics ON v2.receipts;

DROP FUNCTION IF EXISTS v2.review_health(timestamptz, timestamptz, timestamptz);
DROP FUNCTION IF EXISTS v2.review_health_as_owner(timestamptz, timestamptz, timestamptz);
DROP FUNCTION IF EXISTS v2.product_metrics(timestamptz, timestamptz, interval, timestamptz);
DROP FUNCTION IF EXISTS v2.product_metrics_as_owner(timestamptz, timestamptz, interval, timestamptz);

REVOKE USAGE ON SCHEMA v2 FROM memax_v2_metrics;

-- The role is cluster-wide: other databases (test clones, another
-- environment) may still hold grants to it, and then it stays.
DO $$
BEGIN
    DROP ROLE IF EXISTS memax_v2_metrics;
EXCEPTION WHEN dependent_objects_still_exist OR insufficient_privilege OR object_in_use THEN
    RAISE NOTICE 'role memax_v2_metrics kept: %', SQLERRM;
END $$;
