-- 038: v2_reads
--
-- Reads (R-, plan 25 §5.3 "Reads are not receipts", §5.10, §5.18): what
-- agents read, recorded off the request path, and the rollups that feed
-- the "read by" counts, fading and the north-star metric.
--
--   reads         R-: one agent read of one space, append-only and
--                 partitioned by month on read_at. Written in batches by
--                 the API's in-process recorder (internal/reads), every
--                 250 ms, and synchronously for a reported compile load.
--   read_rollups  per space, per subject (a memory, or a compile run), per
--                 UTC day, per reader: how many reads and the last one.
--
-- # What a read holds
--
-- Who read (the agent connection, or a person reporting their agent's
-- session start), what (memory ids, or the compile run that was loaded),
-- through what (recall, search, get, list, digest or compile_load), over
-- which surface, the session ref and when. Never memory text and never
-- query text, not even hashed: a query is short, so an unsalted hash of it
-- can be confirmed by guessing, nothing reads it, and Forget couldn't
-- reach it.
--
-- # Reads are not receipts
--
-- A read changes nothing in the record, so it has no receipt and no
-- receipt trigger, and the flush never runs through Ledger.Apply. It does
-- go through internal/ledger (RecordReads, RecordCompileLoad), as memax_v2
-- with app.space_ids set to the batch's spaces, so row-level security
-- holds. memax_v2 may INSERT reads but never UPDATE or DELETE them; it may
-- INSERT and bump the counters of read_rollups (they are counters, the
-- way last_seen_at is), and never DELETE them.
--
-- # A compile read is not expanded
--
-- A session-start ping that reports a loaded compile (C-) counts as a read
-- of every fact in it (§5.10). Expanding that into one row per fact would
-- cost hundreds of rows per session start, every day, for every agent. So
-- a compile read stores the compile, its rollup is one row keyed by the
-- compile run, and the facts are resolved through compile_runs.refs (an
-- immutable list per run, GIN-indexed below) when the counts are read.
--
-- # Rollup grain
--
-- (space, subject, UTC day, reader, agent):
--   * per subject (memory or compile), because "read by" is per memory
--     and fading asks when each memory was last read;
--   * per day, the coarsest grain that still answers 7- and 30-day
--     windows and "unread for 60 days", with last_read_at exact;
--   * per reader (the connection for an agent; the person, with the agent
--     the hook named, for a person's report), because "read by" lists
--     agents and the north star counts distinct agents in a week.
-- Its size is bounded by what is actually read each day, not by how
-- often: a busy agent re-reading the same memory adds to a counter.
--
-- # Partitions and retention
--
-- One partition per UTC month, created ahead of need by
-- v2.ensure_reads_partitions (SECURITY DEFINER: memax_v2 can't create
-- tables), which the flusher calls the first time it meets a month (and
-- the month after it), so an insert never waits on a job that didn't
-- run. There is deliberately no DEFAULT partition: a row there would make
-- creating its month's partition fail. The worker's daily reads_maintain
-- job also ensures next month and prunes.
--
-- Retention follows the longest activity retention of any plan (D9: Team
-- keeps a year of activity): 13 months of raw reads (twelve whole months
-- plus the current one) and the same of rollups. Shorter plans see less
-- through the API, which filters by plan once V2 billing exists; storage
-- doesn't. v2.prune_reads drops whole partitions (no row-by-row DELETE on
-- the hot table) and deletes old rollup days, and refuses a cutoff less
-- than 90 days back, so a bad call can't erase what fading needs.
--
-- # Cross-space reads
--
-- Two functions read across spaces, both SECURITY DEFINER, setting
-- app.sweep while they run, and both return aggregates only:
--   v2.read_metrics   the north star and its coverage (§5.18).
--   v2.prune_reads    the retention sweep.
-- Their policies admit rows only to the function owner (the migrating
-- role, `TO CURRENT_USER` here) while app.sweep has their value. Unlike
-- v2.dirty_targets (031), memax_v2 can't borrow them by setting app.sweep
-- itself: the policies don't apply to it.

