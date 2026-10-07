-- 050: v2_notification_settings
--
-- A person's notification preferences (plan 25 §9, Phase 2 epic 2.6; the
-- handoff's Notifications board): for each event Memax can tell them
-- about, whether it reaches them by email, and the quiet hours, in their
-- own time zone, during which email waits.
--
--   notification_settings  one row per person: their email choices (only
--                          the ones they made; the others follow the
--                          defaults in ledger.NotificationEvents), the
--                          quiet hours, whether decision gates still come
--                          through them, how long a proposal waits before
--                          the Review reminder, and a version, which is
--                          the ETag an edit sends as If-Match.
--
-- # One copy of each choice
--
-- The morning edition's email is not stored here. It stays where Dream
-- reads it and where its one-click unsubscribe turns it off,
-- v2.dream_settings.morning_email (047), and so does the person's time
-- zone, which the quiet hours are read in. The settings endpoint reads and
-- writes that column. So that an edit made before an unsubscribe can't
-- quietly turn the email back on, a trigger on dream_settings moves this
-- row's version whenever morning_email changes, by any path (the link,
-- Settings › Account, the notifications page): the late edit then answers
-- 412 edit_clash.
--
-- The defaults live in the column defaults below and in
-- ledger.DefaultNotificationSettings, which reads a person without a row
-- (TestNotificationDefaultsMatchTheSchema keeps the two equal). A person
-- with no row is version 1, as is a row nobody has edited, so the trigger
-- making a row never clashes with a version someone read before it.
--
-- # V1's framework
--
-- V1's notifications (plan 17) are an inbox, public.notifications, and one
-- switch, notifications_enabled in public.user_preferences. Neither holds a
-- person's per-event choices, and neither sits under V2's row-level
-- security, so the choices live here, beside dream_settings. Delivery goes
-- through V1's email framework (internal/email), as the morning email
-- already does.
--
-- # Not the record
--
-- Preferences aren't the record, so they carry no receipts (as
-- dream_settings doesn't), and they hold no memory's words. RLS is ENABLEd
-- and FORCEd, keyed on app.person_id; memax_v2 may read, insert and update
-- its person's row and never delete it (a person's row goes with the
-- person). The dream sweeper reads the quiet hours, to hold a morning
-- email until they end, through a policy keyed on app.sweep = 'dream' that
-- applies to that role only (TestSweepPoliciesAreRoleBound).

CREATE TABLE v2.notification_settings (
    person_id         uuid PRIMARY KEY REFERENCES public.users (id) ON DELETE CASCADE,
    version           integer NOT NULL DEFAULT 1,
    -- {"<event>": true|false}: only the email choices the person made.
    email             jsonb NOT NULL DEFAULT '{}'::jsonb,
    quiet_on          boolean NOT NULL DEFAULT true,
    quiet_from        time NOT NULL DEFAULT '20:00',
    quiet_until       time NOT NULL DEFAULT '08:00',
    -- Decision gates still reach the person during quiet hours.
    gates_through     boolean NOT NULL DEFAULT true,
    -- A proposal waiting this many days brings the daily Review reminder.
    review_after_days smallint NOT NULL DEFAULT 1,
    -- The last edit's Idempotency-Key and a SHA-256 of its request, so a
    -- retry of it replays instead of clashing with its own new version.
    last_key          text,
    last_key_hash     bytea,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_settings_version_check CHECK (version >= 1),
    CONSTRAINT notification_settings_email_check CHECK (jsonb_typeof(email) = 'object' AND NOT email ? 'morning_edition'),
    CONSTRAINT notification_settings_quiet_check CHECK (quiet_from <> quiet_until),
    CONSTRAINT notification_settings_days_check CHECK (review_after_days BETWEEN 1 AND 14),
    CONSTRAINT notification_settings_key_check CHECK (
        (last_key IS NULL) = (last_key_hash IS NULL) AND (last_key IS NULL OR char_length(last_key) BETWEEN 1 AND 255))
);

COMMENT ON TABLE v2.notification_settings IS
    'A person''s notification preferences: email per event, quiet hours, the Review reminder. The morning edition''s email and the time zone stay in v2.dream_settings.';

-- The morning email's choice changed (dream_settings, 047): move the
-- version, making the row if there is none (version 2: a person without a
-- row reads as version 1). It runs as whoever changed the setting: the
-- person (memax_v2, scoped to them by app.person_id) or the one-click
-- unsubscribe function (v2.unsubscribe_dream_email, as its owner).
CREATE FUNCTION v2.dream_email_moves_notification_version() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    INSERT INTO v2.notification_settings AS n (person_id, version) VALUES (NEW.person_id, 2)
    ON CONFLICT (person_id) DO UPDATE SET version = n.version + 1, updated_at = now();
    RETURN NULL;
END $$;

CREATE TRIGGER dream_settings_email_moves_version
    AFTER UPDATE OF morning_email ON v2.dream_settings
    FOR EACH ROW WHEN (OLD.morning_email IS DISTINCT FROM NEW.morning_email)
    EXECUTE FUNCTION v2.dream_email_moves_notification_version();

-- A row made with the email off (its default is on) changes it too.
CREATE TRIGGER dream_settings_email_off_moves_version
    AFTER INSERT ON v2.dream_settings
    FOR EACH ROW WHEN (NOT NEW.morning_email)
    EXECUTE FUNCTION v2.dream_email_moves_notification_version();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.notification_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.notification_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY notification_settings_person ON v2.notification_settings TO memax_v2
    USING (person_id = (SELECT v2.current_person_id()))
    WITH CHECK (person_id = (SELECT v2.current_person_id()));
CREATE POLICY notification_settings_dream_sweep ON v2.notification_settings FOR SELECT TO memax_v2_dream_sweeper
    USING (current_setting('app.sweep', true) = 'dream');

-- ---------------------------------------------------------------------
-- Grants: the least each role needs
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.notification_settings TO memax_v2;
GRANT UPDATE (version, email, quiet_on, quiet_from, quiet_until, gates_through, review_after_days,
              last_key, last_key_hash, updated_at) ON v2.notification_settings TO memax_v2;
GRANT SELECT (person_id, quiet_on, quiet_from, quiet_until) ON v2.notification_settings TO memax_v2_dream_sweeper;
