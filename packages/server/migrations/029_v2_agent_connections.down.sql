-- Revert 029: v2_agent_connections
--
-- Drops agent connections. Run only where losing them is acceptable
-- (local, test, or before any agent has been connected). Receipts that
-- name connections stay: receipts are append-only.
-- agent_connections' policy reads agent_connection_spaces; drop it first.
DROP POLICY IF EXISTS agent_connections_scope ON v2.agent_connections;
DROP TABLE IF EXISTS v2.agent_connection_spaces;
DROP TABLE IF EXISTS v2.agent_connections;

DROP FUNCTION IF EXISTS v2.revoke_agent_credential(uuid);
DROP FUNCTION IF EXISTS v2.agent_credential_access(text, uuid, uuid);
DROP FUNCTION IF EXISTS v2.require_agent_receipt();
DROP FUNCTION IF EXISTS v2.agent_connections_state_guard();
DROP FUNCTION IF EXISTS v2.agent_state_transition_allowed(text, text);
DROP FUNCTION IF EXISTS v2.current_person_id();

DROP INDEX IF EXISTS v2.receipts_agent_actor_idx;

-- Back to 028's verbs. NOT VALID: connection receipts written while 029
-- was in place stay (receipts are append-only), but new ones are refused.
ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid')) NOT VALID;