-- ---------------------------------------------------------------------
-- Reads
-- ---------------------------------------------------------------------

CREATE TABLE v2.reads (
    id            uuid NOT NULL,                       -- uuidv7, from Go
    tenant_id     uuid NOT NULL,
    space_id      uuid NOT NULL,
    seq           bigint NOT NULL,                     -- display number: R-<seq>, per tenant
    reader_kind   text NOT NULL,                       -- agent | person
    connection_id uuid,                                -- the agent connection; null for a person
    person_id     uuid NOT NULL,                       -- the person, or the person the agent works for
    agent         text,                                -- the agent kind, if known (claude-code)
    kind          text NOT NULL,                       -- recall | search | get | list | digest | compile_load
    via           text NOT NULL,                       -- mcp | api | cli
    session_ref   text,
    compile_id    uuid,                                -- the compile run read (digest of a compiled file, compile_load)
    memory_ids    uuid[] NOT NULL DEFAULT '{}',        -- memories returned directly; a compile's facts aren't listed
    memories      integer NOT NULL,                    -- memories the read covered (a compile: every fact in it)
    read_at       timestamptz NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, read_at),
    CONSTRAINT reads_seq_check CHECK (seq >= 1),
    CONSTRAINT reads_reader_kind_check CHECK (reader_kind IN ('agent', 'person')),
    CONSTRAINT reads_connection_check CHECK ((reader_kind = 'agent') = (connection_id IS NOT NULL)),
    CONSTRAINT reads_agent_check CHECK (agent IS NULL OR agent IN
        ('claude-code', 'codex', 'cursor', 'chatgpt', 'claude', 'gemini-cli', 'copilot', 'opencode', 'windsurf', 'other')),
    CONSTRAINT reads_kind_check CHECK (kind IN ('recall', 'search', 'get', 'list', 'digest', 'compile_load')),
    CONSTRAINT reads_via_check CHECK (via IN ('mcp', 'api', 'cli')),
    CONSTRAINT reads_compile_check CHECK (kind <> 'compile_load' OR compile_id IS NOT NULL),
    CONSTRAINT reads_session_ref_check CHECK (session_ref IS NULL OR (session_ref <> '' AND char_length(session_ref) <= 255)),
    CONSTRAINT reads_memories_check CHECK (memories >= cardinality(memory_ids) AND cardinality(memory_ids) <= 200)
) PARTITION BY RANGE (read_at);

-- Activity's reads, newest first, and a space's reads in a window.
CREATE INDEX reads_space_seq_idx ON v2.reads (space_id, seq DESC);
CREATE INDEX reads_space_time_idx ON v2.reads (space_id, read_at);
-- An agent's reads (reads_7d, its sessions).
CREATE INDEX reads_connection_idx ON v2.reads (connection_id, read_at) WHERE connection_id IS NOT NULL;
-- Whose loads we observe: the compile loads of a space's targets.
CREATE INDEX reads_compile_load_idx ON v2.reads (space_id, compile_id, read_at) WHERE kind = 'compile_load';

COMMENT ON TABLE v2.reads IS
    'R-: one agent read of one space. Append-only, monthly partitions, no text. Not receipts (plan 25 §5.3).';

-- ---------------------------------------------------------------------
-- Rollups
-- ---------------------------------------------------------------------

