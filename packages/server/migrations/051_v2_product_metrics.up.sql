-- 051: v2_product_metrics
--
-- The product metrics of plan 25 §5.18 that judge the gates of §12:
-- activation and time to first file (Phase 2), week-4 keeping (Phase 3),
-- team pull (Phase 4), and review health. They are computed from receipts,
-- reads, agent connections and imports, by weekly signup cohort, across
-- every space, and returned as counts and durations only: no memory text,
-- no ids. internal/ledger/product_metrics.go holds the definitions in
-- prose; this file holds them in SQL, and the two must say the same.
--
-- # The definitions
--
-- V2 start    the first time the person was a member of a space on the
--             V2 record: for each space they belong to, the later of their
--             joining and the space's move to V2 (hubs.v2_enabled_at, or
--             its first `switched` receipt, which survives a switch back).
--             A person with no V2 start isn't in any cohort: receipts can't
--             see someone who signed up and stopped before a space existed
--             (the web's funnel events can).
-- Cohort      `from_v1` when the person owned a V1 memory (not an
--             onboarding seed) written before their V2 start; else `new`.
--             The gate is about new people. A V1 person's switch connects
--             their V1 keys and grants at once, so counting them would
--             measure the switch, not onboarding: they are reported beside
--             the new cohort, never inside it. People with an admin role
--             are staff, and in neither.
-- Signup      `new`: the account's creation (users.created_at), so someone
--             who signed up on Monday and ran init on Wednesday missed
--             their first session. `from_v1`: their V2 start.
-- First       p_first_session after signup (24 hours from Go). Init takes
-- session     minutes, but an agent's connection appears when that agent
--             first signs in over OAuth, which is when the person next
--             opens it: a day covers "ran init, opened Codex after lunch"
--             and still means the first sitting, not the first week.
-- Activated   by the end of the first session: 2+ agent connections of the
--             person connected (their creation receipt, `connected`), none
--             disconnected by then, AND a compile delivered to a file by the
--             person's own CLI or daemon (a `delivered` receipt about a
--             compile run whose actor is the person, or one of their
--             connections). MCP and copy-out targets are delivered by Memax
--             the moment they compile, so they say nothing about the person.
--             Counted only once the session has closed, numerator and
--             denominator alike.
-- First file  the person's first delivered compile minus the creation of
--             their first `init` import (§7.3's "first file in under five
--             minutes" is init's promise, and the import is init's first
--             trace on the server; detect, sign-in and connect before it
--             have budgets of about a minute). People whose first file came
--             before any init import (memax link, say) aren't in it.
--             Signup to first file is reported beside it.
-- Week 4      activated people with a `kept` receipt of their own in days
--             21 to 28 after signup; eligible once day 28 has passed.
-- Team pull   people for whom another person joined a V2 space they own
--             within 60 days of signup; eligible once day 60 has passed.
--             Plans aren't billed yet, so this is every person, not Pro.
-- Review      per week a proposal was made (its `proposed` receipt at
--             stream version 1): what decided it first (kept, rejected,
--             folded by the judge, forgotten) or nothing yet, and the time
--             from the proposal to a kept or rejected decision.
--
-- # Who may compute them
--
-- Both functions read across spaces, so they follow 038's v2.read_metrics:
-- SECURITY DEFINER, with a function-level SET of app.sweep, and SELECT
-- policies keyed on that value that apply to the function's owner only
-- (the migrating role, `TO CURRENT_USER`), so memax_v2 setting app.sweep
-- itself sees nothing more than its scope (TestSweepPoliciesAreRoleBound).
-- They return counts and durations, never ids.
--
-- Unlike v2.read_metrics, memax_v2 can't call them: a weekly count of a
-- small cohort says who signed up when, which no request needs. Only a new
-- NOLOGIN role, memax_v2_metrics, may execute them, and it has no other
-- privilege (no table, no column): Ledger.GetProductMetrics switches to it
-- for the one read, as the sweepers do (039, 040, 047). The worker's daily
-- job, the admin endpoint and cmd/v2-gate-metrics all go through it.

