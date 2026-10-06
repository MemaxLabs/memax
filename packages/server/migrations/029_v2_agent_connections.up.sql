-- 029: v2_agent_connections
--
-- Agent connections (plan 25 §5.15, epic 1.8): every API key and OAuth
-- grant that acts on the V2 record resolves to one agent connection, so
-- receipts name the agent ("CX") rather than a credential, and the
-- person decides how much it may write in each space.
--
--   agent_connections        one per credential: who the agent works for
--                            (person_id), which agent it is, its surface,
--                            the credential it is bound to, and its state
--                            (active | paused | disconnected).
--   agent_connection_spaces  its autonomy (read | propose | write) in each
--                            space it is connected to.
--
-- The same three guarantees as 028 hold here:
--
-- 1. Receipt or refused. A deferred constraint trigger refuses the
--    COMMIT unless every inserted or updated row points at a receipt
--    about the connection (object_kind = 'agent') written in the same
--    transaction. The one exception is last_seen_at, which is usage
--    data, like 028's derived index columns: an UPDATE that changes only
--    last_seen_at needs no receipt.
--
-- 2. The state machine is checked twice: in Go (internal/ledger) and by
--    v2.agent_state_transition_allowed here. disconnected is terminal.
--
-- 3. Isolation. RLS is ENABLEd and FORCEd on both tables. A connection is
--    visible to the person it works for (app.person_id, a new scope
--    setting the ledger sets per transaction) and to the members of the
--    spaces it is connected to (app.space_ids). Writes to
--    agent_connection_spaces must stay inside app.space_ids.
--
-- Deliberate choices:
--   * credential_id has no foreign key. V1 revokes an API key by DELETEing
--     its row; a foreign key would either block that or cascade an
--     unreceipted change into the record. A connection whose credential is
--     gone can't authenticate, so it does nothing.
--   * memax_v2 has no privileges on V1's api_keys and oauth_grants. Two
--     SECURITY DEFINER functions give it exactly what the ledger needs:
--     v2.agent_credential_access (is this credential active and the
--     person's, and may it write?) and v2.revoke_agent_credential
--     (Disconnect revokes the credential in the same transaction, and
--     only with a `disconnected` receipt written in that transaction).
--   * agent_connection_spaces repeats person_id so its RLS policy needn't
--     read agent_connections (whose policy reads agent_connection_spaces;
--     the two can't refer to each other). A composite foreign key keeps
--     the copy honest.
--   * receipts_action_check is replaced to admit the connection verbs.
--     Any later migration that widens it again must keep this list.

-- ---------------------------------------------------------------------
-- Scope: the person the transaction acts for
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.current_person_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.person_id', true), '')::uuid $$;

COMMENT ON FUNCTION v2.current_person_id() IS
    'The person in the current transaction scope (app.person_id): the actor, or the person an agent works for. NULL when unset.';

-- ---------------------------------------------------------------------
-- Vocabulary (mirrored in Go, parity-tested)
-- ---------------------------------------------------------------------

-- from_state NULL means "creating the connection".
CREATE FUNCTION v2.agent_state_transition_allowed(from_state text, to_state text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT CASE
        WHEN from_state IS NULL THEN to_state = 'active'
        WHEN from_state = 'disconnected' THEN false
        WHEN from_state = to_state THEN true
        ELSE (from_state, to_state) IN (
            ('active', 'paused'),
            ('paused', 'active'),
            ('active', 'disconnected'),
            ('paused', 'disconnected'))
    END
$$;

-- ---------------------------------------------------------------------
-- Receipts: the connection verbs, and an index for per-agent counts
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected'));

-- "Writes 7d" and an agent's recent writes read receipts by agent.
CREATE INDEX receipts_agent_actor_idx ON v2.receipts (actor_id, recorded_at DESC) WHERE actor_kind = 'agent';

-- ---------------------------------------------------------------------
-- Agent connections
-- ---------------------------------------------------------------------

CREATE TABLE v2.agent_connections (
    id                 uuid PRIMARY KEY,                       -- uuidv7, from Go; receipts.actor_id of the agent's writes
    tenant_id          uuid NOT NULL,                          -- whose record it belongs to: the person's own tenant
    person_id          uuid NOT NULL REFERENCES public.users (id), -- who the agent works for
    agent              text NOT NULL,                          -- claude-code, codex, …, other
    display_name       text NOT NULL,                          -- "Claude Code"
    surface            text NOT NULL,                          -- cli | ide | cloud | chat
    credential_kind    text NOT NULL,                          -- api_key | oauth_grant
    credential_id      uuid NOT NULL,                          -- api_keys.id or oauth_grants.id (no FK, see header)
    client_id          text,                                   -- CIMD client_id URL (plan 25 §5.15), once OAuth supports it
    state              text NOT NULL DEFAULT 'active',         -- active | paused | disconnected
    stream_version     integer NOT NULL DEFAULT 1,             -- latest receipts.stream_version on this connection
    last_seen_at       timestamptz,                            -- usage data; updated without a receipt
    disconnected_at    timestamptz,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agent_connections_credential_key UNIQUE (credential_kind, credential_id),
    CONSTRAINT agent_connections_id_person_key UNIQUE (id, person_id),
    -- A connection belongs to its person's own tenant. Connections owned
    -- by a team (a shared bot) are a later decision.
    CONSTRAINT agent_connections_tenant_check CHECK (tenant_id = person_id),
    CONSTRAINT agent_connections_agent_check CHECK (agent IN
        ('claude-code', 'codex', 'cursor', 'chatgpt', 'claude', 'gemini-cli', 'copilot', 'opencode', 'windsurf', 'other')),
    CONSTRAINT agent_connections_surface_check CHECK (surface IN ('cli', 'ide', 'cloud', 'chat')),
    CONSTRAINT agent_connections_credential_kind_check CHECK (credential_kind IN ('api_key', 'oauth_grant')),
    CONSTRAINT agent_connections_state_check CHECK (state IN ('active', 'paused', 'disconnected')),
    CONSTRAINT agent_connections_disconnected_check CHECK ((state = 'disconnected') = (disconnected_at IS NOT NULL)),
    CONSTRAINT agent_connections_display_name_check CHECK (display_name <> '' AND char_length(display_name) <= 100),
    CONSTRAINT agent_connections_client_id_check CHECK (client_id IS NULL OR (client_id LIKE 'https://%' AND char_length(client_id) <= 2048)),
    CONSTRAINT agent_connections_stream_version_check CHECK (stream_version >= 1)
);

CREATE INDEX agent_connections_person_idx ON v2.agent_connections (person_id);

COMMENT ON TABLE v2.agent_connections IS
    'An agent working for a person through one credential (API key or OAuth grant). Written only through internal/ledger.';

CREATE TABLE v2.agent_connection_spaces (
    connection_id      uuid NOT NULL,
    space_id           uuid NOT NULL,
    tenant_id          uuid NOT NULL,
    person_id          uuid NOT NULL,                          -- the connection's person, repeated for RLS (see header)
    autonomy           text NOT NULL,                          -- read | propose | write
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, space_id),
    CONSTRAINT agent_connection_spaces_connection_fkey FOREIGN KEY (connection_id, person_id)
        REFERENCES v2.agent_connections (id, person_id),
    CONSTRAINT agent_connection_spaces_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT agent_connection_spaces_autonomy_check CHECK (autonomy IN ('read', 'propose', 'write'))
);

CREATE INDEX agent_connection_spaces_space_idx ON v2.agent_connection_spaces (space_id);
CREATE INDEX agent_connection_spaces_person_idx ON v2.agent_connection_spaces (person_id);

COMMENT ON TABLE v2.agent_connection_spaces IS
    'An agent connection''s autonomy in one space. Written only through internal/ledger.';

-- ---------------------------------------------------------------------
-- The state machine (disconnected is terminal)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.agent_connections_state_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    from_state text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        from_state := OLD.state;
    END IF;
    IF NOT v2.agent_state_transition_allowed(from_state, NEW.state) THEN
        RAISE EXCEPTION 'agent connection %: state % -> % is not allowed', NEW.id, COALESCE(from_state, '(new)'), NEW.state
            USING ERRCODE = 'MXL02';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER agent_connections_state_guard_insert
    BEFORE INSERT ON v2.agent_connections
    FOR EACH ROW EXECUTE FUNCTION v2.agent_connections_state_guard();

-- Fires on a state change, and on any change to a disconnected
-- connection.
CREATE TRIGGER agent_connections_state_guard_update
    BEFORE UPDATE ON v2.agent_connections
    FOR EACH ROW
    WHEN (OLD.state IS DISTINCT FROM NEW.state OR OLD.state = 'disconnected')
    EXECUTE FUNCTION v2.agent_connections_state_guard();

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.require_agent_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    rid uuid;
    sid uuid;
    oid uuid;
    n v2.agent_connections;
    o v2.agent_connections;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'agent_connections' THEN
        IF TG_OP = 'UPDATE' THEN
            -- last_seen_at is usage data, not the record.
            n := NEW; o := OLD;
            n.last_seen_at := NULL; o.last_seen_at := NULL;
            IF n IS NOT DISTINCT FROM o THEN
                RETURN NULL;
            END IF;
        END IF;
        rid := NEW.last_receipt_id; sid := NULL; oid := NEW.id;
    WHEN 'agent_connection_spaces' THEN
        rid := NEW.last_receipt_id; sid := NEW.space_id; oid := NEW.connection_id;
    ELSE
        RAISE EXCEPTION 'v2.require_agent_receipt is not configured for table %', TG_TABLE_NAME;
    END CASE;

    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = rid
           AND r.txid = pg_current_xact_id()
           AND r.object_kind = 'agent'
           AND r.object_id = oid
           AND (sid IS NULL OR r.space_id = sid)
    ) THEN
        RAISE EXCEPTION 'v2.%: % without a receipt written in the same transaction', TG_TABLE_NAME, TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER agent_connections_require_receipt
    AFTER INSERT OR UPDATE ON v2.agent_connections
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_agent_receipt();

CREATE CONSTRAINT TRIGGER agent_connection_spaces_require_receipt
    AFTER INSERT OR UPDATE ON v2.agent_connection_spaces
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_agent_receipt();

-- ---------------------------------------------------------------------
-- The bound credential (V1's api_keys and oauth_grants)
-- ---------------------------------------------------------------------

-- What a credential allows, for a connection the ledger is about to
-- make: 'write' when it is active, belongs to p_person and carries
-- memory:write; 'read' when it is active and the person's but can't
-- write; NULL when it is revoked, expired, someone else's or unknown.
-- It answers only about the person's own credentials, so it can't be
-- used to learn whose a credential is.
CREATE FUNCTION v2.agent_credential_access(p_kind text, p_id uuid, p_person uuid) RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
    SELECT CASE WHEN 'memory:write' = ANY (c.perms) THEN 'write' ELSE 'read' END
      FROM (
        SELECT k.default_permissions AS perms
          FROM public.api_keys k
         WHERE p_kind = 'api_key' AND k.id = p_id AND k.user_id = p_person
           AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())
        UNION ALL
        SELECT g.default_permissions
          FROM public.oauth_grants g
         WHERE p_kind = 'oauth_grant' AND g.id = p_id AND g.user_id = p_person
           AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at > now())
      ) c
     LIMIT 1
