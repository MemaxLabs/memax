-- 050: v2_conflict_question
--
-- The judge's question, answer labels and suggested answer for a conflict
-- (ReviewConflict, plan 25 §5.8). When stage 1 finds that a memory
-- contradicts a decision in force, the model also writes a short question
-- a person answers to settle it ("Fly.io or Railway for the v2 API?"), one
-- label per answer (the proposal's choice, the decision in force, both,
-- open) and the answer it suggests, if its sources settle it. They are
-- stored with the verdict and served by GET /v2/memories/{ref}/conflict.
--
--   judge_verdicts.question   one line; may quote memories, so purged at Forget
--   judge_verdicts.labels     {proposal, decision, both, open}: short lines;
--                             purged at Forget
--   judge_verdicts.suggested  proposal | decision | both | open: a code,
--                             never words
--
-- The question and labels are judge words on both sides of a pair, like
-- the rationale: Forget nulls them with it (internal/ledger/forget.go), and
-- a verdict that names a forgotten memory is written without them (the
-- forgotten guard, extended here).

ALTER TABLE v2.judge_verdicts
    ADD COLUMN question  text,
    ADD COLUMN labels    jsonb,
    ADD COLUMN suggested text,
    ADD CONSTRAINT judge_verdicts_labels_check CHECK (labels IS NULL OR jsonb_typeof(labels) = 'object'),
    ADD CONSTRAINT judge_verdicts_suggested_check CHECK (suggested IS NULL OR suggested IN ('proposal', 'decision', 'both', 'open'));

GRANT UPDATE (question, labels) ON v2.judge_verdicts TO memax_v2;

CREATE OR REPLACE FUNCTION v2.judge_verdicts_forgotten_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM v2.memories m
         WHERE m.lifecycle = 'forgotten'
           AND (m.id = NEW.memory_id OR m.id = NEW.related_memory_id
                OR m.id IN (SELECT (c ->> 'memory_id')::uuid FROM jsonb_array_elements(NEW.candidates) c
                             WHERE c ->> 'memory_id' ~ '^[0-9a-f-]{36}$'))
    ) THEN
        NEW.rationale := NULL;
        NEW.merged_statement := NULL;
        NEW.question := NULL;
        NEW.labels := NULL;
    END IF;
    RETURN NEW;
END $$;
