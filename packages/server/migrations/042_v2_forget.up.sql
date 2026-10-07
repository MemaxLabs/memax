-- 042: v2_forget
--
-- Forget (plan 25 §5.13, rule 7; Phase 2 epic 2.4), and account and space
-- deletion through the ledger.
--
--   tombstones       what Forget did, one per forgotten object (a memory, or
--                    a whole space): who asked and when, what went with it,
--                    and where its propagation stands. IDs, refs and counts;
--                    never words.
--   propagations     one row per destination of a Forget (each target
--                    recompiled, the old artifacts re-rendered, the caches,
--                    the forget ledger's copy): the tombstone's step list.
--   agent_notices    "tell every agent on its next read": per agent
--                    connection, delivered once in its next MCP response.
--   forget_requests  an agent's memax_forget: a request a person confirms
--                    (Forget) or declines on the web.
--   space_ledgers    every space's id and tenant, kept after the space is
--                    deleted: receipts, seals and tombstones refer to it, so
--                    V1 can delete a hub once Forget has retired its V2 rows.
--
-- # What Forget purges, in its one transaction (internal/ledger/forget.go)
--
-- The statement of every version (memory_versions.statement); the
-- sources' quotes, URIs, refs, locators and content hashes; the decision
-- fields, conditions and scope (memories_forgotten_purged_check now says
-- so); the derived columns (search, content_sha256, minhash_bands); the
-- embeddings (037's triggers); the receipts' reasons and salts
-- (v2.redact_receipt_reasons); the judge's rationale and merged statement
-- on every verdict that names the memory, on either side of the pair; the
-- stored idempotency request hashes; a gate's question, context and
-- options when the memory is its answer; and, in the Brief, every prose
-- line that cites it (the current version gets a new B- without it, and
-- older versions keep the line's citations without its words). A deferred
-- constraint trigger (memories_forgotten_words) refuses the commit if a
-- forgotten memory still has a version with words, a source with a quote
-- or a URI, or no tombstone.
--
-- # Which receipts allow which purge
--
--   * The memory's own rows: its `forgot` receipt (028's rule).
--   * Judge verdicts: a receipt about the judged memory, the related memory
--     or any candidate (require_receipt now lists all three), so the
--     forgotten memory's `forgot` receipt covers the verdicts of the other
--     side of every pair without a receipt on it.
--   * A gate: a `forgot` receipt about the gate itself (its words are
--     forgotten with the decision it became), which also lets
--     v2.redact_receipt_reasons clear its receipts' reasons.
--   * Older Brief versions: a receipt about the Brief (the new version's
--     `revised`, or `purged` when the current version didn't cite it), and
--     only beside a `forgot` receipt in the same transaction
--     (brief_versions_purge_guard).
--   * Drift evidence (a target's observations): a `purged` receipt about the
--     target.
--   * Sources: only beside a `forgot` receipt (sources_purge_guard).
--
-- # Space deletion
--
-- Foreign keys from v2 to public.hubs blocked V1's DELETE of a hub once it
-- held V2 records. Now receipts, chain heads and checkpoints refer to
-- v2.space_ledgers instead (one row per hub, filled by a trigger on
-- public.hubs and backfilled here), and v2.retire_space deletes every other
-- V2 row of a space that a `forgot` receipt on the space, written in the
-- same transaction, forgot entirely. What remains is content-free: the
-- receipts (reasons redacted), the seals, the tombstones and the notices,
-- so the chain still verifies (cmd/v2-verify-receipts), and the forget
-- ledger can be re-applied after a restore (cmd/v2-reapply-forgets).
--
-- # Receipt vocabulary
--
-- receipts_action_check gains purged (a forgotten memory's words left
-- another object: drift evidence on a target, an older Brief version),
-- forget_requested (an agent asked a person to forget a memory) and
-- forget_declined (a person kept it instead). forgot was in 028's list.
-- The list keeps every verb 028, 029, 031, 035 and 036 admit.

-- ---------------------------------------------------------------------
-- Receipt actions
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'purged', 'forget_requested', 'forget_declined'));

-- Forget finds a space's forgets by action.
CREATE INDEX receipts_space_forgot_idx ON v2.receipts (space_id, seq DESC) WHERE action = 'forgot';

-- ---------------------------------------------------------------------
-- Space ledgers: a space's identity, kept after the space is gone
-- ---------------------------------------------------------------------

CREATE TABLE v2.space_ledgers (
    space_id   uuid PRIMARY KEY,
    tenant_id  uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    retired_at timestamptz,                      -- the space was deleted: only receipts, seals and tombstones remain
    CONSTRAINT space_ledgers_space_tenant_key UNIQUE (space_id, tenant_id)
);

COMMENT ON TABLE v2.space_ledgers IS
    'Every space''s id and tenant. Receipts, seals and tombstones refer to it, so they outlive the space''s hub row.';

INSERT INTO v2.space_ledgers (space_id, tenant_id) SELECT id, tenant_id FROM public.hubs;

-- Every new hub gets its ledger row, and a hub deleted with nothing in its
-- ledger takes the row with it. A hub whose ledger holds receipts keeps the
-- row (the foreign key refuses the delete, and the refusal is swallowed).
CREATE FUNCTION v2.hubs_space_ledger() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET app.sweep = 'space_ledger_hub'
    AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO v2.space_ledgers (space_id, tenant_id) VALUES (NEW.id, NEW.tenant_id)
        ON CONFLICT (space_id) DO NOTHING;
        RETURN NULL;
    END IF;
    BEGIN
        DELETE FROM v2.space_ledgers WHERE space_id = OLD.id AND retired_at IS NULL;
    EXCEPTION WHEN foreign_key_violation THEN
        NULL; -- receipts, seals or tombstones refer to it: it stays
    END;
    RETURN NULL;
END $$;

REVOKE ALL ON FUNCTION v2.hubs_space_ledger() FROM PUBLIC;

CREATE TRIGGER hubs_space_ledger
    AFTER INSERT OR DELETE ON public.hubs
    FOR EACH ROW EXECUTE FUNCTION v2.hubs_space_ledger();

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_space_fkey;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id);
ALTER TABLE v2.receipt_chain_heads DROP CONSTRAINT receipt_chain_heads_space_fkey;
ALTER TABLE v2.receipt_chain_heads ADD CONSTRAINT receipt_chain_heads_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id);
ALTER TABLE v2.receipt_checkpoints DROP CONSTRAINT receipt_checkpoints_space_fkey;
ALTER TABLE v2.receipt_checkpoints ADD CONSTRAINT receipt_checkpoints_space_fkey
    FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id);

