-- 046: v2_dream
--
-- Dream editions (plan 25 §5.10, Phase 2 epic 2.2). Dream is the
-- overnight upkeep of a space, done in the open: each run is an edition
-- (D-n per tenant) of small actions, each one a ledger change applied as
-- Dream, with its receipts, and each one undoable.
--
--   dream_editions     one per run that had something to read (D-): when
--                      it ran, the input window it read (notes after a
--                      cursor, record changes after the previous edition),
--                      counts, and what it found that needs a person
--   dream_actions      every change an edition made: fold notes into a
--                      memory, propose a new fact from notes, fold a
--                      duplicate proposal, flag a conflict, flag a stale
--                      fact, fade an unread one, revise the Brief. Each
--                      holds its inverse (states, versions and links;
--                      never words) and, once undone, the undid receipt
--   note_refs          N- display IDs for the notes (V1 memories, until
--                      cutover) an edition read
--   dream_schedules    when each V2 space is next due, in its owner's
--                      local night (bookkeeping: no receipts)
--   dream_settings     a person's time zone and the morning email
--   dream_email_sends  which morning emails went out (bookkeeping)
--
-- The guarantees of 028 hold for the record tables (editions, actions,
-- note refs):
--
-- 1. Receipt or refused. A deferred constraint trigger refuses the COMMIT
--    unless each inserted or updated row points at a receipt written in
--    the same transaction, in the same space, about the edition, the
--    memory the action changed or the Brief. Editions and note refs never
--    change; an action changes once, when it is undone, and only its
--    undo columns.
--
-- 2. No words. Editions and actions hold ids, refs, states, versions and
--    counts. The words Dream writes live where every other word lives
--    (memory_versions for a proposal, brief_versions for Brief prose), so
--    Forget purges them in the one place, and an edition shows a forgotten
--    memory as forgotten, never its words.
--
-- 3. Isolation. RLS is ENABLEd and FORCEd on every table, keyed on
--    app.space_ids (dream_settings: app.person_id), and memax_v2 gets only
--    the grants the ledger needs: no DELETE, no TRUNCATE.
--
-- # The sweep (no River Pro)
--
-- §5.17: Dream is scheduled by an idempotent catch-up sweep over a
-- due-time column, as the compile sweeper, the seal sweep and gate expiry
-- are. dream_schedules.due_at is when a space's next edition is due, in
-- its owner's local night. The sweep runs as its own role,
-- memax_v2_dream_sweeper (NOLOGIN, NOBYPASSRLS), whose policies are keyed
-- on app.sweep = 'dream' and apply to that role only, as 039 and 040 do:
-- memax_v2 setting app.sweep sees nothing more. It reads which spaces are
-- on V2, their owners' zones and the week's activity (to rank the owner's
-- busiest spaces for the plan's nightly cadence), moves each due space's
-- due_at past now (once, however many nights it missed) and queues one
-- dream_space job per (space, slot); an edition is unique per slot.
--
-- # Deleting a space
--
-- Dream's rows of a space go with its hub (ON DELETE CASCADE) or with the
-- memories and Brief versions they name, so v2.retire_space (044) needs
-- no change: everything that remains of a retired space is content-free.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains published (an edition) and folded (notes
-- folded into a memory as lineage). flagged, merged, proposed, faded,
-- restored, revised and undid were already there. The list keeps every
-- verb 028, 029, 031, 035, 036, 042 and 044 admit.

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
     'returned', 'drafted',
     'purged', 'forget_requested', 'forget_declined',
     'published', 'folded'));

-- What changed in a space since a time: Dream's input check, and the
-- sweep's ranking of an owner's busiest spaces.
CREATE INDEX receipts_space_recorded_idx ON v2.receipts (space_id, recorded_at);

-- Kept memories with a date to be checked again: Dream's stale phase.
CREATE INDEX memories_stale_due_idx ON v2.memories (space_id, stale_after)
    WHERE stale_after IS NOT NULL AND lifecycle = 'kept';

