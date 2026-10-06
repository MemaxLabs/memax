-- A view's columns can't be dropped with CREATE OR REPLACE.
DROP VIEW v2.spaces;

CREATE VIEW v2.spaces WITH (security_barrier = true) AS
    SELECT h.id, h.tenant_id, h.space_kind AS kind, h.name, h.slug, h.owner_id,
           h.repository, h.rules, h.created_at
      FROM public.hubs h
     WHERE h.id = ANY ((SELECT v2.current_space_ids())::uuid[]);

GRANT SELECT ON v2.spaces TO memax_v2;

ALTER TABLE public.hubs DROP COLUMN v2_enabled_at;