-- A space's tenant, for the sealer and the verifier when the space's hub
-- is gone (ledger.SpaceScope). Ids only.
CREATE FUNCTION v2.space_ledger(p_space uuid)
    RETURNS TABLE (tenant_id uuid, retired_at timestamptz)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET app.sweep = 'space_ledger'
    AS $$ SELECT l.tenant_id, l.retired_at FROM v2.space_ledgers l WHERE l.space_id = p_space $$;

REVOKE ALL ON FUNCTION v2.space_ledger(uuid) FROM PUBLIC;

-- ---------------------------------------------------------------------
-- Tombstones
-- ---------------------------------------------------------------------

CREATE TABLE v2.tombstones (
    id           uuid PRIMARY KEY,                  -- uuidv7, from Go
    tenant_id    uuid NOT NULL,
    space_id     uuid NOT NULL,
    op_id        uuid NOT NULL,                     -- the Forget it belongs to: the primary tombstone (op_id = id)
    object_kind  text NOT NULL,                     -- memory | space
    object_id    uuid NOT NULL,                     -- the memory (no FK: a retired space's memories are deleted)
    object_ref   text NOT NULL,                     -- "M-0201", or "space"
    carried      text,                              -- why it went with the primary: folded | updates | cites | space
    note         text,                              -- the person's own note, which stays (the confirmation says so)
    by_kind      text NOT NULL,                     -- person | memax (a re-applied forget)
    by_id        uuid,
    requested_by uuid,                              -- the agent connection whose request led to it, if any
    via          text NOT NULL,
    receipt_id   uuid NOT NULL REFERENCES v2.receipts (id), -- the forgot receipt
    forgotten_at timestamptz NOT NULL,
    kept_at      timestamptz,                       -- when it was first kept, if it ever was
    reads_before integer NOT NULL DEFAULT 0,        -- reads before it was forgotten, directly and through compiles
    gone         jsonb NOT NULL DEFAULT '{}'::jsonb, -- counts: versions, sources, embeddings, verdicts, gates, files
    status       text NOT NULL DEFAULT 'propagating', -- propagating | done
    propagation  jsonb NOT NULL DEFAULT '{}'::jsonb, -- the job's summary: targets, artifacts, caches, notices
    completed_at timestamptz,
    reapplied_at timestamptz,                       -- set when the forget ledger re-applied it after a restore
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tombstones_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id),
    CONSTRAINT tombstones_op_fkey FOREIGN KEY (op_id) REFERENCES v2.tombstones (id),
    CONSTRAINT tombstones_object_key UNIQUE (object_id),
    CONSTRAINT tombstones_kind_check CHECK (object_kind IN ('memory', 'space')),
    CONSTRAINT tombstones_ref_check CHECK (object_ref <> '' AND char_length(object_ref) <= 32),
    CONSTRAINT tombstones_carried_check CHECK (carried IS NULL OR carried IN ('folded', 'updates', 'cites', 'space')),
    CONSTRAINT tombstones_primary_check CHECK ((op_id = id) = (carried IS NULL)),
    CONSTRAINT tombstones_note_check CHECK (note IS NULL OR (note <> '' AND char_length(note) <= 500)),
    CONSTRAINT tombstones_by_check CHECK (by_kind IN ('person', 'memax') AND (by_kind <> 'person' OR by_id IS NOT NULL)),
    CONSTRAINT tombstones_status_check CHECK (status IN ('propagating', 'done')),
    CONSTRAINT tombstones_done_check CHECK ((status = 'done') = (completed_at IS NOT NULL)),
    CONSTRAINT tombstones_gone_check CHECK (jsonb_typeof(gone) = 'object' AND jsonb_typeof(propagation) = 'object'),
    CONSTRAINT tombstones_reads_check CHECK (reads_before >= 0)
);

