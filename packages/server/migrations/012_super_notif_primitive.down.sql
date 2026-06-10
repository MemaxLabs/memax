-- Revert 012: super_notif_primitive
--
-- Postgres has no DROP VALUE for enum types, so the enum extension in
-- 012.up.sql is forward-only. Rolling back `applied_auto` would
-- require creating a new type without the value, rewriting the column,
-- dropping the old type, and renaming. That's a write-locking,
-- table-rewrite operation — out of scope for a routine revert. Plan
-- 17 paid the same cost when it added `audience` and `resolution`.
-- Roll forward fixes; don't roll back the enum.
--
-- Dropping the partial indices IS reversible without data movement,
-- so we do those.

DROP INDEX IF EXISTS notifications_super_cohort_idx;
DROP INDEX IF EXISTS notifications_pending_onboarding_by_recipient_idx;
