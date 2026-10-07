-- 047: v2_switch
--
-- "Switch to V2", one space at a time (plan 25 §10, Phase 2 epic 2.8), and
-- notes (N-), the V1 content a switched space keeps.
--
--   note_refs       one row per note of a space on V2: its N- number, where
--                   its words live (a V1 memory, a persona or an agent
--                   config), who wrote them, and what the switch did with
--                   it (offered for bulk keep, left for Dream to fold, or
--                   kept as a note only). IDs, refs and codes; never words.
--   notes (view)    the notes of the spaces in scope with their words, read
--                   from the V1 rows that hold them (public.memories,
--                   personas, agent_configs). 028's view, widened.
--   note_chunks     the V1 chunks of those notes, for search (the owner's,
--                   memax_search include_notes and /v2/spaces/{space}/notes).
--   space_switches  where a space's switch stands: the steps done, the
--                   counts, the V1 import that offers a person's own V1
--                   memories for bulk keep, the V1 plan (grandfathered, D9).
--                   Bookkeeping, like v2.imports: no receipts of its own;
--                   the switch's receipts are on the space's stream.
--   v1_dream_runs   V1's Dream runs of a space, read-only edition history
--   (view)          (counts, no report words, no undo).
--
-- # Notes stay where V1 put them until cutover
--
-- §5.4: v2.notes starts as a view over V1 public.memories, and the
-- physical move happens at cutover (3.8). The words are never copied into
-- v2: a note's words are in its V1 row, and only there. Forget of a note
-- (internal/ledger/forget_note.go) deletes that row as V1's own delete
-- does (chunks, attachment rows and topic links cascade), and scrubs V1's
-- derived copies that name it (board cards citing it, notifications about
-- it, Dream's reasons, the activity summary of its title), through
-- v2.purge_note_words, a SECURITY DEFINER function that runs only beside
-- the note's forgot receipt in the same transaction. A deferred trigger
-- refuses the commit if a forgotten note's V1 row is still there, or it
-- has no tombstone.
--
-- Why a table of refs beside a view, not a copied table: a copy is a
-- second place for Forget to reach and for the switch to keep in step with
-- V1 while the space can still switch back; the refs table holds what the
-- view can't (a stable N- number allocated once, the disposition Dream
-- reads, forced RLS, receipts), and search reads V1's own chunks, already
-- indexed (GIN on search_vector, trigram on search_text).
--
-- # Hand-off to Dream (branch v2-dream, its migration 047)
--
-- Dream reads v2.notes. This view keeps Dream's columns in Dream's order
-- (id … created_at, body, author_kind, agent, source, content_type,
-- source_path) and adds origin, seq, disposition, hold and trust after
-- them, so one CREATE OR REPLACE serves both. Dream folds only notes whose
-- disposition is 'fold' (agent-written V1 memories, personas, agent
-- configs, a person's documents longer than one statement, and every note
-- written after the switch): a person's own V1 memories are 'candidate',
-- already offered for bulk keep through the switch's V1 import, and
-- archived or credential-bearing ones are 'note' (searchable, never
-- proposed). Dream's note_refs (N- numbers allocated by an edition) is
-- this table: merging, Dream's migration adds `edition_id` here instead of
-- creating the table, and its receipt check admits the edition (this one
-- admits the space or the note); its numbering of a note the switch didn't
-- number inserts a row with origin 'memory', v1_id = note_id and
-- disposition 'fold'.
--
-- # The switch itself (internal/ledger/switch.go)
--
-- POST /v2/spaces/{space}:switch starts it (GET …/switch is the dry-run
-- preview); small spaces switch in the request, others in the River job
-- space_switch, step by step, each step idempotent, so a failure resumes
-- where it stopped. V1 rows are never changed or deleted by a switch: the
-- roles map as ResolveUserScope reads them, the notes are numbered beside
-- the V1 rows, and hubs.v2_enabled_at is the last thing set. Switching back
-- clears it, and V1 behaves exactly as before.
--
-- # A V1 team hub can become a project space
--
-- V1 had personal and team hubs; a solo developer's "team" hub per
-- repository is a V2 project space. The kind (and with it the tenant: the
-- owner, not the hub) may change from team to project while the space has
-- no V2 record yet. Every v2 row refers to (space_id, tenant_id), and
-- v2.space_ledgers' tenant follows the hub's here, so a space with any
-- receipt refuses the change by foreign key.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains noted (the switch numbered a space's notes),
-- switched (the space moved to the V2 record) and switched_back (it went
-- back to V1). They are on the space's own stream (object_kind space,
-- object_ref "space"). A note's Forget is `forgot` about the note
-- (object_kind note, which 028 admits). The list keeps every verb 028,
-- 029, 031, 035, 036, 042, 044 and 046 admit; a branch that adds verbs in
-- parallel merges by taking the union of both lists.

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
     'exported',
     'noted', 'switched', 'switched_back'));