CREATE INDEX tombstones_space_idx ON v2.tombstones (space_id, forgotten_at DESC, id DESC);
CREATE INDEX tombstones_op_idx ON v2.tombstones (op_id);

COMMENT ON TABLE v2.tombstones IS
    'What Forget did: the object''s id and ref, who asked and when, and where propagation stands. Never words. The forget ledger re-applied after a restore.';

-- A tombstone is written beside its object's forgot receipt, in the same
-- transaction. Its status and summary move later without one: they are
-- bookkeeping about delivery, like a gate's delivered_at.
CREATE FUNCTION v2.require_tombstone_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = NEW.receipt_id
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_id = NEW.object_id
           AND r.action = 'forgot'
    ) THEN
        RAISE EXCEPTION 'v2.tombstones: INSERT without a forgot receipt written in the same transaction'
            USING ERRCODE = 'MXR01',
                  HINT = 'A tombstone is written by internal/ledger''s Forget, beside the forgot receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER tombstones_require_receipt
    AFTER INSERT ON v2.tombstones
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_tombstone_receipt();

-- ---------------------------------------------------------------------
-- Propagations: the tombstone's step list
-- ---------------------------------------------------------------------

CREATE TABLE v2.propagations (
    id               uuid PRIMARY KEY,
    op_id            uuid NOT NULL REFERENCES v2.tombstones (id),
    tenant_id        uuid NOT NULL,
    space_id         uuid NOT NULL,
    destination_kind text NOT NULL,                 -- target | artifacts | caches | ledger
    destination_id   uuid,                          -- the target, for a target step
    label            text,                          -- the target's label ("AGENTS.md"); no words
    status           text NOT NULL DEFAULT 'pending', -- pending | done | held | stopped | failed
    detail           jsonb NOT NULL DEFAULT '{}'::jsonb, -- {generation, compile, delivery, kind, count, keys}: ids, refs and counts
    done_at          timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT propagations_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id),
    CONSTRAINT propagations_destination_key UNIQUE NULLS NOT DISTINCT (op_id, destination_kind, destination_id),
    CONSTRAINT propagations_kind_check CHECK (destination_kind IN ('target', 'artifacts', 'caches', 'ledger')),
    CONSTRAINT propagations_target_check CHECK ((destination_kind = 'target') = (destination_id IS NOT NULL)),
    CONSTRAINT propagations_status_check CHECK (status IN ('pending', 'done', 'held', 'stopped', 'failed')),
    CONSTRAINT propagations_label_check CHECK (label IS NULL OR char_length(label) <= 300),
    CONSTRAINT propagations_detail_check CHECK (jsonb_typeof(detail) = 'object')
);

CREATE INDEX propagations_space_idx ON v2.propagations (space_id);

COMMENT ON TABLE v2.propagations IS
    'One row per destination of a Forget: each target recompiled, the old artifacts re-rendered, the caches purged, the forget ledger copied. Bookkeeping; no words.';

-- ---------------------------------------------------------------------
-- Agent notices: tell every agent on its next read
-- ---------------------------------------------------------------------

