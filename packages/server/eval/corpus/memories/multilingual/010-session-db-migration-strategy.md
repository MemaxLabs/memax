## Claude Session: Database Migration Strategy

Session captured: April 5, 2026
Agent: Claude (claude.ai, Sonnet 4)
User: Jiahao
Duration: ~1.5 hours
Branch: kind-stability-migration

### Context

We currently have a `category` column on the `memories` table with values like `architecture`, `debugging`, `meeting-notes`, `personal`, `project-config`. This was our original taxonomy but it's proving too rigid and doesn't capture how memories actually behave. The design doc (03-memory-model.md) proposes replacing this with a two-dimensional system: `kind` (what the memory IS) and `stability` (how it changes over time).

### Discussion Summary

**Current category values and their problems:**

| category | Problem |
|----------|---------|
| architecture | Mixes rationale ("why we chose X") with reference ("how X works") |
| debugging | Could be a volatile incident log or a stable "how to fix X" pattern |
| meeting-notes | Always episodic, but some meetings produce stable decisions |
| personal | Not really a category -- it's an access level |
| project-config | Actually procedural knowledge, not a "config" |

**Proposed kind/stability mapping:**

| kind | Description | Example |
|------|-------------|---------|
| semantic | Factual knowledge, concepts | "Memax uses pgvector for vector search" |
| episodic | Time-bound events, experiences | "April 7 standup notes" |
| procedural | How-to, processes, runbooks | "How to deploy to staging" |
| rationale | Decision records, trade-offs | "Why we chose PostgreSQL over MongoDB" |

| stability | Description | Decay behavior |
|-----------|-------------|----------------|
| stable | Unlikely to change | No decay |
| evolving | Updates periodically | Slow decay |
| volatile | Time-sensitive, may become stale | Fast decay |

**Migration approach Claude suggested:**

Phase 1 (non-breaking): Add `kind` and `stability` columns with defaults
```sql
ALTER TABLE memories ADD COLUMN kind TEXT NOT NULL DEFAULT 'semantic';
ALTER TABLE memories ADD COLUMN stability TEXT NOT NULL DEFAULT 'evolving';
```

Phase 2 (backfill): Use an LLM (Haiku) to classify existing memories
```
For each memory:
  1. Read title + first 500 chars of content
  2. Ask Haiku: "Classify this memory's kind (semantic/episodic/procedural/rationale)
     and stability (stable/evolving/volatile)"
  3. Update the row
```

Estimated cost for backfill: ~$0.15 per 1000 memories (Haiku input + output tokens). For our current 12k memories, that's about $1.80.

Phase 3 (cutover): Remove `category` column, update all queries
```sql
-- After verifying backfill quality
ALTER TABLE memories DROP COLUMN category;
```

**Trade-offs discussed:**

1. **Why not keep both?** Category and kind/stability serve different purposes. Keeping both adds confusion about which to use for retrieval. Better to do a clean migration.

2. **Why use LLM for backfill instead of a mapping table?** A simple mapping (architecture -> semantic/evolving) loses nuance. An architecture memory about "why we chose X" should be rationale/stable, not semantic/evolving. LLM classification handles this.

3. **Risk of wrong classification:** We estimated ~10% error rate for Haiku classification. Mitigation: let users correct the classification via the web UI, and log corrections to improve future classification prompts.

4. **Impact on retrieval scoring:** The `stability` field feeds into the recency decay function. Volatile memories decay faster (half-life: 7 days), stable memories don't decay at all. This is a significant change to ranking behavior -- need to re-run eval suite after migration.

### Action Items

- [ ] Write the migration SQL (Phase 1) -- Jiahao
- [ ] Build the Haiku classification job as a River worker task -- Jiahao
- [ ] Update the retrieval scoring to use stability for decay -- Ziyang
- [ ] Add kind/stability to the push and update API endpoints -- Ziyang
- [ ] Update the web UI memory detail view to show and edit kind/stability -- Sarah
- [ ] Run the eval suite before and after migration, compare nDCG -- Ziyang
- [ ] Update the SDK and CLI to support kind/stability in push commands -- Ziyang

### Decision

Proceeding with the three-phase migration. Phase 1 (add columns) ships this week. Phase 2 (backfill) runs over the weekend. Phase 3 (remove category) happens after eval confirms no regression.

Timeline: ~2 weeks total.