-- ---------------------------------------------------------------------
-- A team hub becomes a project space (before it has a V2 record)
-- ---------------------------------------------------------------------

CREATE OR REPLACE FUNCTION v2.hubs_space_columns() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.space_kind := COALESCE(NEW.space_kind,
            CASE NEW.hub_type WHEN 'personal' THEN 'personal' WHEN 'team' THEN 'team' END);
        NEW.tenant_id := CASE WHEN NEW.space_kind = 'team' THEN NEW.id ELSE NEW.owner_id END;
        RETURN NEW;
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'a space''s id is fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.space_kind IS DISTINCT FROM OLD.space_kind THEN
        -- The one change allowed: a V1 team hub switching to V2 as a
        -- project space. Its tenant becomes its owner; v2.space_ledgers
        -- follows (hubs_space_ledger_tenant), and every v2 row's foreign key
        -- to (space_id, tenant_id) refuses it once the space has a record.
        IF OLD.space_kind = 'team' AND NEW.space_kind = 'project' THEN
            NEW.tenant_id := NEW.owner_id;
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'a space''s kind is fixed when it is created, except a team hub switching to V2 as a project (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'a space''s tenant is fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;

COMMENT ON FUNCTION v2.hubs_space_columns() IS
    'Fills hubs.space_kind and hubs.tenant_id on insert (tenant = owner for personal/project, the space for team) and keeps id, kind and tenant immutable, except a team hub becoming a project space (047) before it has a V2 record.';

-- The space's ledger row follows a tenant change. Every receipt, seal,
-- tombstone and notice refers to (space_id, tenant_id) there, so this
-- UPDATE fails by foreign key once the space has any of them.
CREATE FUNCTION v2.hubs_space_ledger_tenant() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET app.sweep = 'space_ledger_hub'
    AS $$
BEGIN
    UPDATE v2.space_ledgers SET tenant_id = NEW.tenant_id WHERE space_id = NEW.id AND tenant_id = OLD.tenant_id;
    RETURN NULL;
END $$;

REVOKE ALL ON FUNCTION v2.hubs_space_ledger_tenant() FROM PUBLIC;

-- Not UPDATE OF tenant_id: the kind change sets it in a BEFORE trigger,
-- and a column list fires only on the columns the statement names.
CREATE TRIGGER hubs_space_ledger_tenant
    AFTER UPDATE ON public.hubs
    FOR EACH ROW
    WHEN (NEW.tenant_id IS DISTINCT FROM OLD.tenant_id)
    EXECUTE FUNCTION v2.hubs_space_ledger_tenant();

-- ---------------------------------------------------------------------
-- Note refs (N-)
-- ---------------------------------------------------------------------