CREATE TABLE v2.agent_notices (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL,
    space_id      uuid NOT NULL,
    connection_id uuid NOT NULL,                    -- the agent connection to tell
    person_id     uuid NOT NULL,                    -- the person it works for (RLS)
    op_id         uuid NOT NULL REFERENCES v2.tombstones (id),
    kind          text NOT NULL,                    -- forgotten | space_forgotten
    refs          text[] NOT NULL DEFAULT '{}',     -- what was forgotten ("M-0201"); empty for a whole space
    read_it       boolean NOT NULL,                 -- it read the memory (else: it is connected to the space)
    created_at    timestamptz NOT NULL DEFAULT now(),
    delivered_at  timestamptz,                      -- when it was told; set once, without a receipt
    delivered_via text,                             -- the MCP tool (or api) the notice rode on
    CONSTRAINT agent_notices_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES v2.space_ledgers (space_id, tenant_id),
    CONSTRAINT agent_notices_op_key UNIQUE (connection_id, op_id),
    CONSTRAINT agent_notices_kind_check CHECK (kind IN ('forgotten', 'space_forgotten')),
    CONSTRAINT agent_notices_refs_check CHECK (cardinality(refs) <= 1000),
    CONSTRAINT agent_notices_via_check CHECK (delivered_via IS NULL OR char_length(delivered_via) <= 64),
    CONSTRAINT agent_notices_delivered_check CHECK ((delivered_at IS NULL) = (delivered_via IS NULL))
);

-- What a connection hasn't been told yet: every MCP response asks.
CREATE INDEX agent_notices_pending_idx ON v2.agent_notices (connection_id, created_at) WHERE delivered_at IS NULL;
CREATE INDEX agent_notices_op_idx ON v2.agent_notices (op_id);

COMMENT ON TABLE v2.agent_notices IS
    'Forget''s notices: each agent connection that read a forgotten memory (or is connected to its space) is told once, on its next MCP response. Refs only.';

-- A notice is delivered once.
CREATE FUNCTION v2.agent_notices_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.delivered_at IS NOT NULL
       OR (NEW.id, NEW.tenant_id, NEW.space_id, NEW.connection_id, NEW.person_id, NEW.op_id, NEW.kind, NEW.refs,
           NEW.read_it, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.tenant_id, OLD.space_id, OLD.connection_id, OLD.person_id, OLD.op_id, OLD.kind, OLD.refs,
           OLD.read_it, OLD.created_at)
    THEN
        RAISE EXCEPTION 'agent notice %: a notice is delivered once, and nothing else about it changes', OLD.id
            USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER agent_notices_guard
    BEFORE UPDATE ON v2.agent_notices
    FOR EACH ROW EXECUTE FUNCTION v2.agent_notices_guard();

-- ---------------------------------------------------------------------
-- Forget requests: an agent asks, a person forgets or keeps it
-- ---------------------------------------------------------------------

CREATE TABLE v2.forget_requests (
    id                 uuid PRIMARY KEY,
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    memory_id          uuid NOT NULL,
    requested_by       uuid NOT NULL,               -- the agent connection (the receipt's actor_id)
    agent              text,                        -- its agent slug
    session_ref        text,
    status             text NOT NULL DEFAULT 'waiting', -- waiting | forgotten | declined
    decided_by         uuid,                        -- the person who forgot or kept it
    decided_at         timestamptz,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT forget_requests_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT forget_requests_status_check CHECK (status IN ('waiting', 'forgotten', 'declined')),
    CONSTRAINT forget_requests_decided_check CHECK ((status = 'waiting') = (decided_at IS NULL)),
    CONSTRAINT forget_requests_agent_check CHECK (agent IS NULL OR (agent <> '' AND char_length(agent) <= 64)),
    CONSTRAINT forget_requests_session_check CHECK (session_ref IS NULL OR char_length(session_ref) <= 255)
);

-- One waiting request per memory and agent: asking again is the same request.
CREATE UNIQUE INDEX forget_requests_waiting_key ON v2.forget_requests (memory_id, requested_by) WHERE status = 'waiting';
CREATE INDEX forget_requests_space_idx ON v2.forget_requests (space_id, created_at DESC) WHERE status = 'waiting';

COMMENT ON TABLE v2.forget_requests IS
    'An agent asked a person to forget a memory (memax_forget). The reason is on the receipt, and goes with the memory if it is forgotten.';

-- A request ends once.
CREATE FUNCTION v2.forget_requests_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.status <> 'waiting' OR NEW.status = 'waiting'
       OR (NEW.id, NEW.tenant_id, NEW.space_id, NEW.memory_id, NEW.requested_by, NEW.agent, NEW.session_ref,
           NEW.created_receipt_id, NEW.created_at)
          IS DISTINCT FROM
          (OLD.id, OLD.tenant_id, OLD.space_id, OLD.memory_id, OLD.requested_by, OLD.agent, OLD.session_ref,
           OLD.created_receipt_id, OLD.created_at)
    THEN
        RAISE EXCEPTION 'forget request %: a request ends once, forgotten or declined', OLD.id
            USING ERRCODE = 'MXL04';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER forget_requests_guard
    BEFORE UPDATE ON v2.forget_requests
    FOR EACH ROW EXECUTE FUNCTION v2.forget_requests_guard();

-- ---------------------------------------------------------------------
-- What a forgotten memory keeps: nothing that holds its words
-- ---------------------------------------------------------------------

ALTER TABLE v2.memories DROP CONSTRAINT memories_forgotten_purged_check;
ALTER TABLE v2.memories ADD CONSTRAINT memories_forgotten_purged_check CHECK (
    lifecycle <> 'forgotten'
    OR (embedding IS NULL AND search IS NULL AND content_sha256 IS NULL AND minhash_bands IS NULL
        AND decision IS NULL AND conditions = '[]'::jsonb AND scope = '{}'::jsonb
        AND cardinality(flags) = 0));

-- At commit, a forgotten memory has a tombstone, and none of its versions
-- or sources holds words.
CREATE FUNCTION v2.memories_forgotten_words() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM v2.memories m WHERE m.id = NEW.id AND m.lifecycle = 'forgotten') THEN
        RETURN NULL; -- deleted since (a retired space)
    END IF;
    IF EXISTS (SELECT 1 FROM v2.memory_versions v WHERE v.memory_id = NEW.id AND v.statement IS NOT NULL) THEN
        RAISE EXCEPTION 'memory %: forgotten, but a version still holds its words', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget purges every version (internal/ledger/forget.go).';
    END IF;
    IF EXISTS (
        SELECT 1 FROM v2.memory_sources ms JOIN v2.sources s ON s.id = ms.source_id
         WHERE ms.memory_id = NEW.id AND (s.quote IS NOT NULL OR s.uri IS NOT NULL OR s.content_hash IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'memory %: forgotten, but a source still holds a quote, a URI or a hash', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget purges every source (internal/ledger/forget.go).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM v2.tombstones t WHERE t.object_id = NEW.id) THEN
        RAISE EXCEPTION 'memory %: forgotten without a tombstone', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget writes the tombstone beside the forgot receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER memories_forgotten_words
    AFTER UPDATE OF lifecycle ON v2.memories
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (NEW.lifecycle = 'forgotten')
    EXECUTE FUNCTION v2.memories_forgotten_words();

-- Sources change only when Forget purges them.
CREATE FUNCTION v2.sources_purge_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.txid = pg_current_xact_id() AND r.space_id = NEW.space_id AND r.action = 'forgot'
    ) THEN
        RAISE EXCEPTION 'source %: sources change only when Forget purges them', OLD.id
            USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER sources_purge_guard
    BEFORE UPDATE ON v2.sources
    FOR EACH ROW EXECUTE FUNCTION v2.sources_purge_guard();

