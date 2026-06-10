## Deployment Process (January 2026)

Deploy the API server to Fly.io using `flyctl deploy` from the packages/server directory. Staging URL is staging.memax.app (single combined URL). Run database migrations manually before each deploy with `go run ./cmd/migrate up`. Set env vars using `fly secrets set`. The web app deploys to Vercel by pushing to the `deploy/web` branch. Worker process runs on the same Fly machine as the API server (combined process). CI/CD is not yet set up — deploys are manual from local machines.

**Important:** Always run `fly scale count 1` after deploying to prevent duplicate workers. We only have a single Fly machine in staging right now.

Note: This process was documented in January. See the updated deployment process for current steps.
