-- 041 down: v2_ask
--
-- Drops the Ask meter and takes 'memory' out of the source kinds. Sources
-- of kind 'memory' that kept answers wrote stay (the record is never
-- deleted), so the restored kind CHECK (028's list, exactly) is NOT VALID.

DROP TABLE IF EXISTS v2.ask_usage;

ALTER TABLE v2.sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE v2.sources ADD CONSTRAINT sources_kind_check CHECK (kind IN
    ('session', 'pr', 'file', 'url', 'issue', 'email', 'note', 'import')) NOT VALID;