GRANT UPDATE (ref, uri, locator, content_hash) ON v2.sources TO memax_v2;

-- Brief versions are immutable, except that Forget redacts the prose that
-- cited a forgotten memory.
CREATE FUNCTION v2.brief_versions_purge_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF (NEW.id, NEW.brief_id, NEW.version, NEW.tenant_id, NEW.space_id, NEW.seq, NEW.parent_version,
        NEW.receipt_id, NEW.created_at)
       IS DISTINCT FROM
       (OLD.id, OLD.brief_id, OLD.version, OLD.tenant_id, OLD.space_id, OLD.seq, OLD.parent_version,
        OLD.receipt_id, OLD.created_at)
       OR NOT EXISTS (
           SELECT 1 FROM v2.receipts r
            WHERE r.txid = pg_current_xact_id() AND r.space_id = NEW.space_id AND r.action = 'forgot')
    THEN
        RAISE EXCEPTION 'Brief version %: versions change only when Forget redacts what cited a forgotten memory', OLD.id
            USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER brief_versions_purge_guard
    BEFORE UPDATE ON v2.brief_versions
    FOR EACH ROW EXECUTE FUNCTION v2.brief_versions_purge_guard();

GRANT UPDATE (title, summary, structure, last_receipt_id) ON v2.brief_versions TO memax_v2;

-- Drift evidence loses the changes that cited a forgotten memory.
GRANT UPDATE (changeset) ON v2.target_observations TO memax_v2;

-- The stored idempotency request hash is purged with the words: it could
-- confirm a guess of them. A key whose hash is gone replays its original
-- result for the same command (ledger.claimKey).
ALTER TABLE v2.command_keys ALTER COLUMN request_hash DROP NOT NULL;
GRANT UPDATE (request_hash) ON v2.command_keys TO memax_v2;

-- The judge's words go on both sides of a pair.
GRANT UPDATE (rationale, merged_statement, last_receipt_id) ON v2.judge_verdicts TO memax_v2;