CREATE TABLE v2.note_refs (
    note_id         uuid PRIMARY KEY,                       -- the note: the V1 memory's id, or a name-based uuid of a persona or agent config
    tenant_id       uuid NOT NULL,
    space_id        uuid NOT NULL,
    seq             bigint NOT NULL,                        -- display number: N-<seq>, per tenant
    origin          text NOT NULL DEFAULT 'memory',         -- memory | persona | agent_config: the V1 table its words live in
    v1_id           uuid NOT NULL,                          -- that row's id (= note_id for a memory)
    author_kind     text,                                   -- person | agent, as V1 recorded who wrote it
    author_id       uuid,                                   -- the V1 owner: the person, or the person the agent worked for
    agent           text,                                   -- the agent's slug, for an agent's note
    trust           text,                                   -- the class it is cited at: person | agent_own_work | repository | external
    disposition     text NOT NULL DEFAULT 'fold',           -- candidate | fold | note
    hold            text,                                   -- why a person's note isn't a candidate: long | secret | archived | format | external
    stream_version  integer NOT NULL DEFAULT 0,             -- receipts about the note itself (its Forget)
    receipt_id      uuid NOT NULL REFERENCES v2.receipts (id), -- the receipt that numbered it (the switch's noted)
    last_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    forgotten_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT note_refs_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT note_refs_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT note_refs_source_key UNIQUE (space_id, origin, v1_id),
    CONSTRAINT note_refs_seq_check CHECK (seq >= 1),
    CONSTRAINT note_refs_origin_check CHECK (origin IN ('memory', 'persona', 'agent_config')),
    CONSTRAINT note_refs_memory_id_check CHECK (origin <> 'memory' OR v1_id = note_id),
    CONSTRAINT note_refs_author_check CHECK (author_kind IS NULL OR author_kind IN ('person', 'agent')),
    CONSTRAINT note_refs_agent_check CHECK (agent IS NULL OR (agent <> '' AND char_length(agent) <= 64)),
    CONSTRAINT note_refs_trust_check CHECK (trust IS NULL OR trust IN ('person', 'agent_own_work', 'repository', 'external')),
    CONSTRAINT note_refs_disposition_check CHECK (disposition IN ('candidate', 'fold', 'note')),
    CONSTRAINT note_refs_hold_check CHECK (hold IS NULL OR hold IN ('long', 'secret', 'archived', 'format', 'external')),
    CONSTRAINT note_refs_stream_version_check CHECK (stream_version >= 0)
);

CREATE INDEX note_refs_space_idx ON v2.note_refs (space_id, seq);
CREATE INDEX note_refs_v1_idx ON v2.note_refs (v1_id);

COMMENT ON TABLE v2.note_refs IS
    'N-: the notes of a space on V2, numbered once, with where their words live (a V1 row), who wrote them and what the switch did with them. Never words.';

-- A row is written or changed only beside a receipt in the same
-- transaction, about the space (the switch's noted) or the note (its
-- Forget).
CREATE FUNCTION v2.require_note_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = NEW.last_receipt_id
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_id IN (NEW.space_id, NEW.note_id)
    ) THEN
        RAISE EXCEPTION 'v2.note_refs: % without a receipt written in the same transaction', TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER note_refs_require_receipt
    AFTER INSERT OR UPDATE ON v2.note_refs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_note_receipt();

-- A forgotten note stays forgotten.
CREATE FUNCTION v2.note_refs_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.forgotten_at IS NOT NULL THEN
        RAISE EXCEPTION 'note %: forgotten, so nothing about it changes', OLD.note_id USING ERRCODE = 'MXL01';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER note_refs_guard
    BEFORE UPDATE ON v2.note_refs
    FOR EACH ROW EXECUTE FUNCTION v2.note_refs_guard();

-- ---------------------------------------------------------------------
-- The notes, with their words (read from V1)
-- ---------------------------------------------------------------------

