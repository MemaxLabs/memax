-- 032: mcp_oauth_audience
--
-- MCP v2 OAuth hardening (plan 25 §5.15, epic 1.7):
--
--   oauth_grants.resource  the MCP resource the client asked for (RFC 8707
--                          resource indicator), e.g. https://api.memax.app/mcp.
--                          Access tokens issued for the grant carry it as
--                          their audience (aud) and work only there. NULL on
--                          grants from before this migration, and when the
--                          client named no resource: their tokens are bound
--                          to the MCP endpoints in general.
--   oauth_grants.scope     the scope the person granted (memax:read,
--                          memax:propose, memax:write). It caps the agent
--                          connection's autonomy: memax:propose never keeps,
--                          memax:read never writes. NULL on older grants,
--                          whose cap follows their permissions.
--   oauth_clients.metadata_url  set for a client registered by a Client ID
--                          Metadata Document (its client_id is that URL),
--                          with when Memax last fetched it.

ALTER TABLE public.oauth_grants
    ADD COLUMN resource text,
    ADD COLUMN scope text,
    ADD CONSTRAINT oauth_grants_resource_check CHECK (resource IS NULL OR (resource <> '' AND char_length(resource) <= 2048)),
    ADD CONSTRAINT oauth_grants_scope_check CHECK (scope IS NULL OR char_length(scope) <= 200);

ALTER TABLE public.oauth_clients
    ADD COLUMN metadata_url text,
    ADD COLUMN metadata_fetched_at timestamptz,
    ADD CONSTRAINT oauth_clients_metadata_url_check CHECK (metadata_url IS NULL OR metadata_url = client_id);