CREATE TABLE v2.read_rollups (
    space_id     uuid NOT NULL,
    tenant_id    uuid NOT NULL,
    subject_kind text NOT NULL,                        -- memory | compile
    subject_id   uuid NOT NULL,                        -- the memory, or the compile run
    day          date NOT NULL,                        -- UTC
    reader_key   uuid NOT NULL,                        -- the connection (an agent), or the person
    reader_kind  text NOT NULL,
    person_id    uuid NOT NULL,
    agent        text NOT NULL DEFAULT '',             -- '' when the reader named none
    reads        integer NOT NULL,
    last_read_at timestamptz NOT NULL,
    PRIMARY KEY (space_id, subject_id, day, reader_key, agent),
    CONSTRAINT read_rollups_subject_kind_check CHECK (subject_kind IN ('memory', 'compile')),
    CONSTRAINT read_rollups_reader_kind_check CHECK (reader_kind IN ('agent', 'person')),
    CONSTRAINT read_rollups_reads_check CHECK (reads >= 1)
);

-- The north star (agents per space per week) and fading windows.
CREATE INDEX read_rollups_space_day_idx ON v2.read_rollups (space_id, day);
-- Retention, and the cross-space metric.
CREATE INDEX read_rollups_day_idx ON v2.read_rollups (day);

COMMENT ON TABLE v2.read_rollups IS
    'Reads per space, subject (memory or compile run), UTC day and reader. Counters; no text.';

-- "Which compiles contain M-0219": a compile read counts for every fact in it.
CREATE INDEX compile_runs_refs_idx ON v2.compile_runs USING gin (refs);

-- ---------------------------------------------------------------------
-- Partitions
-- ---------------------------------------------------------------------

-- Creates the month of p_at and the p_ahead months after it, if missing.
-- Returns how many it created. Safe to race: a partition another session
-- created meanwhile is not an error.
CREATE FUNCTION v2.ensure_reads_partitions(p_at timestamptz, p_ahead integer DEFAULT 1) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET TimeZone = 'UTC'
    AS $$
DECLARE
    first_month timestamp := date_trunc('month', p_at AT TIME ZONE 'UTC');
    lo timestamp;
    name text;
    created integer := 0;