-- ---------------------------------------------------------------------
-- The metrics role
-- ---------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2_metrics') THEN
        BEGIN
            CREATE ROLE memax_v2_metrics NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2_metrics' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2_metrics must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM pg_auth_members m
          JOIN pg_roles r ON r.oid = m.roleid
          JOIN pg_roles u ON u.oid = m.member
         WHERE r.rolname = 'memax_v2_metrics' AND u.rolname = current_user
    ) THEN
        BEGIN
            EXECUTE format('GRANT memax_v2_metrics TO %I', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2_metrics;

-- ---------------------------------------------------------------------
-- Cohorts: activation, first file, week 4, team pull
-- ---------------------------------------------------------------------

-- One row per (signup week, cohort) for people who signed up in
-- [p_from, p_to), and one per cohort over the whole range (week NULL), as
-- of p_now: anything recorded after p_now is left out, and a window that
-- hasn't closed by p_now is in no denominator. Durations are seconds, to
-- a tenth.
CREATE FUNCTION v2.product_metrics(p_from timestamptz, p_to timestamptz, p_first_session interval, p_now timestamptz)
    RETURNS TABLE (week date, cohort text, people bigint, sessions_closed bigint,
                   two_connections bigint, two_agent_kinds bigint, compiled bigint, activated bigint, agents_read bigint,
                   first_files bigint, first_files_under_5m bigint, first_file_p50 double precision,
                   first_file_p90 double precision, signup_to_file_p50 double precision,
                   retention_eligible bigint, retained bigint, team_eligible bigint, team_pulled bigint)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET TimeZone = 'UTC'
    SET app.sweep = 'product_metrics'
    AS $$
    WITH switched AS (
        -- When each space first moved to the V2 record (a switch back
        -- clears v2_enabled_at; the receipt stays).
        SELECT r.space_id, min(r.recorded_at) AS at
          FROM v2.receipts r
         WHERE r.object_kind = 'space' AND r.action = 'switched' AND r.recorded_at <= p_now
         GROUP BY r.space_id
    ), v2_spaces AS MATERIALIZED (
        SELECT h.id, h.owner_id, LEAST(h.v2_enabled_at, s.at) AS v2_at
          FROM public.hubs h
          LEFT JOIN switched s ON s.space_id = h.id
         WHERE h.v2_enabled_at IS NOT NULL OR s.at IS NOT NULL
    ), starts AS (
        SELECT m.user_id AS person_id, min(GREATEST(m.joined_at, v.v2_at)) AS v2_start
          FROM public.hub_members m
          JOIN v2_spaces v ON v.id = m.hub_id
         GROUP BY m.user_id
    ), people AS (
        SELECT u.id AS person_id, k.kind,
               CASE k.kind WHEN 'new' THEN u.created_at ELSE s.v2_start END AS signup_at
          FROM starts s
          JOIN public.users u ON u.id = s.person_id
          CROSS JOIN LATERAL (
              SELECT CASE WHEN EXISTS (
                         SELECT 1 FROM public.memories m
                          WHERE m.owner_id = u.id AND m.created_at < s.v2_start
                            AND m.source_kind IS DISTINCT FROM 'onboarding-seed')
                     THEN 'from_v1' ELSE 'new' END AS kind) k
         WHERE s.v2_start <= p_now
           AND NOT EXISTS (SELECT 1 FROM public.admin_roles a WHERE a.user_id = u.id)
    ), cohort AS MATERIALIZED (
        SELECT p.person_id, p.kind, p.signup_at, date_trunc('week', p.signup_at)::date AS week,
               p.signup_at + p_first_session AS session_end
          FROM people p
         WHERE p.signup_at >= p_from AND p.signup_at < p_to AND p.signup_at <= p_now
    ), conns AS MATERIALIZED (
        -- The cohort's agent connections and when each was connected (its
        -- creation receipt).
        SELECT c.person_id, c.id, c.agent, r.recorded_at AS connected_at, c.disconnected_at
          FROM cohort p
          JOIN v2.agent_connections c ON c.person_id = p.person_id
          JOIN v2.receipts r ON r.id = c.created_receipt_id
         WHERE r.recorded_at <= p_now
    ), acts AS MATERIALIZED (
        -- The cohort's own deliveries (their CLI or daemon wrote a compile
        -- to disk) and Keeps. An agent's keep isn't a person keeping.
        SELECT p.person_id, r.action, r.recorded_at
          FROM v2.receipts r
          JOIN cohort p ON r.actor_kind = 'person' AND r.actor_id = p.person_id
         WHERE ((r.action = 'delivered' AND r.object_kind = 'compile') OR (r.action = 'kept' AND r.object_kind = 'memory'))
           AND r.recorded_at >= p_from AND r.recorded_at <= p_now
        UNION ALL
        SELECT c.person_id, r.action, r.recorded_at
          FROM v2.receipts r
          JOIN conns c ON r.actor_kind = 'agent' AND r.actor_id = c.id
         WHERE r.action = 'delivered' AND r.object_kind = 'compile'
           AND r.recorded_at >= p_from AND r.recorded_at <= p_now
    ), per_act AS (
        SELECT a.person_id,
               bool_or(a.action = 'delivered' AND a.recorded_at <= p.session_end) AS compiled,
               min(a.recorded_at) FILTER (WHERE a.action = 'delivered') AS first_file_at,
               bool_or(a.action = 'kept' AND a.recorded_at >= p.signup_at + interval '21 days'
                                         AND a.recorded_at < p.signup_at + interval '28 days') AS kept_week4
          FROM acts a
          JOIN cohort p ON p.person_id = a.person_id
         GROUP BY a.person_id
    ), per_conn AS (
        SELECT c.person_id, count(*) AS connections, count(DISTINCT c.agent) AS kinds
          FROM conns c
          JOIN cohort p ON p.person_id = c.person_id
         WHERE c.connected_at <= p.session_end
           AND (c.disconnected_at IS NULL OR c.disconnected_at > p.session_end)
         GROUP BY c.person_id
    ), init_imports AS (
        -- init's first upload, by the person or through one of their
        -- connections.
        SELECT x.person_id, min(x.created_at) AS first_import_at
          FROM (SELECT p.person_id, i.created_at
                  FROM v2.imports i
                  JOIN cohort p ON i.actor_kind = 'person' AND i.actor_id = p.person_id
                 WHERE i.origin = 'init' AND i.created_at <= p_now
                UNION ALL
                SELECT c.person_id, i.created_at
                  FROM v2.imports i
                  JOIN conns c ON i.actor_kind = 'agent' AND i.actor_id = c.id
                 WHERE i.origin = 'init' AND i.created_at <= p_now) x
         GROUP BY x.person_id
    ), read_in_session AS (
        -- An agent of theirs read (MCP, or a session-start hook's compile
        -- load) in the first session. reads.person_id is the person an
        -- agent works for.
        SELECT DISTINCT p.person_id
          FROM v2.reads r
          JOIN cohort p ON p.person_id = r.person_id
         WHERE r.read_at >= p_from AND r.read_at < p_to + p_first_session
           AND r.read_at >= p.signup_at AND r.read_at <= LEAST(p.session_end, p_now)
    ), pulled AS (
        SELECT DISTINCT p.person_id
          FROM cohort p
          JOIN v2_spaces v ON v.owner_id = p.person_id
          JOIN public.hub_members m ON m.hub_id = v.id AND m.user_id <> p.person_id
         WHERE m.joined_at >= p.signup_at AND m.joined_at < p.signup_at + interval '60 days'
           AND m.joined_at <= p_now
    ), per_person AS (
        SELECT p.week, p.kind, p.signup_at,
               p.session_end <= p_now AS closed,
               COALESCE(pc.connections, 0) >= 2 AS two_connections,
               COALESCE(pc.kinds, 0) >= 2 AS two_kinds,
               COALESCE(pa.compiled, false) AS compiled,
               COALESCE(pc.connections, 0) >= 2 AND COALESCE(pa.compiled, false) AS activated,
               rs.person_id IS NOT NULL AS agents_read,
               pa.first_file_at,
               CASE WHEN pa.first_file_at >= ii.first_import_at
                    THEN extract(epoch FROM pa.first_file_at - ii.first_import_at) END AS first_file_s,
               extract(epoch FROM pa.first_file_at - p.signup_at) AS signup_to_file_s,
               COALESCE(pa.kept_week4, false) AS kept_week4,
               p.signup_at + interval '28 days' <= p_now AS week4_closed,
               p.signup_at + interval '60 days' <= p_now AS team_closed,
               pl.person_id IS NOT NULL AS pulled
          FROM cohort p
          LEFT JOIN per_conn pc ON pc.person_id = p.person_id
          LEFT JOIN per_act pa ON pa.person_id = p.person_id
          LEFT JOIN init_imports ii ON ii.person_id = p.person_id
          LEFT JOIN read_in_session rs ON rs.person_id = p.person_id
          LEFT JOIN pulled pl ON pl.person_id = p.person_id
    )
    SELECT x.week, x.kind, count(*),
           count(*) FILTER (WHERE x.closed),
           count(*) FILTER (WHERE x.closed AND x.two_connections),
           count(*) FILTER (WHERE x.closed AND x.two_kinds),
           count(*) FILTER (WHERE x.closed AND x.compiled),
           count(*) FILTER (WHERE x.closed AND x.activated),
           count(*) FILTER (WHERE x.closed AND x.agents_read),
           count(x.first_file_s),
           count(*) FILTER (WHERE x.first_file_s < 300),
           round(percentile_cont(0.5) WITHIN GROUP (ORDER BY x.first_file_s)::numeric, 1)::double precision,
           round(percentile_cont(0.9) WITHIN GROUP (ORDER BY x.first_file_s)::numeric, 1)::double precision,
           round(percentile_cont(0.5) WITHIN GROUP (ORDER BY x.signup_to_file_s)::numeric, 1)::double precision,
           count(*) FILTER (WHERE x.closed AND x.activated AND x.week4_closed),
           count(*) FILTER (WHERE x.closed AND x.activated AND x.week4_closed AND x.kept_week4),
           count(*) FILTER (WHERE x.team_closed),
           count(*) FILTER (WHERE x.team_closed AND x.pulled)
      FROM per_person x
     GROUP BY GROUPING SETS ((x.week, x.kind), (x.kind))
     ORDER BY x.kind, x.week NULLS LAST