$$;

-- Disconnect's revocation. It sets revoked_at on the bound API key or
-- OAuth grant, which V1's resolvers (ResolveAPIKey, ResolveOAuthGrant,
-- and the refresh path) already refuse, so the credential stops working
-- on the next request. It runs only for a disconnected connection with a
-- `disconnected` receipt written in this transaction, in a space in the
-- current scope, so a credential is never revoked without a receipt.
-- Returns whether a credential was revoked (false when V1 had already
-- deleted or revoked it).
CREATE FUNCTION v2.revoke_agent_credential(p_connection uuid) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    c record;
    revoked integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts
         WHERE object_id = p_connection
           AND object_kind = 'agent'
           AND action = 'disconnected'
           AND txid = pg_current_xact_id()
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
    ) THEN
        RAISE EXCEPTION 'agent connection %: a credential is revoked only by a Disconnect in the same transaction', p_connection
            USING ERRCODE = 'MXR01';
    END IF;
    SELECT credential_kind, credential_id, person_id INTO c
      FROM v2.agent_connections
     WHERE id = p_connection AND state = 'disconnected';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'agent connection %: not disconnected', p_connection
            USING ERRCODE = 'MXL02';
    END IF;
    IF c.credential_kind = 'api_key' THEN
        UPDATE public.api_keys SET revoked_at = now()
         WHERE id = c.credential_id AND user_id = c.person_id AND revoked_at IS NULL;
    ELSE
        UPDATE public.oauth_grants SET revoked_at = now(), updated_at = now()
         WHERE id = c.credential_id AND user_id = c.person_id AND revoked_at IS NULL;
    END IF;
    GET DIAGNOSTICS revoked = ROW_COUNT;
    RETURN revoked > 0;
