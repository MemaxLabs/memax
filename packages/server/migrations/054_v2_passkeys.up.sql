-- 054: v2_passkeys
--
-- Passkeys (WebAuthn) and the passkey re-check (plan 25 §5.15 and the
-- carry-over "a passkey re-check at Keep time is the strong guarantee for
-- quarantine and D15"; internal/passkeys has the design, and
-- internal/websurface/THREAT_MODEL.md the threat model).
--
-- A person signs in with a passkey, and once they have one, the decisions
-- that need a person (keeping a quarantined proposal, keeping or answering
-- a decision where the space needs a person on the web, raising what an
-- agent may do, Forget, forgetting the account, removing a passkey) ask
-- for a fresh, user-verified assertion bound to the person, their session
-- and the request. Such a change records the assurance human_web_verified.
--
--   passkeys            one row per credential: its id, COSE public key,
--                       sign count, transports, backup flags and AAGUID, a
--                       name the person chose, when it was added and last
--                       used. No secret: a public key verifies, it can't
--                       sign. The WebAuthn user handle is the person's id
--                       (16 opaque bytes), so it needs no column.
--   passkey_challenges  the challenges Memax handed out: purpose register,
--                       sign_in or check; a check's binding (the session
--                       and the SHA-256 of the request it confirms); the
--                       ceremony's options as go-webauthn recorded them;
--                       when it expires (at most 10 minutes; 5 in
--                       practice) and when it was used. Used once: the
--                       first assertion that verifies marks it, in the same
--                       statement that checks it was unused.
--
-- Isolation: RLS is ENABLEd and FORCEd on both, keyed on app.person_id, so
-- a transaction sees and changes only its own person's passkeys and
-- challenges. memax_v2 never deletes in v2 (TestEveryV2TableIsLockedDown):
-- removing a passkey, and clearing out expired challenges, go through
-- v2.remove_passkeys and v2.clear_passkey_challenges (SECURITY DEFINER),
-- which delete only the current person's rows (and sign-in challenges,
-- which are nobody's). Sign-in challenges belong to nobody yet (person_id NULL) and
-- hold nothing but a random nonce, so any memax_v2 transaction may read
-- and use them. Signing in has to find a credential before it knows whose
-- it is: v2.passkey_owner (SECURITY DEFINER) answers only the person a
-- credential id belongs to, through a policy bound to the function's owner
-- (`TO CURRENT_USER`, the migrating role) while app.sweep has its value,
-- which memax_v2 can't borrow (TestSweepPoliciesAreRoleBound).
--
-- Assurance: receipts and gate answers admit human_web_verified. The
-- receipt chain's encoding (format 1) carries the assurance as a string,
-- so it is unchanged.

-- ---------------------------------------------------------------------
-- Passkeys
-- ---------------------------------------------------------------------

CREATE TABLE v2.passkeys (
    id                 uuid PRIMARY KEY,                        -- uuidv7, from Go
    person_id          uuid NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    credential_id      bytea NOT NULL,
    public_key         bytea NOT NULL,                          -- COSE_Key
    sign_count         bigint NOT NULL DEFAULT 0,
    transports         text[] NOT NULL DEFAULT '{}',
    backup_eligible    boolean NOT NULL,
    backed_up          boolean NOT NULL,
    aaguid             uuid,
    attestation_format text NOT NULL DEFAULT 'none',
    name               text NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_used_at       timestamptz,
    created_session_id uuid,                                    -- the session that added it
    CONSTRAINT passkeys_credential_id_key UNIQUE (credential_id),
    CONSTRAINT passkeys_credential_id_check CHECK (octet_length(credential_id) BETWEEN 16 AND 1023),
    CONSTRAINT passkeys_public_key_check CHECK (octet_length(public_key) BETWEEN 16 AND 4096),
    CONSTRAINT passkeys_sign_count_check CHECK (sign_count >= 0),
    CONSTRAINT passkeys_name_check CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT passkeys_backup_check CHECK (backup_eligible OR NOT backed_up)
);

CREATE INDEX passkeys_person_idx ON v2.passkeys (person_id, created_at);

COMMENT ON TABLE v2.passkeys IS
    'A person''s passkeys (WebAuthn credentials): public keys only. RLS on app.person_id; sign-in finds an owner through v2.passkey_owner.';

ALTER TABLE v2.passkeys ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.passkeys FORCE ROW LEVEL SECURITY;
CREATE POLICY passkeys_person ON v2.passkeys
    USING (person_id = (SELECT v2.current_person_id()))
    WITH CHECK (person_id = (SELECT v2.current_person_id()));
-- v2.passkey_owner's read across people, bound to its owner.
CREATE POLICY passkeys_sign_in ON v2.passkeys FOR SELECT TO CURRENT_USER
    USING (current_setting('app.sweep', true) = 'passkey_sign_in');

GRANT SELECT, INSERT ON v2.passkeys TO memax_v2;
GRANT UPDATE (name, sign_count, backed_up, last_used_at) ON v2.passkeys TO memax_v2;

