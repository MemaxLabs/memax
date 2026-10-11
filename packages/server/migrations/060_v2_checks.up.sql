-- 060: v2_checks (epic 3.1, step 3; the stale-detection design §1.3, §1.4)
--
-- Check state: what the repository says about a space's conditions,
-- beside the record like reads (R-), never part of it.
--
--   check_runs        one evaluation: a person's machine reporting on a
--                     commit of the default branch (later the GitHub App,
--                     and the date sweep). Counts only, no words.
--   condition_checks  the latest result of each condition, per context
--                     ("default" for the default branch): its outcome,
--                     when it first held (armed), its latest hold, when
--                     its current failure began, and typed evidence, which
--                     may hold paths and a declared constraint, never a
--                     file's contents.
--
-- A condition that stopped holding marks its memory stale through the
-- ledger (ledger.FlagStale), as Memax, with a `flagged` receipt whose
-- source is the run ({kind: check, ref: <run id>}): sealed, so it never
-- names a path. Paths go in the receipt's reason, which Forget redacts.
--
-- # What needs no receipt
--
-- Neither table is the record: no last_receipt_id, no receipt check.
-- memax_v2 inserts and updates them; it deletes condition_checks rows
-- (Forget's purge, below) and never check_runs.
--
-- # Forget (rule 7)
--
-- Evidence can hold a memory's paths, which its words named, so Forget
-- deletes the memory's condition_checks rows, and the forgotten-words
-- check at commit (044) refuses a forgotten memory that still has one. A
-- report racing a Forget locks the memories it writes about first (the
-- ledger's report takes them FOR UPDATE, in id order), and a row for a
-- forgotten memory is refused outright.
--
-- # Row-level security (rule 13)
--
-- Both are space-scoped on app.space_ids, ENABLEd and FORCEd. Retiring a
-- space takes both with it.

CREATE TABLE v2.check_runs (
    id           uuid PRIMARY KEY,                   -- uuidv7, from Go
    tenant_id    uuid NOT NULL,
    space_id     uuid NOT NULL,
    observer     text NOT NULL,                      -- device | github | memax
    person_id    uuid,                               -- whose machine ran it
    device       text,                               -- the CLI's device id; never a host name
    repository   text NOT NULL,                      -- owner/name, the space's
    branch       text NOT NULL,                      -- the default branch's name
    context      text NOT NULL DEFAULT 'default',    -- default | pull_request
    commit_sha   text NOT NULL,
    commit_at    timestamptz NOT NULL,               -- the commit's time
    trigger      text NOT NULL,
    agent        text,                               -- the agent whose session start asked for it
    checked_at   timestamptz NOT NULL,               -- the machine's clock (an offline report keeps it)
    recorded_at  timestamptz NOT NULL DEFAULT now(),
    complete     boolean NOT NULL,                   -- every condition at that commit was evaluated
    counts       jsonb NOT NULL,                     -- {holds, fails, unknown, ignored}
    capped       boolean NOT NULL DEFAULT false,     -- it tripped the bulk cap, so it flagged nothing
    CONSTRAINT check_runs_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT check_runs_id_space_key UNIQUE (id, space_id),
    CONSTRAINT check_runs_observer_check CHECK (observer IN ('device', 'github', 'memax')),
    CONSTRAINT check_runs_device_check CHECK ((observer = 'device') = (person_id IS NOT NULL AND device IS NOT NULL)),
    CONSTRAINT check_runs_device_id_check CHECK (device IS NULL OR device ~ '^[A-Za-z0-9._:-]{1,64}$'),
    CONSTRAINT check_runs_repository_check CHECK (repository ~ '^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$'),
    CONSTRAINT check_runs_branch_check CHECK (branch ~ '^[A-Za-z0-9._/-]{1,100}$'),
    CONSTRAINT check_runs_context_check CHECK (context IN ('default', 'pull_request')),
    CONSTRAINT check_runs_commit_check CHECK (commit_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    CONSTRAINT check_runs_trigger_check CHECK (trigger IN
        ('head_moved', 'session_start', 'schedule', 'conditions', 'verify', 'push', 'pull_request', 'reconcile', 'armed', 'date')),
    CONSTRAINT check_runs_agent_check CHECK (agent IS NULL OR agent ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    CONSTRAINT check_runs_counts_check CHECK (jsonb_typeof(counts) = 'object')
);

CREATE INDEX check_runs_space_idx ON v2.check_runs (space_id, recorded_at DESC);

COMMENT ON TABLE v2.check_runs IS
    'One evaluation of a space''s conditions at a commit: who ran it, where, and counts. No words.';

CREATE TABLE v2.condition_checks (
    memory_id           uuid NOT NULL,
    condition_id        text NOT NULL,               -- "c1"
    context             text NOT NULL,               -- "default"
    space_id            uuid NOT NULL,
    tenant_id           uuid NOT NULL,
    condition_hash      text NOT NULL,               -- conditions.Hash: a redefined condition starts over
    outcome             text NOT NULL,               -- holds | fails | unknown
    armed_at            timestamptz,                 -- when it first held on the default branch
    armed_commit        text,
    held_commit         text,                        -- its latest hold
    held_commit_at      timestamptz,
    failed_commit       text,                        -- where its current failure began
    failed_commit_at    timestamptz,
    failed_at           timestamptz,                 -- when Memax first saw that failure
    first_failed_run_id uuid,
    resolved            text,                        -- the manifest that held, for a bare manifest name
    last_run_id         uuid NOT NULL,
    last_commit         text NOT NULL,
    last_commit_at      timestamptz NOT NULL,
    evidence            jsonb,                       -- typed (ledger.CheckEvidence), ≤ 2 KB
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, condition_id, context),
    CONSTRAINT condition_checks_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT condition_checks_condition_check CHECK (condition_id ~ '^c[1-9][0-9]{0,3}$'),
    CONSTRAINT condition_checks_context_check CHECK (context = 'default' OR context ~ '^pr:[1-9][0-9]{0,9}$'),
    CONSTRAINT condition_checks_hash_check CHECK (condition_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT condition_checks_outcome_check CHECK (outcome IN ('holds', 'fails', 'unknown')),
    CONSTRAINT condition_checks_armed_check CHECK ((armed_at IS NULL) = (armed_commit IS NULL)),
    CONSTRAINT condition_checks_held_check CHECK ((held_commit IS NULL) = (held_commit_at IS NULL)),
    CONSTRAINT condition_checks_failed_check CHECK (
        (failed_commit IS NULL) = (failed_commit_at IS NULL) AND (failed_commit IS NULL) = (failed_at IS NULL)),
    CONSTRAINT condition_checks_evidence_check CHECK (
        evidence IS NULL OR (jsonb_typeof(evidence) = 'object' AND octet_length(evidence::text) <= 2048)),
    CONSTRAINT condition_checks_evidence_outcome_check CHECK (evidence IS NULL OR outcome = 'fails')
);

CREATE INDEX condition_checks_space_idx ON v2.condition_checks (space_id, context) WHERE armed_at IS NOT NULL;

COMMENT ON TABLE v2.condition_checks IS
    'The latest result of each condition of a memory, per context. Evidence may hold paths, never contents; Forget deletes the memory''s rows.';

-- A result never lands on a forgotten memory.
CREATE FUNCTION v2.condition_checks_forgotten_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM v2.memories m WHERE m.id = NEW.memory_id AND m.lifecycle = 'forgotten') THEN
        RAISE EXCEPTION 'memory %: forgotten, so it has no conditions to check', NEW.memory_id
            USING ERRCODE = 'MXF02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER condition_checks_forgotten_guard
    BEFORE INSERT OR UPDATE ON v2.condition_checks
    FOR EACH ROW EXECUTE FUNCTION v2.condition_checks_forgotten_guard();

-- 044's check at commit, plus the check results: a forgotten memory keeps
-- none of them.
CREATE OR REPLACE FUNCTION v2.memories_forgotten_words() RETURNS trigger
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
    IF EXISTS (SELECT 1 FROM v2.condition_checks c WHERE c.memory_id = NEW.id) THEN
        RAISE EXCEPTION 'memory %: forgotten, but a check result still holds its paths', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget deletes the memory''s condition_checks (internal/ledger/forget.go).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM v2.tombstones t WHERE t.object_id = NEW.id) THEN
        RAISE EXCEPTION 'memory %: forgotten without a tombstone', NEW.id
            USING ERRCODE = 'MXF01', HINT = 'Forget writes the tombstone beside the forgot receipt.';
    END IF;
    RETURN NULL;
END $$;

-- ---------------------------------------------------------------------
-- Retiring a space takes its check state with it
-- ---------------------------------------------------------------------

-- 056's v2.retire_space, plus the check state before the memories.
CREATE OR REPLACE FUNCTION v2.retire_space(p_space uuid) RETURNS void
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
    -- memax init's uploads (043): bookkeeping that refers to the hub and
    -- the memories, so it goes first.
    DELETE FROM v2.import_conflicts WHERE space_id = p_space;
    DELETE FROM v2.import_items WHERE space_id = p_space;
    DELETE FROM v2.imports WHERE space_id = p_space;
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
    DELETE FROM v2.machines WHERE space_id = p_space;
    DELETE FROM v2.brief_versions WHERE space_id = p_space;
    DELETE FROM v2.briefs WHERE space_id = p_space;
    DELETE FROM v2.condition_checks WHERE space_id = p_space;
    DELETE FROM v2.check_runs WHERE space_id = p_space;
    DELETE FROM v2.memory_versions WHERE space_id = p_space;
    DELETE FROM v2.memories WHERE space_id = p_space;
    DELETE FROM v2.command_keys WHERE space_id = p_space;
    DELETE FROM v2.agent_connection_spaces WHERE space_id = p_space;
    DELETE FROM v2.reads WHERE space_id = p_space;
    DELETE FROM v2.read_rollups WHERE space_id = p_space;
    UPDATE v2.space_ledgers SET retired_at = now() WHERE space_id = p_space;
END $$;
REVOKE ALL ON FUNCTION v2.retire_space(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.retire_space(uuid) TO memax_v2;

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.check_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.check_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY check_runs_space ON v2.check_runs
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.condition_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.condition_checks FORCE ROW LEVEL SECURITY;
CREATE POLICY condition_checks_space ON v2.condition_checks
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.check_runs TO memax_v2;
GRANT SELECT, INSERT, DELETE ON v2.condition_checks TO memax_v2;
GRANT UPDATE (condition_hash, outcome, armed_at, armed_commit, held_commit, held_commit_at, failed_commit,
              failed_commit_at, failed_at, first_failed_run_id, resolved, last_run_id, last_commit, last_commit_at,
              evidence, created_at, updated_at)
    ON v2.condition_checks TO memax_v2;