-- A verdict that names a forgotten memory (an index or judge job that lost
-- a race with Forget) keeps no words.
CREATE FUNCTION v2.judge_verdicts_forgotten_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM v2.memories m
         WHERE m.lifecycle = 'forgotten'
           AND (m.id = NEW.memory_id OR m.id = NEW.related_memory_id
                OR m.id IN (SELECT (c ->> 'memory_id')::uuid FROM jsonb_array_elements(NEW.candidates) c
                             WHERE c ->> 'memory_id' ~ '^[0-9a-f-]{36}$'))
    ) THEN
        NEW.rationale := NULL;
        NEW.merged_statement := NULL;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER judge_verdicts_forgotten_guard
    BEFORE INSERT ON v2.judge_verdicts
    FOR EACH ROW EXECUTE FUNCTION v2.judge_verdicts_forgotten_guard();

-- A compile run never contains a forgotten memory (a compile that read the
-- record before a Forget committed, recorded after it).
CREATE FUNCTION v2.compile_runs_forgotten_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF cardinality(NEW.refs) > 0 AND EXISTS (
        SELECT 1 FROM v2.memories m
         WHERE m.space_id = NEW.space_id AND m.lifecycle = 'forgotten'
           AND m.seq = ANY (ARRAY(SELECT substr(r, 3)::bigint FROM unnest(NEW.refs) r WHERE r ~ '^M-[0-9]{1,18}$'))
    ) THEN
        RAISE EXCEPTION 'compile run %: it contains a forgotten memory; compile again', NEW.id
            USING ERRCODE = 'MXF02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER compile_runs_forgotten_guard
    BEFORE INSERT ON v2.compile_runs
    FOR EACH ROW EXECUTE FUNCTION v2.compile_runs_forgotten_guard();

-- A gate's words go when the decision it became is forgotten: Forget
-- writes a forgot receipt about the gate and nulls its question, context
-- and options. Nothing else about an ended gate changes.
GRANT UPDATE (question, context, options) ON v2.decision_gates TO memax_v2;

CREATE OR REPLACE FUNCTION v2.decision_gates_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.decision_gates;
    o v2.decision_gates;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT v2.gate_status_transition_allowed(NULL, NEW.status) THEN
            RAISE EXCEPTION 'gate %: a gate starts waiting, not %', NEW.id, NEW.status
                USING ERRCODE = 'MXL03';
        END IF;
        RETURN NEW;
    END IF;
    -- Telling the asking agent how it ended is not a change to the gate.
    n := NEW; o := OLD;
    n.delivered_at := NULL; o.delivered_at := NULL;
    IF n IS NOT DISTINCT FROM o THEN
        RETURN NEW;
    END IF;
    -- Forget's purge: an ended gate loses its words, with a forgot receipt
    -- about it in this transaction.
    IF OLD.status <> 'waiting' AND NEW.status = OLD.status
       AND NEW.question IS NULL AND NEW.context IS NULL AND NEW.options IS NULL
       AND EXISTS (
           SELECT 1 FROM v2.receipts r
            WHERE r.id = NEW.last_receipt_id AND r.txid = pg_current_xact_id()
              AND r.object_id = NEW.id AND r.action = 'forgot')
    THEN
        n.question := o.question; n.context := o.context; n.options := o.options;
        n.stream_version := o.stream_version; n.last_receipt_id := o.last_receipt_id; n.updated_at := o.updated_at;
        IF n IS NOT DISTINCT FROM o THEN
            RETURN NEW;
        END IF;
    END IF;
    IF NOT v2.gate_status_transition_allowed(OLD.status, NEW.status) THEN
        RAISE EXCEPTION 'gate %: status % -> % is not allowed; a gate ends once', NEW.id, OLD.status, NEW.status
            USING ERRCODE = 'MXL03';
    END IF;
    RETURN NEW;
END $$;

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1), extended
-- ---------------------------------------------------------------------

