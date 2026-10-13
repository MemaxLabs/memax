-- 061: v2_start_keys
--
-- Start keys for cloud agents (cloud warm-start design §4, epic 3.2): a
-- cloud agent's sandbox (Claude Code's cloud sessions, Copilot's cloud
-- agent) starts its sessions with an API key whose one permission is
-- compile:read. It is bound to an agent connection on the cloud surface,
-- at Read, in one space, and the API refuses it everywhere but the cloud
-- start (POST /v2/spaces/{space}/session-starts).
--
-- memax_v2 still has no privileges on V1's api_keys (029). Two SECURITY
-- DEFINER functions give the ledger exactly what it needs, each only with
-- its receipt written in the same transaction, as 029's
-- v2.revoke_agent_credential does:
--
--   v2.mint_start_key   makes the start key of a cloud connection just
--                       connected (its `connected` receipt in this
--                       transaction): the connection's credential_id, its
--                       person, its agent and its one space; compile:read
--                       only; an expiry within a year.
--   v2.rekey_start_key  gives a cloud connection's key a new secret and a
--                       new expiry (its `rekeyed` receipt in this
--                       transaction): the same row, so the connection, its
--                       receipts and its reads stay one history, and the
--                       old secret stops working at once.
--
-- Both take the key's SHA-256, never the key: the secret exists only in
-- the answer to the person who made it.
--
-- receipts_action_check gains rekeyed. The list keeps every verb 028
-- through 059 admit; a branch that adds verbs in parallel merges by taking
-- the union of both lists (TestReceiptEnumsMatchTheSchema holds the last
-- migration to the spec's ReceiptAction enum).

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
     'conditions_changed',
     'rekeyed'));

-- The start key of a cloud connection the same transaction connected.
CREATE FUNCTION v2.mint_start_key(p_connection uuid, p_hash text, p_prefix text, p_name text, p_expires timestamptz)
    RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    c record;
    sp uuid;
    n integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts
         WHERE object_id = p_connection AND object_kind = 'agent' AND action = 'connected'
           AND txid = pg_current_xact_id()
    ) THEN
        RAISE EXCEPTION 'agent connection %: a start key is made only by the Connect that made its connection, in the same transaction', p_connection
            USING ERRCODE = 'MXR01';
    END IF;
    SELECT person_id, agent, surface, credential_kind, credential_id, state INTO c
      FROM v2.agent_connections WHERE id = p_connection;
    IF NOT FOUND OR c.surface <> 'cloud' OR c.credential_kind <> 'api_key' OR c.state <> 'active' THEN
        RAISE EXCEPTION 'agent connection %: start keys are for active cloud connections bound to an API key', p_connection
            USING ERRCODE = 'MXL02';
    END IF;
    SELECT count(*), min(space_id::text)::uuid INTO n, sp
      FROM v2.agent_connection_spaces WHERE connection_id = p_connection;
    IF n <> 1 THEN
        RAISE EXCEPTION 'agent connection %: a start key is for one space, not %', p_connection, n
            USING ERRCODE = 'MXL02';
    END IF;
    IF p_hash !~ '^[0-9a-f]{64}$' OR p_prefix !~ '^mxk_[0-9a-f]{4}$' OR char_length(p_name) NOT BETWEEN 1 AND 200
       OR p_expires <= now() OR p_expires > now() + interval '366 days' THEN
        RAISE EXCEPTION 'agent connection %: a start key needs a SHA-256, an mxk_ prefix, a name and an expiry within a year', p_connection
            USING ERRCODE = '22023';
    END IF;
    INSERT INTO public.api_keys (id, user_id, name, key_hash, prefix, scopes, expires_at, hub_id, agent_name,
                                 hub_scope_mode, hub_ids, default_permissions, trust_level, standalone)
    VALUES (c.credential_id, c.person_id, p_name, p_hash, p_prefix, ARRAY['start'], p_expires, sp, c.agent,
            'hub_allowlist', ARRAY[sp], ARRAY['compile:read'], 'standard', false);
END $$;

-- A new secret and expiry for a cloud connection's start key, beside its
-- `rekeyed` receipt in this transaction. Returns false when there is no
-- live start key to rekey (it was revoked, or deleted in Settings).
CREATE FUNCTION v2.rekey_start_key(p_connection uuid, p_hash text, p_prefix text, p_expires timestamptz)
    RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    c record;
    changed integer;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts
         WHERE object_id = p_connection AND object_kind = 'agent' AND action = 'rekeyed'
           AND txid = pg_current_xact_id()
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[])
    ) THEN
        RAISE EXCEPTION 'agent connection %: a start key is rekeyed only with a rekeyed receipt in the same transaction', p_connection
            USING ERRCODE = 'MXR01';
    END IF;
    SELECT person_id, surface, credential_kind, credential_id, state INTO c
      FROM v2.agent_connections WHERE id = p_connection;
    IF NOT FOUND OR c.surface <> 'cloud' OR c.credential_kind <> 'api_key' OR c.state = 'disconnected' THEN
        RAISE EXCEPTION 'agent connection %: only a cloud connection''s start key is rekeyed', p_connection
            USING ERRCODE = 'MXL02';
    END IF;
    IF p_hash !~ '^[0-9a-f]{64}$' OR p_prefix !~ '^mxk_[0-9a-f]{4}$'
       OR p_expires <= now() OR p_expires > now() + interval '366 days' THEN
        RAISE EXCEPTION 'agent connection %: a start key needs a SHA-256, an mxk_ prefix and an expiry within a year', p_connection
            USING ERRCODE = '22023';
    END IF;
    UPDATE public.api_keys
       SET key_hash = p_hash, prefix = p_prefix, expires_at = p_expires, last_used = NULL
     WHERE id = c.credential_id AND user_id = c.person_id AND revoked_at IS NULL
       AND default_permissions = ARRAY['compile:read'];
    GET DIAGNOSTICS changed = ROW_COUNT;
    RETURN changed > 0;
END $$;

REVOKE ALL ON FUNCTION v2.mint_start_key(uuid, text, text, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION v2.rekey_start_key(uuid, text, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.mint_start_key(uuid, text, text, text, timestamptz) TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.rekey_start_key(uuid, text, text, timestamptz) TO memax_v2;
