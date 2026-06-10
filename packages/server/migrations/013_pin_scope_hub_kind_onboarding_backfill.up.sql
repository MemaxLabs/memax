-- 013: pin_scope_hub_kind_onboarding_backfill
--
-- Plan 18 follow-up — existing onboarding notification rows (founder
-- welcome + first-week checklist) were emitted before the payload
-- carried `pin_scope_hub_kind`. Without the field, the client
-- renderer can't tell them apart from "global" pinned notifications
-- and shows them in team-hub `/memories` views too. Backfill sets
-- the field to "personal" for every row whose source_kind belongs to
-- the onboarding family, scoping the existing rows the same way new
-- emissions are scoped going forward (emitter.go now sets it).
--
-- Idempotent: jsonb_set with `create_if_missing := true` will set the
-- key whether or not it exists, and rows that already match are a
-- no-op. Re-running the migration is safe.

UPDATE notifications
SET payload = jsonb_set(
  payload,
  '{pin_scope_hub_kind}',
  '"personal"'::jsonb,
  true
)
WHERE source_kind IN ('onboarding', 'onboarding_welcome')
  AND COALESCE(payload->>'pin_scope_hub_kind', '') = '';