-- ---------------------------------------------------------------------
-- The dream sweeper's role
-- ---------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'memax_v2_dream_sweeper') THEN
        BEGIN
            CREATE ROLE memax_v2_dream_sweeper NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- created concurrently by a migration on another database
        END;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_roles
         WHERE rolname = 'memax_v2_dream_sweeper' AND (rolsuper OR rolbypassrls OR rolcanlogin)
    ) THEN
        RAISE EXCEPTION 'role memax_v2_dream_sweeper must be NOLOGIN, NOSUPERUSER and NOBYPASSRLS'
            USING HINT = 'Row-level security on schema v2 depends on it. Fix the role with ALTER ROLE, then rerun the migration.';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM pg_auth_members m
          JOIN pg_roles r ON r.oid = m.roleid
          JOIN pg_roles u ON u.oid = m.member
         WHERE r.rolname = 'memax_v2_dream_sweeper' AND u.rolname = current_user
    ) THEN
        BEGIN
            EXECUTE format('GRANT memax_v2_dream_sweeper TO %I', current_user);
        EXCEPTION WHEN duplicate_object OR unique_violation THEN
            NULL; -- granted concurrently
        END;
    END IF;
END $$;

GRANT USAGE ON SCHEMA v2 TO memax_v2_dream_sweeper;

-- Which spaces are on V2, whose they are, and who may keep in them (the
-- morning email's recipients). Ids, roles and addresses; no memory text.
GRANT SELECT (id, tenant_id, owner_id, space_kind, name, slug, rules, v2_enabled_at) ON public.hubs TO memax_v2_dream_sweeper;
GRANT SELECT (hub_id, user_id, role) ON public.hub_members TO memax_v2_dream_sweeper;
GRANT SELECT (id, email, name, display_name) ON public.users TO memax_v2_dream_sweeper;

-- The week's activity per space, to rank an owner's busiest spaces.
GRANT SELECT (space_id, recorded_at, actor_kind) ON v2.receipts TO memax_v2_dream_sweeper;
CREATE POLICY receipts_dream_sweep ON v2.receipts FOR SELECT TO memax_v2_dream_sweeper
    USING (current_setting('app.sweep', true) = 'dream');

-- ---------------------------------------------------------------------
-- Notes: what Dream reads of them
-- ---------------------------------------------------------------------

-- The view keeps 028's columns, in order, and adds what Dream needs: more
-- of the body for the model, and who wrote the note.
CREATE OR REPLACE VIEW v2.notes WITH (security_barrier = true) AS
    SELECT m.id, m.owner_id, m.hub_id AS space_id, m.title,
           left(m.content, 280) AS excerpt, m.state, m.created_at,
           left(m.content, 4000) AS body,
           CASE WHEN m.created_by_type = 'agent' OR COALESCE(m.created_by_slug, '') <> '' OR COALESCE(m.source_agent, '') <> ''
                THEN 'agent' ELSE 'person' END AS author_kind,
           COALESCE(NULLIF(m.created_by_slug, ''), NULLIF(m.source_agent, '')) AS agent,
           m.source, m.content_type, m.source_path
      FROM public.memories m
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[]);

-- ---------------------------------------------------------------------
-- Editions (D-)
-- ---------------------------------------------------------------------

