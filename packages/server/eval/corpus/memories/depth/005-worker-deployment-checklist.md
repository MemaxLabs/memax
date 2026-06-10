Step-by-step checklist for deploying the River worker separately from the API server.

## Why Separate Deploys

The API server (`fly.server.toml`) and the worker (`fly.worker.toml`) share the same Go codebase but run different entry points (`cmd/server/` vs `cmd/worker/`). They must be deployed independently because:

1. The worker processes background jobs (memory ingestion, embedding generation, dream synthesis) that can take minutes. A simultaneous deploy would kill in-progress jobs.
2. The worker has different resource requirements (more CPU, less memory) than the API server.
3. Schema migrations only run from the API server. The worker assumes the schema is already up to date.

## Deployment Order

Always deploy in this order:

1. **API server first** — this runs any pending migrations.
2. **Wait for health check** — confirm `/health` returns 200 and the migration version matches expectations.
3. **Worker second** — the worker connects to the same database and expects the latest schema.

Deploying the worker before the API server can cause the worker to fail if it encounters rows/columns from a newer migration it doesn't understand.

## Pre-Deploy Checklist

- [ ] Confirm all tests pass: `cd packages/server && go test ./...`
- [ ] Check for pending migrations: `ls packages/server/migrations/` — is the latest one committed?
- [ ] Review River job schemas: if any job payload changed, both server and worker must be updated together.
- [ ] Check environment variables: `fly secrets list -c fly.worker.toml` — any new vars needed?

## Deploy Commands

```bash
# API server
cd packages/server
fly deploy -c fly.server.toml --strategy rolling

# Verify health
curl -s https://staging-api.memaxlabs.com/health | jq .

# Worker
fly deploy -c fly.worker.toml --strategy rolling
```

## Rollback

If the worker deploy fails:
1. Check logs: `fly logs -c fly.worker.toml`
2. Roll back: `fly deploy -c fly.worker.toml --image <previous-image-ref>`
3. River jobs will retry automatically (3 attempts with exponential backoff). Check the `river_job` table for failed jobs.

## Monitoring

After deploy, check:
- Worker logs for "started processing" messages
- River dashboard (internal): pending job count should decrease
- Embedding queue depth in Grafana: should not grow unbounded
