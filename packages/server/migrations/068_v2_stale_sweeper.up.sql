-- 068: v2_stale_sweeper
--
-- Dates move from Dream to an hourly sweep (the stale-detection design
-- §2.3 and decision 7): a kept memory whose date to check again
-- (stale_after) has passed is flagged stale by Memax within the hour, via
-- system, its source the date ({kind: date, ref: 2026-10-01}), instead of
-- at the space's next edition (up to a week later on Free). Dream stops
-- flagging dates and lists what went stale instead, so one actor flags.
--
-- The sweep's one cross-space read (which V2 spaces have a memory due)
-- runs as memax_v2_stale_sweeper (NOLOGIN, NOSUPERUSER, NOBYPASSRLS),
-- whose policies are keyed on app.sweep = 'stale' and apply to that role
-- only, as 039, 040 and 047 do: memax_v2 setting app.sweep sees nothing
-- more. It reads a few columns of kept memories with a date, whether a
-- person acted on one since its date (a person's receipt: who and when,
-- never what), and which spaces are on the V2 record. Each space's flags
-- are then a ledger command (FlagDue) in that space's own scope.
--
-- A memory comes due once, so the sweep reads an index of kept memories
-- with a date that aren't stale.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2_stale_sweeper') THEN
        BEGIN
            CREATE ROLE memax_v2_stale_sweeper NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2_stale_sweeper' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2_stale_sweeper must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    -- The migrating role must be able to SET ROLE to it (Neon's owner gets
    -- ADMIN, not SET, on a role it creates: see 040).
    IF NOT pg_has_role(current_user, 'memax_v2_stale_sweeper', 'SET') THEN
        BEGIN
            EXECUTE format('GRANT memax_v2_stale_sweeper TO %I WITH SET TRUE', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2_stale_sweeper;
-- Names in schema public (040 says why).
GRANT USAGE ON SCHEMA public TO memax_v2_stale_sweeper;

GRANT SELECT (id, space_id, lifecycle, flags, stale_after, created_receipt_id) ON v2.memories TO memax_v2_stale_sweeper;
CREATE POLICY memories_stale_sweep ON v2.memories FOR SELECT TO memax_v2_stale_sweeper
    USING (current_setting('app.sweep', true) = 'stale'
           AND lifecycle = 'kept' AND stale_after IS NOT NULL);

GRANT SELECT (id, space_id, stream_id, actor_kind, recorded_at) ON v2.receipts TO memax_v2_stale_sweeper;
CREATE POLICY receipts_stale_sweep ON v2.receipts FOR SELECT TO memax_v2_stale_sweeper
    USING (current_setting('app.sweep', true) = 'stale' AND actor_kind = 'person');

GRANT SELECT (id, v2_enabled_at) ON public.hubs TO memax_v2_stale_sweeper;

-- Kept memories with a date that aren't stale yet, by date: the sweep's
-- read across spaces (047's memories_stale_due_idx is per space).
CREATE INDEX memories_date_due_idx ON v2.memories (stale_after)
    WHERE stale_after IS NOT NULL AND lifecycle = 'kept' AND NOT ('stale' = ANY (flags));
