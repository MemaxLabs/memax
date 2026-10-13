-- 059: v2_conditions (plan 25 §5.9, epic 3.1 step 2; the stale-detection design §1.1)
--
-- A memory's "stays true while" conditions become typed: each is an
-- object with an id stable within the memory (c1, c2, …), a kind
-- (path_exists, file_contains, dep_present, file_unchanged, pr_state), the
-- fields of that kind and nothing else, an origin (derived, judge, dream,
-- person) and, for a derived one, where it was read off (statement, source,
-- scope). At most 8. v2.conditions_valid is the record's guard in SQL;
-- internal/ledger/conditions.Validate is the same in Go
-- (TestConditionsMatchSQL holds them together), and only Go also runs the
-- secret scan over file_contains' text.
--
-- Conditions written before this (untyped: the judge's file_exists, a
-- client's sentence) are typed where they can be, and set aside otherwise,
-- each memory with a receipt by Memax (conditions_changed), so the record
-- never changes without one. On a fresh database nothing is converted.

-- ---------------------------------------------------------------------
-- The validator
-- ---------------------------------------------------------------------

-- A repository-relative path in canonical form.
CREATE FUNCTION v2.condition_path_ok(p text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT p IS NOT NULL AND char_length(p) BETWEEN 1 AND 300
       AND p ~ '^[A-Za-z0-9._@+()\[\]/-]+$'
       AND NOT EXISTS (SELECT 1 FROM unnest(string_to_array(p, '/')) AS s WHERE s IN ('', '.', '..'))
$$;

-- A glob in the compiler's scope syntax.
CREATE FUNCTION v2.condition_glob_ok(g text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT g IS NOT NULL AND char_length(g) BETWEEN 1 AND 300
       AND g ~ '^[A-Za-z0-9._/*?\[\]!@+~-]+$'
       AND NOT EXISTS (SELECT 1 FROM unnest(string_to_array(g, '/')) AS s WHERE s IN ('', '.', '..'))
$$;

-- One line of 1 to n characters.
CREATE FUNCTION v2.condition_line_ok(s text, n integer) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT s IS NOT NULL AND char_length(s) BETWEEN 1 AND n AND s !~ '[[:cntrl:]]'
$$;

CREATE FUNCTION v2.conditions_valid(c jsonb) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
    AS $$
DECLARE
    e jsonb;
    k text;
    allowed text[];
    ids text[] := '{}';
    str text[];
    f text;
BEGIN
    IF c IS NULL OR jsonb_typeof(c) <> 'array' OR jsonb_array_length(c) > 8 THEN
        RETURN false;
    END IF;
    FOR e IN SELECT x FROM jsonb_array_elements(c) AS t(x) LOOP
        IF jsonb_typeof(e) <> 'object' THEN
            RETURN false;
        END IF;
        k := CASE WHEN jsonb_typeof(e -> 'kind') = 'string' THEN e ->> 'kind' END;
        allowed := CASE k
            WHEN 'path_exists'    THEN ARRAY['id', 'kind', 'origin', 'from', 'path', 'glob']
            WHEN 'file_contains'  THEN ARRAY['id', 'kind', 'origin', 'from', 'path', 'text', 'word']
            WHEN 'dep_present'    THEN ARRAY['id', 'kind', 'origin', 'from', 'manifest', 'name', 'range']
            WHEN 'file_unchanged' THEN ARRAY['id', 'kind', 'origin', 'from', 'path', 'blob']
            WHEN 'pr_state'       THEN ARRAY['id', 'kind', 'origin', 'from', 'repo', 'number', 'state']
        END;
        IF allowed IS NULL OR EXISTS (SELECT 1 FROM jsonb_object_keys(e) AS key WHERE key <> ALL (allowed)) THEN
            RETURN false;
        END IF;
        -- Every field but word and number is a string.
        FOREACH f IN ARRAY allowed LOOP
            IF f NOT IN ('word', 'number') AND e ? f AND jsonb_typeof(e -> f) <> 'string' THEN
                RETURN false;
            END IF;
        END LOOP;
        IF NOT coalesce(e ->> 'id' ~ '^c[1-9][0-9]{0,3}$', false) OR (e ->> 'id') = ANY (ids) THEN
            RETURN false;
        END IF;
        ids := ids || (e ->> 'id');
        IF coalesce(e ->> 'origin', '') NOT IN ('derived', 'judge', 'dream', 'person') THEN
            RETURN false;
        END IF;
        IF e ->> 'origin' = 'derived' THEN
            IF coalesce(e ->> 'from', '') NOT IN ('statement', 'source', 'scope') THEN
                RETURN false;
            END IF;
        ELSIF e ? 'from' THEN
            RETURN false;
        END IF;
        CASE k
        WHEN 'path_exists' THEN
            IF (e ? 'path') = (e ? 'glob')
               OR (e ? 'path' AND NOT v2.condition_path_ok(e ->> 'path'))
               OR (e ? 'glob' AND NOT v2.condition_glob_ok(e ->> 'glob')) THEN
                RETURN false;
            END IF;
        WHEN 'file_contains' THEN
            IF NOT v2.condition_path_ok(e ->> 'path') OR NOT v2.condition_line_ok(e ->> 'text', 200)
               OR (e ? 'word' AND jsonb_typeof(e -> 'word') <> 'boolean') THEN
                RETURN false;
            END IF;
        WHEN 'dep_present' THEN
            IF NOT v2.condition_path_ok(e ->> 'manifest')
               OR regexp_replace(e ->> 'manifest', '^.*/', '') NOT IN ('go.mod', 'package.json', 'pyproject.toml', 'Cargo.toml', 'requirements.txt')
               OR NOT v2.condition_line_ok(e ->> 'name', 214) OR (e ->> 'name') ~ '[ \t]'
               OR (e ? 'range' AND NOT v2.condition_line_ok(e ->> 'range', 64)) THEN
                RETURN false;
            END IF;
        WHEN 'file_unchanged' THEN
            IF NOT v2.condition_path_ok(e ->> 'path') OR NOT coalesce(e ->> 'blob' ~ '^([0-9a-f]{40}|[0-9a-f]{64})$', false) THEN
                RETURN false;
            END IF;
        WHEN 'pr_state' THEN
            IF NOT coalesce(e ->> 'repo' ~ '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$', false) OR char_length(e ->> 'repo') > 200
               OR jsonb_typeof(e -> 'number') IS DISTINCT FROM 'number' OR NOT coalesce(e ->> 'number' ~ '^[1-9][0-9]{0,17}$', false)
               OR coalesce(e ->> 'state', '') NOT IN ('open', 'merged', 'closed') THEN
                RETURN false;
            END IF;
        END CASE;
    END LOOP;
    RETURN true;
END $$;

-- ---------------------------------------------------------------------
-- What was written before: typed where it can be, with a receipt
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
     'exported',
     'published', 'folded',
     'noted', 'switched', 'switched_back',
     'unlinked',
     'scoped',
     'conditions_changed'));

-- The typed form of an untyped list: the judge's file_exists becomes
-- path_exists; its dep_present and pr_state, and anything already typed,
-- stay; the rest (sentences, dates, content hashes) is set aside.
CREATE OR REPLACE FUNCTION pg_temp.conditions_typed(c jsonb) RETURNS jsonb
    LANGUAGE plpgsql IMMUTABLE
    AS $$
DECLARE
    e jsonb;
    t jsonb;
    out jsonb := '[]';
    n integer := 0;
BEGIN
    IF jsonb_typeof(c) <> 'array' THEN
        RETURN out;
    END IF;
    FOR e IN SELECT x FROM jsonb_array_elements(c) AS s(x) LOOP
        EXIT WHEN n = 8;
        t := NULL;
        IF jsonb_typeof(e) = 'object' THEN
            CASE e ->> 'kind'
            WHEN 'file_exists' THEN
                t := jsonb_build_object('kind', 'path_exists', 'path', e ->> 'path', 'origin', 'judge');
            WHEN 'dep_present' THEN
                t := jsonb_build_object('kind', 'dep_present', 'manifest', e ->> 'manifest', 'name', e ->> 'name', 'origin', 'judge');
            WHEN 'pr_state' THEN
                t := jsonb_build_object('kind', 'pr_state', 'repo', e ->> 'repo', 'number', e -> 'number', 'state', e ->> 'state', 'origin', 'judge');
            ELSE
                t := e - 'id';
            END CASE;
        END IF;
        IF t IS NOT NULL THEN
            t := jsonb_strip_nulls(t) || jsonb_build_object('id', 'c' || (n + 1));
            IF v2.conditions_valid(jsonb_build_array(t)) THEN
                out := out || jsonb_build_array(t);
                n := n + 1;
            END IF;
        END IF;
    END LOOP;
    RETURN out;
END $$;

ALTER TABLE v2.memories NO FORCE ROW LEVEL SECURITY;
ALTER TABLE v2.receipts NO FORCE ROW LEVEL SECURITY;

WITH legacy AS (
    SELECT m.id, m.tenant_id, m.space_id, m.seq, m.stream_version, pg_temp.conditions_typed(m.conditions) AS typed,
           jsonb_array_length(m.conditions) AS had
      FROM v2.memories m
     WHERE NOT v2.conditions_valid(m.conditions)
), written AS (
    INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action,
                             actor_kind, via, reason, occurred_at, stream_id, stream_version)
    SELECT gen_random_uuid(), l.tenant_id, l.space_id, 'memory', l.id,
           'M-' || CASE WHEN l.seq < 10000 THEN lpad(l.seq::text, 4, '0') ELSE l.seq::text END,
           'conditions_changed', 'memax', 'system',
           format('Memax typed its conditions for stale checks: kept %s of %s.', jsonb_array_length(l.typed), l.had),
           now(), l.id, l.stream_version + 1
      FROM legacy l
    RETURNING id, object_id, stream_version
)
UPDATE v2.memories m
   SET conditions = l.typed, stream_version = w.stream_version, last_receipt_id = w.id, updated_at = now()
  FROM legacy l JOIN written w ON w.object_id = l.id
 WHERE m.id = l.id;

-- Each memory's receipt is checked now (the deferred guard), so the
-- tables can change shape below.
SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE v2.receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE v2.memories FORCE ROW LEVEL SECURITY;
DROP FUNCTION pg_temp.conditions_typed(jsonb);

-- ---------------------------------------------------------------------
-- The guard
-- ---------------------------------------------------------------------

ALTER TABLE v2.memories DROP CONSTRAINT memories_conditions_check;
ALTER TABLE v2.memories ADD CONSTRAINT memories_conditions_check CHECK (v2.conditions_valid(conditions));