CREATE TABLE v2.dream_editions (
    id              uuid PRIMARY KEY,                     -- uuidv7, from Go; also the receipts' stream
    tenant_id       uuid NOT NULL,
    space_id        uuid NOT NULL,
    seq             bigint NOT NULL,                      -- display number: D-<seq>, per tenant
    slot            timestamptz NOT NULL,                 -- the night (or the run-now moment) it answers
    trigger         text NOT NULL,                        -- schedule | manual
    requested_by    uuid,                                 -- the person who asked (manual)
    since           timestamptz,                          -- the input window: record changes after since (the previous edition's until) …
    until           timestamptz NOT NULL,                 -- … up to until
    note_cursor_at  timestamptz,                          -- the last note read: the next edition reads notes after it
    note_cursor_id  uuid,
    started_at      timestamptz NOT NULL,
    finished_at     timestamptz NOT NULL,
    notes_read      uuid[] NOT NULL DEFAULT '{}',         -- the notes it read (V1 public.memories ids; no FK, see 028)
    stats           jsonb NOT NULL DEFAULT '{}'::jsonb,   -- counts and timings; never words
    surfaced        jsonb NOT NULL DEFAULT '[]'::jsonb,   -- what needs a person and isn't Dream's doing: ids only
    receipt_id      uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dream_editions_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT dream_editions_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT dream_editions_slot_key UNIQUE (space_id, slot),
    CONSTRAINT dream_editions_id_space_key UNIQUE (id, space_id),
    CONSTRAINT dream_editions_seq_check CHECK (seq >= 1),
    CONSTRAINT dream_editions_trigger_check CHECK (trigger IN ('schedule', 'manual')),
    CONSTRAINT dream_editions_requested_check CHECK ((trigger = 'manual') = (requested_by IS NOT NULL)),
    CONSTRAINT dream_editions_window_check CHECK (since IS NULL OR since <= until),
    CONSTRAINT dream_editions_cursor_check CHECK ((note_cursor_at IS NULL) = (note_cursor_id IS NULL)),
    CONSTRAINT dream_editions_times_check CHECK (finished_at >= started_at),
    CONSTRAINT dream_editions_notes_check CHECK (cardinality(notes_read) <= 1000),
    CONSTRAINT dream_editions_stats_check CHECK (jsonb_typeof(stats) = 'object'),
    CONSTRAINT dream_editions_surfaced_check CHECK (jsonb_typeof(surfaced) = 'array')
);

CREATE INDEX dream_editions_space_seq_idx ON v2.dream_editions (space_id, seq DESC);

COMMENT ON TABLE v2.dream_editions IS
    'D-: one Dream run that had something to read, published when it ended. Ids and counts, never words.';

-- ---------------------------------------------------------------------
-- Actions
-- ---------------------------------------------------------------------

CREATE TABLE v2.dream_actions (
    id                uuid PRIMARY KEY,
    tenant_id         uuid NOT NULL,
    space_id          uuid NOT NULL,
    edition_id        uuid NOT NULL,
    n                 integer NOT NULL,                     -- its place in the edition, from 1
    kind              text NOT NULL,                        -- fold | propose | dedupe | conflict | stale | fade | brief
    memory_id         uuid,                                 -- the memory it changed (or created)
    version           integer,                              -- that memory's statement version when Dream acted
    related_memory_id uuid,                                 -- dedupe: the proposal kept in Review; conflict: the other side
    brief_id          uuid,                                 -- brief: the Brief …
    brief_version     integer,                              -- … and the version Dream wrote
    note_ids          uuid[] NOT NULL DEFAULT '{}',         -- the notes it rests on
    receipt_ids       uuid[] NOT NULL,                      -- every receipt the action wrote
    inverse           jsonb NOT NULL,                       -- what undoing it restores: states, versions, links; never words
    undone_by         uuid REFERENCES v2.receipts (id),     -- the undid receipt
    undone_at         timestamptz,
    receipt_id        uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id   uuid NOT NULL REFERENCES v2.receipts (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    -- Deferred: an edition's row is written last, once its actions are.
    CONSTRAINT dream_actions_edition_fkey FOREIGN KEY (edition_id, space_id)
        REFERENCES v2.dream_editions (id, space_id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT dream_actions_memory_fkey FOREIGN KEY (memory_id, space_id)
        REFERENCES v2.memories (id, space_id) ON DELETE CASCADE,
    CONSTRAINT dream_actions_related_fkey FOREIGN KEY (related_memory_id, space_id)
        REFERENCES v2.memories (id, space_id) ON DELETE CASCADE,
    CONSTRAINT dream_actions_brief_fkey FOREIGN KEY (brief_id, brief_version)
        REFERENCES v2.brief_versions (brief_id, version) ON DELETE CASCADE,
    CONSTRAINT dream_actions_n_key UNIQUE (edition_id, n),
    CONSTRAINT dream_actions_n_check CHECK (n >= 1),
    CONSTRAINT dream_actions_kind_check CHECK (kind IN ('fold', 'propose', 'dedupe', 'conflict', 'stale', 'fade', 'brief')),
    CONSTRAINT dream_actions_target_check CHECK (
        CASE WHEN kind = 'brief' THEN brief_id IS NOT NULL AND brief_version IS NOT NULL AND memory_id IS NULL
             ELSE memory_id IS NOT NULL AND version IS NOT NULL AND brief_id IS NULL AND brief_version IS NULL END),
    CONSTRAINT dream_actions_related_check CHECK ((kind IN ('dedupe', 'conflict')) = (related_memory_id IS NOT NULL)),
    CONSTRAINT dream_actions_notes_check CHECK (cardinality(note_ids) <= 200
        AND (kind IN ('fold', 'propose')) = (cardinality(note_ids) >= 1)),
    CONSTRAINT dream_actions_receipts_check CHECK (cardinality(receipt_ids) >= 1),
    CONSTRAINT dream_actions_inverse_check CHECK (jsonb_typeof(inverse) = 'object'),
    CONSTRAINT dream_actions_undone_check CHECK ((undone_by IS NULL) = (undone_at IS NULL))
);

CREATE INDEX dream_actions_memory_idx ON v2.dream_actions (memory_id) WHERE memory_id IS NOT NULL;
CREATE INDEX dream_actions_related_idx ON v2.dream_actions (related_memory_id) WHERE related_memory_id IS NOT NULL;
CREATE INDEX dream_actions_space_idx ON v2.dream_actions (space_id, created_at DESC);
-- Undo's "later changes" ignores receipts of commands that were undone.
CREATE INDEX dream_actions_undone_idx ON v2.dream_actions USING gin (receipt_ids) WHERE undone_by IS NOT NULL;

COMMENT ON TABLE v2.dream_actions IS
    'Every change a Dream edition made, with its inverse for Undo. States, versions and links; never words.';

-- An action changes once: when it is undone.
CREATE FUNCTION v2.dream_actions_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.dream_actions;
BEGIN
    n := NEW;
    n.undone_by := OLD.undone_by;
    n.undone_at := OLD.undone_at;
    n.last_receipt_id := OLD.last_receipt_id;
    IF OLD.undone_by IS NOT NULL OR NEW.undone_by IS NULL OR n IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'dream action %: an action changes once, when it is undone', OLD.id USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER dream_actions_guard
    BEFORE UPDATE ON v2.dream_actions
    FOR EACH ROW EXECUTE FUNCTION v2.dream_actions_guard();

-- ---------------------------------------------------------------------
-- Note display IDs (N-)
-- ---------------------------------------------------------------------

CREATE TABLE v2.note_refs (
    note_id    uuid PRIMARY KEY,                            -- the V1 public.memories id (no FK, see 028)
    tenant_id  uuid NOT NULL,
    space_id   uuid NOT NULL,
    seq        bigint NOT NULL,                             -- display number: N-<seq>, per tenant
    edition_id uuid NOT NULL,                               -- the edition that first read it
    receipt_id uuid NOT NULL REFERENCES v2.receipts (id),   -- that edition's published receipt
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT note_refs_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT note_refs_edition_fkey FOREIGN KEY (edition_id, space_id)
        REFERENCES v2.dream_editions (id, space_id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT note_refs_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT note_refs_seq_check CHECK (seq >= 1)
);

CREATE INDEX note_refs_space_idx ON v2.note_refs (space_id);

COMMENT ON TABLE v2.note_refs IS
    'N-: display IDs for notes Dream has read, allocated by the edition that first read them.';

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1)
-- ---------------------------------------------------------------------

-- Its own function, like 036's v2.require_gate_receipt, so the receipt
-- check of the other record tables is untouched.
CREATE FUNCTION v2.require_dream_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    objects uuid[];
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'dream_editions' THEN
        rid := NEW.last_receipt_id; objects := ARRAY[NEW.id];
    WHEN 'dream_actions' THEN
        rid := NEW.last_receipt_id; objects := ARRAY[NEW.memory_id, NEW.brief_id];
    WHEN 'note_refs' THEN
        rid := NEW.receipt_id; objects := ARRAY[NEW.edition_id];
    ELSE
        RAISE EXCEPTION 'v2.require_dream_receipt is not configured for table %', TG_TABLE_NAME;
    END CASE;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = rid
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_id = ANY (objects)
    ) THEN
        RAISE EXCEPTION 'v2.%: % without a receipt written in the same transaction', TG_TABLE_NAME, TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER dream_editions_require_receipt
    AFTER INSERT OR UPDATE ON v2.dream_editions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_dream_receipt();

CREATE CONSTRAINT TRIGGER dream_actions_require_receipt
    AFTER INSERT OR UPDATE ON v2.dream_actions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_dream_receipt();

CREATE CONSTRAINT TRIGGER note_refs_require_receipt
    AFTER INSERT OR UPDATE ON v2.note_refs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_dream_receipt();

-- ---------------------------------------------------------------------
-- Schedules (bookkeeping: no receipts)
-- ---------------------------------------------------------------------

CREATE TABLE v2.dream_schedules (
    space_id       uuid PRIMARY KEY,
    tenant_id      uuid NOT NULL,
    owner_id       uuid NOT NULL,                          -- whose local night it is
    cadence        text NOT NULL,                          -- nightly | weekly
    time_zone      text NOT NULL,                          -- the IANA zone due_at was computed in
    due_at         timestamptz NOT NULL,                   -- the next slot: the owner's local night
    last_slot      timestamptz,                            -- the latest slot queued
    last_queued_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dream_schedules_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT dream_schedules_cadence_check CHECK (cadence IN ('nightly', 'weekly')),
    CONSTRAINT dream_schedules_zone_check CHECK (char_length(time_zone) BETWEEN 1 AND 64)
);

CREATE INDEX dream_schedules_due_idx ON v2.dream_schedules (due_at);

COMMENT ON TABLE v2.dream_schedules IS
    'When each V2 space''s next Dream edition is due, in its owner''s local night. Written only by the dream sweep.';

-- ---------------------------------------------------------------------
-- A person's Dream settings
-- ---------------------------------------------------------------------

CREATE TABLE v2.dream_settings (
    person_id         uuid PRIMARY KEY REFERENCES public.users (id) ON DELETE CASCADE,
    time_zone         text NOT NULL DEFAULT 'UTC',
    time_zone_source  text NOT NULL DEFAULT 'default',     -- default | observed (their app's clock) | set (by them)
    morning_email     boolean NOT NULL DEFAULT true,
    unsubscribe_token text NOT NULL DEFAULT encode(uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid()), 'hex'),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dream_settings_zone_check CHECK (char_length(time_zone) BETWEEN 1 AND 64),
    CONSTRAINT dream_settings_source_check CHECK (time_zone_source IN ('default', 'observed', 'set')),
    CONSTRAINT dream_settings_token_key UNIQUE (unsubscribe_token),
    CONSTRAINT dream_settings_token_check CHECK (char_length(unsubscribe_token) >= 32)
);

COMMENT ON TABLE v2.dream_settings IS
    'A person''s local time zone (Dream runs in their night) and whether they get the morning edition by email.';

-- One-click unsubscribe from the morning email (RFC 8058): the token in
-- the email is the credential. It turns the email off and rotates the
-- token, and answers whether a token matched; callers say the same either
-- way, so it is no oracle.
CREATE FUNCTION v2.unsubscribe_dream_email(p_token text) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    IF p_token IS NULL OR char_length(p_token) < 32 OR char_length(p_token) > 128 THEN
        RETURN false;
    END IF;
    UPDATE v2.dream_settings
       SET morning_email = false,
           unsubscribe_token = encode(uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid()), 'hex'),
           updated_at = now()
     WHERE unsubscribe_token = p_token;
    RETURN FOUND;
END $$;

REVOKE ALL ON FUNCTION v2.unsubscribe_dream_email(text) FROM PUBLIC;

-- ---------------------------------------------------------------------
-- Morning emails sent (bookkeeping: no receipts)
-- ---------------------------------------------------------------------

CREATE TABLE v2.dream_email_sends (
    edition_id uuid NOT NULL,
    person_id  uuid NOT NULL,
    space_id   uuid NOT NULL,
    message_id text,
    sent_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (edition_id, person_id),
    CONSTRAINT dream_email_sends_edition_fkey FOREIGN KEY (edition_id, space_id)
        REFERENCES v2.dream_editions (id, space_id) ON DELETE CASCADE,
    CONSTRAINT dream_email_sends_message_check CHECK (message_id IS NULL OR char_length(message_id) <= 200)
);

COMMENT ON TABLE v2.dream_email_sends IS
    'Which morning-edition emails went out, so a retried job never sends one twice.';

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.dream_editions ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.dream_editions FORCE ROW LEVEL SECURITY;
CREATE POLICY dream_editions_space ON v2.dream_editions
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.dream_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.dream_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY dream_actions_space ON v2.dream_actions
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.note_refs ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.note_refs FORCE ROW LEVEL SECURITY;
CREATE POLICY note_refs_space ON v2.note_refs
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.dream_schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.dream_schedules FORCE ROW LEVEL SECURITY;
CREATE POLICY dream_schedules_space ON v2.dream_schedules FOR SELECT TO memax_v2
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));
CREATE POLICY dream_schedules_sweep ON v2.dream_schedules TO memax_v2_dream_sweeper
    USING (current_setting('app.sweep', true) = 'dream')
    WITH CHECK (current_setting('app.sweep', true) = 'dream');