CREATE OR REPLACE FUNCTION v2.require_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    sid uuid;
    objects uuid[];
    n v2.memories;
    o v2.memories;
    nt v2.targets;
    ot v2.targets;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'memories' THEN
        IF TG_OP = 'UPDATE' THEN
            -- embedding, search and the stage-0 fingerprints are derived
            -- index data, not the record.
            n := NEW; o := OLD;
            n.embedding := NULL; o.embedding := NULL;
            n.search := NULL; o.search := NULL;
            n.content_sha256 := NULL; o.content_sha256 := NULL;
            n.minhash_bands := NULL; o.minhash_bands := NULL;
            IF n IS NOT DISTINCT FROM o THEN
                RETURN NULL;
            END IF;
        END IF;
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'memory_versions' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    WHEN 'sources' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := NULL;
    WHEN 'memory_sources' THEN
        rid := NEW.receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    WHEN 'memory_links' THEN
        -- Ending a link needs the receipt that ended it.
        rid := CASE WHEN TG_OP = 'UPDATE' THEN NEW.ended_receipt_id ELSE NEW.receipt_id END;
        sid := NEW.space_id; objects := ARRAY[NEW.from_memory_id, NEW.to_memory_id];
    WHEN 'briefs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'brief_versions' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.brief_id];
    WHEN 'targets' THEN
        IF TG_OP = 'UPDATE' THEN
            -- The compile path's bookkeeping (see the header of 031):
            -- generation counters, the quiet-window clock, updated_at, and
            -- sync_state moving into or out of "compiling".
            nt := NEW; ot := OLD;
            nt.dirty_gen := NULL; ot.dirty_gen := NULL;
            nt.dirty_at := NULL; ot.dirty_at := NULL;
            nt.compiled_gen := NULL; ot.compiled_gen := NULL;
            nt.updated_at := NULL; ot.updated_at := NULL;
            IF (OLD.sync_state IN ('in_sync', 'pending_delivery') AND NEW.sync_state = 'compiling')
               OR (OLD.sync_state = 'compiling' AND NEW.sync_state IN ('in_sync', 'pending_delivery')) THEN
                nt.sync_state := NULL; ot.sync_state := NULL;
            END IF;
            IF nt IS NOT DISTINCT FROM ot THEN
                RETURN NULL;
            END IF;
        END IF;
        -- A target changes with a receipt about itself, or (recording a
        -- compile, acknowledging a delivery) about its own compile run.
        rid := NEW.last_receipt_id; sid := NEW.space_id;
        objects := ARRAY[NEW.id, NEW.last_compile_id, NEW.delivered_compile_id];
    WHEN 'compile_runs' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.id];
    WHEN 'target_observations' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.target_id];
    WHEN 'judge_verdicts' THEN
        -- A verdict is about a pair: the judged memory, the one it relates
        -- to and every candidate. Forgetting any of them purges its words.
        rid := NEW.last_receipt_id; sid := NEW.space_id;
        objects := ARRAY[NEW.memory_id, NEW.related_memory_id] || ARRAY(
            SELECT (c ->> 'memory_id')::uuid FROM jsonb_array_elements(NEW.candidates) c
             WHERE c ->> 'memory_id' ~ '^[0-9a-f-]{36}$');
    WHEN 'undo_entries' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := NEW.memory_ids;
    WHEN 'forget_requests' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.memory_id];
    ELSE
        RAISE EXCEPTION 'v2.require_receipt is not configured for table %', TG_TABLE_NAME;
    END CASE;

    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = rid
           AND r.txid = pg_current_xact_id()
           AND r.space_id = sid
           AND (objects IS NULL OR r.object_id = ANY (objects))
    ) THEN
        RAISE EXCEPTION 'v2.%: % without a receipt written in the same transaction', TG_TABLE_NAME, TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER forget_requests_require_receipt
    AFTER INSERT OR UPDATE ON v2.forget_requests
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

-- ---------------------------------------------------------------------
-- Retiring a space: what V1's hub delete needs out of the way
-- ---------------------------------------------------------------------

-- Retire deletes a target and its runs together, which point at each
-- other: their foreign keys can wait for the commit.
ALTER TABLE v2.targets ALTER CONSTRAINT targets_last_compile_fkey DEFERRABLE INITIALLY IMMEDIATE;
ALTER TABLE v2.targets ALTER CONSTRAINT targets_delivered_compile_fkey DEFERRABLE INITIALLY IMMEDIATE;

