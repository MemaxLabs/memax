-- Revert 039: v2_receipt_seals
--
-- Seals are evidence derived from receipts, so they go; the receipts stay
-- (append-only), and lose only their reason commitments. Applying 039
-- again commits to every remaining reason afresh and seals from genesis.

DROP FUNCTION IF EXISTS v2.receipt_spaces();
DROP FUNCTION IF EXISTS v2.unsealed_spaces(integer);
DROP POLICY IF EXISTS receipts_seal_sweep ON v2.receipts;
DROP TABLE IF EXISTS v2.receipt_seal_cursor;
DROP TABLE IF EXISTS v2.receipt_checkpoints;
DROP TABLE IF EXISTS v2.receipt_chain_heads;

DROP INDEX IF EXISTS v2.receipts_txid_seq_idx;
DROP INDEX IF EXISTS v2.receipts_space_txid_seq_idx;
ALTER TABLE v2.receipts DROP CONSTRAINT IF EXISTS receipts_source_shape_check;
ALTER TABLE v2.receipts DROP CONSTRAINT IF EXISTS receipts_reason_commitment_check;

-- 028's functions, as they were.
CREATE OR REPLACE FUNCTION v2.redact_receipt_reasons(p_object_id uuid) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    redacted integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts
         WHERE object_id = p_object_id
           AND action = 'forgot'
           AND txid = pg_current_xact_id()
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
    ) THEN
        RAISE EXCEPTION 'object %: reasons are redacted only by a Forget in the same transaction', p_object_id
            USING ERRCODE = 'MXR02';
    END IF;
    UPDATE v2.receipts
       SET reason = NULL
     WHERE object_id = p_object_id
       AND reason IS NOT NULL
       AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    GET DIAGNOSTICS redacted = ROW_COUNT;
    RETURN redacted;
END $$;

CREATE OR REPLACE FUNCTION v2.receipts_stamp() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.txid := pg_current_xact_id();
    NEW.recorded_at := now();
    RETURN NEW;
END $$;

ALTER TABLE v2.receipts DROP COLUMN IF EXISTS reason_sha256;
ALTER TABLE v2.receipts DROP COLUMN IF EXISTS reason_salt;

-- With the columns gone, 028's guard compares the row as it was.
CREATE OR REPLACE FUNCTION v2.receipts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.receipts;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        n := NEW;
        n.reason := OLD.reason;
        IF n IS NOT DISTINCT FROM OLD AND NEW.reason IS NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'receipts are append-only: % is not allowed (only Forget may clear a reason)', TG_OP
        USING ERRCODE = 'MXR02';
END $$;

REVOKE ALL ON v2.receipts FROM memax_v2_sealer;
REVOKE USAGE ON SCHEMA v2 FROM memax_v2_sealer;

-- memax_v2_sealer is cluster-wide: other databases (test clones, another
-- environment) may still hold grants to it, and then it stays.
DO $$
BEGIN
    DROP ROLE IF EXISTS memax_v2_sealer;
EXCEPTION WHEN dependent_objects_still_exist OR insufficient_privilege OR object_in_use THEN
    RAISE NOTICE 'role memax_v2_sealer kept: %', SQLERRM;
END $$;
