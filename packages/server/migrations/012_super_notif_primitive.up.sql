-- 012: super_notif_primitive
--
-- Plan 18 — super-notif primitive on the existing notifications
-- framework. Two structural additions:
--
--   1. ALTER TYPE notification_resolution ADD VALUE 'applied_auto'.
--      Distinguishes server-driven auto-completion (every required
--      checklist item completed → handler calls its own /resolve with
--      action=complete_all) from any future user-driven `applied`
--      action. The funnel slice needs this precision; merging the two
--      would conflate "users who earned the celebration" with "users
--      who explicitly applied an action" (plan 17 reserves `applied`
--      for review_topic_restructure resolve).
--
--   2. Two partial indices supporting the Recorder hot path and the
--      admin super-notif funnel queries. Both target rows that already
--      exist in the table — no schema reshaping, no JSONB GIN cost.
--
-- Forward-only: Postgres has no DROP VALUE for enum types. The down
-- migration is intentionally a no-op (see down.sql). Rolling back the
-- enum requires creating a new type without the value and swapping
-- every column that references it — out of scope for a routine
-- revert. Document this in the PR description.

ALTER TYPE notification_resolution ADD VALUE IF NOT EXISTS 'applied_auto' AFTER 'applied';

-- Recorder hot-path index: every memory-create, ask, configs.Sync,
-- hub-invite-create, and dream-completion site fires a Recorder method
-- that runs `SELECT id, payload FROM notifications WHERE kind=
-- 'checklist' AND source_kind='onboarding' AND recipient_user_id=$1
-- AND status='pending' ORDER BY created_at DESC LIMIT 1`. Partial
-- indexing keeps the index size bounded to rows that matter — users
-- past onboarding hit an empty index slice for sub-millisecond no-op
-- cost. Plan 18 §6.6.
CREATE INDEX IF NOT EXISTS notifications_pending_onboarding_by_recipient_idx
  ON notifications (recipient_user_id, created_at DESC)
  WHERE kind = 'checklist'
    AND source_kind = 'onboarding'
    AND status = 'pending';

-- Admin super-notif funnel index: scans by (kind, source_kind,
-- created_at) for the parent funnel (`status, resolution` aggregation)
-- and the item funnel (one JSONB expansion per cohort window). Partial
-- because only super-notif rows ever carry `items[]`; the rest of the
-- table is skipped. Plan 18 §7.3. GIN on `payload->'items'` is
-- deferred until we cross 500k super-notif rows or add per-item filter
-- queries.
CREATE INDEX IF NOT EXISTS notifications_super_cohort_idx
  ON notifications (kind, source_kind, created_at)
  WHERE kind IN ('checklist', 'digest');
