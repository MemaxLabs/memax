-- 031: v2_brief_targets_compiles
--
-- The Brief, compile targets, compile runs and drift evidence (plan 25
-- §5.4 and §5.7; Phase 1 epics 1.5 and 1.6):
--
--   briefs               one per space; points at its current version
--   brief_versions       B-: ordered sections of memory refs and cited
--                        prose, with the author receipt and the parent
--   targets              a compiled file (or copy-out) and its sync state,
--                        with the generation counters of the compile path
--   compile_runs         C-: every compile, with hashes, the artifact key
--                        and timings; the content lives in object storage
--   target_observations  a hand edit seen on a device (or, later, on
--                        GitHub): the observed hash, the stored content and
--                        the parse-back changeset
--
-- The guarantees of 028 extend to every new table: RLS is ENABLEd and
-- FORCEd and keyed on app.space_ids, memax_v2 gets only the grants the
-- ledger needs (no DELETE, no TRUNCATE, identity columns not updatable),
-- and the deferred receipt trigger refuses any insert or update that has
-- no receipt from the same transaction, in the same space, about the same
-- object.
--
-- # What needs no receipt (derived bookkeeping)
--
-- Exactly one table has exempt columns, v2.targets, and only these:
--
--   dirty_gen, dirty_at  The compile path's generation counter (§5.7): it
--                        counts receipted changes elsewhere (a Keep, an
--                        edit, a Brief revision), and is bumped in those
--                        commands' transactions. Requiring a receipt on the
--                        target too would write one "target touched"
--                        receipt per target per Keep, with no information
--                        the memory's own receipt doesn't already carry.
--   compiled_gen         Which generation the latest compile covers. It
--                        moves with a receipted compile run, or, when a
--                        recompile produced byte-identical output, with no
--                        run at all: nothing in the record changed, so
--                        there is nothing to receipt.
--   sync_state, between  "compiling" is a projection of dirty_gen >
--   compiling and        compiled_gen, so moving into it (on a bump) and
--   in_sync/pending_     back out of it (on an unchanged recompile) is the
--   delivery             same bookkeeping. Every other transition (to or
--                        from drifted or off, pending_delivery → in_sync
--                        on a delivery) is a decision or a fact and needs
--                        its receipt.
--   updated_at           A timestamp.
--
-- Any other column change in the same UPDATE needs a receipt as usual.
--
-- # The sweeper's one cross-space read
--
-- §5.7's sweeper re-enqueues every target whose dirty_gen > compiled_gen,
-- across all spaces, so it can't run inside one space's scope.
-- v2.dirty_targets() is the only way to do that read: it sets app.sweep
-- while it runs and puts it back before it returns,
-- and a SELECT-only policy on v2.targets admits dirty rows while app.sweep
-- has that value. It returns ids only. No other code sets app.sweep.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains the verbs of the new commands: revised (a
-- Brief), configured, requested, observed, pulled, overwritten, stopped
-- (a target) and delivered (a compile run). compiled was already there.
-- The list keeps every verb 029 (agent connections) added.

-- ---------------------------------------------------------------------
-- Receipt actions
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped'));

-- ---------------------------------------------------------------------
-- Vocabulary helpers (mirrored in Go, parity-tested)
-- ---------------------------------------------------------------------

