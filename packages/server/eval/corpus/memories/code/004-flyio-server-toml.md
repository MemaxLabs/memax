## Config Reference: Fly.io Server TOML (fly.server.toml)

Annotated production configuration for the memax API server deployed on Fly.io. This is the canonical deployment config — the worker has a separate file (`fly.worker.toml`).

### Full Configuration

```toml
# fly.server.toml — Memax API Server
# Deploy: fly deploy -c fly.server.toml
# Logs:   fly logs -c fly.server.toml

app = "memax-api"
primary_region = "sjc"          # San Jose — lowest latency to Neon (US-West)
kill_signal = "SIGTERM"
kill_timeout = "30s"            # graceful shutdown budget for in-flight requests

[build]
  dockerfile = "Dockerfile"     # multi-stage Go build, final image ~22MB

[env]
  PORT = "8080"
  GIN_MODE = "release"
  OTEL_SERVICE_NAME = "memax-api"
  LOG_FORMAT = "json"
  MIGRATIONS_DIR = "/app/migrations"
  # Secrets (DATABASE_URL, REDIS_URL, etc.) are set via `fly secrets`
  # NEVER put secrets in this file

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = "suspend"    # suspend idle machines after 5min
  auto_start_machines = true        # wake on incoming request
  min_machines_running = 1          # always keep 1 warm
  [http_service.concurrency]
    type = "requests"
    hard_limit = 250                # max in-flight per machine
    soft_limit = 200                # start scaling at 200

[http_service.checks]
  [http_service.checks.health]
    interval = "15s"
    timeout = "5s"
    grace_period = "10s"
    method = "GET"
    path = "/healthz"

[[vm]]
  size = "shared-cpu-2x"           # 2 vCPU, 512MB — enough for API
  memory = "1gb"                    # bumped from 512MB after OOM on large recalls
  cpus = 2

[[statics]]
  guest_path = "/app/public"
  url_prefix = "/public"

[metrics]
  port = 9091
  path = "/metrics"
```

### Key Decisions

**`auto_stop_machines = "suspend"`** — We use suspend (not stop) so machines resume in ~300ms instead of cold-starting in ~4s. This keeps latency acceptable for agent integrations while saving costs during low-traffic hours (10pm-6am PST).

**`min_machines_running = 1`** — We always keep one machine warm. Without this, the first request after an idle period takes 4+ seconds (cold start), which breaks the hook latency budget (500ms max for Claude Code hooks).

**`memory = "1gb"`** — Bumped from 512MB after we saw OOM kills during large recall operations. The Go heap grows when assembling ranked results with full memory bodies. 1GB gives comfortable headroom.

**`primary_region = "sjc"`** — Co-located with our Neon PostgreSQL instance (also US-West). DB latency is ~2ms instead of ~40ms if we used a different region.

### Related Files

- `fly.worker.toml` — Worker process (River queue consumer), uses `performance-2x` for CPU-heavy embedding/LLM work
- `Dockerfile` — Multi-stage build: `golang:1.23-alpine` for build, `alpine:3.19` for runtime
- `Dockerfile.worker` — Same build stage, different entrypoint (`cmd/worker/`)
- `.github/workflows/deploy.yml` — CI deploys server and worker in parallel after tests pass

### Common Operations

```bash
# Deploy
fly deploy -c fly.server.toml

# Check machine status
fly machine list -c fly.server.toml

# SSH into running machine
fly ssh console -c fly.server.toml

# Set a secret
fly secrets set DATABASE_URL="postgres://..." -c fly.server.toml

# View recent logs
fly logs -c fly.server.toml --region sjc
```
