-- 027: v2_spaces
--
-- A V2 Space keeps V1's physical `hubs` table (plan 25 §5.4: a rename
-- would be pure churn). This adds the Space columns, all non-breaking
-- for V1, which never reads them:
--
--   space_kind  personal | project | team. Backfilled from hub_type
--               ('personal' → personal, 'team' → team). V1 only ever
--               writes those two hub_types, and a trigger fills
--               space_kind for rows V1 inserts. A project space is
--               hub_type 'team' to V1 (a shareable hub), so V1's
--               one-personal-hub-per-owner lookups stay correct; the
--               CHECK below pins "personal" to mean the same thing in
--               both columns.
--   repository  optional "owner/name" (or URL) the space compiles for.
--   rules       space rules (who can keep, who can forget, the
--               autonomy new agents start at, whether decisions need
--               a person on the web). '{}' means the defaults, which
--               live in Go (internal/ledger/policy.Rules).
--   tenant_id   who owns the space's record, for display-ID counters
--               and receipts (see below).
--
-- # Tenancy
--
-- tenant_id is the owning user for personal and project spaces, and
-- the space itself for team spaces. It is fixed when the space is
-- created (a trigger computes it; callers can't choose it) and never
-- changes afterwards: an ownership transfer of a project space keeps
-- its tenant, so its M- numbers never collide or get renumbered.
-- Every v2 row carries (space_id, tenant_id) with a composite foreign
-- key to (hubs.id, hubs.tenant_id), so a row can't claim a tenant its
-- space doesn't have.

ALTER TABLE public.hubs
    ADD COLUMN space_kind text,
    ADD COLUMN repository text,
    ADD COLUMN rules jsonb DEFAULT '{}'::jsonb NOT NULL,
    ADD COLUMN tenant_id uuid;

-- No ELSE: an unknown hub_type leaves NULL and the SET NOT NULL below
-- fails loudly instead of guessing.
UPDATE public.hubs
   SET space_kind = CASE hub_type WHEN 'personal' THEN 'personal' WHEN 'team' THEN 'team' END,
       tenant_id = CASE hub_type WHEN 'team' THEN id ELSE owner_id END;

ALTER TABLE public.hubs
    ALTER COLUMN space_kind SET NOT NULL,
    ALTER COLUMN tenant_id SET NOT NULL,
    ADD CONSTRAINT hubs_space_kind_check CHECK (space_kind IN ('personal', 'project', 'team')),
    ADD CONSTRAINT hubs_space_kind_personal_check CHECK ((space_kind = 'personal') = (hub_type = 'personal')),
    ADD CONSTRAINT hubs_team_tenant_check CHECK (space_kind <> 'team' OR tenant_id = id),
    ADD CONSTRAINT hubs_rules_object_check CHECK (jsonb_typeof(rules) = 'object'),
    ADD CONSTRAINT hubs_repository_check CHECK (repository IS NULL OR (repository <> '' AND char_length(repository) <= 500)),
    ADD CONSTRAINT hubs_id_tenant_id_key UNIQUE (id, tenant_id);

CREATE FUNCTION v2.hubs_space_columns() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.space_kind := COALESCE(NEW.space_kind,
            CASE NEW.hub_type WHEN 'personal' THEN 'personal' WHEN 'team' THEN 'team' END);
        NEW.tenant_id := CASE WHEN NEW.space_kind = 'team' THEN NEW.id ELSE NEW.owner_id END;
        RETURN NEW;
    END IF;

    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'a space''s tenant is fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.space_kind IS DISTINCT FROM OLD.space_kind OR NEW.id IS DISTINCT FROM OLD.id THEN
        RAISE EXCEPTION 'a space''s kind and id are fixed when it is created (space %)', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;

COMMENT ON FUNCTION v2.hubs_space_columns() IS
    'Fills hubs.space_kind and hubs.tenant_id on insert (tenant = owner for personal/project, the space for team) and keeps id, kind and tenant immutable.';

CREATE TRIGGER hubs_space_columns
    BEFORE INSERT OR UPDATE OF id, space_kind, tenant_id ON public.hubs
    FOR EACH ROW EXECUTE FUNCTION v2.hubs_space_columns();
