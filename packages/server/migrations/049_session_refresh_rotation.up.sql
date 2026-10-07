-- 049: session_refresh_rotation
--
-- Refresh tokens are hashed and rotated, and sessions can be listed and
-- revoked (plan 25 §5.15 and §5.16 "Tokens"; internal/sessions has the
-- design).
--
-- A session is one sign-in: the web app, the CLI (a loopback login or the
-- email code), a device code, or an MCP client's OAuth grant. It is a
-- refresh-token family: each refresh retires the token it was given and
-- issues the next one, and the session keeps only the SHA-256 of the
-- current one (refresh_token_hash). A retired token presented again after
-- the grace window revokes the whole session (reuse detection); within it,
-- the same retired token yields the session's current token again, so
-- two processes that refreshed at once (two tabs, the daemon and an MCP
-- server sharing ~/.memax/credentials.json) both end up with it.
--
-- Existing tokens are hashed in place, so nobody signs in again: a client
-- presents its token, the server hashes it and finds the session. The
-- plain-text column stays, always NULL, for one release: a server older
-- than this one that is still running during the rollout writes plain
-- text into it, and the trigger below hashes that and clears it before the
-- row is stored (an older server's refresh looks a token up by its plain
-- text, finds nothing, and the client signs in again; only refreshes in
-- the minute of a rolling deploy can hit that). A later migration drops
-- the column and the trigger. The old values survive only in dead tuples
-- until autovacuum, and in WAL and backups for their retention.
--
--   kind            web | cli | device | mcp: what signed in. The surface
--                   claim (migration 030) is still what assurance reads; a
--                   device-code session is kind device and surface cli.
--   client          what the session's client is, in words ("memax-cli
--                   0.9.0 on zz-mbp", "Chrome on macOS", "Claude").
--   user_agent, created_ip, created_city, last_ip, last_city
--                   where it signed in and was last refreshed from, when
--                   known (for the web app, as its server signed them).
--   last_used_at    the last refresh, or the last request (written at most
--                   every few minutes).
--   generation      how many times the session was refreshed.
--   revoked_at, revoked_reason
--                   when and why it ended before it expired: signed_out,
--                   revoked (by the person), revoked_others (by the person,
--                   from another session), reuse_detected, grant_revoked.

ALTER TABLE public.sessions
    ADD COLUMN refresh_token_hash text,
    ADD COLUMN kind text,
    ADD COLUMN client text NOT NULL DEFAULT '',
    ADD COLUMN user_agent text NOT NULL DEFAULT '',
    ADD COLUMN created_ip text NOT NULL DEFAULT '',
    ADD COLUMN created_city text NOT NULL DEFAULT '',
    ADD COLUMN last_ip text NOT NULL DEFAULT '',
    ADD COLUMN last_city text NOT NULL DEFAULT '',
    ADD COLUMN last_used_at timestamptz,
    ADD COLUMN rotated_at timestamptz,
    ADD COLUMN generation integer NOT NULL DEFAULT 0,
    ADD COLUMN revoked_at timestamptz,
    ADD COLUMN revoked_reason text,
    ALTER COLUMN refresh_token DROP NOT NULL;

UPDATE public.sessions SET
    refresh_token_hash = encode(sha256(convert_to(refresh_token, 'UTF8')), 'hex'),
    kind = CASE
        WHEN grant_id IS NOT NULL OR agent_name <> '' THEN 'mcp'
        WHEN surface = 'web' THEN 'web'
        ELSE 'cli'
    END,
    last_used_at = created_at,
    rotated_at = created_at,
    refresh_token = NULL;

DROP INDEX IF EXISTS public.idx_sessions_refresh;
ALTER TABLE public.sessions DROP CONSTRAINT sessions_refresh_token_key;

ALTER TABLE public.sessions
    ALTER COLUMN refresh_token_hash SET NOT NULL,
    ALTER COLUMN kind SET NOT NULL,
    ALTER COLUMN last_used_at SET NOT NULL,
    ALTER COLUMN last_used_at SET DEFAULT now(),
    ALTER COLUMN rotated_at SET NOT NULL,
    ALTER COLUMN rotated_at SET DEFAULT now(),
    ADD CONSTRAINT sessions_refresh_token_hash_key UNIQUE (refresh_token_hash),
    -- No plain-text token is ever stored (the trigger clears what an older
    -- server writes before this check runs).
    ADD CONSTRAINT sessions_refresh_token_plaintext CHECK (refresh_token IS NULL),
    ADD CONSTRAINT sessions_refresh_token_hash_check CHECK (refresh_token_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT sessions_kind_check CHECK (kind IN ('web', 'cli', 'device', 'mcp')),
    ADD CONSTRAINT sessions_client_check CHECK (char_length(client) <= 200),
    ADD CONSTRAINT sessions_user_agent_check CHECK (char_length(user_agent) <= 512),
    ADD CONSTRAINT sessions_revoked_check CHECK (
        (revoked_at IS NULL) = (revoked_reason IS NULL)
        AND (revoked_reason IS NULL OR revoked_reason IN
            ('signed_out', 'revoked', 'revoked_others', 'reuse_detected', 'grant_revoked'))
    );

-- A person's live sessions, newest use first.
CREATE INDEX idx_sessions_user_live ON public.sessions (user_id, last_used_at DESC)
    WHERE revoked_at IS NULL;
-- The reaper finds sessions that ended a while ago.
CREATE INDEX idx_sessions_expires ON public.sessions (expires_at);

-- Writes from a server older than this migration, during a rolling deploy:
-- hash the plain text it sends and drop it, and say what kind of session
-- it is. Dropped with the plain-text column.
CREATE FUNCTION public.sessions_hash_plaintext_refresh() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.refresh_token IS NOT NULL THEN
        NEW.refresh_token_hash := encode(sha256(convert_to(NEW.refresh_token, 'UTF8')), 'hex');
        NEW.refresh_token := NULL;
        NEW.rotated_at := now();
    END IF;
    IF NEW.kind IS NULL THEN
        NEW.kind := CASE
            WHEN NEW.grant_id IS NOT NULL OR NEW.agent_name <> '' THEN 'mcp'
            WHEN NEW.surface = 'web' THEN 'web'
            ELSE 'cli'
        END;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER sessions_hash_plaintext_refresh
    BEFORE INSERT OR UPDATE ON public.sessions
    FOR EACH ROW EXECUTE FUNCTION public.sessions_hash_plaintext_refresh();

-- Every refresh token a session retired, by hash: presenting one again is
-- either a concurrent refresh (within the grace window, answered with the
-- session's current token) or reuse (after it, which revokes the session).
--
--   successor_sealed  the token that replaced this one, AES-256-GCM sealed
--                     under a key derived from this token, which is never
--                     stored: only a client holding the retired token can
--                     open it. The server reads it only within the grace
--                     window, and wipes it after.
--
-- Rows are pruned a few weeks after they retire (reuse of an older token
-- is then just an unknown token) and go with their session.
CREATE TABLE public.session_retired_tokens (
    token_hash       text        PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    session_id       uuid        NOT NULL REFERENCES public.sessions(id) ON DELETE CASCADE,
    generation       integer     NOT NULL,
    retired_at       timestamptz NOT NULL DEFAULT now(),
    successor_sealed bytea
);

CREATE INDEX idx_session_retired_tokens_session ON public.session_retired_tokens (session_id);
CREATE INDEX idx_session_retired_tokens_retired ON public.session_retired_tokens (retired_at);
CREATE INDEX idx_session_retired_tokens_sealed ON public.session_retired_tokens (retired_at)
    WHERE successor_sealed IS NOT NULL;
