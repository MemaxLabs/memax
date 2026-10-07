-- Revert 048: v2_switch
--
-- Restores 047's receipt verbs, note refs and notes view, 027's hub columns rule,
-- 043's import locations and 044's tombstones, propagations and notices.
-- Receipts are append-only, so the switch's receipts written meanwhile
-- stay, and the restored CHECK is NOT VALID. Switch notices and note
-- tombstones go (bookkeeping without words); spaces stay on whatever record
-- hubs.v2_enabled_at says.

DELETE FROM v2.agent_notices WHERE kind = 'switched';
DROP INDEX v2.agent_notices_switched_key;
ALTER TABLE v2.agent_notices DROP CONSTRAINT agent_notices_autonomy_check;
ALTER TABLE v2.agent_notices DROP CONSTRAINT agent_notices_op_check;
ALTER TABLE v2.agent_notices DROP CONSTRAINT agent_notices_kind_check;
ALTER TABLE v2.agent_notices ADD CONSTRAINT agent_notices_kind_check CHECK (kind IN ('forgotten', 'space_forgotten'));
ALTER TABLE v2.agent_notices DROP COLUMN autonomy;
ALTER TABLE v2.agent_notices ALTER COLUMN op_id SET NOT NULL;

CREATE OR REPLACE FUNCTION v2.agent_notices_guard() RETURNS trigger
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

COMMENT ON TABLE v2.agent_notices IS
    'Forget''s notices: each agent connection that read a forgotten memory (or is connected to its space) is told once, on its next MCP response. Refs only.';

ALTER TABLE v2.import_items DROP CONSTRAINT import_items_location_check;
ALTER TABLE v2.import_items ADD CONSTRAINT import_items_location_check CHECK (location IN ('repository', 'home')) NOT VALID;
ALTER TABLE v2.imports DROP CONSTRAINT imports_origin_check;
ALTER TABLE v2.imports DROP COLUMN origin;

DROP TABLE v2.space_switches;

DELETE FROM v2.propagations WHERE destination_kind = 'attachments';
ALTER TABLE v2.propagations DROP CONSTRAINT propagations_kind_check;
ALTER TABLE v2.propagations ADD CONSTRAINT propagations_kind_check
    CHECK (destination_kind IN ('target', 'artifacts', 'caches', 'ledger'));

DROP INDEX v2.tombstones_note_key;
ALTER TABLE v2.tombstones DROP CONSTRAINT tombstones_kind_check;
ALTER TABLE v2.tombstones ADD CONSTRAINT tombstones_kind_check CHECK (object_kind IN ('memory', 'space')) NOT VALID;

DROP TRIGGER note_refs_forgotten_words ON v2.note_refs;
DROP FUNCTION v2.note_refs_forgotten_words();
DROP FUNCTION v2.note_words_left(text, uuid);
DROP FUNCTION v2.purge_note_words(uuid);

DROP VIEW v2.v1_dream_runs;
DROP VIEW v2.note_chunks;
DROP VIEW v2.notes;
CREATE VIEW v2.notes WITH (security_barrier = true) AS
    SELECT m.id, m.owner_id, m.hub_id AS space_id, m.title,
           left(m.content, 280) AS excerpt, m.state, m.created_at,
           left(m.content, 4000) AS body,
           CASE WHEN m.created_by_type = 'agent' OR COALESCE(m.created_by_slug, '') <> '' OR COALESCE(m.source_agent, '') <> ''
                THEN 'agent' ELSE 'person' END AS author_kind,
           COALESCE(NULLIF(m.created_by_slug, ''), NULLIF(m.source_agent, '')) AS agent,
           m.source, m.content_type, m.source_path
      FROM public.memories m
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
GRANT SELECT ON v2.notes TO memax_v2;

-- Note refs back to 047's: the switch's (no edition) go; Dream's stay.
DELETE FROM v2.note_refs WHERE edition_id IS NULL;
DROP TRIGGER note_refs_guard ON v2.note_refs;
DROP TRIGGER note_refs_require_receipt ON v2.note_refs;
DROP TRIGGER note_refs_defaults ON v2.note_refs;
DROP FUNCTION v2.note_refs_guard();
DROP FUNCTION v2.require_note_receipt();
DROP FUNCTION v2.note_refs_defaults();
REVOKE UPDATE ON v2.note_refs FROM memax_v2;
DROP INDEX v2.note_refs_v1_idx;
ALTER TABLE v2.note_refs
    DROP CONSTRAINT note_refs_source_key,
    DROP COLUMN origin, DROP COLUMN v1_id, DROP COLUMN author_kind, DROP COLUMN author_id, DROP COLUMN agent,
    DROP COLUMN trust, DROP COLUMN disposition, DROP COLUMN hold, DROP COLUMN stream_version,
    DROP COLUMN last_receipt_id, DROP COLUMN forgotten_at, DROP COLUMN updated_at;
ALTER TABLE v2.note_refs ALTER COLUMN edition_id SET NOT NULL;
COMMENT ON TABLE v2.note_refs IS
    'N-: display IDs for notes Dream has read, allocated by the edition that first read them.';
CREATE CONSTRAINT TRIGGER note_refs_require_receipt
    AFTER INSERT OR UPDATE ON v2.note_refs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_dream_receipt();

DROP TRIGGER hubs_space_ledger_tenant ON public.hubs;
DROP FUNCTION v2.hubs_space_ledger_tenant();

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

    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'a space''s tenant is fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.space_kind IS DISTINCT FROM OLD.space_kind OR NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'a space''s kind and id are fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;

COMMENT ON FUNCTION v2.hubs_space_columns() IS
    'Fills hubs.space_kind and hubs.tenant_id on insert (tenant = owner for personal/project, the space for team) and keeps id, kind and tenant immutable.';

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
     'published', 'folded')) NOT VALID;
