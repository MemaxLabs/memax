-- 034: v2_recall_indexes
--
-- Indexes for MCP recall and search on the V2 record (plan 25 §5.11,
-- internal/v2recall), so the lexical lanes stay inside recall's 250 ms
-- budget as a space grows:
--
--   memories_search_idx         full-text lane: search @@ to_tsquery(...)
--   memory_versions_trgm_idx    trigram lane: query <% statement (pg_trgm
--                               word similarity, for typos and partial
--                               words), on the same unaccented lower-case
--                               form the search vector uses.
--
-- Both cover forgotten memories trivially: Forget nulls search and the
-- statement. The vector lane (exact, space-filtered KNN on embedding) will
-- need no index until a space passes about 50k memories (§5.11).

CREATE INDEX memories_search_idx ON v2.memories USING gin (search);

CREATE INDEX memory_versions_trgm_idx ON v2.memory_versions
    USING gin (public.immutable_unaccent(lower(statement)) public.gin_trgm_ops);
