## Config: GitHub Actions CI/CD Workflow

Main CI pipeline for the memax monorepo. Runs on every push to `main` and on all pull requests. Handles linting, testing, building, and conditional deployment.

### Workflow File (`.github/workflows/ci.yml`)

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true       # cancel previous runs on same branch

env:
  TURBO_TOKEN: ${{ secrets.TURBO_TOKEN }}
  TURBO_TEAM: memaxlabs
  NODE_VERSION: "22"
  GO_VERSION: "1.23"
  PNPM_VERSION: "9"

jobs:
  lint:
    name: Lint & Typecheck
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with:
          version: ${{ env.PNPM_VERSION }}
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ env.NODE_VERSION }}
          cache: "pnpm"
      - run: pnpm install --frozen-lockfile
      - run: pnpm format --check         # Prettier
      - run: pnpm lint                    # tsc --noEmit + go vet

  test-server:
    name: Server Tests (Go)
    runs-on: ubuntu-latest
    services:
      postgres:
        image: pgvector/pgvector:pg16
        env:
          POSTGRES_DB: memax_test
          POSTGRES_USER: memax
          POSTGRES_PASSWORD: memax_test
        ports: ["5432:5432"]
        options: >-
          --health-cmd="pg_isready -U memax"
          --health-interval=10s
          --health-timeout=5s
          --health-retries=5
      redis:
        image: redis:7-alpine
        ports: ["6379:6379"]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache-dependency-path: packages/server/go.sum
      - name: Run tests
        working-directory: packages/server
        env:
          DATABASE_URL: "postgres://memax:memax_test@localhost:5432/memax_test?sslmode=disable"
          REDIS_URL: "redis://localhost:6379"
          TEST_DATABASE_URL: "postgres://memax:memax_test@localhost:5432/memax_test?sslmode=disable"
        run: |
          go test -race -coverprofile=coverage.out -covermode=atomic ./...
          go tool cover -func=coverage.out | tail -1

  test-web:
    name: Web Tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with:
          version: ${{ env.PNPM_VERSION }}
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ env.NODE_VERSION }}
          cache: "pnpm"
      - run: pnpm install --frozen-lockfile
      - run: pnpm --filter @memaxlabs/web test

  build:
    name: Build All Packages
    runs-on: ubuntu-latest
    needs: [lint]
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with:
          version: ${{ env.PNPM_VERSION }}
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ env.NODE_VERSION }}
          cache: "pnpm"
      - run: pnpm install --frozen-lockfile
      - run: pnpm build

  deploy-server:
    name: Deploy API Server
    runs-on: ubuntu-latest
    needs: [test-server, build]
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - run: flyctl deploy -c fly.server.toml --remote-only
        working-directory: packages/server
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}

  deploy-worker:
    name: Deploy Worker
    runs-on: ubuntu-latest
    needs: [test-server, build]
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    steps:
      - uses: actions/checkout@v4
      - uses: superfly/flyctl-actions/setup-flyctl@master
      - run: flyctl deploy -c fly.worker.toml --remote-only
        working-directory: packages/server
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
```

### Key Design Decisions

- **`concurrency.cancel-in-progress: true`** — Saves CI minutes by canceling stale runs when a new push arrives on the same branch
- **`services` for postgres/redis** — GitHub Actions runs real PostgreSQL (with pgvector) and Redis containers. No mocks in CI — tests run against real databases
- **`--frozen-lockfile`** — Ensures CI uses exact versions from `pnpm-lock.yaml`, preventing "works on CI but not locally" drift
- **Deploy gates** — Server and worker deployments only run on pushes to `main` (not PRs) and only after tests + build pass
- **Parallel deploys** — `deploy-server` and `deploy-worker` run in parallel (both depend on `test-server` and `build`, not each other)
- **`--remote-only`** — Fly.io builds the Docker image on their builders, not on the GitHub Actions runner. Faster and avoids runner disk space issues

### Secrets Referenced

- `TURBO_TOKEN` — Turborepo remote cache token
- `FLY_API_TOKEN` — Fly.io deployment token
- `VERCEL_TOKEN` — Vercel deployment (handled by Vercel's GitHub integration, not this workflow)
