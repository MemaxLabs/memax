## Error: Migration 021 Dirty State — Recovery Steps

Hit a dirty migration state on local dev after a crash during `go run ./cmd/server/`. The server refused to start with the following error:

### Error Output

```
$ go run ./cmd/server/
2026-04-07T09:12:33.145-07:00 INF Starting Memax API server version=0.14.2
2026-04-07T09:12:33.287-07:00 INF Connecting to database host=localhost port=5432 db=memax
2026-04-07T09:12:33.301-07:00 INF Running migrations dir=/app/migrations
2026-04-07T09:12:33.315-07:00 ERR Migration failed error="Dirty database version 21. Fix and force version."

error: Dirty database version 21. Fix and force version.
exit status 1
```

### What Happened

Migration 021 (`021_add_hub_role_enum.up.sql`) was adding a new PostgreSQL enum type for hub roles. The migration partially executed — the enum was created but the `ALTER TABLE` that adds the column using the enum failed because I had a typo in the column name. The process crashed mid-migration, leaving the `schema_migrations` table in a dirty state:

```sql
memax=# SELECT * FROM schema_migrations;
 version | dirty
---------+-------
      21 | t
(1 row)
```

### Recovery Steps

**Step 1: Check what migration 021 actually did**

```bash
$ cat packages/server/migrations/021_add_hub_role_enum.up.sql
```

```sql
-- Create the hub_role enum
CREATE TYPE hub_role AS ENUM ('owner', 'admin', 'member', 'viewer');

-- Add role column to hub_members (TYPO: 'roel' instead of 'role')
ALTER TABLE hub_members ADD COLUMN roel hub_role NOT NULL DEFAULT 'member';
```

**Step 2: Check what was actually applied**

```sql
memax=# SELECT typname FROM pg_type WHERE typname = 'hub_role';
 typname
---------
 hub_role
(1 row)

memax=# SELECT column_name FROM information_schema.columns
        WHERE table_name = 'hub_members' AND column_name IN ('role', 'roel');
(0 rows)
```

The enum was created but the column was not added (the typo would have caused an error on the column name, but since the error was actually a crash, we need to check).

**Step 3: Fix the migration file**

```sql
-- Fixed: 'roel' → 'role'
ALTER TABLE hub_members ADD COLUMN role hub_role NOT NULL DEFAULT 'member';
```

**Step 4: Clean up the partial state and force the version**

```bash
# Drop the enum that was partially created
$ psql memax -c "DROP TYPE IF EXISTS hub_role;"

# Force the migration version back to 20 (the last clean version)
$ psql memax -c "UPDATE schema_migrations SET version = 20, dirty = false;"
```

**Step 5: Re-run the server**

```bash
$ go run ./cmd/server/
2026-04-07T09:18:44.102-07:00 INF Running migrations dir=/app/migrations
2026-04-07T09:18:44.187-07:00 INF Migration 021 applied successfully
2026-04-07T09:18:44.201-07:00 INF Server started addr=:8080
```

### Prevention

- Always test migrations locally with `psql` before running via the server
- Use `BEGIN; ... COMMIT;` in migration files so they're atomic (PostgreSQL supports transactional DDL)
- If using `golang-migrate`, the `-force` flag can reset the dirty state: `migrate -path ./migrations -database $DATABASE_URL force 20`
- Keep the local dev debug skill in mind — it covers dirty migration recovery in detail
