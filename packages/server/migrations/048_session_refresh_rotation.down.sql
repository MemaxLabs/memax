-- 048 down: the plain-text tokens can't be recovered from their hashes, so
-- every session ends and everyone signs in again.
DROP TABLE IF EXISTS public.session_retired_tokens;

DROP TRIGGER IF EXISTS sessions_hash_plaintext_refresh ON public.sessions;
DROP FUNCTION IF EXISTS public.sessions_hash_plaintext_refresh();

DELETE FROM public.sessions;

DROP INDEX IF EXISTS public.idx_sessions_user_live;
DROP INDEX IF EXISTS public.idx_sessions_expires;

ALTER TABLE public.sessions
    DROP CONSTRAINT IF EXISTS sessions_refresh_token_hash_key,
    DROP CONSTRAINT IF EXISTS sessions_refresh_token_plaintext,
    DROP CONSTRAINT IF EXISTS sessions_refresh_token_hash_check,
    DROP CONSTRAINT IF EXISTS sessions_kind_check,
    DROP CONSTRAINT IF EXISTS sessions_client_check,
    DROP CONSTRAINT IF EXISTS sessions_user_agent_check,
    DROP CONSTRAINT IF EXISTS sessions_revoked_check,
    DROP COLUMN refresh_token_hash,
    DROP COLUMN kind,
    DROP COLUMN client,
    DROP COLUMN user_agent,
    DROP COLUMN created_ip,
    DROP COLUMN created_city,
    DROP COLUMN last_ip,
    DROP COLUMN last_city,
    DROP COLUMN last_used_at,
    DROP COLUMN rotated_at,
    DROP COLUMN generation,
    DROP COLUMN revoked_at,
    DROP COLUMN revoked_reason,
    ALTER COLUMN refresh_token SET NOT NULL,
    ADD CONSTRAINT sessions_refresh_token_key UNIQUE (refresh_token);

CREATE INDEX idx_sessions_refresh ON public.sessions USING btree (refresh_token);
