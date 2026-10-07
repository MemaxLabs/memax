-- 051 down: passkeys and human_web_verified.
--
-- Receipts are append-only and sealed, so a human_web_verified one can't
-- be rewritten: the assurance checks come back NOT VALID, admitting the
-- rows already written and refusing new ones.

ALTER TABLE v2.decision_gates DROP CONSTRAINT decision_gates_assurance_check;
ALTER TABLE v2.decision_gates ADD CONSTRAINT decision_gates_assurance_check
    CHECK (assurance IS NULL OR assurance IN ('human_web', 'client_attested')) NOT VALID;

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_assurance_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_assurance_check
    CHECK (assurance IS NULL OR assurance IN ('human_web', 'client_attested')) NOT VALID;

DROP TABLE IF EXISTS v2.passkey_challenges;
DROP FUNCTION IF EXISTS v2.passkey_owner(bytea);
DROP TABLE IF EXISTS v2.passkeys;
