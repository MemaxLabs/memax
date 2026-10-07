-- Revert 051: v2_conflict_question
--
-- Drops the judge's question, labels and suggested answer (044's guard
-- again). The words go with the columns.

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
    END IF;
    RETURN NEW;
END $$;

REVOKE UPDATE (question, labels) ON v2.judge_verdicts FROM memax_v2;

ALTER TABLE v2.judge_verdicts
    DROP CONSTRAINT judge_verdicts_suggested_check,
    DROP CONSTRAINT judge_verdicts_labels_check,
    DROP COLUMN suggested,
    DROP COLUMN labels,
    DROP COLUMN question;
