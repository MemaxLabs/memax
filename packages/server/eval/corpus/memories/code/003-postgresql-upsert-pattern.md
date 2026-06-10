## Code Pattern: PostgreSQL Upsert with pgx in Go

Standard upsert pattern we use across the memax server for idempotent memory insertion. This uses PostgreSQL's `INSERT ... ON CONFLICT` with `pgx` and handles the common gotchas.

### Basic Upsert Pattern

```go
func (s *Store) UpsertMemory(ctx context.Context, m *model.Memory) error {
    query := `
        INSERT INTO memories (
            id, owner_id, hub_id, content_hash,
            title, kind, stability,
            source, source_agent, source_path,
            created_at, updated_at
        ) VALUES (
            @id, @owner_id, @hub_id, @content_hash,
            @title, @kind, @stability,
            @source, @source_agent, @source_path,
            @created_at, @updated_at
        )
        ON CONFLICT (owner_id, content_hash)
        DO UPDATE SET
            title      = EXCLUDED.title,
            kind       = EXCLUDED.kind,
            stability  = EXCLUDED.stability,
            source     = EXCLUDED.source,
            updated_at = now()
        WHERE memories.updated_at < EXCLUDED.updated_at
        RETURNING id, created_at, updated_at
    `

    args := pgx.NamedArgs{
        "id":           m.ID,
        "owner_id":     m.OwnerID,
        "hub_id":       m.HubID,
        "content_hash": m.ContentHash,
        "title":        m.Title,
        "kind":         m.Kind,
        "stability":    m.Stability,
        "source":       m.Source,
        "source_agent": m.SourceAgent,
        "source_path":  m.SourcePath,
        "created_at":   m.CreatedAt,
        "updated_at":   m.UpdatedAt,
    }

    return s.pool.QueryRow(ctx, query, args).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
}
```

### Key Gotchas

1. **Use `EXCLUDED` not `VALUES`** — In the `DO UPDATE SET` clause, `EXCLUDED` refers to the row that would have been inserted. Using `VALUES(col)` is deprecated in PostgreSQL 15+.

2. **Add a `WHERE` guard on updates** — The `WHERE memories.updated_at < EXCLUDED.updated_at` prevents stale data from overwriting newer data in race conditions. Without this, two concurrent upserts for the same content hash could clobber each other.

3. **`RETURNING` may return zero rows** — If the `ON CONFLICT DO UPDATE` WHERE clause prevents the update, PostgreSQL returns zero rows. Use `pgx.ErrNoRows` to detect this:

```go
err := s.pool.QueryRow(ctx, query, args).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
if errors.Is(err, pgx.ErrNoRows) {
    // conflict detected but row was newer — this is NOT an error
    return nil
}
return err
```

4. **Composite conflict target** — Our unique constraint is `(owner_id, content_hash)`, not just `content_hash`. This ensures each user's content-hash dedup is independent. The migration for this:

```sql
-- Migration 018: add composite unique constraint for idempotent upserts
ALTER TABLE memories
    ADD CONSTRAINT memories_owner_content_hash_unique
    UNIQUE (owner_id, content_hash);
```

5. **Batch upserts** — For bulk sync, use `pgx.Batch` to send multiple upserts in a single roundtrip:

```go
batch := &pgx.Batch{}
for _, m := range memories {
    batch.Queue(query, argsFor(m))
}
results := s.pool.SendBatch(ctx, batch)
defer results.Close()
```

### When NOT to Use Upsert

- **Append-only tables** (like `audit_log`) — always INSERT, never update
- **When you need to know if it was insert vs update** — use `INSERT ... ON CONFLICT DO UPDATE ... RETURNING (xmax = 0) AS inserted` to distinguish
- **Cross-table atomicity** — if the upsert needs to cascade to chunks or embeddings, wrap it in a transaction instead