-- A repository-relative POSIX path: the compiler's isRepoPath. No "..",
-- no "." segments, no empty segments, no leading or trailing slash.
CREATE FUNCTION v2.repo_path_valid(p text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT p IS NOT NULL
       AND char_length(p) BETWEEN 1 AND 300
       AND p ~ '^[A-Za-z0-9._/-]+$'
       AND NOT EXISTS (SELECT 1 FROM unnest(string_to_array(p, '/')) AS s WHERE s IN ('', '.', '..'))
$$;

CREATE FUNCTION v2.sha256_valid(h text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT h ~ '^[0-9a-f]{64}$' $$;

-- ---------------------------------------------------------------------
-- Briefs: one per space, versioned
-- ---------------------------------------------------------------------

CREATE TABLE v2.briefs (
    id                 uuid PRIMARY KEY,                    -- uuidv7, from Go; also the receipts' stream
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    current_version    integer NOT NULL,                    -- brief_versions.version now in force (If-Match)
    stream_version     integer NOT NULL DEFAULT 1,          -- latest receipts.stream_version on this Brief
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT briefs_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT briefs_space_key UNIQUE (space_id),
    CONSTRAINT briefs_id_space_key UNIQUE (id, space_id),
    CONSTRAINT briefs_versions_check CHECK (current_version >= 1 AND stream_version >= 1)
);

COMMENT ON TABLE v2.briefs IS
    'One Brief per space. The words of each version are in brief_versions.';

CREATE TABLE v2.brief_versions (
    id              uuid PRIMARY KEY,
    brief_id        uuid NOT NULL,
    version         integer NOT NULL,
    tenant_id       uuid NOT NULL,
    space_id        uuid NOT NULL,
    seq             bigint NOT NULL,                        -- display number: B-<seq>, per tenant
    parent_version  integer,                                -- the version it was revised from
    title           text NOT NULL,                          -- the compiled file's "# title"
    summary         text,                                   -- one line under the title
    structure       jsonb NOT NULL,                         -- {"sections": [{key, heading, items: [{ref} | {text, cites}]}]}
    receipt_id      uuid NOT NULL REFERENCES v2.receipts (id), -- the author receipt
    last_receipt_id uuid NOT NULL REFERENCES v2.receipts (id), -- the latest change (creation, or a later Forget redaction)
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT brief_versions_brief_fkey FOREIGN KEY (brief_id, space_id) REFERENCES v2.briefs (id, space_id),
    CONSTRAINT brief_versions_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT brief_versions_version_key UNIQUE (brief_id, version),
    CONSTRAINT brief_versions_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT brief_versions_version_check CHECK (version >= 1 AND seq >= 1),
    CONSTRAINT brief_versions_parent_check CHECK (
        (version = 1 AND parent_version IS NULL) OR (version > 1 AND parent_version BETWEEN 1 AND version - 1)),
    CONSTRAINT brief_versions_title_check CHECK (title <> '' AND char_length(title) <= 200),
    CONSTRAINT brief_versions_summary_check CHECK (summary IS NULL OR (summary <> '' AND char_length(summary) <= 500)),
    CONSTRAINT brief_versions_structure_check CHECK (
        jsonb_typeof(structure) = 'object' AND jsonb_typeof(structure -> 'sections') = 'array')
);

CREATE INDEX brief_versions_space_idx ON v2.brief_versions (space_id);

-- A Brief always points at one of its own versions. Deferred, because the
-- first version and the Brief are written in the same transaction.
ALTER TABLE v2.briefs ADD CONSTRAINT briefs_current_version_fkey
    FOREIGN KEY (id, current_version) REFERENCES v2.brief_versions (brief_id, version)
    DEFERRABLE INITIALLY DEFERRED;

-- ---------------------------------------------------------------------
-- Targets: what a space compiles to
-- ---------------------------------------------------------------------

CREATE TABLE v2.targets (
    id                   uuid PRIMARY KEY,                  -- uuidv7, from Go; also the receipts' stream
    tenant_id            uuid NOT NULL,
    space_id             uuid NOT NULL,
    kind                 text NOT NULL,                     -- the compiler's adapter kind
    path                 text,                              -- repository-relative; null for chatgpt (copied out)
    settings             jsonb NOT NULL DEFAULT '{}'::jsonb, -- include, stale, size_budget, scoped, user_owned
    delivery             text NOT NULL,                     -- local | pr | mcp | copy
    sync_state           text NOT NULL,                     -- in_sync | compiling | pending_delivery | drifted | off
    dirty_gen            bigint NOT NULL DEFAULT 1,         -- bumped by every change that affects the output
    dirty_at             timestamptz NOT NULL DEFAULT now(), -- when dirty_gen last moved (the quiet window)
    compiled_gen         bigint NOT NULL DEFAULT 0,         -- the generation the latest compile covers
    last_compile_id      uuid,                              -- the latest compile run, whatever its status
    delivered_compile_id uuid,                              -- the run whose output is on disk, if Memax wrote it
    delivered_sha256     text,                              -- drift hash of what is on disk (the baseline)
    delivered_files      jsonb NOT NULL DEFAULT '[]'::jsonb, -- [{path, sha256, observation?}]: the baseline per file
    delivered_at         timestamptz,
    stream_version       integer NOT NULL DEFAULT 1,
    created_receipt_id   uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id      uuid NOT NULL REFERENCES v2.receipts (id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT targets_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT targets_id_space_key UNIQUE (id, space_id),
    CONSTRAINT targets_path_key UNIQUE NULLS NOT DISTINCT (space_id, kind, path),
    CONSTRAINT targets_kind_check CHECK (kind IN
        ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules')),
    CONSTRAINT targets_delivery_check CHECK (delivery IN ('local', 'pr', 'mcp', 'copy')),
    CONSTRAINT targets_sync_state_check CHECK (sync_state IN ('in_sync', 'compiling', 'pending_delivery', 'drifted', 'off')),
    -- ChatGPT has no file: it is copied out (or read over MCP). Files are
    -- never "copy".
    CONSTRAINT targets_path_check CHECK (
        (kind = 'chatgpt' AND path IS NULL AND delivery IN ('copy', 'mcp'))
        OR (kind <> 'chatgpt' AND v2.repo_path_valid(path) AND delivery <> 'copy')),
    CONSTRAINT targets_settings_check CHECK (jsonb_typeof(settings) = 'object'),
    CONSTRAINT targets_delivered_files_check CHECK (jsonb_typeof(delivered_files) = 'array'),
    CONSTRAINT targets_delivered_sha256_check CHECK (delivered_sha256 IS NULL OR v2.sha256_valid(delivered_sha256)),
    CONSTRAINT targets_gen_check CHECK (dirty_gen >= 1 AND compiled_gen >= 0 AND compiled_gen <= dirty_gen),
    CONSTRAINT targets_stream_version_check CHECK (stream_version >= 1)
);

CREATE INDEX targets_space_idx ON v2.targets (space_id);
-- The sweeper's scan (v2.dirty_targets).
CREATE INDEX targets_dirty_idx ON v2.targets (dirty_at) WHERE dirty_gen > compiled_gen AND sync_state <> 'off';

COMMENT ON TABLE v2.targets IS
    'A compiled file (or copy-out) of a space, its settings, and where its compile and delivery stand.';

-- ---------------------------------------------------------------------
-- Compile runs: every compile
-- ---------------------------------------------------------------------

CREATE TABLE v2.compile_runs (
    id                 uuid PRIMARY KEY,                    -- uuidv7, from Go; also the receipts' stream
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    seq                bigint NOT NULL,                     -- display number: C-<seq>, per tenant
    target_id          uuid NOT NULL,
    brief_id           uuid NOT NULL,
    brief_version      integer NOT NULL,
    generation         bigint NOT NULL,                     -- the target's dirty_gen this run compiled
    status             text NOT NULL,                       -- compiled | delivered | failed
    input_sha256       text NOT NULL,                       -- the CompileInput, without the run's own id and time
    output_sha256      text,                                -- every output's sha256 (one output: its own)
    drift_sha256       text,                                -- every output's drift_sha256: the hash to deliver against
    artifact_key       text,                                -- object storage key of the content
    bytes              integer,
    lines              integer,
    refs               text[] NOT NULL DEFAULT '{}',         -- memories whose statements it contains
    dropped_for_budget text[] NOT NULL DEFAULT '{}',
    files              jsonb NOT NULL DEFAULT '[]'::jsonb,   -- per output: path or label, hashes, sizes, refs (no content)
    warnings           jsonb NOT NULL DEFAULT '[]'::jsonb,
    error              text,                                -- why a failed run failed (no memory text)
    enqueued_at        timestamptz NOT NULL,                -- when the generation was dirtied
    started_at         timestamptz NOT NULL,                -- when the compile job started on it
    compiled_at        timestamptz NOT NULL,                -- the compile time (in the header), or when it failed
    delivered_at       timestamptz,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT compile_runs_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT compile_runs_target_fkey FOREIGN KEY (target_id, space_id) REFERENCES v2.targets (id, space_id),
    CONSTRAINT compile_runs_brief_fkey FOREIGN KEY (brief_id, brief_version) REFERENCES v2.brief_versions (brief_id, version),
    CONSTRAINT compile_runs_brief_space_fkey FOREIGN KEY (brief_id, space_id) REFERENCES v2.briefs (id, space_id),
    CONSTRAINT compile_runs_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT compile_runs_id_target_key UNIQUE (id, target_id),
    CONSTRAINT compile_runs_seq_check CHECK (seq >= 1 AND generation >= 1),
    CONSTRAINT compile_runs_status_check CHECK (status IN ('compiled', 'delivered', 'failed')),
    CONSTRAINT compile_runs_input_check CHECK (v2.sha256_valid(input_sha256)),
    CONSTRAINT compile_runs_output_check CHECK (
        (status = 'failed' AND error IS NOT NULL AND char_length(error) BETWEEN 1 AND 2000
            AND output_sha256 IS NULL AND drift_sha256 IS NULL AND artifact_key IS NULL)
        OR (status <> 'failed' AND error IS NULL
            AND v2.sha256_valid(output_sha256) AND v2.sha256_valid(drift_sha256)
            AND artifact_key <> '' AND char_length(artifact_key) <= 500
            AND bytes >= 0 AND lines >= 0)),
    CONSTRAINT compile_runs_delivered_check CHECK ((status = 'delivered') = (delivered_at IS NOT NULL)),
    CONSTRAINT compile_runs_files_check CHECK (jsonb_typeof(files) = 'array' AND jsonb_typeof(warnings) = 'array'),
    CONSTRAINT compile_runs_timing_check CHECK (started_at >= enqueued_at - interval '1 minute' AND compiled_at >= started_at)
);

CREATE INDEX compile_runs_target_seq_idx ON v2.compile_runs (target_id, seq DESC);
CREATE INDEX compile_runs_space_idx ON v2.compile_runs (space_id);

COMMENT ON TABLE v2.compile_runs IS
    'C-: one compile of one target. The content is in object storage under artifact_key.';

-- A target's runs are its own.
ALTER TABLE v2.targets
    ADD CONSTRAINT targets_last_compile_fkey
        FOREIGN KEY (last_compile_id, id) REFERENCES v2.compile_runs (id, target_id),
    ADD CONSTRAINT targets_delivered_compile_fkey
        FOREIGN KEY (delivered_compile_id, id) REFERENCES v2.compile_runs (id, target_id);

-- ---------------------------------------------------------------------
-- Target observations: drift evidence
-- ---------------------------------------------------------------------

CREATE TABLE v2.target_observations (
    id                 uuid PRIMARY KEY,
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    target_id          uuid NOT NULL,
    path               text NOT NULL,                       -- the file that changed
    observed_sha256    text NOT NULL,                       -- drift hash of the observed content
    observer_kind      text NOT NULL,                       -- device | github
    observer_id        text NOT NULL,                       -- the device, or the repository
    commit_sha         text,                                -- the commit, when the observer knows it
    artifact_key       text NOT NULL,                       -- object storage key of the observed content
    bytes              integer NOT NULL,
    base_compile_id    uuid,                                -- the compile the edit was compared against
    base_sha256        text,                                -- the baseline hash the edit departs from
    changeset          jsonb NOT NULL,                      -- the compiler's parseBack: {changes, drift}
    status             text NOT NULL,                       -- open | pulled | overwritten | stopped | dismissed
    resolution         jsonb,                               -- what resolving it did (proposals, removals)
    observed_at        timestamptz NOT NULL,
    resolved_at        timestamptz,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT target_observations_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT target_observations_target_fkey FOREIGN KEY (target_id, space_id) REFERENCES v2.targets (id, space_id),
    CONSTRAINT target_observations_base_fkey FOREIGN KEY (base_compile_id, target_id) REFERENCES v2.compile_runs (id, target_id),
    CONSTRAINT target_observations_path_check CHECK (v2.repo_path_valid(path)),
    CONSTRAINT target_observations_sha_check CHECK (
        v2.sha256_valid(observed_sha256) AND (base_sha256 IS NULL OR v2.sha256_valid(base_sha256))),
    CONSTRAINT target_observations_observer_check CHECK (
        observer_kind IN ('device', 'github') AND observer_id <> '' AND char_length(observer_id) <= 200),
    CONSTRAINT target_observations_commit_check CHECK (commit_sha IS NULL OR commit_sha ~ '^[0-9a-f]{7,64}$'),
    CONSTRAINT target_observations_artifact_check CHECK (artifact_key <> '' AND char_length(artifact_key) <= 500 AND bytes >= 0),
    CONSTRAINT target_observations_changeset_check CHECK (
        jsonb_typeof(changeset) = 'object' AND jsonb_typeof(changeset -> 'changes') = 'array'),
    CONSTRAINT target_observations_status_check CHECK (status IN ('open', 'pulled', 'overwritten', 'stopped', 'dismissed')),
    CONSTRAINT target_observations_resolved_check CHECK ((status = 'open') = (resolved_at IS NULL)),
    CONSTRAINT target_observations_resolution_check CHECK (resolution IS NULL OR jsonb_typeof(resolution) = 'object')
);

-- At most one open observation per file: a newer one dismisses the older.
CREATE UNIQUE INDEX target_observations_open_key ON v2.target_observations (target_id, path) WHERE status = 'open';
CREATE INDEX target_observations_target_idx ON v2.target_observations (target_id, created_at DESC);
CREATE INDEX target_observations_space_idx ON v2.target_observations (space_id);

COMMENT ON TABLE v2.target_observations IS
    'Drift evidence: a compiled file seen changed outside Memax, with the stored content and what parse-back made of it.';

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1), extended to the new tables
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
            -- embedding/search are derived index data, not the record.
            n := NEW; o := OLD;
            n.embedding := NULL; o.embedding := NULL;
            n.search := NULL; o.search := NULL;
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
        rid := NEW.receipt_id; sid := NEW.space_id; objects := ARRAY[NEW.from_memory_id, NEW.to_memory_id];
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

CREATE CONSTRAINT TRIGGER briefs_require_receipt
    AFTER INSERT OR UPDATE ON v2.briefs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER brief_versions_require_receipt
    AFTER INSERT OR UPDATE ON v2.brief_versions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER targets_require_receipt
    AFTER INSERT OR UPDATE ON v2.targets
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER compile_runs_require_receipt
    AFTER INSERT OR UPDATE ON v2.compile_runs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER target_observations_require_receipt
    AFTER INSERT OR UPDATE ON v2.target_observations
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

-- ---------------------------------------------------------------------
-- The sweeper's read (§5.7 step 4)
-- ---------------------------------------------------------------------

-- Targets whose latest compile is behind, oldest change first, as ids.
-- It sets app.sweep only while it runs (in its body: a function's own SET
-- of a custom setting needs a superuser, and Neon's owner isn't one); the
-- policy targets_dirty_sweep below admits exactly the rows it returns.
CREATE FUNCTION v2.dirty_targets(p_limit integer)
    RETURNS TABLE (target_id uuid, space_id uuid)
    LANGUAGE plpgsql STABLE
    AS $$
DECLARE
    prev text := current_setting('app.sweep', true);
BEGIN
    -- Set here and put back below, not with the function's own SET: only a
    -- superuser may SET a custom setting that way, and Neon's owner isn't one.
    PERFORM set_config('app.sweep', 'dirty_targets', true);
    RETURN QUERY
    SELECT t.id, t.space_id
      FROM v2.targets t
     WHERE t.dirty_gen > t.compiled_gen AND t.sync_state <> 'off'
     ORDER BY t.dirty_at, t.id
     LIMIT least(greatest(p_limit, 1), 1000);
    PERFORM set_config('app.sweep', coalesce(prev, ''), true);
END $$;

COMMENT ON FUNCTION v2.dirty_targets(integer) IS
    'The compile sweeper''s cross-space read: ids of targets whose dirty_gen is ahead of compiled_gen.';

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.briefs ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.briefs FORCE ROW LEVEL SECURITY;
CREATE POLICY briefs_space ON v2.briefs
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.brief_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.brief_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY brief_versions_space ON v2.brief_versions
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.targets FORCE ROW LEVEL SECURITY;
CREATE POLICY targets_space ON v2.targets
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
-- Read-only, and only inside v2.dirty_targets (see the header).
CREATE POLICY targets_dirty_sweep ON v2.targets FOR SELECT
    USING (current_setting('app.sweep', true) = 'dirty_targets'
           AND dirty_gen > compiled_gen AND sync_state <> 'off');

ALTER TABLE v2.compile_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.compile_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY compile_runs_space ON v2.compile_runs
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.target_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.target_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY target_observations_space ON v2.target_observations
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. No DELETE or TRUNCATE; identity
-- columns (id, tenant, space, seq, kind, the creating receipt, what a
-- run compiled) are not updatable.
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.briefs TO memax_v2;
GRANT UPDATE (current_version, stream_version, last_receipt_id, updated_at) ON v2.briefs TO memax_v2;

-- Versions are immutable until Forget learns to redact cited prose
-- (forget-propagation epic), which will grant UPDATE (structure,
-- last_receipt_id) then.
GRANT SELECT, INSERT ON v2.brief_versions TO memax_v2;

GRANT SELECT, INSERT ON v2.targets TO memax_v2;
GRANT UPDATE (path, settings, delivery, sync_state, dirty_gen, dirty_at, compiled_gen, last_compile_id,
              delivered_compile_id, delivered_sha256, delivered_files, delivered_at, stream_version,
              last_receipt_id, updated_at)
    ON v2.targets TO memax_v2;

GRANT SELECT, INSERT ON v2.compile_runs TO memax_v2;
GRANT UPDATE (status, delivered_at, last_receipt_id) ON v2.compile_runs TO memax_v2;

GRANT SELECT, INSERT ON v2.target_observations TO memax_v2;
GRANT UPDATE (status, resolution, resolved_at, last_receipt_id) ON v2.target_observations TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.dirty_targets(integer) TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.repo_path_valid(text), v2.sha256_valid(text) TO memax_v2;
