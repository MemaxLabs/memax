-- Revert 030: session_surface
ALTER TABLE public.sessions DROP COLUMN IF EXISTS surface;
ALTER TABLE public.auth_codes DROP COLUMN IF EXISTS surface;
