-- 037: v2_memory_embeddings
--
-- Embeddings of V2 memory versions (plan 25 §5.11), for the vector lane of
-- recall and search, the judge's vector candidates (§5.8) and Remember's
-- near-duplicate check.
--
-- # Shape: a side table keyed by memory, version and model
--
--   * By version: an embedding is derived from one statement version, so
--     an edit never leaves a stale vector behind (the lanes join
--     memories.current_version), and an Undo that restores an older
--     version finds that version's vector still there.
--   * By model: switching models (voyage-4 today, eval-gated) indexes the
--     new model beside the old one, and the query side flips once the new
--     model is complete. One column on v2.memories couldn't hold two.
--   * Not on v2.memories: memories.embedding (028) is left unused. Writing
--     it would rewrite the wide, hot memories row for derived data. It is
--     kept (and commented) only because the receipt trigger of 028–036
--     names it; drop it at cutover.
--
-- # Exact, scoped search, no HNSW
--
-- Searches are exact: ORDER BY embedding <=> $q LIMIT k over one space's
-- rows, found through the (space_id, model) btree (plan §5.11: 100%
-- recall, and fast at ≤ 100k rows per space). There is no global HNSW
-- index: pgvector caps an HNSW scan at ef_search rows and post-filters by
-- space, the V1 truncation the recall-pool fix chased. A per-space partial
-- HNSW comes only past about 50k rows in one space.
--
-- The column's storage is PLAIN. pgvector's default (EXTERNAL) moves a
-- 2 KB halfvec(1024) out of line into TOAST, and an exact scan then
-- fetches every vector from the TOAST table: 21 ms against 7 ms for 3,000
-- rows on pgvector 0.8 (measured while writing this). A row of about
-- 2.1 KB fits a page, three to a page.
--
-- # Forget
--
-- Forget purges the words in its own transaction (§5.13), and the vectors
-- go with them, in the same transaction, by trigger:
--   * a memory moving to `forgotten` loses every embedding;
--   * a statement version set to NULL loses that version's embeddings.
-- Inserting an embedding for a purged version or a forgotten memory is
-- refused (MXE01), so an index job racing a Forget can't put a vector
-- back. memax_v2 may SELECT and INSERT here, never DELETE: the purge runs
-- as v2.purge_memory_embeddings (SECURITY DEFINER), which deletes only
-- rows of spaces in the transaction's scope.
--
-- Embeddings are derived index data: like memories.search, writing them
-- needs no receipt.

CREATE TABLE v2.memory_embeddings (
    memory_id  uuid NOT NULL,
    version    integer NOT NULL,
    model      text NOT NULL,                      -- the index model, e.g. voyage-4
    space_id   uuid NOT NULL,
    embedding  halfvec(1024) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (memory_id, version, model),
    CONSTRAINT memory_embeddings_version_fkey FOREIGN KEY (memory_id, version) REFERENCES v2.memory_versions (memory_id, version),
    CONSTRAINT memory_embeddings_memory_fkey FOREIGN KEY (memory_id, space_id) REFERENCES v2.memories (id, space_id),
    CONSTRAINT memory_embeddings_model_check CHECK (model <> '' AND char_length(model) <= 64)
);

ALTER TABLE v2.memory_embeddings ALTER COLUMN embedding SET STORAGE PLAIN;

-- The scoped scan: one space's rows for one model.
CREATE INDEX memory_embeddings_space_model_idx ON v2.memory_embeddings (space_id, model);

COMMENT ON TABLE v2.memory_embeddings IS
    'Derived: one embedding per memory version and model. Searched exactly, per space. Purged with the words at Forget.';

COMMENT ON COLUMN v2.memories.embedding IS
    'Unused: embeddings live in v2.memory_embeddings (037), by version and model. Drop at cutover.';

-- An embedding needs words: refuse one for a purged version or a
-- forgotten memory (an index job that lost a race with Forget).
CREATE FUNCTION v2.memory_embeddings_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM v2.memory_versions v
          JOIN v2.memories m ON m.id = v.memory_id
         WHERE v.memory_id = NEW.memory_id AND v.version = NEW.version
           AND v.statement IS NOT NULL AND m.lifecycle <> 'forgotten'
    ) THEN
        RAISE EXCEPTION 'memory %: version % has no words to embed', NEW.memory_id, NEW.version
            USING ERRCODE = 'MXE01';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER memory_embeddings_guard
    BEFORE INSERT ON v2.memory_embeddings
    FOR EACH ROW EXECUTE FUNCTION v2.memory_embeddings_guard();

-- Forget's purge of the vectors, in the transaction that purges the words.
-- SECURITY DEFINER because memax_v2 has no DELETE; it deletes only rows of
-- spaces in the transaction's scope.
CREATE FUNCTION v2.purge_memory_embeddings() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    IF TG_TABLE_NAME = 'memories' THEN
        DELETE FROM v2.memory_embeddings
         WHERE memory_id = NEW.id
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    ELSE
        DELETE FROM v2.memory_embeddings
         WHERE memory_id = NEW.memory_id AND version = NEW.version
           AND space_id = ANY ((SELECT v2.current_space_ids())::uuid[]);
    END IF;
    RETURN NULL;
END $$;

REVOKE ALL ON FUNCTION v2.purge_memory_embeddings() FROM PUBLIC;

CREATE TRIGGER memories_purge_embeddings
    AFTER UPDATE OF lifecycle ON v2.memories
    FOR EACH ROW
    WHEN (NEW.lifecycle = 'forgotten' AND OLD.lifecycle IS DISTINCT FROM 'forgotten')
    EXECUTE FUNCTION v2.purge_memory_embeddings();

CREATE TRIGGER memory_versions_purge_embeddings
    AFTER UPDATE OF statement ON v2.memory_versions
    FOR EACH ROW
    WHEN (NEW.statement IS NULL AND OLD.statement IS NOT NULL)
    EXECUTE FUNCTION v2.purge_memory_embeddings();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.memory_embeddings ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.memory_embeddings FORCE ROW LEVEL SECURITY;
CREATE POLICY memory_embeddings_space ON v2.memory_embeddings
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. Rows are written once and never
-- updated (a new version or model is a new row); there is no DELETE.
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.memory_embeddings TO memax_v2;