ALTER TABLE v2.dream_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.dream_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY dream_settings_person ON v2.dream_settings TO memax_v2
    USING (person_id = (SELECT v2.current_person_id()))
    WITH CHECK (person_id = (SELECT v2.current_person_id()));
CREATE POLICY dream_settings_sweep ON v2.dream_settings TO memax_v2_dream_sweeper
    USING (current_setting('app.sweep', true) = 'dream')
    WITH CHECK (current_setting('app.sweep', true) = 'dream');

ALTER TABLE v2.dream_email_sends ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.dream_email_sends FORCE ROW LEVEL SECURITY;
CREATE POLICY dream_email_sends_sweep ON v2.dream_email_sends TO memax_v2_dream_sweeper
    USING (current_setting('app.sweep', true) = 'dream')
    WITH CHECK (current_setting('app.sweep', true) = 'dream');

-- ---------------------------------------------------------------------
-- Grants: the least each role needs
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.dream_editions TO memax_v2;
GRANT SELECT, INSERT ON v2.dream_actions TO memax_v2;
GRANT UPDATE (undone_by, undone_at, last_receipt_id) ON v2.dream_actions TO memax_v2;
GRANT SELECT, INSERT ON v2.note_refs TO memax_v2;
GRANT SELECT ON v2.dream_schedules TO memax_v2;
GRANT SELECT, INSERT ON v2.dream_settings TO memax_v2;
GRANT UPDATE (time_zone, time_zone_source, morning_email, updated_at) ON v2.dream_settings TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.unsubscribe_dream_email(text) TO memax_v2;

GRANT SELECT, INSERT ON v2.dream_schedules TO memax_v2_dream_sweeper;
GRANT UPDATE (owner_id, cadence, time_zone, due_at, last_slot, last_queued_at, updated_at)
    ON v2.dream_schedules TO memax_v2_dream_sweeper;
GRANT SELECT (person_id, time_zone, time_zone_source, morning_email, unsubscribe_token) ON v2.dream_settings TO memax_v2_dream_sweeper;
GRANT INSERT (person_id) ON v2.dream_settings TO memax_v2_dream_sweeper;
GRANT SELECT, INSERT ON v2.dream_email_sends TO memax_v2_dream_sweeper;