BEGIN
    IF p_ahead < 0 OR p_ahead > 12 THEN
        RAISE EXCEPTION 'ensure_reads_partitions: p_ahead must be 0 to 12, not %', p_ahead;
    END IF;
    FOR i IN 0 .. p_ahead LOOP
        lo := first_month + make_interval(months => i);
        name := 'reads_' || to_char(lo, 'YYYY_MM');
        CONTINUE WHEN to_regclass('v2.' || name) IS NOT NULL;
        BEGIN
            EXECUTE format('CREATE TABLE v2.%I PARTITION OF v2.reads FOR VALUES FROM (%L) TO (%L)',
                name, (lo AT TIME ZONE 'UTC'), ((lo + interval '1 month') AT TIME ZONE 'UTC'));
            -- Direct access to a partition sees nothing: memax_v2 has no
            -- grants on it, and RLS is forced with a policy that admits
            -- nothing. Everything goes through v2.reads and its policies
            -- (a query on the parent applies the parent's policies only).
            EXECUTE format('ALTER TABLE v2.%I ENABLE ROW LEVEL SECURITY', name);
            EXECUTE format('ALTER TABLE v2.%I FORCE ROW LEVEL SECURITY', name);
            EXECUTE format('CREATE POLICY %I ON v2.%I USING (false) WITH CHECK (false)', name || '_direct', name);
            created := created + 1;
        EXCEPTION WHEN duplicate_table OR unique_violation THEN
            NULL; -- created concurrently
        END;
    END LOOP;
    RETURN created;
END $$;

COMMENT ON FUNCTION v2.ensure_reads_partitions(timestamptz, integer) IS
    'Creates the monthly partitions of v2.reads for p_at''s month and p_ahead more.';

-- This month and the next two, so a fresh database records reads at once.
SELECT v2.ensure_reads_partitions(now(), 2);

-- ---------------------------------------------------------------------
-- Retention
-- ---------------------------------------------------------------------

-- Drops every reads partition that ends on or before p_before's month
-- and every rollup day before p_before. p_before must be at least 90 days
-- ago. Returns the partitions dropped.
-- Only a superuser may give a function its own SET of a custom setting such
-- as app.sweep (Neon's owner role isn't one), so v2.prune_reads sets it and puts it
-- back itself, around v2.prune_reads_as_owner, which holds the query and runs as
-- the owner inside it.
CREATE FUNCTION v2.prune_reads_as_owner(p_before timestamptz)
    RETURNS integer
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    SET TimeZone = 'UTC'
    AS $$
DECLARE
    cutoff timestamp := date_trunc('month', p_before AT TIME ZONE 'UTC');
    part record;
    month timestamp;
    dropped integer := 0;
BEGIN
    IF p_before > now() - interval '90 days' THEN
        RAISE EXCEPTION 'prune_reads: keep at least 90 days of reads (fading needs 60); % is too recent', p_before
            USING ERRCODE = 'MXR03';
    END IF;
    FOR part IN
        SELECT c.relname
          FROM pg_inherits i
          JOIN pg_class c ON c.oid = i.inhrelid
         WHERE i.inhparent = 'v2.reads'::regclass
    LOOP
        CONTINUE WHEN part.relname !~ '^reads_[0-9]{4}_[0-9]{2}$';
        month := to_timestamp(substr(part.relname, 7), 'YYYY_MM')::timestamp;
        IF month + interval '1 month' <= cutoff THEN
            EXECUTE format('DROP TABLE v2.%I', part.relname);
            dropped := dropped + 1;
        END IF;
    END LOOP;
    DELETE FROM v2.read_rollups WHERE day < (p_before AT TIME ZONE 'UTC')::date;
    RETURN dropped;
END $$;

REVOKE ALL ON FUNCTION v2.prune_reads_as_owner(timestamptz) FROM PUBLIC;

CREATE FUNCTION v2.prune_reads(p_before timestamptz)
    RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET TimeZone = 'UTC'
    AS $$
DECLARE
    prev text := current_setting('app.sweep', true);
    result integer;
BEGIN
    PERFORM set_config('app.sweep', 'prune_reads', true);
    result := v2.prune_reads_as_owner(p_before);
    PERFORM set_config('app.sweep', coalesce(prev, ''), true);
    RETURN result;
END $$;

COMMENT ON FUNCTION v2.prune_reads(timestamptz) IS
    'Retention: drops reads partitions that ended by p_before''s month, and rollup days before it.';

-- ---------------------------------------------------------------------
-- The north star (plan 25 §5.18), aggregates only
-- ---------------------------------------------------------------------

-- Over the 7 UTC days ending p_day (inclusive):
--   spaces_read           spaces with at least one memory or compile read
--   spaces_two_agents     spaces read by 2+ agent kinds (the north star)
--   spaces_two_connections spaces read by 2+ agent connections
--   spaces_hook_loads     spaces whose reads include a session-start compile load
--   connections_reading   agent connections with a recorded read
--   connections_seen      active connections last seen in the window: the
--                         coverage is reading / seen (§5.18's caveat: an
--                         agent that loads files natively without a hook
--                         is seen but never read)
-- An agent kind is the reader's agent slug; a reader that named none counts
-- by its connection.
-- Only a superuser may give a function its own SET of a custom setting such
-- as app.sweep (Neon's owner role isn't one), so v2.read_metrics sets it and puts it
-- back itself, around v2.read_metrics_as_owner, which holds the query and runs as
-- the owner inside it.
CREATE FUNCTION v2.read_metrics_as_owner(p_day date)
    RETURNS TABLE (spaces_read bigint, spaces_two_agents bigint, spaces_two_connections bigint, spaces_hook_loads bigint, connections_reading bigint, connections_seen bigint)
    LANGUAGE sql STABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
    WITH week AS (
        SELECT r.space_id, r.reader_key, r.reader_kind,
               CASE WHEN r.agent <> '' THEN r.agent ELSE r.reader_key::text END AS agent_kind,
               r.subject_kind
          FROM v2.read_rollups r
         WHERE r.day BETWEEN p_day - 6 AND p_day
    ), per_space AS (
        SELECT space_id,
               count(DISTINCT agent_kind) AS kinds,
               count(DISTINCT reader_key) FILTER (WHERE reader_kind = 'agent') AS connections
          FROM week
         GROUP BY space_id
    ), loads AS (
        SELECT count(DISTINCT space_id) AS n
          FROM v2.reads
         WHERE kind = 'compile_load'
           AND read_at >= (p_day - 6)::timestamp AT TIME ZONE 'UTC'
           AND read_at < (p_day + 1)::timestamp AT TIME ZONE 'UTC'
    )
    SELECT (SELECT count(*) FROM per_space),
           (SELECT count(*) FROM per_space WHERE kinds >= 2),
           (SELECT count(*) FROM per_space WHERE connections >= 2),
           (SELECT n FROM loads),
           (SELECT count(DISTINCT reader_key) FROM week WHERE reader_kind = 'agent'),
           (SELECT count(*) FROM v2.agent_connections c
             WHERE c.state <> 'disconnected'
               AND c.last_seen_at >= (p_day - 6)::timestamp AT TIME ZONE 'UTC')
$$;

REVOKE ALL ON FUNCTION v2.read_metrics_as_owner(date) FROM PUBLIC;

CREATE FUNCTION v2.read_metrics(p_day date)
    RETURNS TABLE (spaces_read bigint, spaces_two_agents bigint, spaces_two_connections bigint, spaces_hook_loads bigint, connections_reading bigint, connections_seen bigint)
    LANGUAGE plpgsql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    prev text := current_setting('app.sweep', true);
BEGIN
    PERFORM set_config('app.sweep', 'read_metrics', true);
    RETURN QUERY SELECT * FROM v2.read_metrics_as_owner(p_day);
    PERFORM set_config('app.sweep', coalesce(prev, ''), true);
END $$;

COMMENT ON FUNCTION v2.read_metrics(date) IS
    'The north star (spaces read by 2+ agents in a week) and its coverage. Counts only.';

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.reads ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.reads FORCE ROW LEVEL SECURITY;
CREATE POLICY reads_space ON v2.reads
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
-- Read-only, and only inside v2.read_metrics (see the header).
CREATE POLICY reads_metrics ON v2.reads FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'read_metrics' AND kind = 'compile_load');

