# Retrieval Eval Corpus

This corpus is intentionally split across small files so retrieval evals can grow
without creating a large, unreviewable JSON fixture.

## Layout

```text
corpus/
  manifest.json              corpus version and included shards
  memories/
    smoke/
      metadata.json          memory metadata and body file names
      *.md                   memory bodies
  queries/
    smoke.json               query definitions for the bootstrap eval
  scopes/
    core.json                eval users, hubs, memberships, and roles
```

## Rules

- Put long memory content in Markdown files, not JSON strings.
- Keep metadata in small shard-local `metadata.json` files.
- Split query sets by scenario, such as `temporal.json`, `shared-hub.json`, or
  `negative.json`.
- Model realistic retrieval metadata from the start: author user, hub, hub
  membership, pushed time, source, source agent, project context, and event
  dates.
- Generated artifacts, such as fixture embeddings or derived chunks, should live
  under `generated/` and be keyed by content hash.
- Do not use real customer or team data unless it has been explicitly approved,
  redacted, and documented.