-- 028's columns, in order, then Dream's (v2-dream's 047), then the notes'
-- own. Seeds (V1's onboarding memories) are no one's notes: they stay out.
CREATE OR REPLACE VIEW v2.notes WITH (security_barrier = true) AS
    SELECT m.id, m.owner_id, m.hub_id AS space_id, m.title,
           left(m.content, 280) AS excerpt, m.state, m.created_at,
           left(m.content, 4000) AS body,
           CASE WHEN m.created_by_type = 'agent' OR COALESCE(m.created_by_slug, '') <> '' OR COALESCE(m.source_agent, '') <> ''
                THEN 'agent' ELSE 'person' END AS author_kind,
           COALESCE(NULLIF(m.created_by_slug, ''), NULLIF(m.source_agent, '')) AS agent,
           m.source, m.content_type, m.source_path,
           'memory'::text AS origin, r.seq, COALESCE(r.disposition, 'fold') AS disposition, r.hold, r.trust,
           char_length(m.content) AS length, m.updated_at
      FROM public.memories m
      LEFT JOIN v2.note_refs r ON r.note_id = m.id AND r.space_id = m.hub_id
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[])
       AND m.source_kind IS DISTINCT FROM 'onboarding-seed'
    UNION ALL
    SELECT r.note_id, p.owner_id, r.space_id, p.name,
           left(p.content, 280), 'active'::text, p.created_at,
           left(p.content, 4000),
           COALESCE(r.author_kind, 'agent'), r.agent,
           'persona'::text, 'markdown'::text, p.source_file_path,
           r.origin, r.seq, r.disposition, r.hold, r.trust,
           char_length(p.content), p.updated_at
      FROM v2.note_refs r
      JOIN public.personas p ON p.id = r.v1_id
     WHERE r.origin = 'persona' AND r.space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
    UNION ALL
    SELECT r.note_id, a.owner_id, r.space_id, a.file_path,
           left(a.content, 280), 'active'::text, a.created_at,
           left(a.content, 4000),
           COALESCE(r.author_kind, 'person'), r.agent,
           'agent_config'::text, 'markdown'::text, a.file_path,
           r.origin, r.seq, r.disposition, r.hold, r.trust,
           char_length(a.content), a.updated_at
      FROM v2.note_refs r
      JOIN public.agent_configs a ON a.id = r.v1_id
     WHERE r.origin = 'agent_config' AND r.space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);

COMMENT ON VIEW v2.notes IS
    'N-: the notes of the spaces in scope, with their words read from the V1 rows that hold them. Never compiled; Dream folds the ones whose disposition is fold.';

-- The V1 chunks of the notes in scope, for search.
CREATE VIEW v2.note_chunks WITH (security_barrier = true) AS
    SELECT c.memory_id AS note_id, m.hub_id AS space_id, m.owner_id, c.chunk_index, c.content, c.search_vector, c.search_text
      FROM public.chunks c
      JOIN public.memories m ON m.id = c.memory_id
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[])
       AND m.source_kind IS DISTINCT FROM 'onboarding-seed';

COMMENT ON VIEW v2.note_chunks IS
    'The V1 chunks of the notes in scope (their indexed words), for searching notes.';

-- V1's Dream runs, as read-only edition history: counts, never the
-- report's words, and no undo (plan 25 §10).
CREATE VIEW v2.v1_dream_runs WITH (security_barrier = true) AS
    SELECT d.id, d.hub_id AS space_id, d.status, d.mode, d.started_at, d.finished_at,
           d.memories_scanned, d.duplicates_merged, d.contradictions_found, d.memories_archived,
           d.memories_organized, d.topics_restructured,
           (SELECT count(*) FROM public.dream_actions a WHERE a.run_id = d.id) AS actions
      FROM public.dream_runs d
     WHERE d.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[]);

COMMENT ON VIEW v2.v1_dream_runs IS
    'V1''s Dream runs of the spaces in scope: read-only history, counts only, no undo.';

-- ---------------------------------------------------------------------
-- Forgetting a note: its V1 row goes, as V1's delete takes it
-- ---------------------------------------------------------------------