ALTER TABLE v2.read_rollups ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.read_rollups FORCE ROW LEVEL SECURITY;
CREATE POLICY read_rollups_space ON v2.read_rollups
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
CREATE POLICY read_rollups_metrics ON v2.read_rollups FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'read_metrics');
-- Retention's DELETE, only inside v2.prune_reads (memax_v2 has no DELETE
-- privilege at all; the function's owner does). A DELETE with a WHERE
-- clause needs the rows to be visible too, hence the SELECT policy.
CREATE POLICY read_rollups_prune ON v2.read_rollups FOR DELETE TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'prune_reads');
CREATE POLICY read_rollups_prune_select ON v2.read_rollups FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'prune_reads');

CREATE POLICY agent_connections_metrics ON v2.agent_connections FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'read_metrics');

-- ---------------------------------------------------------------------
-- Grants: append-only reads, counter-only rollups, no DELETE or TRUNCATE
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.reads TO memax_v2;
GRANT SELECT, INSERT ON v2.read_rollups TO memax_v2;
GRANT UPDATE (reads, last_read_at) ON v2.read_rollups TO memax_v2;

REVOKE ALL ON FUNCTION v2.ensure_reads_partitions(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.prune_reads(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.ensure_reads_partitions(timestamptz, integer) TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.prune_reads(timestamptz) TO memax_v2;
REVOKE ALL ON FUNCTION v2.read_metrics(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.read_metrics(date) TO memax_v2;