-- Deletes every V2 row of a space that refers to its hub, once a forgot
-- receipt about the space, in this transaction, has forgotten everything
-- in it. It keeps what is content-free and outlives the space: receipts
-- (their reasons already redacted), seals, tombstones, propagations and
-- agent notices, and the space's ledger row, which it marks retired.
-- SECURITY DEFINER because memax_v2 may never DELETE; it deletes only the
-- space in the transaction's scope.
CREATE FUNCTION v2.retire_space(p_space uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    IF NOT (p_space = ANY ((SELECT v2.current_space_ids())::uuid[])) THEN
        RAISE EXCEPTION 'space %: not in this transaction''s scope', p_space USING ERRCODE = 'MXR02';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.object_id = p_space AND r.object_kind = 'space' AND r.action = 'forgot'
           AND r.txid = pg_current_xact_id() AND r.space_id = p_space
    ) THEN
        RAISE EXCEPTION 'space %: a space is retired only by a Forget of the whole space in the same transaction', p_space
            USING ERRCODE = 'MXR02';
    END IF;
    IF EXISTS (SELECT 1 FROM v2.memories m WHERE m.space_id = p_space AND m.lifecycle <> 'forgotten') THEN
        RAISE EXCEPTION 'space %: forget every memory before retiring it', p_space USING ERRCODE = 'MXR02';
    END IF;
    SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey DEFERRED;
    DELETE FROM v2.memory_embeddings WHERE space_id = p_space;
    DELETE FROM v2.judge_verdicts WHERE space_id = p_space;
    DELETE FROM v2.memory_links WHERE space_id = p_space;
    DELETE FROM v2.memory_sources WHERE space_id = p_space;
    DELETE FROM v2.sources WHERE space_id = p_space;
    DELETE FROM v2.forget_requests WHERE space_id = p_space;
    DELETE FROM v2.decision_gates WHERE space_id = p_space;
    DELETE FROM v2.undo_entries WHERE space_id = p_space;
    DELETE FROM v2.target_observations WHERE space_id = p_space;
    DELETE FROM v2.compile_runs WHERE space_id = p_space;
    DELETE FROM v2.targets WHERE space_id = p_space;
    DELETE FROM v2.brief_versions WHERE space_id = p_space;
    DELETE FROM v2.briefs WHERE space_id = p_space;
    DELETE FROM v2.memory_versions WHERE space_id = p_space;
    DELETE FROM v2.memories WHERE space_id = p_space;
    DELETE FROM v2.command_keys WHERE space_id = p_space;
    DELETE FROM v2.agent_connection_spaces WHERE space_id = p_space;
    DELETE FROM v2.reads WHERE space_id = p_space;
    DELETE FROM v2.read_rollups WHERE space_id = p_space;
    UPDATE v2.space_ledgers SET retired_at = now() WHERE space_id = p_space;
END $$;

REVOKE ALL ON FUNCTION v2.retire_space(uuid) FROM PUBLIC;

-- ---------------------------------------------------------------------
-- The forget ledger's sweep (cmd/v2-reapply-forgets)
-- ---------------------------------------------------------------------

-- Every space with a tombstone, for re-applying the forget ledger after a
-- restore. Ids only; the owner's role alone may run it (as 038's).
CREATE FUNCTION v2.forgotten_spaces() RETURNS TABLE (space_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET app.sweep = 'forgotten_spaces'
    AS $$ SELECT DISTINCT t.space_id FROM v2.tombstones t $$;

REVOKE ALL ON FUNCTION v2.forgotten_spaces() FROM PUBLIC;

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.space_ledgers ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.space_ledgers FORCE ROW LEVEL SECURITY;
CREATE POLICY space_ledgers_space ON v2.space_ledgers FOR SELECT
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
-- The owner's functions only: the hub trigger (app.sweep space_ledger_hub)
-- adds and removes rows, the tenant lookup (space_ledger) reads one, and
-- v2.retire_space marks the space in scope retired.
CREATE POLICY space_ledgers_owner ON v2.space_ledgers TO CURRENT_USER
    USING (current_setting('app.sweep', true) IN ('space_ledger', 'space_ledger_hub')
           OR space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (current_setting('app.sweep', true) = 'space_ledger_hub'
           OR space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.tombstones ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.tombstones FORCE ROW LEVEL SECURITY;
CREATE POLICY tombstones_space ON v2.tombstones
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
CREATE POLICY tombstones_sweep ON v2.tombstones FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'forgotten_spaces');

ALTER TABLE v2.propagations ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.propagations FORCE ROW LEVEL SECURITY;
CREATE POLICY propagations_space ON v2.propagations
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- A notice is the connection's: its person reads and marks it in any space
-- (an agent no longer connected to the space is still told).
ALTER TABLE v2.agent_notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.agent_notices FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_notices_scope ON v2.agent_notices
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]) OR person_id = (SELECT v2.current_person_id()))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]) OR person_id = (SELECT v2.current_person_id()));

ALTER TABLE v2.forget_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.forget_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY forget_requests_space ON v2.forget_requests
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. No DELETE or TRUNCATE; identity
-- columns are not updatable.
-- ---------------------------------------------------------------------

GRANT SELECT ON v2.space_ledgers TO memax_v2, memax_v2_sealer;

GRANT SELECT, INSERT ON v2.tombstones TO memax_v2;
GRANT UPDATE (status, propagation, completed_at, reapplied_at, updated_at) ON v2.tombstones TO memax_v2;

GRANT SELECT, INSERT ON v2.propagations TO memax_v2;
GRANT UPDATE (label, status, detail, done_at, updated_at) ON v2.propagations TO memax_v2;

GRANT SELECT, INSERT ON v2.agent_notices TO memax_v2;
GRANT UPDATE (delivered_at, delivered_via) ON v2.agent_notices TO memax_v2;

GRANT SELECT, INSERT ON v2.forget_requests TO memax_v2;
GRANT UPDATE (status, decided_by, decided_at, last_receipt_id, updated_at) ON v2.forget_requests TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.retire_space(uuid) TO memax_v2;
