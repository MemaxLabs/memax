-- Revert 036: v2_decision_gates
--
-- Drops the gates. Run only where losing them is acceptable (local, test,
-- or before any gate was asked). The decisions answers became stay: they
-- are memories with receipts of their own. The receipts the gate commands
-- wrote (asked, answered, withdrawn) stay too, because receipts are
-- append-only, so the restored action CHECK (035's list, exactly) is NOT
-- VALID.

DROP TABLE IF EXISTS v2.decision_gates;
DROP FUNCTION IF EXISTS v2.require_gate_receipt();
DROP FUNCTION IF EXISTS v2.decision_gates_guard();
DROP FUNCTION IF EXISTS v2.gate_status_transition_allowed(text, text);

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded')) NOT VALID;
