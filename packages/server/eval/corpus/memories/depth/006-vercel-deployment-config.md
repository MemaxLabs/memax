Vercel deployment configuration for the Memax web app (memax.app).

## Project Setup

The web app lives in `packages/web/` within the monorepo. Vercel is configured with:

- **Root directory:** `packages/web`
- **Build command:** `cd ../.. && pnpm build --filter @memaxlabs/web`
- **Output directory:** `.next`
- **Node.js version:** 22.x
- **Install command:** `pnpm install --frozen-lockfile`

The build command runs from the repo root so Turborepo can resolve workspace dependencies like `@memaxlabs/ui`.

## Environment Variables

Production environment variables set in Vercel dashboard:

- `NEXT_PUBLIC_API_URL` — points to `https://api.memaxlabs.com`
- `NEXT_PUBLIC_APP_URL` — `https://memax.app`
- `NEXT_PUBLIC_DOCS_URL` — `https://memax.dev`
- `NEXT_PUBLIC_POSTHOG_KEY` — PostHog analytics key
- `AUTH_SECRET` — NextAuth.js secret for session encryption
- `AUTH_GITHUB_ID` / `AUTH_GITHUB_SECRET` — GitHub OAuth app credentials
- `AUTH_GOOGLE_ID` / `AUTH_GOOGLE_SECRET` — Google OAuth app credentials

Staging overrides (applied to the `staging` branch):
- `NEXT_PUBLIC_API_URL` = `https://staging-api.memaxlabs.com`
- `NEXT_PUBLIC_APP_URL` = `https://staging.memax.app`

## Preview Deployments

Every PR gets a preview deployment automatically. Preview URLs follow the pattern `memax-web-<hash>.vercel.app`. Preview deployments use staging API by default.

## ISR Configuration

Incremental Static Regeneration is enabled for:
- `/blog/*` pages — revalidate every 3600 seconds
- `/changelog` — revalidate every 1800 seconds
- Dynamic routes (`/m/[id]`, `/hub/[slug]`) use on-demand revalidation via `revalidateTag()` when memories are updated.

## Custom Domain

- `memax.app` — production (CNAME to `cname.vercel-dns.com`)
- `www.memax.app` — redirects to `memax.app`
- `staging.memax.app` — staging branch

## Known Issues

- Monorepo builds are slower (~3 min) because Vercel doesn't cache `node_modules` across workspace packages well. We're exploring Turborepo Remote Cache to improve this.
- The `@memaxlabs/ui` package must be built before `@memaxlabs/web`. Turborepo handles this via the `dependsOn` config in `turbo.json`.
