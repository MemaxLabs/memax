-- 041: v2_ask
--
-- ⌘K Ask on the V2 record (plan 25 §5.11, Phase 1 epic 1.10). An Ask is a
-- read: it writes no record row and no receipt. Two things change here.
--
-- 1. A memory can cite memories. Keeping an answer (⌘↵) remembers it as
--    the person's own memory with the memories it cited as its sources
--    (kind 'memory', ref "M-0219", locator {"memory": <id>}). The ledger
--    resolves each cited memory in the same space and takes its trust, so
--    the kept answer's trust is the minimum of the memories it rests on:
--    an answer can never be trusted more than what it was built from.
--
-- 2. Ask is metered. Plan 25 D9 gives Free 50 asks a month; policy.Decide
--    enforces the plan's limit, and this table is the monthly count it
--    reads. It is bookkeeping about a person, not part of the record (no
--    words, no receipts), like an agent connection's last_seen_at:
--
--      ask_usage  one row per person per month (period is the month's
--                 first day, UTC): asks answered by the model. Asks that
--                 never reach the model (nothing kept matched, answers
--                 are off, refused) are released and don't count.
--
--    Isolation: RLS is ENABLEd and FORCEd, keyed on app.person_id, so a
--    transaction only ever sees and counts its own person's asks. memax_v2
--    may insert and update the count, never delete. The row goes with the
--    person (ON DELETE CASCADE), since nothing else refers to it.

-- ---------------------------------------------------------------------
-- Sources: kind 'memory'
-- ---------------------------------------------------------------------

ALTER TABLE v2.sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE v2.sources ADD CONSTRAINT sources_kind_check CHECK (kind IN
    ('session', 'pr', 'file', 'url', 'issue', 'email', 'note', 'import', 'memory'));

-- ---------------------------------------------------------------------
-- The Ask meter
-- ---------------------------------------------------------------------

CREATE TABLE v2.ask_usage (
    person_id  uuid NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    period     date NOT NULL,
    asks       integer NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (person_id, period),
    CONSTRAINT ask_usage_asks_check CHECK (asks >= 0),
    CONSTRAINT ask_usage_period_check CHECK (period = date_trunc('month', period)::date)
);

COMMENT ON TABLE v2.ask_usage IS
    'Asks answered by the model, per person per month (UTC). The Ask meter policy.Decide reads for the plan limit (D9). Not the record: no words, no receipts.';

ALTER TABLE v2.ask_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.ask_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY ask_usage_person ON v2.ask_usage
    USING (person_id = (SELECT v2.current_person_id()))
    WITH CHECK (person_id = (SELECT v2.current_person_id()));

GRANT SELECT, INSERT ON v2.ask_usage TO memax_v2;
GRANT UPDATE (asks, updated_at) ON v2.ask_usage TO memax_v2;
