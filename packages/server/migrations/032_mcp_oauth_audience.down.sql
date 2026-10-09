ALTER TABLE public.oauth_clients
    DROP CONSTRAINT IF EXISTS oauth_clients_metadata_url_check,
    DROP COLUMN IF EXISTS metadata_fetched_at,
    DROP COLUMN IF EXISTS metadata_url;

ALTER TABLE public.oauth_grants
    DROP CONSTRAINT IF EXISTS oauth_grants_scope_check,
    DROP CONSTRAINT IF EXISTS oauth_grants_resource_check,
    DROP COLUMN IF EXISTS scope,
    DROP COLUMN IF EXISTS resource;
