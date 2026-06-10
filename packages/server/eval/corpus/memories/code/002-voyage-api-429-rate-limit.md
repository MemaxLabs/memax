## Error Log: Voyage AI 429 Rate Limit During Bulk Ingest

Encountered persistent 429 errors from the Voyage AI embeddings API during a bulk `memax sync` of ~2,400 markdown files from the memax docs directory. The CLI was hitting the rate limit within 15 seconds of starting the sync.

### Error Output

```
$ memax sync ./docs --hub personal
Scanning... 2,417 files found
Pushing batch 1/49 (50 files)... ✓
Pushing batch 2/49 (50 files)... ✓
Pushing batch 3/49 (50 files)... ✓
Pushing batch 4/49 (50 files)... ✗

Error: Embedding generation failed for batch 4
  Status: 429 Too Many Requests
  Response: {"detail":"Rate limit exceeded. Please retry after 2 seconds.","type":"rate_limit_error"}
  Retry-After: 2

Retrying batch 4 in 3s...
Pushing batch 4/49 (50 files)... ✗

Error: Embedding generation failed for batch 4
  Status: 429 Too Many Requests
  Response: {"detail":"Rate limit exceeded. Please retry after 5 seconds.","type":"rate_limit_error"}
  Retry-After: 5

FATAL: Max retries (3) exceeded for batch 4. Aborting sync.
  Synced: 150/2,417 files
  Failed: 2,267 files
  Duration: 47s
```

### Investigation

Checked the Voyage AI dashboard — our plan allows 300 RPM and 1M tokens/minute. Each batch of 50 files generates ~50 embedding requests (one per chunk), but some large files produce 8-12 chunks each. So batch 3 alone generated ~400 embedding calls, blowing through the RPM limit.

The worker's `embed.Embedder` was sending chunks individually rather than batching them into Voyage's multi-input endpoint:

```go
// BEFORE — one API call per chunk
for _, chunk := range chunks {
    vec, err := e.embedder.Embed(ctx, chunk.Text)
    // ...
}

// AFTER — batch up to 128 inputs per API call
batches := splitIntoBatches(chunks, 128)
for _, batch := range batches {
    texts := extractTexts(batch)
    vecs, err := e.embedder.EmbedBatch(ctx, texts)
    // ...
}
```

### Resolution

1. **Reduced CLI batch size** from 50 to 20 files per push batch (`memax sync` flag: `--batch-size`)
2. **Switched to `EmbedBatch`** in the worker — sends up to 128 texts in a single Voyage API call instead of one-at-a-time
3. **Added exponential backoff** with jitter on 429 responses: base 2s, max 30s, jitter ±500ms
4. **Added `--dry-run` flag** to `memax sync` so users can preview the file count before committing

After the fix, the same 2,417-file sync completes in ~3 minutes with zero 429 errors. Voyage API usage dropped from ~12,000 RPM to ~200 RPM for the same workload.

### Relevant Config

```bash
VOYAGE_API_KEY=voyage-xxx
VOYAGE_MODEL=voyage-3-large
VOYAGE_BATCH_SIZE=128   # max inputs per API call
VOYAGE_RPM_LIMIT=300    # our plan's rate limit
```
