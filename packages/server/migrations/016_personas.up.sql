-- 016: personas
--
-- Personas: first-class identity objects extracted from synced identity
-- configs (SOUL.md, IDENTITY.md, persona files). One row per source file;
-- upserted whenever the source config changes. Applying a persona writes
-- it back into a target agent's identity config via the normal config
-- sync machinery — personas never bypass agent_configs.
CREATE TABLE personas (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    owner_id uuid NOT NULL,
    source_agent text NOT NULL,
    source_scope text DEFAULT 'global' NOT NULL,
    source_file_path text NOT NULL,
    name text NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    UNIQUE (owner_id, source_agent, source_scope, source_file_path)
);

CREATE INDEX personas_owner_idx ON personas (owner_id);
