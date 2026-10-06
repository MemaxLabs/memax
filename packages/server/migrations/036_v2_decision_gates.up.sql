-- 036: v2_decision_gates
--
-- Decision gates (G-; plan 25 §3, §5.4, §5.12, E8; Phase 1 epic 1.11). An
-- agent stops and asks a person to decide between two to four options,
-- and the person's answer becomes a kept decision authored by them, which
-- compiles into every target. Gates leave V1's board_slots for the V2
-- record here, so they survive the retirement of boards.
--
--   decision_gates  one per question: the asking connection and session,
--                   the question, its context and options, when it
--                   expires, and how it ended (answered, with the kept
--                   decision it became, or withdrawn)
--
-- The guarantees of 028 hold here:
--
-- 1. Receipt or refused. A deferred constraint trigger refuses the COMMIT
--    unless each inserted or updated row points at a receipt about the
--    gate (object_kind = 'gate', object_id = the gate) written in the same
--    transaction, in the same space. The one exempt column is
--    delivered_at: when the asking agent was told how its gate ended. That
--    is a read, not a change to the record (reads are not receipts, plan
--    §5.3), so an UPDATE that changes only delivered_at needs no receipt.
--
-- 2. The status machine is checked twice, in Go (internal/ledger) and by
--    a trigger here: a gate starts waiting and ends once, answered or
--    withdrawn; nothing else about it changes after it is asked. Expired
--    is not stored. A waiting gate whose expires_at has passed reads as
--    expired, and the ledger refuses to answer or withdraw it (with its
--    own clock, the one every command's time comes from).
--
-- 3. Isolation. RLS is ENABLEd and FORCEd, keyed on app.space_ids, and
--    memax_v2 gets only the grants the ledger needs: no DELETE, no
--    TRUNCATE, and the words and identity columns are not updatable.
--
-- # Words
--
-- question, context and options hold the asking agent's words, and so
-- does the decision an answer becomes (its statement, in
-- memory_versions). Receipts never do. Forgetting that decision must purge
-- the gate's words as well (forget-propagation epic): the columns are
-- nullable for it, and only a waiting gate must have them.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains asked (an agent asked a gate) and withdrawn
-- (the asking agent, or a person, took the question back). answered was in
-- 028's list. The list keeps every verb 028, 029, 031 and 035 admit.

-- ---------------------------------------------------------------------
-- Receipt actions
-- ---------------------------------------------------------------------

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn'));

-- ---------------------------------------------------------------------
-- Vocabulary (mirrored in Go, parity-tested)
-- ---------------------------------------------------------------------

-- from_status NULL means "asking the gate".
CREATE FUNCTION v2.gate_status_transition_allowed(from_status text, to_status text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT CASE
        WHEN from_status IS NULL THEN to_status = 'waiting'
        ELSE (from_status, to_status) IN (('waiting', 'answered'), ('waiting', 'withdrawn'))
    END
$$;

-- ---------------------------------------------------------------------
-- Decision gates
-- ---------------------------------------------------------------------

CREATE TABLE v2.decision_gates (
    id                 uuid PRIMARY KEY,                     -- uuidv7, from Go
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    seq                bigint NOT NULL,                      -- display number: G-<seq>, per tenant
    question           text,                                 -- the asking agent's words; purged at Forget
    context            text,
    options            jsonb,                                -- [{label, detail}], 2 to 4
    status             text NOT NULL DEFAULT 'waiting',      -- waiting | answered | withdrawn (expired is read from expires_at)
    expires_at         timestamptz NOT NULL,
    asked_by           uuid NOT NULL,                        -- the agent connection (the asked receipt's actor_id)
    agent              text,                                 -- its agent slug ("codex")
    requesting_session text,                                 -- the agent session that asked
    answer_option      integer,                              -- the chosen option, from 1
    answer_memory_id   uuid,                                 -- the kept decision the answer became
    answered_by        uuid,                                 -- the person who answered
    answered_at        timestamptz,
    assurance          text,                                 -- the answer's: human_web | client_attested
    withdrawn_by_kind  text,                                 -- agent (the one that asked) | person
    withdrawn_by       uuid,
    withdrawn_at       timestamptz,
    delivered_at       timestamptz,                          -- when the asking agent was told how it ended; no receipt
    stream_version     integer NOT NULL DEFAULT 1,           -- latest receipts.stream_version on this gate (If-Match)
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT decision_gates_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id),
    CONSTRAINT decision_gates_tenant_seq_key UNIQUE (tenant_id, seq),
    CONSTRAINT decision_gates_answer_fkey FOREIGN KEY (answer_memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT decision_gates_seq_check CHECK (seq >= 1),
    CONSTRAINT decision_gates_status_check CHECK (status IN ('waiting', 'answered', 'withdrawn')),
    CONSTRAINT decision_gates_question_check CHECK (question IS NULL OR (question <> '' AND char_length(question) <= 300)),
    CONSTRAINT decision_gates_context_check CHECK (context IS NULL OR char_length(context) <= 2000),
    CONSTRAINT decision_gates_options_check CHECK (options IS NULL
        OR (jsonb_typeof(options) = 'array' AND jsonb_array_length(options) BETWEEN 2 AND 4)),
    CONSTRAINT decision_gates_words_check CHECK (status <> 'waiting' OR (question IS NOT NULL AND options IS NOT NULL)),
    CONSTRAINT decision_gates_agent_check CHECK (agent IS NULL OR (agent <> '' AND char_length(agent) <= 64)),
    CONSTRAINT decision_gates_session_check CHECK (requesting_session IS NULL OR char_length(requesting_session) <= 255),
    CONSTRAINT decision_gates_answer_check CHECK ((status = 'answered') = (answer_option IS NOT NULL
        AND answer_memory_id IS NOT NULL AND answered_by IS NOT NULL AND answered_at IS NOT NULL AND assurance IS NOT NULL)),
    CONSTRAINT decision_gates_answer_option_check CHECK (answer_option IS NULL OR answer_option BETWEEN 1 AND 4),
    CONSTRAINT decision_gates_assurance_check CHECK (assurance IS NULL OR assurance IN ('human_web', 'client_attested')),
    CONSTRAINT decision_gates_withdrawn_check CHECK ((status = 'withdrawn') = (withdrawn_by_kind IS NOT NULL
        AND withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL)),
    CONSTRAINT decision_gates_withdrawn_kind_check CHECK (withdrawn_by_kind IS NULL OR withdrawn_by_kind IN ('agent', 'person')),
    CONSTRAINT decision_gates_stream_version_check CHECK (stream_version >= 1)
);

CREATE INDEX decision_gates_space_seq_idx ON v2.decision_gates (space_id, seq DESC);
-- What a connection hasn't been told yet (recall's delivery), and what it
-- has waiting (the cap on open questions per agent).
CREATE INDEX decision_gates_asked_by_idx ON v2.decision_gates (asked_by, space_id) WHERE delivered_at IS NULL;

COMMENT ON TABLE v2.decision_gates IS
    'G-: an agent''s question waiting for a person; the answer becomes a kept decision. Written only through internal/ledger.';

-- ---------------------------------------------------------------------
-- The status machine (a gate ends once)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.decision_gates_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.decision_gates;
    o v2.decision_gates;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NOT v2.gate_status_transition_allowed(NULL, NEW.status) THEN
            RAISE EXCEPTION 'gate %: a gate starts waiting, not %', NEW.id, NEW.status
                USING ERRCODE = 'MXL03';
        END IF;
        RETURN NEW;
    END IF;
    -- Telling the asking agent how it ended is not a change to the gate.
    n := NEW; o := OLD;
    n.delivered_at := NULL; o.delivered_at := NULL;
    IF n IS NOT DISTINCT FROM o THEN
        RETURN NEW;
    END IF;
    IF NOT v2.gate_status_transition_allowed(OLD.status, NEW.status) THEN
        RAISE EXCEPTION 'gate %: status % -> % is not allowed; a gate ends once', NEW.id, OLD.status, NEW.status
            USING ERRCODE = 'MXL03';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER decision_gates_guard
    BEFORE INSERT OR UPDATE ON v2.decision_gates
    FOR EACH ROW EXECUTE FUNCTION v2.decision_gates_guard();

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1)
-- ---------------------------------------------------------------------

-- Its own function, like 029's v2.require_agent_receipt, so the receipt
-- check of the other record tables (v2.require_receipt) is untouched.
CREATE FUNCTION v2.require_gate_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.decision_gates;
    o v2.decision_gates;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- delivered_at records a read (the asking agent was told), not a change.
        n := NEW; o := OLD;
        n.delivered_at := NULL; o.delivered_at := NULL;
        IF n IS NOT DISTINCT FROM o THEN
            RETURN NULL;
        END IF;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = NEW.last_receipt_id
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_kind = 'gate'
           AND r.object_id = NEW.id
    ) THEN
        RAISE EXCEPTION 'v2.decision_gates: % without a receipt written in the same transaction', TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER decision_gates_require_receipt
    AFTER INSERT OR UPDATE ON v2.decision_gates
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_gate_receipt();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.decision_gates ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.decision_gates FORCE ROW LEVEL SECURITY;
CREATE POLICY decision_gates_space ON v2.decision_gates
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs
-- ---------------------------------------------------------------------

-- The words (question, context, options) and the identity columns are not
-- updatable; Forget will grant UPDATE on the words when it learns to purge
-- them.
GRANT SELECT, INSERT ON v2.decision_gates TO memax_v2;
GRANT UPDATE (status, answer_option, answer_memory_id, answered_by, answered_at, assurance,
              withdrawn_by_kind, withdrawn_by, withdrawn_at, delivered_at,
              stream_version, last_receipt_id, updated_at)
    ON v2.decision_gates TO memax_v2;

GRANT EXECUTE ON FUNCTION v2.gate_status_transition_allowed(text, text) TO memax_v2;
