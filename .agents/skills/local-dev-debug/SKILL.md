---
name: local-dev-debug
description: "Use when debugging local development issues in the Memax repo — especially pnpm dev failures, PostgreSQL migration problems, Redis cache inspection, local infra drift, or anything requiring direct use of psql and redis-cli. ALWAYS use this skill for dirty migrations, schema inspection, queue/cache debugging, or when local dev behaves differently from staging/production."
---

# Local Dev Debug

Use this skill when the problem is local development behavior, not product logic. The goal is to inspect the actual local state quickly and recover it without guessing.

## What this skill is for

- `pnpm dev` startup failures
- dirty PostgreSQL migrations
- local schema drift
- Redis cache / key inspection
- queue state inspection
- environment/config mismatches
- "works in CI/staging but not locally"

## Assumptions

- `psql` is available locally
- `redis-cli` is available locally
- `.env` / `.env.example` define the canonical local connection settings
- local data is often disposable, but do not reset it unless the user explicitly wants that

## Canonical local endpoints

There are two common local execution contexts in this repo. Use the right addresses for the current shell.

### Inside the devcontainer / compose network

- PostgreSQL:

```bash
postgres://memax:memax@postgres:5432/memax?sslmode=disable
```

- Redis:

```bash
redis://redis:6379
```

These service-hostname URLs are the correct defaults when the shell is running inside the devcontainer.

### On the host machine

Use the host-mapped ports from your local `.env` or compose setup, often:

- PostgreSQL:

```bash
postgres://memax:memax@127.0.0.1:5432/memax?sslmode=disable
```

- Redis:

```bash
redis://127.0.0.1:6379
```

Do not assume `127.0.0.1` works inside the devcontainer. In this repo, the devcontainer should prefer `postgres` and `redis`.

## First steps

Always inspect before changing state.

1. Check the effective environment:

```bash
printf 'DATABASE_URL=%s\n' "$DATABASE_URL"
printf 'REDIS_URL=%s\n' "$REDIS_URL"
```

If those are unset and you are inside the devcontainer, use the canonical defaults above before diagnosing connectivity.

2. Check whether the core tools exist:

```bash
command -v psql
command -v redis-cli
```

3. Prefer direct database inspection over inferring state from app logs.

## PostgreSQL: dirty migration recovery

When startup shows:

```text
Dirty database version X. Fix and force version.
```

inspect the migration table and the partially applied objects first:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -c "SELECT version, dirty FROM schema_migrations;"
```

Then inspect the objects introduced by the failing migration:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -c "SELECT to_regclass('public.some_table');" \
  -c "SELECT column_name FROM information_schema.columns WHERE table_name='some_table';" \
  -c "SELECT indexname FROM pg_indexes WHERE tablename='some_table' ORDER BY indexname;"
```

### Recovery rule

- If the migration objects **do not exist**, force back to the previous clean version.
- If the migration objects **fully exist**, clear `dirty` on the current version.
- If the migration is **partially applied**, either complete the objects manually or force back and re-run only if the migration is idempotent.

### Examples

Force back one version clean:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -c "UPDATE schema_migrations SET version = 23, dirty = false WHERE version = 24 AND dirty = true;"
```

Mark current version clean:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -c "UPDATE schema_migrations SET dirty = false WHERE version = 24 AND dirty = true;"
```

After clearing state, verify with the standalone migrator:

```bash
cd packages/server && go run ./cmd/migrate
```

Do not tell the user "just reset the DB" unless:

- the DB is explicitly disposable, and
- inspection would take longer than a reset, and
- the user is fine losing local state

## PostgreSQL: useful inspection commands

List tables:

```bash
psql "$DATABASE_URL" -c "\dt"
```

Quick connectivity check:

```bash
psql "$DATABASE_URL" -c "SELECT current_database(), current_user;"
```

Describe a table:

```bash
psql "$DATABASE_URL" -c "\d+ memories"
```

Inspect indexes:

```bash
psql "$DATABASE_URL" -c "SELECT indexname, indexdef FROM pg_indexes WHERE tablename = 'chunks';"
```

Check migration version:

```bash
psql "$DATABASE_URL" -c "SELECT version, dirty FROM schema_migrations;"
```

## Redis: inspection and cache debugging

Always inspect keys narrowly. Avoid `KEYS *` unless the local dataset is tiny and disposable.

Prefer:

```bash
redis-cli -u "$REDIS_URL" SCAN 0 MATCH 'memax:*' COUNT 100
```

Quick connectivity check:

```bash
redis-cli -u "$REDIS_URL" PING
```

Inspect a specific key:

```bash
redis-cli -u "$REDIS_URL" TYPE "memax:recall:..."
redis-cli -u "$REDIS_URL" TTL "memax:recall:..."
redis-cli -u "$REDIS_URL" GET "memax:recall:..."
```

Delete a specific key:

```bash
redis-cli -u "$REDIS_URL" DEL "memax:recall:..."
```

If you need to inspect many keys, use `SCAN`, not `KEYS`.

## Local dev server verification

After fixing DB/Redis state, verify components directly before returning to `pnpm dev`.

Server migrations:

```bash
cd packages/server && go run ./cmd/migrate
```

API server:

```bash
cd packages/server && go run ./cmd/server
```

Worker:

```bash
cd packages/server && go run ./cmd/worker
```

This isolates whether the failure is:

- migration-time
- API startup
- worker startup
- turborepo orchestration

## What to avoid

- Do not assume app logs tell the whole story. Inspect DB/Redis directly.
- Do not reset local state before checking whether a smaller repair is possible.
- Do not use broad destructive Redis or Postgres commands unless the user approves reset-level recovery.
- Do not leave the database in a manually modified state without re-running the real migrator afterwards.

## Output standard

When you finish a local-debug pass, report:

1. the concrete root cause
2. the exact command(s) used to inspect/fix it
3. the final verified state
4. any follow-up hardening needed in code or docs