END $$;

REVOKE ALL ON FUNCTION v2.agent_credential_access(text, uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.revoke_agent_credential(uuid) FROM PUBLIC;

-- ---------------------------------------------------------------------
-- Row-level security
-- ---------------------------------------------------------------------

ALTER TABLE v2.agent_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.agent_connections FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_connections_scope ON v2.agent_connections
    USING (person_id = (SELECT v2.current_person_id())
           OR id IN (SELECT s.connection_id FROM v2.agent_connection_spaces s
                      WHERE s.space_id = ANY ((SELECT v2.current_space_ids())::uuid[])))
    WITH CHECK (person_id = (SELECT v2.current_person_id())
           OR id IN (SELECT s.connection_id FROM v2.agent_connection_spaces s
                      WHERE s.space_id = ANY ((SELECT v2.current_space_ids())::uuid[])));

ALTER TABLE v2.agent_connection_spaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.agent_connection_spaces FORCE ROW LEVEL SECURITY;
CREATE POLICY agent_connection_spaces_scope ON v2.agent_connection_spaces
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
           OR person_id = (SELECT v2.current_person_id()))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. No DELETE or TRUNCATE; identity
-- columns (id, tenant, person, agent, surface, credential, creating
-- receipt) are not updatable.
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.agent_connections TO memax_v2;
GRANT UPDATE (display_name, state, stream_version, last_seen_at, disconnected_at, last_receipt_id, updated_at)
    ON v2.agent_connections TO memax_v2;

GRANT SELECT, INSERT ON v2.agent_connection_spaces TO memax_v2;
GRANT UPDATE (autonomy, last_receipt_id, updated_at) ON v2.agent_connection_spaces TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.agent_credential_access(text, uuid, uuid) TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.revoke_agent_credential(uuid) TO memax_v2;
