-- Multi-tenant padding for the V1 retrieval eval (plan 25 §11, "plus a
-- multi-tenant run"; see RESULTS.md): 20 other tenants, each with a near-copy
-- of every eval memory and chunk (the same text, embeddings with small uniform
-- noise, cosine ≈ 0.97 to the original), in their own personal hub. The eval
-- owner can't see them; a global HNSW scan that is filtered by owner after the
-- fact has to skip past them.
--
-- Run TestRetrievalQuality once to migrate and seed the database, then
--   psql "$DATABASE_URL" -f eval/multitenant_pad.sql
-- and run it again. The eval's cleanup only touches its own users, so the
-- padding stays until you drop the database.
BEGIN;
CREATE TEMP TABLE pad_owner AS
  SELECT gen_random_uuid() AS user_id, gen_random_uuid() AS hub_id, n
    FROM generate_series(1, 20) AS n;
INSERT INTO users (id, email, name)
  SELECT user_id, 'pad' || n || '@pad.eval', 'Pad ' || n FROM pad_owner;
INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind)
  SELECT hub_id, 'Pad ' || n, 'pad-' || n || '-' || left(hub_id::text, 8), 'personal', user_id, 'personal' FROM pad_owner;
INSERT INTO hub_members (hub_id, user_id, role) SELECT hub_id, user_id, 'owner' FROM pad_owner;

CREATE TEMP TABLE pad_memory AS
  SELECT m.id AS old_id, gen_random_uuid() AS new_id, p.user_id, p.hub_id
    FROM memories m CROSS JOIN pad_owner p
   WHERE m.owner_id IN (SELECT id FROM users WHERE email LIKE '%.eval@memax.local');

INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash, summary, kind, stability,
                      retrieval_weight, tags, boundary, state, source, source_path, version, created_at, updated_at,
                      accessed_at, project_context, hint, source_agent, event_dates)
  SELECT pm.new_id, pm.hub_id, pm.user_id, m.title, m.content, m.content_type, m.content_hash, m.summary, m.kind,
         m.stability, m.retrieval_weight, m.tags, m.boundary, m.state, m.source, m.source_path, m.version, m.created_at,
         m.updated_at, m.accessed_at, m.project_context, m.hint, m.source_agent, m.event_dates
    FROM pad_memory pm JOIN memories m ON m.id = pm.old_id;

INSERT INTO chunks (id, memory_id, content, heading_chain, chunk_index, token_count, kind, stability, retrieval_weight,
                    created_at, embedding, search_text, project_repo, hint, language, search_config, search_vector,
                    tags_text, metadata_text)
  SELECT gen_random_uuid(), pm.new_id, c.content, c.heading_chain, c.chunk_index, c.token_count, c.kind, c.stability,
         c.retrieval_weight, c.created_at,
         (SELECT array_agg(x + (random() - 0.5) * 0.03 ORDER BY i)::vector
            FROM unnest(c.embedding::real[]) WITH ORDINALITY AS u(x, i)),
         c.search_text, c.project_repo, c.hint, c.language, c.search_config, c.search_vector, c.tags_text, c.metadata_text
    FROM pad_memory pm JOIN chunks c ON c.memory_id = pm.old_id
   WHERE c.embedding IS NOT NULL;
COMMIT;
ANALYZE memories;
ANALYZE chunks;
SELECT 'padded: ' || (SELECT count(*) FROM chunks) || ' chunks, ' || (SELECT count(DISTINCT owner_id) FROM memories) || ' owners';
