## Code Review: Chunk Metadata Propagation Through Queue Jobs

PR #247 review notes from the `ingest-metadata-tags` branch. The PR adds `tags_text` and `metadata_text` fields to chunk records so that retrieval can boost results based on tag matches and structured metadata.

### Problem Statement

When a user pushes a memory with tags like `["postgresql", "migration", "debugging"]`, those tags are stored on the `memories` table but never propagated to the `chunks` table. Since retrieval operates on chunks (not memories), tag-based boosting was impossible — we had to join back to `memories` for every chunk result, which added ~12ms per recall query.

### What the PR Does

1. **Adds `tags_text` and `metadata_text` columns to `chunks`** (migration 024):

```sql
ALTER TABLE chunks
    ADD COLUMN tags_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN metadata_text TEXT NOT NULL DEFAULT '';

-- Backfill existing chunks from their parent memory
UPDATE chunks c
SET
    tags_text = COALESCE(m.tags_text, ''),
    metadata_text = COALESCE(m.metadata_text, '')
FROM memories m
WHERE c.memory_id = m.id;
```

2. **Propagates tags through the River queue job args**:

```go
// internal/worker/memory_process.go
type MemoryProcessArgs struct {
    MemoryID       string                `json:"memory_id"`
    Title          string                `json:"title"`
    Body           string                `json:"body"`
    TagsText       string                `json:"tags_text"`       // NEW
    MetadataText   string                `json:"metadata_text"`   // NEW
    ProjectContext *model.ProjectContext  `json:"project_context"`
}
```

3. **Includes tags in the embedding input** so semantic search considers them:

```go
func (w *MemoryProcessWorker) buildEmbedInput(job *river.Job[MemoryProcessArgs]) string {
    parts := []string{job.Args.Title, job.Args.Body}
    if job.Args.TagsText != "" {
        parts = append(parts, "tags: "+job.Args.TagsText)
    }
    if job.Args.MetadataText != "" {
        parts = append(parts, "metadata: "+job.Args.MetadataText)
    }
    return strings.Join(parts, "\n\n")
}
```

### Review Comments

**Comment 1 (Ziyang):** The `MemoryProcessArgs` struct now duplicates data that's already in the `memories` table. Should we just pass `MemoryID` and read from the DB in the worker? **Resolution:** Keep the denormalized approach. The worker runs asynchronously — by the time it processes the job, the memory might have been updated or deleted. Snapshot-in-args ensures consistency.

**Comment 2 (Ziyang):** The backfill migration (024) will lock the `chunks` table during `UPDATE`. For large tables this could cause downtime. **Resolution:** Changed to batched update with `LIMIT 1000` in a loop, wrapped in advisory lock.

**Comment 3 (Ziyang):** Make sure `tags_text` format matches what the retrieval ranker expects. Currently tags are stored as `["go", "postgresql"]` JSON array but `tags_text` should be space-separated for BM25. **Resolution:** Added `TagsToText()` helper that joins with spaces: `"go postgresql migration"`.

### Outcome

Merged as commit `d92c4f8`. Tag-based retrieval boost now works without the join — recall latency for tag-heavy queries dropped from ~85ms to ~45ms (p95).
