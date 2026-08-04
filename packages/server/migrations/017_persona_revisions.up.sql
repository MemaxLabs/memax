-- 017: persona_revisions
--
-- Immutable version history for personas. One row per persona version,
-- written by the store whenever UpsertPersona lands a real content change.
-- Identity is the user's most irreplaceable agent asset — history makes
-- every apply/extract reversible.
CREATE TABLE persona_revisions (
    id uuid DEFAULT gen_random_uuid() NOT NULL PRIMARY KEY,
    persona_id uuid NOT NULL REFERENCES personas(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL,
    version integer NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    UNIQUE (persona_id, version)
);

CREATE INDEX persona_revisions_persona_idx ON persona_revisions (persona_id);
