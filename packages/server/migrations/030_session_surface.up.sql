-- 030: session_surface
--
-- Which surface a sign-in was for: the web app or the CLI (plan 25
-- Phase 0 log, the human_web carry-over). Both get the same kind of
-- token, so /v2 couldn't tell a person on the web from an agent driving
-- the CLI with the person's login. The surface is decided when the login
-- is completed, from where its one-time code is delivered:
--
--   web  the code was redirected to the web app's origin (APP_BASE_URL),
--        so only a browser on memax.app receives it;
--   cli  anything else: a loopback redirect (`memax login`), or tokens
--        returned directly in the response.
--
-- It is stored on the code and on the session, and every access token
-- the session mints (login and refresh) carries it as a claim. A token's
-- surface is never what the client says. On its own it doesn't make a
-- request human_web: /v2 also requires the web app's server-side proxy to
-- sign the request (internal/websurface).
--
-- NULL is a session from before this migration; it counts as cli.

ALTER TABLE public.auth_codes
    ADD COLUMN surface text,
    ADD CONSTRAINT auth_codes_surface_check CHECK (surface IS NULL OR surface IN ('web', 'cli'));

ALTER TABLE public.sessions
    ADD COLUMN surface text,
    ADD CONSTRAINT sessions_surface_check CHECK (surface IS NULL OR surface IN ('web', 'cli'));
