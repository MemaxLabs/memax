-- 033: v2_space_switch
--
-- Each space moves to the V2 record on its own (plan 25 §10: a "Switch to
-- V2" step per space). hubs.v2_enabled_at is when it did; NULL means the
-- space is still on V1, and every surface keeps V1's behaviour there.
-- internal/spacemode reads it; until the Switch to V2 step ships (2.8),
-- only tests and dev seeding (cmd/v2-switch-space) set it.
--
-- v2.spaces gains the column, so /v2 clients (the CLI's local MCP server)
-- can tell which spaces are on V2.

ALTER TABLE public.hubs ADD COLUMN v2_enabled_at timestamptz;

CREATE OR REPLACE VIEW v2.spaces WITH (security_barrier = true) AS
    SELECT h.id, h.tenant_id, h.space_kind AS kind, h.name, h.slug, h.owner_id,
           h.repository, h.rules, h.created_at, h.v2_enabled_at
      FROM public.hubs h
     WHERE h.id = ANY ((SELECT v2.current_space_ids())::uuid[]);
