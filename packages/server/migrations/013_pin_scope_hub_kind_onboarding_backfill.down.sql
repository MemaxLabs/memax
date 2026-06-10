-- Revert 013: pin_scope_hub_kind_onboarding_backfill
--
-- Strips pin_scope_hub_kind from onboarding rows so the payload
-- returns to its pre-013 shape. Idempotent — rows without the key
-- are a no-op. A real rollback also reverts the emitter change in
-- the application code; this DDL is safe to run standalone.

UPDATE notifications
SET payload = payload - 'pin_scope_hub_kind'
WHERE source_kind IN ('onboarding', 'onboarding_welcome')
  AND payload ? 'pin_scope_hub_kind';
