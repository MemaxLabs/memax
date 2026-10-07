-- Revert 027: v2_spaces
DROP TRIGGER IF EXISTS hubs_space_columns ON public.hubs;
DROP FUNCTION IF EXISTS v2.hubs_space_columns();

ALTER TABLE public.hubs
    DROP CONSTRAINT IF EXISTS hubs_id_tenant_id_key,
    DROP CONSTRAINT IF EXISTS hubs_repository_check,
    DROP CONSTRAINT IF EXISTS hubs_rules_object_check,
    DROP CONSTRAINT IF EXISTS hubs_team_tenant_check,
    DROP CONSTRAINT IF EXISTS hubs_space_kind_personal_check,
    DROP CONSTRAINT IF EXISTS hubs_space_kind_check,
    DROP COLUMN IF EXISTS tenant_id,
    DROP COLUMN IF EXISTS rules,
    DROP COLUMN IF EXISTS repository,
    DROP COLUMN IF EXISTS space_kind;
