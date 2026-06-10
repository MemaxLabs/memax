Plan for migrating from the current embedding model to a newer version.

## Current State

We use Voyage AI's `voyage-code-3` model (1024 dimensions) for all memory embeddings. The embeddings are stored in the `chunks` table as `vector(1024)` columns indexed with `ivfflat` (lists=100).

## Why Migrate

Voyage AI is releasing `voyage-3-large` which benchmarks significantly better on MTEB for code and mixed-domain text. Their internal benchmarks show:
- +4.2% on code retrieval (HumanEval-X)
- +2.8% on mixed-domain Q&A (NaturalQuestions)
- Same 1024 dimensions, so storage is unchanged
- Latency is ~15% higher per batch (acceptable given our async embedding pipeline)

## Migration Strategy

### Phase 1: Dual-Write (1 week)

New memories get embedded with both `voyage-code-3` and `voyage-3-large`. Both embeddings are stored. This doubles embedding costs temporarily (~$0.003/memory instead of ~$0.0015).

### Phase 2: A/B Testing (2 weeks)

Route 10% of retrieval queries to the new embeddings. Compare nDCG@5 and MRR against our eval corpus. If the new model is worse on any slice by more than 2%, investigate before proceeding.

### Phase 3: Backfill (1-2 weeks)

Re-embed all existing chunks with the new model. This is a background job processed by the worker. At current corpus sizes (~50K chunks), this would cost roughly $75 in API calls and take ~8 hours with rate limiting.

### Phase 4: Cutover

Switch all retrieval queries to use the new embeddings. Keep old embeddings for 30 days as rollback insurance. After 30 days, drop the old embedding column.

## Risks

- **Semantic drift:** The new model might rank results differently for edge-case queries. Our eval corpus needs to cover these before Phase 4.
- **Cost:** Dual-write doubles embedding costs. At our current scale this is ~$50/month extra during the transition.
- **Index rebuild:** If we change dimensions (we're not, but future models might), we'd need to rebuild the ivfflat index, which locks the table.

## Timeline

Targeting Phase 1 start in late April, full cutover by mid-May. Jiahao owns the implementation; Ziyang reviews the eval results at each phase gate.
