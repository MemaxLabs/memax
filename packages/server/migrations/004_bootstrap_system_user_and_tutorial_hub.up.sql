-- 004: bootstrap_system_user_and_tutorial_hub
--
-- Plan 23 §5.4 — system actor + tutorial-template hub.
--
-- Onboarding seed memories live as templates inside a system-owned
-- team hub. The signup worker copies each enabled template into the
-- new user's personal hub. Templates need a real owner row so all
-- existing FK invariants (hubs.owner_id → users.id, memories.owner_id
-- → users.id) hold without code-level shims.
--
-- Why deterministic UUIDs:
--   - Migration 004 inserts the actual seed memories with
--     owner_id = the system user UUID + hub_id = the tutorial hub
--     UUID. Both UUIDs MUST be stable across environments so the
--     ON CONFLICT DO NOTHING in 004's data inserts works.
--   - Application code references the system user / tutorial hub via
--     these constants (see model.SystemUserID, model.TutorialHubID
--     to be added with the worker job).
--
-- Why ON CONFLICT (slug) for the hub: hubs.slug is globally unique
-- under plan 19 for team hubs. If a previous half-applied migration
-- created the hub under a different id but the same slug, the simple
-- ON CONFLICT (id) DO NOTHING would still try to insert a duplicate
-- slug and fail. Targeting the slug catches that case.
--
-- Hub members row: existing team-hub invariants (every team hub has
-- at least one admin member) require the system user to be an admin
-- of the tutorial hub. ON CONFLICT (hub_id, user_id) DO NOTHING is
-- safe across re-runs.

INSERT INTO public.users (
  id,
  email,
  name,
  display_name,
  avatar_url,
  can_create_hub,
  personal_plan_id,
  personal_plan_scope,
  created_at,
  updated_at
) VALUES (
  '00000000-0000-0000-0000-00000000ada7'::uuid,
  '',
  'memax-system',
  'Memax',
  '',
  false,
  'personal_free',
  'personal',
  now(),
  now()
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.hubs (
  id,
  name,
  slug,
  hub_type,
  owner_id,
  icon,
  accent,
  settings,
  created_at,
  updated_at
) VALUES (
  '00000000-0000-0000-0000-00000000babe'::uuid,
  'Memax tutorial',
  '__memax_tutorial__',
  'team',
  '00000000-0000-0000-0000-00000000ada7'::uuid,
  '✦',
  'violet',
  '{}'::jsonb,
  now(),
  now()
)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO public.hub_members (hub_id, user_id, role, joined_at)
VALUES (
  '00000000-0000-0000-0000-00000000babe'::uuid,
  '00000000-0000-0000-0000-00000000ada7'::uuid,
  'admin',
  now()
)
ON CONFLICT (hub_id, user_id) DO NOTHING;
