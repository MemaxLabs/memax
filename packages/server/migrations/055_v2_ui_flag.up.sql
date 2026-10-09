-- 055: v2_ui_flag
--
-- The per-person V2 UI flag (plan 25 E1: the Ledger UI lives in packages/web
-- "behind a per-user ui=v2 flag"). internal/v2ui decides it, in one place:
-- the V2 UI is on for a person who is a member of a space on the V2 record
-- (hubs.v2_enabled_at, migration 033), whom an operator turned it on for, or
-- whose account was created at or after V2_UI_SINCE; an operator turning it
-- off wins over all of these, as an escape hatch.
--
-- Only the operator's choice is stored, because it is the only input that
-- isn't already in the database: membership and v2_enabled_at are read
-- live (so switching a space to V2 and back turns the flag on and off by
-- itself), and V2_UI_SINCE is configuration. One nullable column is the
-- smallest store that fits a three-way choice:
--
--   users.v2_ui  NULL   the rules decide (the default)
--                true   an operator turned the V2 UI on
--                false  an operator turned it off (wins over every rule)
--
-- Who changed it and when goes to admin_audit (resource_type 'user',
-- action 'v2_ui'), written by the same statement that changes it, so the
-- column needs no history of its own. It holds no words: Forget and
-- export have nothing to do here.

ALTER TABLE public.users ADD COLUMN v2_ui boolean;

COMMENT ON COLUMN public.users.v2_ui IS
    'An operator''s V2 UI choice: NULL lets internal/v2ui''s rules decide, true turns it on, false turns it off (wins).';
