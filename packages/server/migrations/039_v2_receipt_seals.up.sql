-- 039: v2_receipt_seals
--
-- Tamper evidence for receipts (plan 25 §5.3 "Hash chain, kept off the
-- write path"; epic 1.1). A River sweep finds spaces with new receipts and
-- a per-space sealer chains them, h_i = SHA-256(h_(i-1) ‖ leaf_i) with
-- leaf_i = SHA-256(0x00 ‖ canonical(receipt_i)), in (txid, seq) order and
-- only below the snapshot's xmin, so a receipt whose transaction is still
-- in flight is never skipped: it is sealed, in its place, once it commits.
-- Each run cuts a checkpoint over what it sealed: the range, an RFC 6962
-- Merkle root of its leaves, the chain hash at its end, and an Ed25519
-- signature over all of that. internal/receiptchain holds the encoding;
-- docs live there.
--
-- # Nothing here is on the write path
--
-- Writing a receipt costs one extra SHA-256 (the reason commitment, below)
-- and no lock. A per-receipt hash chained on the write path would
-- serialise every write in a space behind an advisory lock (rejected in
-- the plan).
--
-- # The reason commitment
--
-- Forget redacts receipts' reasons (v2.redact_receipt_reasons), so a
-- sealed form can't contain the reason itself. It contains reason_sha256 =
-- SHA-256(reason_salt ‖ utf8(reason)), computed by the stamp trigger when
-- the receipt is written, from a random 16-byte salt. Redaction nulls the
-- reason and the salt together and leaves the commitment, so:
--   * the chain stays valid after a Forget (the leaf never changes), and
--     the forgot receipt that authorised the redaction is itself sealed;
--   * with the salt gone, the commitment can't confirm a guess of the
--     purged words (an unsalted hash of a short reason could);
--   * while the reason exists, the verifier checks it against the
--     commitment, and a reason that vanished with no forgot receipt for
--     its object is reported.
--
-- # Who writes seals
--
-- Seals are evidence about receipts, derivable from them, not changes to
-- the record, so they carry no receipt: a seal receipt would itself need
-- sealing by the next run, which would write another, forever. Instead
-- only a separate role writes them: memax_v2_sealer (NOLOGIN, NOBYPASSRLS,
-- like memax_v2). memax_v2, the role every API transaction runs as, may
-- read checkpoints and heads but never write them, so a bug or an
-- injection in a request path can't forge or move a seal. Integrity rests
-- on the signatures and on the copies in object storage, not on grants.
--
-- # Cross-space reads
--
-- v2.unsealed_spaces (the sweep) and v2.receipt_spaces (the nightly
-- verifier) each SET app.sweep for their own execution, a SELECT-only
-- policy admits receipts while it is set, and they return space ids only.
-- Unlike v2.dirty_targets (031), the policies apply to memax_v2_sealer
-- only, so memax_v2 (every request) can't borrow them by setting app.sweep
-- itself.

-- ---------------------------------------------------------------------
-- The sealer's role
-- ---------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2_sealer') THEN
        BEGIN
            CREATE ROLE memax_v2_sealer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2_sealer' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2_sealer must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM pg_auth_members m
          JOIN pg_roles r ON r.oid = m.roleid
          JOIN pg_roles u ON u.oid = m.member
         WHERE r.rolname = 'memax_v2_sealer' AND u.rolname = current_user
    ) THEN
        BEGIN
            EXECUTE format('GRANT memax_v2_sealer TO %I', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2_sealer;

-- ---------------------------------------------------------------------
-- Receipts: the reason commitment, the source's shape, the sealer's index
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts
    ADD COLUMN reason_salt bytea,      -- random; purged with the reason at Forget
    ADD COLUMN reason_sha256 bytea;    -- SHA-256(reason_salt ‖ utf8(reason)); kept at Forget

CREATE OR REPLACE FUNCTION v2.receipts_stamp() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.txid := pg_current_xact_id();
    NEW.recorded_at := now();
    -- The commitment is the database's, never the caller's.
    IF NEW.reason IS NULL THEN
        NEW.reason_salt := NULL;
        NEW.reason_sha256 := NULL;
    ELSE
        NEW.reason_salt := uuid_send(gen_random_uuid());
        NEW.reason_sha256 := sha256(NEW.reason_salt || convert_to(NEW.reason, 'UTF8'));
    END IF;
    RETURN NEW;
END $$;

-- Receipts are immutable except that `reason` may be cleared, and clearing
-- it purges its salt too (the commitment stays, so the seal holds).
CREATE OR REPLACE FUNCTION v2.receipts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.receipts;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        n := NEW;
        n.reason := OLD.reason;
        n.reason_salt := OLD.reason_salt;
        IF n IS NOT DISTINCT FROM OLD AND NEW.reason IS NULL THEN
            NEW.reason_salt := NULL;
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'receipts are append-only: % is not allowed (only Forget may clear a reason)', TG_OP
        USING ERRCODE = 'MXR02';
END $$;

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
       SET reason = NULL, reason_salt = NULL
     WHERE object_id = p_object_id
       AND reason IS NOT NULL
       AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    GET DIAGNOSTICS redacted = ROW_COUNT;
    RETURN redacted;
END $$;

-- Existing reasons get their commitment. The owner does it, outside RLS
-- and past the append-only guard, once.
ALTER TABLE v2.receipts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE v2.receipts DISABLE TRIGGER receipts_guard;
UPDATE v2.receipts SET reason_salt = uuid_send(gen_random_uuid()) WHERE reason IS NOT NULL;
UPDATE v2.receipts SET reason_sha256 = sha256(reason_salt || convert_to(reason, 'UTF8')) WHERE reason IS NOT NULL;
ALTER TABLE v2.receipts ENABLE TRIGGER receipts_guard;
ALTER TABLE v2.receipts FORCE ROW LEVEL SECURITY;

ALTER TABLE v2.receipts
    ADD CONSTRAINT receipts_reason_commitment_check CHECK (
        (reason IS NULL OR (reason_salt IS NOT NULL AND reason_sha256 IS NOT NULL))
        AND (reason_salt IS NULL OR reason IS NOT NULL)
        AND (reason_salt IS NULL OR octet_length(reason_salt) = 16)
        AND (reason_sha256 IS NULL OR octet_length(reason_sha256) = 32)),
    -- The sealed form carries a source's kind and ref; nothing else may
    -- hide in it unsealed.
    ADD CONSTRAINT receipts_source_shape_check CHECK (
        source IS NULL
        OR (source - 'kind' - 'ref' = '{}'::jsonb
            AND jsonb_typeof(source -> 'kind') = 'string'
            AND jsonb_typeof(source -> 'ref') = 'string'));

-- The sealer reads a space's receipts after its head, in chain order; the
-- sweep reads every space's after its cursor.
CREATE INDEX receipts_space_txid_seq_idx ON v2.receipts (space_id, txid, seq);
CREATE INDEX receipts_txid_seq_idx ON v2.receipts (txid, seq);

-- ---------------------------------------------------------------------
-- Chain heads, checkpoints and the sweep's cursor
-- ---------------------------------------------------------------------

CREATE TABLE v2.receipt_chain_heads (
    space_id           uuid PRIMARY KEY,
    tenant_id          uuid NOT NULL,
    last_txid          xid8 NOT NULL,                  -- the last sealed receipt's (txid, seq): the watermark
    last_seq           bigint NOT NULL,
    position           bigint NOT NULL,                -- receipts sealed so far (0: none)
    head_sha256        bytea NOT NULL,                 -- the chain hash after them (the genesis hash at 0)
    checkpoints        bigint NOT NULL,                -- the last checkpoint's number
    sealed_at          timestamptz,
    verified_at        timestamptz,                    -- the verifier's last full check
    verified_position  bigint,
    verify_problems    integer,                        -- 0 when it found nothing wrong
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT receipt_chain_heads_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT receipt_chain_heads_position_check CHECK (position >= 0 AND checkpoints >= 0 AND last_seq >= 0),
    CONSTRAINT receipt_chain_heads_hash_check CHECK (octet_length(head_sha256) = 32),
    CONSTRAINT receipt_chain_heads_verified_check CHECK (
        (verified_at IS NULL) = (verified_position IS NULL) AND (verified_at IS NULL) = (verify_problems IS NULL))
);

COMMENT ON TABLE v2.receipt_chain_heads IS
    'Where each space''s receipt hash chain stands. Written only by memax_v2_sealer.';

CREATE TABLE v2.receipt_checkpoints (
    id               uuid PRIMARY KEY,
    tenant_id        uuid NOT NULL,
    space_id         uuid NOT NULL,
    number           bigint NOT NULL,                  -- 1, 2, 3 … per space
    position_from    bigint NOT NULL,                  -- the range, as positions in the space's chain (from 1)
    position_to      bigint NOT NULL,
    receipts         integer NOT NULL,
    first_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id  uuid NOT NULL REFERENCES v2.receipts (id),
    last_seq         bigint NOT NULL,
    last_txid        xid8 NOT NULL,                    -- bookkeeping; not signed (txids don't survive a logical restore)
    prev_sha256      bytea NOT NULL,                   -- the chain hash before the range
    chain_sha256     bytea NOT NULL,                   -- after it
    merkle_root      bytea NOT NULL,                   -- RFC 6962 over the range's leaves
    format           smallint NOT NULL,                -- the canonical encoding's version
    key_id           text,                             -- null: unsigned (no signing key configured)
    signature        bytea,                            -- Ed25519 over the checkpoint statement
    sealed_at        timestamptz NOT NULL,
    object_key       text,                             -- where the copy in object storage is, once uploaded
    uploaded_at      timestamptz,
    upload_attempts  integer NOT NULL DEFAULT 0,
    upload_error     text,
    CONSTRAINT receipt_checkpoints_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT receipt_checkpoints_number_key UNIQUE (space_id, number),
    CONSTRAINT receipt_checkpoints_from_key UNIQUE (space_id, position_from),
    CONSTRAINT receipt_checkpoints_range_check CHECK (
        number >= 1 AND position_from >= 1 AND position_to >= position_from
        AND receipts = position_to - position_from + 1),
    CONSTRAINT receipt_checkpoints_hash_check CHECK (
        octet_length(prev_sha256) = 32 AND octet_length(chain_sha256) = 32 AND octet_length(merkle_root) = 32),
    CONSTRAINT receipt_checkpoints_format_check CHECK (format = 1),
    CONSTRAINT receipt_checkpoints_signature_check CHECK (
        (key_id IS NULL) = (signature IS NULL)
        AND (key_id IS NULL OR (key_id <> '' AND char_length(key_id) <= 100))
        AND (signature IS NULL OR octet_length(signature) = 64)),
    CONSTRAINT receipt_checkpoints_upload_check CHECK (
        (object_key IS NULL) = (uploaded_at IS NULL)
        AND (object_key IS NULL OR (object_key <> '' AND char_length(object_key) <= 500))
        AND upload_attempts >= 0
        AND (upload_error IS NULL OR char_length(upload_error) <= 1000))
);

CREATE INDEX receipt_checkpoints_space_number_idx ON v2.receipt_checkpoints (space_id, number DESC);
CREATE INDEX receipt_checkpoints_pending_idx ON v2.receipt_checkpoints (space_id, number) WHERE object_key IS NULL;

COMMENT ON TABLE v2.receipt_checkpoints IS
    'Signed checkpoints of each space''s receipt chain: a range, its Merkle root and the chain hash. Written only by memax_v2_sealer.';

-- The sweep's place in the global receipt order: every receipt at or
-- before it has had its space's seal job queued.
CREATE TABLE v2.receipt_seal_cursor (
    id         boolean PRIMARY KEY DEFAULT true,
    last_txid  xid8 NOT NULL DEFAULT '0',
    last_seq   bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT receipt_seal_cursor_one_row CHECK (id)
);

INSERT INTO v2.receipt_seal_cursor DEFAULT VALUES;

-- ---------------------------------------------------------------------
-- The sweep and the verifier's listing (ids only)
-- ---------------------------------------------------------------------

-- The next p_limit receipts after the cursor whose transactions have all
-- ended (txid below this snapshot's xmin), in (txid, seq) order: returns
-- their spaces and moves the cursor past them. A receipt from a
-- transaction still in flight stops the scan at its place; the next sweep
-- takes it up after it commits. The caller queues the seal jobs in the
-- same transaction, so a failed enqueue leaves the cursor where it was.
CREATE FUNCTION v2.unsealed_spaces(p_limit integer) RETURNS TABLE (space_id uuid)
    LANGUAGE plpgsql
    SET app.sweep = 'unsealed_spaces'
    AS $$
DECLARE
    c v2.receipt_seal_cursor;
    w xid8 := pg_snapshot_xmin(pg_current_snapshot());
BEGIN
    SELECT * INTO c FROM v2.receipt_seal_cursor WHERE id FOR UPDATE;
    RETURN QUERY
    WITH batch AS (
        SELECT r.space_id, r.txid, r.seq
          FROM v2.receipts r
         WHERE (r.txid, r.seq) > (c.last_txid, c.last_seq) AND r.txid < w
         ORDER BY r.txid, r.seq
         LIMIT least(greatest(p_limit, 1), 100000)
    ), last AS (
        SELECT b.txid, b.seq FROM batch b ORDER BY b.txid DESC, b.seq DESC LIMIT 1
    ), moved AS (
        UPDATE v2.receipt_seal_cursor k
           SET last_txid = last.txid, last_seq = last.seq, updated_at = now()
          FROM last
         WHERE k.id
        RETURNING 1
    )
    SELECT DISTINCT b.space_id FROM batch b;
END $$;

COMMENT ON FUNCTION v2.unsealed_spaces(integer) IS
    'The seal sweep: spaces with receipts after the cursor and below the xmin watermark; moves the cursor past them.';

-- Every space with receipts, for the nightly verifier.
CREATE FUNCTION v2.receipt_spaces() RETURNS TABLE (space_id uuid)
    LANGUAGE sql STABLE
    SET app.sweep = 'receipt_spaces'
    AS $$ SELECT DISTINCT r.space_id FROM v2.receipts r $$;

COMMENT ON FUNCTION v2.receipt_spaces() IS
    'The nightly verifier''s listing: every space that has receipts, as ids.';

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

-- Read-only, and only inside the two functions above (see the header).
CREATE POLICY receipts_seal_sweep ON v2.receipts FOR SELECT TO memax_v2_sealer
    USING (current_setting('app.sweep', true) IN ('unsealed_spaces', 'receipt_spaces'));

ALTER TABLE v2.receipt_chain_heads ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.receipt_chain_heads FORCE ROW LEVEL SECURITY;
CREATE POLICY receipt_chain_heads_space ON v2.receipt_chain_heads
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.receipt_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.receipt_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY receipt_checkpoints_space ON v2.receipt_checkpoints
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.receipt_seal_cursor ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.receipt_seal_cursor FORCE ROW LEVEL SECURITY;
CREATE POLICY receipt_seal_cursor_sweep ON v2.receipt_seal_cursor TO memax_v2_sealer
    USING (current_setting('app.sweep', true) = 'unsealed_spaces')
    WITH CHECK (current_setting('app.sweep', true) = 'unsealed_spaces');

-- ---------------------------------------------------------------------
-- Grants. memax_v2 reads seals; only memax_v2_sealer writes them, and it
-- reads receipts (under RLS) and nothing else of the record. Neither may
-- DELETE or TRUNCATE.
-- ---------------------------------------------------------------------

GRANT SELECT ON v2.receipt_chain_heads, v2.receipt_checkpoints TO memax_v2;

GRANT SELECT ON v2.receipts TO memax_v2_sealer;
GRANT SELECT, INSERT ON v2.receipt_chain_heads TO memax_v2_sealer;
GRANT UPDATE (last_txid, last_seq, position, head_sha256, checkpoints, sealed_at,
              verified_at, verified_position, verify_problems, updated_at)
    ON v2.receipt_chain_heads TO memax_v2_sealer;
GRANT SELECT, INSERT ON v2.receipt_checkpoints TO memax_v2_sealer;
GRANT UPDATE (object_key, uploaded_at, upload_attempts, upload_error) ON v2.receipt_checkpoints TO memax_v2_sealer;
GRANT SELECT, UPDATE (last_txid, last_seq, updated_at) ON v2.receipt_seal_cursor TO memax_v2_sealer;

REVOKE ALL ON FUNCTION v2.unsealed_spaces(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.receipt_spaces() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.unsealed_spaces(integer) TO memax_v2_sealer;
GRANT EXECUTE ON FUNCTION v2.receipt_spaces() TO memax_v2_sealer;