$$;

COMMENT ON FUNCTION v2.product_metrics(timestamptz, timestamptz, interval, timestamptz) IS
    'The gates'' cohort metrics (plan 25 §5.18, §12): activation, first file, week-4 keeping, team pull. Counts and seconds only; memax_v2_metrics only.';

-- ---------------------------------------------------------------------
-- Review health
-- ---------------------------------------------------------------------

-- One row per week (Monday, UTC) in which proposals were made in
-- [p_from, p_to), and one for the whole range (week NULL): how many, what
-- decided each first, and the time from proposal to Keep or Reject.
CREATE FUNCTION v2.review_health(p_from timestamptz, p_to timestamptz, p_now timestamptz)
    RETURNS TABLE (week date, proposals bigint, kept bigint, rejected bigint, folded bigint, forgotten bigint,
                   open bigint, decision_p50 double precision, decision_p90 double precision)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET TimeZone = 'UTC'
    SET app.sweep = 'product_metrics'
    AS $$
    WITH proposed AS MATERIALIZED (
        SELECT r.object_id, r.space_id, r.seq, r.recorded_at AS proposed_at,
               date_trunc('week', r.recorded_at)::date AS week
          FROM v2.receipts r
         WHERE r.object_kind = 'memory' AND r.action = 'proposed' AND r.stream_version = 1
           AND r.recorded_at >= p_from AND r.recorded_at < p_to AND r.recorded_at <= p_now
    ), outcome AS (
        SELECT p.week, d.action,
               CASE WHEN d.action IN ('kept', 'rejected') THEN extract(epoch FROM d.recorded_at - p.proposed_at) END AS decision_s
          FROM proposed p
          LEFT JOIN LATERAL (
              SELECT r.action, r.recorded_at
                FROM v2.receipts r
               WHERE r.object_id = p.object_id AND r.seq > p.seq
                 AND r.action IN ('kept', 'rejected', 'merged', 'forgot')
                 AND r.recorded_at <= p_now
               ORDER BY r.seq
               LIMIT 1) d ON true
    )
    SELECT o.week, count(*),
           count(*) FILTER (WHERE o.action = 'kept'),
           count(*) FILTER (WHERE o.action = 'rejected'),
           count(*) FILTER (WHERE o.action = 'merged'),
           count(*) FILTER (WHERE o.action = 'forgot'),
           count(*) FILTER (WHERE o.action IS NULL),
           round(percentile_cont(0.5) WITHIN GROUP (ORDER BY o.decision_s)::numeric, 1)::double precision,
           round(percentile_cont(0.9) WITHIN GROUP (ORDER BY o.decision_s)::numeric, 1)::double precision
      FROM outcome o
     GROUP BY GROUPING SETS ((o.week), ())
     ORDER BY o.week NULLS LAST
