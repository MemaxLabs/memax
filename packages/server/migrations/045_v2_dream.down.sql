-- Revert 045: v2_dream. It fails once Dream has published an edition
-- (receipts with the verbs published or folded can't be re-checked
-- against 044's list), which is the point: receipts are never deleted.

DROP TABLE IF EXISTS v2.dream_email_sends;
DROP FUNCTION IF EXISTS v2.unsubscribe_dream_email(text);
DROP TABLE IF EXISTS v2.dream_settings;
DROP TABLE IF EXISTS v2.dream_schedules;
DROP TABLE IF EXISTS v2.note_refs;
DROP TABLE IF EXISTS v2.dream_actions;
DROP FUNCTION IF EXISTS v2.dream_actions_guard();
DROP TABLE IF EXISTS v2.dream_editions;
DROP FUNCTION IF EXISTS v2.require_dream_receipt();

DROP VIEW IF EXISTS v2.notes;
CREATE VIEW v2.notes WITH (security_barrier = true) AS
    SELECT m.id, m.owner_id, m.hub_id AS space_id, m.title,
           left(m.content, 280) AS excerpt, m.state, m.created_at
      FROM public.memories m
     WHERE m.hub_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
GRANT SELECT ON v2.notes TO memax_v2;

DROP POLICY IF EXISTS receipts_dream_sweep ON v2.receipts;
REVOKE ALL ON v2.receipts FROM memax_v2_dream_sweeper;
REVOKE ALL ON public.hubs, public.hub_members, public.users FROM memax_v2_dream_sweeper;
REVOKE USAGE ON SCHEMA v2 FROM memax_v2_dream_sweeper;
-- The role is cluster-wide (other databases' migrations may use it), so
-- it stays, with no privileges here.

DROP INDEX IF EXISTS v2.memories_stale_due_idx;
DROP INDEX IF EXISTS v2.receipts_space_recorded_idx;

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'returned', 'drafted',
     'purged', 'forget_requested', 'forget_declined'));
