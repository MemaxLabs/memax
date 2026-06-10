Incident report: staging deploy failure on April 3, 2026.

## Timeline

- **14:22 UTC** — Pushed migration 48 (add `project_context` JSONB column to `memories` table) to staging via `fly deploy -c fly.server.toml`.
- **14:23 UTC** — Server started, ran migration 48. Migration applied the `ALTER TABLE` successfully but the server crashed during startup health check before marking migration as complete.
- **14:24 UTC** — Fly.io restarted the server. Migration runner detected version 48 as "dirty" and refused to start. Server entered crash loop.
- **14:30 UTC** — Noticed staging was down via Slack alert from UptimeRobot.
- **14:35 UTC** — Connected to staging database via `fly proxy` + `psql`.
- **14:42 UTC** — Found the `schema_migrations` table had `version=48, dirty=true`.

## Root Cause

The migration itself was fine — the `ALTER TABLE` completed. But the `golang-migrate` library marks the migration dirty before running it and only clears the dirty flag after success. Since the server crashed after the SQL ran but before the flag was cleared, the database was in a "dirty" state even though the schema was correct.

The crash was caused by an unrelated nil pointer in the new `project_context` handler code that ran during startup initialization.

## Recovery Steps

1. Verified the `project_context` column existed and was correctly typed: `\d memories` in psql.
2. Manually cleared the dirty flag: `UPDATE schema_migrations SET dirty = false WHERE version = 48;`
3. Fixed the nil pointer bug in the handler (missing nil check on `ProjectContext` field).
4. Re-deployed. Server started cleanly.

## Lessons Learned

- **Never add application code that depends on a new migration in the same deploy.** The migration and the code that uses it should ship in separate deploys. Migration first, then code.
- **Add a "force-clean" migration CLI command** so we don't need raw psql access to recover from dirty migrations. Tracked as a CLI task.
- **The health check timeout was too aggressive** (5 seconds). Bumped to 15 seconds for staging.

## Impact

Staging was down for ~20 minutes. No production impact. No data loss.
