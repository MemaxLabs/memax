-- 028: v2_ledger
--
-- The V2 record (plan 25 §5.3–§5.5, Phase 0 subset): receipts, memories
-- and their projections, display-ID counters, idempotency keys and the
-- read-only `notes` view over V1 memories.
--
-- Three guarantees live here, in the database, not in Go:
--
-- 1. Receipt or refused (HANDOFF rule 1). A deferred constraint trigger
--    on every record table rejects the COMMIT unless each inserted or
--    updated row points at a receipt written in the same transaction
--    (receipts.txid = pg_current_xact_id()), in the same space, about
--    the same object. receipts.txid and recorded_at are stamped by a
--    trigger, so they can't be forged. Receipts are append-only: no
--    DELETE, no TRUNCATE, and the only UPDATE is v2.redact_receipt_reasons
--    (SECURITY DEFINER), which nulls `reason` when the object is
--    forgotten in the same transaction.
--
-- 2. Content-free receipts. Receipts never hold memory text; statements
--    live only in memory_versions.statement and source quotes only in
--    sources.quote, both nullable so Forget can purge the words without
--    rewriting history. (Enforced by internal/ledger and its tests; a
--    trigger can't know what counts as "the words".)
--
-- 3. Space isolation (rule 13). Row-level security is ENABLEd and
--    FORCEd on every table, keyed on the transaction's app.space_ids
--    (id_counters: app.tenant_ids). The app switches to the memax_v2
--    role (026) for every V2 transaction, so superusers and BYPASSRLS
--    login roles are held to the policies too. Missing scope = no rows.
--
-- The lifecycle (§5.5) is checked twice: the Go transition table in
-- internal/ledger/lifecycle, and v2.lifecycle_transition_allowed here
-- (a BEFORE trigger). A test asserts the two tables agree.
--
-- Deliberate choices:
--   * Foreign keys to public.hubs are NO ACTION: V1 can't delete a space
--     (or the user owning it) once it holds V2 records, because that
--     would delete receipts. Deleting a V2 space or account must go
--     through the ledger (forget + tombstones) — a later epic.
--   * memories.embedding and memories.search are derived index data:
--     an UPDATE that changes only those columns needs no receipt (the
--     index job writes them). Any other column change does.
--   * Note links (folded_from) carry no FK to public.memories: V1 can
--     still hard-delete a note before cutover.

-- ---------------------------------------------------------------------
-- Vocabulary helpers (IMMUTABLE; mirrored in Go, parity-tested)
-- ---------------------------------------------------------------------

-- Displayed state: forgotten > conflict > stale > proposed > merged >
-- faded > kept. `rejected` is internal (no mark) and wins over flags.
CREATE FUNCTION v2.display_state(lifecycle text, flags text[]) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT CASE
        WHEN lifecycle = 'forgotten' THEN 'forgotten'
        WHEN lifecycle = 'rejected' THEN 'rejected'
        WHEN 'conflict' = ANY (flags) THEN 'conflict'
        WHEN 'stale' = ANY (flags) THEN 'stale'
        ELSE lifecycle
    END
$$;

-- from_state NULL means "creating the memory".
CREATE FUNCTION v2.lifecycle_transition_allowed(from_state text, to_state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT CASE
        WHEN from_state IS NULL THEN to_state IN ('proposed', 'kept')
        WHEN from_state = 'forgotten' THEN false
        WHEN to_state = 'forgotten' THEN true
        WHEN from_state = to_state THEN from_state IN ('proposed', 'kept')
        ELSE (from_state, to_state) IN (
            ('proposed', 'kept'),
            ('proposed', 'rejected'),
            ('proposed', 'merged'),
            ('merged', 'proposed'),
            ('kept', 'faded'),
            ('faded', 'kept'))
    END
$$;

CREATE FUNCTION v2.flags_valid(flags text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT flags IS NOT NULL
       AND array_position(flags, NULL) IS NULL
       AND flags <@ ARRAY['conflict', 'stale']::text[]
       AND cardinality(flags) = (SELECT count(DISTINCT f) FROM unnest(flags) AS f)
$$;

-- ---------------------------------------------------------------------
-- Receipts
-- ---------------------------------------------------------------------

CREATE TABLE v2.receipts (
    id             uuid PRIMARY KEY,                       -- uuidv7, from Go
    tenant_id      uuid NOT NULL,
    space_id       uuid NOT NULL,
    object_kind    text NOT NULL,
    object_id      uuid NOT NULL,
    object_ref     text NOT NULL,                          -- "M-0219"
    action         text NOT NULL,                          -- past tense
    actor_kind     text NOT NULL,
    actor_id       uuid,                                   -- user or agent connection; null for memax/dream/repository
    agent          text,                                   -- agent slug the action came through ("claude-code"), if any
    via            text NOT NULL,
    assurance      text,                                   -- for keeps: human_web | client_attested
    session_ref    text,
    source         jsonb,                                  -- {kind, ref}: a reference, never a quote
    reason         text,                                   -- redacted to null at Forget
    occurred_at    timestamptz NOT NULL,                   -- client time (offline queues keep it)
    recorded_at    timestamptz NOT NULL DEFAULT now(),     -- stamped by trigger
    txid           xid8 NOT NULL DEFAULT pg_current_xact_id(), -- stamped by trigger
    stream_id      uuid NOT NULL,
    stream_version integer NOT NULL,
    seq            bigint GENERATED ALWAYS AS IDENTITY,
    CONSTRAINT receipts_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT receipts_stream_version_key UNIQUE (stream_id, stream_version),
    CONSTRAINT receipts_seq_key UNIQUE (seq),
    CONSTRAINT receipts_object_kind_check CHECK (object_kind IN
        ('memory', 'note', 'brief', 'target', 'compile', 'handoff', 'gate', 'dream', 'agent', 'space')),
    CONSTRAINT receipts_action_check CHECK (action IN
        ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
         'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid')),
    CONSTRAINT receipts_actor_kind_check CHECK (actor_kind IN ('person', 'agent', 'dream', 'memax', 'repository')),
    CONSTRAINT receipts_actor_id_check CHECK (actor_kind NOT IN ('person', 'agent') OR actor_id IS NOT NULL),
    CONSTRAINT receipts_via_check CHECK (via IN
        ('web', 'cli', 'mcp', 'review', 'api', 'email', 'slack', 'github', 'linear', 'import', 'system')),
    CONSTRAINT receipts_assurance_check CHECK (assurance IS NULL OR assurance IN ('human_web', 'client_attested')),
    CONSTRAINT receipts_object_ref_check CHECK (object_ref <> '' AND char_length(object_ref) <= 32),
    CONSTRAINT receipts_agent_check CHECK (agent IS NULL OR (agent <> '' AND char_length(agent) <= 64)),
    CONSTRAINT receipts_session_ref_check CHECK (session_ref IS NULL OR char_length(session_ref) <= 255),
    CONSTRAINT receipts_source_check CHECK (source IS NULL OR jsonb_typeof(source) = 'object'),
    CONSTRAINT receipts_reason_check CHECK (reason IS NULL OR char_length(reason) <= 2000),
    CONSTRAINT receipts_stream_version_check CHECK (stream_version >= 1)
);

CREATE INDEX receipts_space_seq_idx ON v2.receipts (space_id, seq DESC);
CREATE INDEX receipts_object_seq_idx ON v2.receipts (object_id, seq DESC);

COMMENT ON TABLE v2.receipts IS
    'Append-only, content-free log of every change to the V2 record. Never holds memory text.';

CREATE FUNCTION v2.receipts_stamp() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.txid := pg_current_xact_id();
    NEW.recorded_at := now();
    RETURN NEW;
END $$;

CREATE TRIGGER receipts_stamp
    BEFORE INSERT ON v2.receipts
    FOR EACH ROW EXECUTE FUNCTION v2.receipts_stamp();

-- Receipts are immutable except that `reason` may be cleared.
CREATE FUNCTION v2.receipts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.receipts;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        n := NEW;
        n.reason := OLD.reason;
        IF n IS NOT DISTINCT FROM OLD AND NEW.reason IS NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'receipts are append-only: % is not allowed (only Forget may clear a reason)', TG_OP
        USING ERRCODE = 'MXR02';
END $$;

CREATE TRIGGER receipts_guard
    BEFORE UPDATE OR DELETE ON v2.receipts
    FOR EACH ROW EXECUTE FUNCTION v2.receipts_guard();

CREATE TRIGGER receipts_guard_truncate
    BEFORE TRUNCATE ON v2.receipts
    FOR EACH STATEMENT EXECUTE FUNCTION v2.receipts_guard();

-- ---------------------------------------------------------------------
-- Memories: one statement per row
-- ---------------------------------------------------------------------

CREATE TABLE v2.memories (
    id                 uuid PRIMARY KEY,                    -- uuidv7, from Go
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    seq                bigint NOT NULL,                     -- display number: M-<seq>, per tenant
    section            text NOT NULL,
    kind               text NOT NULL DEFAULT 'fact',
    lifecycle          text NOT NULL,
    flags              text[] NOT NULL DEFAULT '{}',
    state              text GENERATED ALWAYS AS (v2.display_state(lifecycle, flags)) STORED,
    trust              text NOT NULL,
    current_version    integer NOT NULL DEFAULT 1,          -- latest memory_versions.version (If-Match for edits)
    stream_version     integer NOT NULL DEFAULT 1,          -- latest receipts.stream_version on this memory
    stale_after        timestamptz,
    conditions         jsonb NOT NULL DEFAULT '[]'::jsonb,  -- "stays true while" predicates (§5.9)
    decision           jsonb,                               -- kind = decision: why, options, consequences, area, status
    scope              jsonb NOT NULL DEFAULT '{}'::jsonb,  -- where it applies, e.g. {"paths": ["packages/web/**"]}
    valid_from         timestamptz,
    valid_to           timestamptz,
    embedding          halfvec(1024),                       -- derived; written by the index job
    search             tsvector,                            -- derived from the current statement
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT memories_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT memories_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT memories_id_space_key UNIQUE (id, space_id),
    CONSTRAINT memories_seq_check CHECK (seq >= 1),
    CONSTRAINT memories_section_check CHECK (section IN ('decisions', 'conventions', 'preferences', 'open_question')),
    CONSTRAINT memories_kind_check CHECK (kind IN ('fact', 'decision')),
    CONSTRAINT memories_lifecycle_check CHECK (lifecycle IN ('proposed', 'kept', 'merged', 'faded', 'forgotten', 'rejected')),
    CONSTRAINT memories_flags_check CHECK (v2.flags_valid(flags)),
    CONSTRAINT memories_flags_lifecycle_check CHECK (cardinality(flags) = 0 OR lifecycle IN ('proposed', 'kept')),
    CONSTRAINT memories_stale_kept_check CHECK (NOT ('stale' = ANY (flags)) OR lifecycle = 'kept'),
    CONSTRAINT memories_trust_check CHECK (trust IN ('person', 'agent_own_work', 'repository', 'external')),
    CONSTRAINT memories_versions_check CHECK (current_version >= 1 AND stream_version >= 1),
    CONSTRAINT memories_decision_check CHECK (decision IS NULL OR (kind = 'decision' AND jsonb_typeof(decision) = 'object')),
    CONSTRAINT memories_conditions_check CHECK (jsonb_typeof(conditions) = 'array'),
    CONSTRAINT memories_scope_check CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT memories_validity_check CHECK (valid_from IS NULL OR valid_to IS NULL OR valid_to >= valid_from),
    -- Forget purges the words from the derived columns too.
    CONSTRAINT memories_forgotten_purged_check CHECK (lifecycle <> 'forgotten' OR (embedding IS NULL AND search IS NULL))
);

CREATE INDEX memories_space_seq_idx ON v2.memories (space_id, seq DESC);
CREATE INDEX memories_space_state_seq_idx ON v2.memories (space_id, state, seq DESC);

COMMENT ON TABLE v2.memories IS
    'M-: one statement under a seal. The words live in memory_versions; this row is the projection.';

CREATE FUNCTION v2.memories_lifecycle_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    from_state text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        from_state := OLD.lifecycle;
    END IF;
    IF NOT v2.lifecycle_transition_allowed(from_state, NEW.lifecycle) THEN
        RAISE EXCEPTION 'memory %: lifecycle % -> % is not allowed', NEW.id, COALESCE(from_state, '(new)'), NEW.lifecycle
            USING ERRCODE = 'MXL01';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER memories_lifecycle_guard_insert
    BEFORE INSERT ON v2.memories
    FOR EACH ROW EXECUTE FUNCTION v2.memories_lifecycle_guard();

-- Fires on a lifecycle change, and on any change to a forgotten memory
-- (forgotten is terminal).
CREATE TRIGGER memories_lifecycle_guard_update
    BEFORE UPDATE ON v2.memories
    FOR EACH ROW
    WHEN (OLD.lifecycle IS DISTINCT FROM NEW.lifecycle OR OLD.lifecycle = 'forgotten')
    EXECUTE FUNCTION v2.memories_lifecycle_guard();

CREATE TABLE v2.memory_versions (
    memory_id       uuid NOT NULL,
    version         integer NOT NULL,
    space_id        uuid NOT NULL,
    statement       text,                                   -- null once forgotten
    receipt_id      uuid NOT NULL REFERENCES v2.receipts (id), -- the receipt that wrote this version
    last_receipt_id uuid NOT NULL REFERENCES v2.receipts (id), -- the latest change (creation or purge)
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, version),
    CONSTRAINT memory_versions_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT memory_versions_version_check CHECK (version >= 1),
    CONSTRAINT memory_versions_statement_check CHECK (statement IS NULL OR (statement <> '' AND char_length(statement) <= 8000))
);

CREATE INDEX memory_versions_space_idx ON v2.memory_versions (space_id);

-- ---------------------------------------------------------------------
-- Sources and links
-- ---------------------------------------------------------------------

CREATE TABLE v2.sources (
    id                 uuid PRIMARY KEY,
    space_id           uuid NOT NULL REFERENCES public.hubs (id),
    kind               text NOT NULL,
    uri                text,                                -- URL, file path, PR URL…
    ref                text NOT NULL,                       -- shown to people: "PR #212", "go.mod:14", "N-0882"
    locator            jsonb NOT NULL DEFAULT '{}'::jsonb,  -- {path, line, commit} / {repo, number} …
    external           boolean NOT NULL DEFAULT false,
    trust_class        text NOT NULL,
    quote              text,                                -- supporting excerpt; purged at Forget
    content_hash       text,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sources_id_space_key UNIQUE (id, space_id),
    CONSTRAINT sources_kind_check CHECK (kind IN ('session', 'pr', 'file', 'url', 'issue', 'email', 'note', 'import')),
    CONSTRAINT sources_trust_class_check CHECK (trust_class IN ('person', 'agent_own_work', 'repository', 'external')),
    CONSTRAINT sources_external_check CHECK (external = (trust_class = 'external')),
    CONSTRAINT sources_ref_check CHECK (ref <> '' AND char_length(ref) <= 500),
    CONSTRAINT sources_uri_check CHECK (uri IS NULL OR char_length(uri) <= 2048),
    CONSTRAINT sources_quote_check CHECK (quote IS NULL OR char_length(quote) <= 4000),
    CONSTRAINT sources_locator_check CHECK (jsonb_typeof(locator) = 'object')
);

CREATE INDEX sources_space_idx ON v2.sources (space_id);

CREATE TABLE v2.memory_sources (
    memory_id  uuid NOT NULL,
    source_id  uuid NOT NULL,
    space_id   uuid NOT NULL,
    receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, source_id),
    CONSTRAINT memory_sources_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT memory_sources_source_fkey FOREIGN KEY (source_id, space_id) REFERENCES v2.sources (id, space_id)
);