-- Takes one of the current person's passkeys off their account (p_id), or
-- all of them (NULL), and returns what it removed. RLS still applies to
-- the owner (FORCE), so only app.person_id's rows are visible to it.
CREATE FUNCTION v2.remove_passkeys(p_id uuid) RETURNS SETOF v2.passkeys
    LANGUAGE sql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
    DELETE FROM v2.passkeys
     WHERE person_id = v2.current_person_id() AND (p_id IS NULL OR id = p_id)
    RETURNING *
    $$;

REVOKE ALL ON FUNCTION v2.remove_passkeys(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.remove_passkeys(uuid) TO memax_v2;

-- The person a credential belongs to, for signing in with it. Nothing
-- else about the credential leaves: the caller then reads it in that
-- person's scope.
CREATE FUNCTION v2.passkey_owner(p_credential_id bytea) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET app.sweep = 'passkey_sign_in'
    AS $$ SELECT person_id FROM v2.passkeys WHERE credential_id = p_credential_id $$;

REVOKE ALL ON FUNCTION v2.passkey_owner(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.passkey_owner(bytea) TO memax_v2;

-- ---------------------------------------------------------------------
-- Challenges
-- ---------------------------------------------------------------------

CREATE TABLE v2.passkey_challenges (
    challenge     text PRIMARY KEY,                             -- base64url, as clientDataJSON returns it
    purpose       text NOT NULL,                                -- register | sign_in | check
    person_id     uuid REFERENCES public.users (id) ON DELETE CASCADE,
    session_id    uuid,                                         -- register, check: the session it was handed to
    action_sha256 text,                                         -- check: the request it confirms
    ceremony      jsonb NOT NULL,                               -- go-webauthn's SessionData
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    used_at       timestamptz,
    CONSTRAINT passkey_challenges_purpose_check CHECK (purpose IN ('register', 'sign_in', 'check')),
    CONSTRAINT passkey_challenges_person_check CHECK ((purpose = 'sign_in') = (person_id IS NULL)),
    CONSTRAINT passkey_challenges_binding_check CHECK (
        purpose = 'sign_in'
        OR (session_id IS NOT NULL AND (purpose <> 'check' OR action_sha256 ~ '^[0-9a-f]{64}$'))),
    CONSTRAINT passkey_challenges_challenge_check CHECK (challenge ~ '^[A-Za-z0-9_-]{22,128}$'),
    CONSTRAINT passkey_challenges_expiry_check CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes')
);

CREATE INDEX passkey_challenges_expires_idx ON v2.passkey_challenges (expires_at);

COMMENT ON TABLE v2.passkey_challenges IS
    'WebAuthn challenges: short-lived, used once, a check bound to its person, session and request. RLS on app.person_id; sign-in challenges (no person) are anyone''s.';

ALTER TABLE v2.passkey_challenges ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.passkey_challenges FORCE ROW LEVEL SECURITY;
CREATE POLICY passkey_challenges_person ON v2.passkey_challenges
    USING (person_id IS NULL OR person_id = (SELECT v2.current_person_id()))
    WITH CHECK (person_id IS NULL OR person_id = (SELECT v2.current_person_id()));

GRANT SELECT, INSERT ON v2.passkey_challenges TO memax_v2;
GRANT UPDATE (used_at) ON v2.passkey_challenges TO memax_v2;

-- Clears challenges that expired before p_before (at most 200 at a time),
-- the current person's and nobody's (sign-in); with p_all_mine, every one
-- of the current person's too (forgetting the account). Returns how many.
CREATE FUNCTION v2.clear_passkey_challenges(p_before timestamptz, p_all_mine boolean) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
DECLARE
    n integer;
BEGIN
    WITH gone AS (
        DELETE FROM v2.passkey_challenges
         WHERE challenge IN (
            SELECT challenge FROM v2.passkey_challenges
             WHERE (person_id IS NULL OR person_id = v2.current_person_id())
               AND (expires_at < p_before OR (p_all_mine AND person_id = v2.current_person_id()))
             LIMIT CASE WHEN p_all_mine THEN NULL ELSE 200 END)
        RETURNING 1)
    SELECT count(*) INTO n FROM gone;
    RETURN n;
END
$$;

REVOKE ALL ON FUNCTION v2.clear_passkey_challenges(timestamptz, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.clear_passkey_challenges(timestamptz, boolean) TO memax_v2;

-- ---------------------------------------------------------------------
-- Assurance: human_web_verified
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_assurance_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_assurance_check
    CHECK (assurance IS NULL OR assurance IN ('human_web', 'human_web_verified', 'client_attested')) NOT VALID;
ALTER TABLE v2.receipts VALIDATE CONSTRAINT receipts_assurance_check;

ALTER TABLE v2.decision_gates DROP CONSTRAINT decision_gates_assurance_check;
ALTER TABLE v2.decision_gates ADD CONSTRAINT decision_gates_assurance_check
    CHECK (assurance IS NULL OR assurance IN ('human_web', 'human_web_verified', 'client_attested')) NOT VALID;
ALTER TABLE v2.decision_gates VALIDATE CONSTRAINT decision_gates_assurance_check;