-- The words of one note, out of every V1 row that holds them, beside the
-- note's forgot receipt in this transaction. A memory: V1's delete
-- (chunks, attachment rows and topic links cascade), and the copies V1
-- derived from it that name it by id (board cards and their history and
-- feedback citing it, notifications about it, Dream's reasons for actions
-- on it) or by its title (the activity log's summary of the push). A
-- persona: the persona and its revisions. An agent config: the synced
-- copy, and the deleted copies V1 keeps of that file. It returns the
-- storage keys of the memory's attachments, for the propagation to delete.
CREATE FUNCTION v2.purge_note_words(p_note uuid) RETURNS text[]
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    r       v2.note_refs;
    title   text;
    summary text;
    keys    text[] := '{}';
BEGIN
    SELECT * INTO r FROM v2.note_refs n
     WHERE n.note_id = p_note AND n.space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    IF NOT FOUND THEN
        RAISE EXCEPTION 'note %: not in this transaction''s scope', p_note USING ERRCODE = 'MXR02';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts rc
         WHERE rc.object_id = p_note AND rc.object_kind = 'note' AND rc.action = 'forgot'
           AND rc.txid = pg_current_xact_id() AND rc.space_id = r.space_id
    ) THEN
        RAISE EXCEPTION 'note %: its words are purged only by its Forget in the same transaction', p_note
            USING ERRCODE = 'MXR02';
    END IF;

    CASE r.origin
    WHEN 'memory' THEN
        SELECT m.title INTO title FROM public.memories m WHERE m.id = r.v1_id;
        SELECT COALESCE(array_agg(a.storage_key ORDER BY a.created_at), '{}') INTO keys
          FROM public.memory_attachments a WHERE a.memory_id = r.v1_id;
        DELETE FROM public.board_feedback WHERE r.v1_id = ANY (cite_memory_ids);
        DELETE FROM public.board_slot_history WHERE r.v1_id = ANY (cite_memory_ids);
        DELETE FROM public.board_slots WHERE r.v1_id = ANY (cite_memory_ids);
        DELETE FROM public.notifications
         WHERE strpos(payload::text, r.v1_id::text) > 0 OR strpos(COALESCE(source_id, ''), r.v1_id::text) > 0;
        UPDATE public.dream_actions SET reason = ''
         WHERE reason <> '' AND (r.v1_id::text = ANY (source_memory_ids) OR result_memory_id = r.v1_id::text);
        -- The activity log keeps a push's title, compacted as V1 does
        -- (handler.compactActivitySummary: spaces folded, 96 characters).
        summary := btrim(regexp_replace(COALESCE(title, ''), '\s+', ' ', 'g'));
        IF char_length(summary) > 96 THEN
            summary := left(summary, 95) || '…';
        END IF;
        IF summary <> '' THEN
            UPDATE public.usage_events SET metadata = metadata - 'summary'
             WHERE hub_id = r.space_id AND metadata ->> 'summary' = summary;
        END IF;
        DELETE FROM public.memories WHERE id = r.v1_id;
    WHEN 'persona' THEN
        DELETE FROM public.personas WHERE id = r.v1_id;
    WHEN 'agent_config' THEN
        DELETE FROM public.agent_config_tombstones t
         USING public.agent_configs a
         WHERE a.id = r.v1_id AND t.owner_id = a.owner_id AND t.agent = a.agent
           AND t.file_path = a.file_path AND t.scope = a.scope;
        DELETE FROM public.agent_configs WHERE id = r.v1_id;
    END CASE;
    RETURN keys;
END $$;

REVOKE ALL ON FUNCTION v2.purge_note_words(uuid) FROM PUBLIC;