CREATE INDEX memory_sources_source_idx ON v2.memory_sources (source_id);
CREATE INDEX memory_sources_space_idx ON v2.memory_sources (space_id);

CREATE TABLE v2.memory_links (
    id             uuid PRIMARY KEY,
    space_id       uuid NOT NULL,
    kind           text NOT NULL,
    from_memory_id uuid NOT NULL,
    to_memory_id   uuid,
    to_note_id     uuid,                                    -- V1 public.memories id (no FK, see header)
    receipt_id     uuid NOT NULL REFERENCES v2.receipts (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT memory_links_from_fkey FOREIGN KEY (from_memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT memory_links_to_fkey FOREIGN KEY (to_memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT memory_links_kind_check CHECK (kind IN ('merged_into', 'supersedes', 'conflicts_with', 'closes', 'folded_from')),
    CONSTRAINT memory_links_target_check CHECK ((to_memory_id IS NULL) <> (to_note_id IS NULL)),
    CONSTRAINT memory_links_note_check CHECK ((kind = 'folded_from') = (to_note_id IS NOT NULL)),
    CONSTRAINT memory_links_self_check CHECK (to_memory_id IS DISTINCT FROM from_memory_id),
    CONSTRAINT memory_links_unique UNIQUE NULLS NOT DISTINCT (kind, from_memory_id, to_memory_id, to_note_id)
);

CREATE INDEX memory_links_from_idx ON v2.memory_links (from_memory_id);
CREATE INDEX memory_links_to_idx ON v2.memory_links (to_memory_id) WHERE to_memory_id IS NOT NULL;
CREATE INDEX memory_links_space_idx ON v2.memory_links (space_id);

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.require_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    sid uuid;
    objects uuid[];
    n v2.memories;
    o v2.memories;
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

CREATE CONSTRAINT TRIGGER memories_require_receipt
    AFTER INSERT OR UPDATE ON v2.memories
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER memory_versions_require_receipt
    AFTER INSERT OR UPDATE ON v2.memory_versions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER sources_require_receipt
    AFTER INSERT OR UPDATE ON v2.sources
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER memory_sources_require_receipt
    AFTER INSERT OR UPDATE ON v2.memory_sources
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

CREATE CONSTRAINT TRIGGER memory_links_require_receipt
    AFTER INSERT OR UPDATE ON v2.memory_links
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_receipt();

-- Forget's redaction: the one UPDATE receipts allow. It only runs when
-- the object has a `forgot` receipt in the same transaction, so a
-- reason can't be cleared outside a receipted Forget.
CREATE FUNCTION v2.redact_receipt_reasons(p_object_id uuid) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    redacted integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts
         WHERE object_id = p_object_id
           AND action = 'forgot'
           AND txid = pg_current_xact_id()
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
    ) THEN
        RAISE EXCEPTION 'object %: reasons are redacted only by a Forget in the same transaction', p_object_id
            USING ERRCODE = 'MXR02';
    END IF;
    UPDATE v2.receipts
       SET reason = NULL
     WHERE object_id = p_object_id
       AND reason IS NOT NULL
       AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    GET DIAGNOSTICS redacted = ROW_COUNT;
    RETURN redacted;
END $$;

REVOKE ALL ON FUNCTION v2.redact_receipt_reasons(uuid) FROM PUBLIC;

-- ---------------------------------------------------------------------
-- Display-ID counters and idempotency keys
-- ---------------------------------------------------------------------

CREATE TABLE v2.id_counters (
    tenant_id uuid NOT NULL,
    prefix    text NOT NULL,
    next      bigint NOT NULL,
    PRIMARY KEY (tenant_id, prefix),
    CONSTRAINT id_counters_prefix_check CHECK (prefix IN ('M', 'N', 'H', 'C', 'R', 'D', 'B', 'G')),
    CONSTRAINT id_counters_next_check CHECK (next >= 1)
);

COMMENT ON TABLE v2.id_counters IS
    'Per-tenant display-ID counters (M-0219 …), allocated in the writing transaction.';

-- One row per applied command, so a retried command (same actor,
-- space and Idempotency-Key) returns the original result without
-- writing. `outcome` is null only inside the transaction that claimed
-- the key, so no other session ever sees it null. request_hash lets a
-- reused key with different content be refused instead of silently
-- replayed. Refused commands are not stored: a retry is decided again.
--
-- request_hash is a SHA-256 of the command, statement included. Forget
-- must clear it for the forgotten object (forget-propagation epic), or
-- it could confirm a guess of the purged words.
CREATE TABLE v2.command_keys (
    space_id        uuid NOT NULL REFERENCES public.hubs (id),
    actor_kind      text NOT NULL,
    actor_id        uuid,
    idempotency_key text NOT NULL,
    command         text NOT NULL,
    request_hash    bytea NOT NULL,
    outcome         text,
    policy          jsonb NOT NULL DEFAULT '{}'::jsonb,     -- the policy decision that shaped the outcome (code, message)
    object_id       uuid,
    receipt_ids     uuid[] NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT command_keys_key UNIQUE NULLS NOT DISTINCT (space_id, actor_kind, actor_id, idempotency_key),
    CONSTRAINT command_keys_key_check CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    CONSTRAINT command_keys_outcome_check CHECK (outcome IS NULL OR outcome IN ('applied', 'proposed', 'needs_confirmation'))
);

CREATE INDEX command_keys_created_idx ON v2.command_keys (created_at);

-- ---------------------------------------------------------------------
-- Read-only views over V1 tables (until cutover)
-- ---------------------------------------------------------------------

-- Spaces in the current scope. memax_v2 has no privileges on
-- public.hubs; it reads spaces only through this view.
CREATE VIEW v2.spaces WITH (security_barrier = true) AS
    SELECT h.id, h.tenant_id, h.space_kind AS kind, h.name, h.slug, h.owner_id,
           h.repository, h.rules, h.created_at
      FROM public.hubs h
     WHERE h.id = ANY ((SELECT v2.current_space_ids())::uuid[]);

-- N-: V1 memories become notes (plan 25 §5.4, §10). Notes are input to
-- Dream and Review, never kept context. The physical move happens at
-- cutover (3.8); until then this is a scoped, read-only window.
CREATE VIEW v2.notes WITH (security_barrier = true) AS
    SELECT m.id, m.owner_id, m.hub_id AS space_id, m.title,
           left(m.content, 280) AS excerpt, m.state, m.created_at
      FROM public.memories m
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[]);

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY receipts_space ON v2.receipts
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.memories ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.memories FORCE ROW LEVEL SECURITY;
CREATE POLICY memories_space ON v2.memories
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.memory_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.memory_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_versions_space ON v2.memory_versions
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.sources FORCE ROW LEVEL SECURITY;
CREATE POLICY sources_space ON v2.sources
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.memory_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.memory_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_sources_space ON v2.memory_sources
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.memory_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.memory_links FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_links_space ON v2.memory_links
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.command_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.command_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY command_keys_space ON v2.command_keys
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

ALTER TABLE v2.id_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.id_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY id_counters_tenant ON v2.id_counters
    USING (tenant_id = ANY ((SELECT v2.current_tenant_ids())::uuid[]))
    WITH CHECK (tenant_id = ANY ((SELECT v2.current_tenant_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. No DELETE or TRUNCATE anywhere;
-- identity columns (id, tenant, space, seq, creating receipt) are not
-- updatable.
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.receipts TO memax_v2;

GRANT SELECT, INSERT ON v2.memories TO memax_v2;
GRANT UPDATE (section, kind, lifecycle, flags, trust, current_version, stream_version, stale_after,
              conditions, decision, scope, valid_from, valid_to, embedding, search,
              last_receipt_id, updated_at)
    ON v2.memories TO memax_v2;

GRANT SELECT, INSERT ON v2.memory_versions TO memax_v2;
GRANT UPDATE (statement, last_receipt_id) ON v2.memory_versions TO memax_v2;

GRANT SELECT, INSERT ON v2.sources TO memax_v2;
GRANT UPDATE (quote, last_receipt_id) ON v2.sources TO memax_v2;

GRANT SELECT, INSERT ON v2.memory_sources TO memax_v2;
GRANT SELECT, INSERT ON v2.memory_links TO memax_v2;

GRANT SELECT, INSERT ON v2.id_counters TO memax_v2;
GRANT UPDATE (next) ON v2.id_counters TO memax_v2;

GRANT SELECT, INSERT ON v2.command_keys TO memax_v2;
GRANT UPDATE (outcome, policy, object_id, receipt_ids) ON v2.command_keys TO memax_v2;

GRANT SELECT ON v2.spaces, v2.notes TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.redact_receipt_reasons(uuid) TO memax_v2;