$$;

COMMENT ON FUNCTION v2.review_health(timestamptz, timestamptz, timestamptz) IS
    'Review health (plan 25 §5.18): proposals per week, how each was first decided, and the time to Keep or Reject. Counts and seconds only; memax_v2_metrics only.';

-- ---------------------------------------------------------------------
-- Row-level security: rows admitted only inside the two functions
-- ---------------------------------------------------------------------

-- Each applies to the functions' owner only (TO CURRENT_USER), never to
-- memax_v2, so setting app.sweep from a request admits nothing.
CREATE POLICY receipts_product_metrics ON v2.receipts FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'product_metrics');
CREATE POLICY agent_connections_product_metrics ON v2.agent_connections FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'product_metrics');
CREATE POLICY imports_product_metrics ON v2.imports FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'product_metrics');
CREATE POLICY reads_product_metrics ON v2.reads FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'product_metrics');

-- ---------------------------------------------------------------------
-- Grants: execute only, for the metrics role only
-- ---------------------------------------------------------------------

REVOKE ALL ON FUNCTION v2.product_metrics(timestamptz, timestamptz, interval, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.review_health(timestamptz, timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.product_metrics(timestamptz, timestamptz, interval, timestamptz) TO memax_v2_metrics;
GRANT EXECUTE ON FUNCTION v2.review_health(timestamptz, timestamptz, timestamptz) TO memax_v2_metrics;