-- Whether a note's V1 row is still there (the deferred check below).
CREATE FUNCTION v2.note_words_left(p_origin text, p_v1 uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
    SELECT CASE p_origin
        WHEN 'memory' THEN EXISTS (SELECT 1 FROM public.memories WHERE id = p_v1)
        WHEN 'persona' THEN EXISTS (SELECT 1 FROM public.personas WHERE id = p_v1)
        WHEN 'agent_config' THEN EXISTS (SELECT 1 FROM public.agent_configs WHERE id = p_v1)
        ELSE true
    END
$$;

REVOKE ALL ON FUNCTION v2.note_words_left(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.note_words_left(text, uuid) TO memax_v2;

-- At commit, a forgotten note's V1 row is gone, and it has a tombstone.
CREATE FUNCTION v2.note_refs_forgotten_words() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF v2.note_words_left(NEW.origin, NEW.v1_id) THEN
        RAISE EXCEPTION 'note %: forgotten, but its V1 row still holds its words', NEW.note_id
            USING ERRCODE = 'MXF01', HINT = 'Forget purges a note through v2.purge_note_words (internal/ledger/forget_note.go).';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM v2.tombstones t WHERE t.object_id = NEW.note_id AND t.object_kind = 'note') THEN
        RAISE EXCEPTION 'note %: forgotten without a tombstone', NEW.note_id
            USING ERRCODE = 'MXF01', HINT = 'Forget writes the tombstone beside the forgot receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER note_refs_forgotten_words
    AFTER UPDATE OF forgotten_at ON v2.note_refs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (NEW.forgotten_at IS NOT NULL)
    EXECUTE FUNCTION v2.note_refs_forgotten_words();

-- A note has a tombstone like a memory: one, ever.
ALTER TABLE v2.tombstones DROP CONSTRAINT tombstones_kind_check;
ALTER TABLE v2.tombstones ADD CONSTRAINT tombstones_kind_check CHECK (object_kind IN ('memory', 'space', 'note'));
CREATE UNIQUE INDEX tombstones_note_key ON v2.tombstones (object_id) WHERE object_kind = 'note';

-- A note's Forget deletes its attachments' stored objects.
ALTER TABLE v2.propagations DROP CONSTRAINT propagations_kind_check;
ALTER TABLE v2.propagations ADD CONSTRAINT propagations_kind_check
    CHECK (destination_kind IN ('target', 'artifacts', 'caches', 'ledger', 'attachments'));

-- ---------------------------------------------------------------------
-- The switch's bookkeeping
-- ---------------------------------------------------------------------

CREATE TABLE v2.space_switches (
    space_id         uuid PRIMARY KEY,
    tenant_id        uuid NOT NULL,                     -- the space's tenant once it switched
    state            text NOT NULL,                     -- running | switched | failed | off
    step             text NOT NULL,                     -- the next step to run, or done
    requested_by     uuid NOT NULL,                     -- the person who asked (the owner)
    idempotency_key  text NOT NULL,                     -- the request that started this run
    options          jsonb NOT NULL DEFAULT '{}'::jsonb, -- {kind, repository}
    preview          jsonb NOT NULL DEFAULT '{}'::jsonb, -- the counts the person saw when it started
    progress         jsonb NOT NULL DEFAULT '{}'::jsonb, -- what each step did: counts and ids
    import_id        uuid,                              -- the V1 import: a person's own V1 memories, offered for bulk keep
    plan             text,                              -- the V1 plan, grandfathered (D9)
    attempts         integer NOT NULL DEFAULT 0,
    error            text,                              -- a code, never words
    started_at       timestamptz NOT NULL DEFAULT now(),
    switched_at      timestamptz,
    switched_back_at timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT space_switches_space_fkey FOREIGN KEY (space_id) REFERENCES public.hubs (id) ON DELETE CASCADE,
    CONSTRAINT space_switches_state_check CHECK (state IN ('running', 'switched', 'failed', 'off')),
    CONSTRAINT space_switches_step_check CHECK (step IN
        ('space', 'notes', 'personas', 'configs', 'candidates', 'agents', 'gates', 'switch', 'done')),
    CONSTRAINT space_switches_done_check CHECK (state <> 'switched' OR (step = 'done' AND switched_at IS NOT NULL)),
    CONSTRAINT space_switches_key_check CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    CONSTRAINT space_switches_error_check CHECK (error IS NULL OR char_length(error) <= 64),
    CONSTRAINT space_switches_json_check CHECK (jsonb_typeof(options) = 'object' AND jsonb_typeof(preview) = 'object'
        AND jsonb_typeof(progress) = 'object')
);

COMMENT ON TABLE v2.space_switches IS
    'Where a space''s Switch to V2 stands: its steps, counts and V1 import. Bookkeeping, no words, no receipts of its own (the switch''s are on the space''s stream).';

-- ---------------------------------------------------------------------
-- Imports: the switch's V1 import
-- ---------------------------------------------------------------------

ALTER TABLE v2.imports ADD COLUMN origin text NOT NULL DEFAULT 'init';
ALTER TABLE v2.imports ADD CONSTRAINT imports_origin_check CHECK (origin IN ('init', 'v1'));
ALTER TABLE v2.import_items DROP CONSTRAINT import_items_location_check;
ALTER TABLE v2.import_items ADD CONSTRAINT import_items_location_check CHECK (location IN ('repository', 'home', 'v1'));

-- ---------------------------------------------------------------------
-- Agent notices: "your autonomy changed" when a space switches
-- ---------------------------------------------------------------------

ALTER TABLE v2.agent_notices ALTER COLUMN op_id DROP NOT NULL;
ALTER TABLE v2.agent_notices ADD COLUMN autonomy text;
ALTER TABLE v2.agent_notices DROP CONSTRAINT agent_notices_kind_check;
ALTER TABLE v2.agent_notices ADD CONSTRAINT agent_notices_kind_check CHECK (kind IN ('forgotten', 'space_forgotten', 'switched'));
ALTER TABLE v2.agent_notices ADD CONSTRAINT agent_notices_op_check CHECK ((op_id IS NULL) = (kind = 'switched'));
ALTER TABLE v2.agent_notices ADD CONSTRAINT agent_notices_autonomy_check CHECK (
    ((kind = 'switched') = (autonomy IS NOT NULL)) AND (autonomy IS NULL OR autonomy IN ('read', 'propose', 'write')));
-- One waiting switch notice per connection and space.
CREATE UNIQUE INDEX agent_notices_switched_key ON v2.agent_notices (connection_id, space_id)
    WHERE kind = 'switched' AND delivered_at IS NULL;

CREATE OR REPLACE FUNCTION v2.agent_notices_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.delivered_at IS NOT NULL
       OR (NEW.id, NEW.tenant_id, NEW.space_id, NEW.connection_id, NEW.person_id, NEW.op_id, NEW.kind, NEW.refs,
           NEW.read_it, NEW.created_at, NEW.autonomy)
          IS DISTINCT FROM
          (OLD.id, OLD.tenant_id, OLD.space_id, OLD.connection_id, OLD.person_id, OLD.op_id, OLD.kind, OLD.refs,
           OLD.read_it, OLD.created_at, OLD.autonomy)
    THEN
        RAISE EXCEPTION 'agent notice %: a notice is delivered once, and nothing else about it changes', OLD.id
            USING ERRCODE = 'MXR02';
    END IF;
    RETURN NEW;
END $$;

COMMENT ON TABLE v2.agent_notices IS
    'What each agent connection is told once, on its next MCP response: memories forgotten since it read them, and a space it is connected to switching to V2 (its autonomy there). Refs only.';

-- ---------------------------------------------------------------------
-- Row-level security and grants
-- ---------------------------------------------------------------------

ALTER TABLE v2.note_refs ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.note_refs FORCE ROW LEVEL SECURITY;
CREATE POLICY note_refs_space ON v2.note_refs
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.space_switches ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.space_switches FORCE ROW LEVEL SECURITY;
CREATE POLICY space_switches_space ON v2.space_switches
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

GRANT SELECT, INSERT ON v2.note_refs TO memax_v2;
GRANT UPDATE (disposition, hold, stream_version, last_receipt_id, forgotten_at, updated_at) ON v2.note_refs TO memax_v2;

GRANT SELECT, INSERT ON v2.space_switches TO memax_v2;
GRANT UPDATE (tenant_id, state, step, requested_by, idempotency_key, options, preview, progress, import_id, plan,
              attempts, error, started_at, switched_at, switched_back_at, updated_at)
    ON v2.space_switches TO memax_v2;

GRANT SELECT ON v2.note_chunks, v2.v1_dream_runs TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.purge_note_words(uuid) TO memax_v2;
