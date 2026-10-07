# @memaxlabs/compile-service

The stateless compile service: [`@memaxlabs/compiler`](../compiler) over HTTP. The Go API server and worker call it to compile a Space's targets (AGENTS.md, the CLAUDE.md shim, scoped Cursor rules, the ChatGPT copy-out) and to read hand edits back (plan 25 §5.7). It takes JSON in and returns JSON out. It holds no state and reads no database; its one credential is the token its callers must send.

It is a thin wrapper on purpose. The compiler is the one implementation of rendering and parse-back, shared with the CLI and CI, so the server never reimplements it in Go.

It runs two ways, from one handler (`src/handler.ts`, a web-standard `Request → Response` function), so both answer every request identically:

- **A Cloudflare Worker** (`src/worker.ts`, `wrangler.jsonc`): how Memax runs it, at `https://compile-staging.memax.app` and `https://compile.memax.app`.
- **A Node server** (`src/server.ts` on `node:http`, `src/main.ts`): for local development, the Go integration tests, CI and self-hosting (the `Dockerfile`).

## API

Every response is JSON. Errors look like a JSON Schema validator's output: a stable `code`, an English `message`, and for a refused body every `issue` with the JSON Pointer of the offending value.

```json
{
  "error": {
    "code": "invalid_input",
    "message": "The compile input is invalid. Fix every issue and send it again.",
    "issues": [
      {
        "instancePath": "/memories/3/state",
        "message": "M-0412 isn't kept; proposals and other non-kept memories never compile"
      }
    ]
  }
}
```

| Route              | Body                                                               | Answer                                                                                                                                                                                 |
| ------------------ | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /compile`    | A `CompileInput` (see the compiler's README)                       | `200` with the `CompileResult`, or `422 invalid_input` with every issue                                                                                                                |
| `POST /parse-back` | `{"last": "<content>" or {"content", "drift_sha256"?}, "current"}` | `200` with the compiler's `ChangeSet` (`changes`, `drift`), plus `drifted` (does `current` differ from the last delivered file?) and `drift_sha256` (the hash to record for `current`) |
| `GET /health`      | —                                                                  | `200 {"status": "ok", "contract_version", "adapters": [{kind, version, role, default_path, default}], "uptime_seconds"}`, or `503` with `"status": "draining"` while shutting down     |

Other errors: `400 invalid_json`, `401 unauthorized` (with `WWW-Authenticate`), `404 not_found`, `405 method_not_allowed` (with `Allow`), `413 too_large`, `415 unsupported_media_type` (send `application/json`), `500 internal_error`, and `503 unavailable` for a request that arrives while the service drains or a Worker that has no token. `X-Request-Id` is echoed back, or generated.

## Authentication

With `COMPILE_SERVICE_TOKEN` set, every route but `GET /health` needs `Authorization: Bearer <token>`. The service checks it before it reads the body (an unauthenticated request is never buffered) and compares SHA-256 digests of the two tokens in constant time; a missing or wrong token gets `401 unauthorized`. Health stays public so uptime checks need no secret. No header and no body is ever logged.

- **The Worker requires it.** A Worker has a public URL, so without the secret it refuses every route but health with `503`, rather than compiling for anyone.
- **The Node server makes it optional.** Unset, it serves without one, which suits local development and a private network; set it whenever anything besides the API and the worker can reach the port.
- **The callers send it.** The API and the worker read the same value from `COMPILE_SERVICE_TOKEN` (`compile.WithToken` in `packages/server/internal/compile`). A `401` there is a permanent error that names the variable to fix. To rotate: put the new value in Doppler and redeploy the Worker and then the API and worker; compiles that fail in between are retried by their jobs.

## Behaviour

- **Limits.** A body may be at most `MAX_BODY_BYTES` (4 MiB): a declared `Content-Length` over it is refused before reading, and a streamed body is cut off at the limit (the rest, up to three more limits, is read and dropped so the connection stays usable). On Node a request has 15 s end to end and 10 s for its headers; a Worker has `limits.cpu_ms` (5 s) of CPU.
- **Logs.** One JSON object per line (stdout on Node, `console.log` on Workers, which Workers Logs indexes): route, status, sizes, duration and request ID. Never a request's content: compile inputs hold memory statements, and parse-back requests hold whole files. On Workers `duration_ms` reads 0, because the runtime's clock doesn't advance during CPU work; Workers Logs records each request's CPU and wall time.
- **Shutdown.** On `SIGTERM` (or `SIGINT`) the Node server stops accepting connections, answers health with `503`, gives in-flight requests `SHUTDOWN_GRACE_MS` (10 s) to finish, then closes every connection and exits. A Worker deploy needs none of this: the runtime finishes in-flight requests on the old version.
- **Dependencies.** None at runtime besides the compiler, which has none either. The Worker uses `nodejs_compat` only so the compiler's drift hashes use the native SHA-256 (`process.getBuiltinModule("node:crypto")`).

| Variable                | Default   | Meaning                                                                                        |
| ----------------------- | --------- | ---------------------------------------------------------------------------------------------- |
| `COMPILE_SERVICE_TOKEN` | unset     | The bearer token every route but health requires. A Worker secret; optional on the Node server |
| `MAX_BODY_BYTES`        | `4194304` | Largest accepted body                                                                          |
| `PORT`                  | `8080`    | Node: port to listen on                                                                        |
| `HOST`                  | `::`      | Node: address to bind; `::` takes IPv6 and IPv4                                                |
| `SHUTDOWN_GRACE_MS`     | `10000`   | Node: how long in-flight requests get on shutdown                                              |

### Workers limits

`scripts/measure.mjs` runs every golden fixture and the largest inputs the service takes, in Node (CPU time, and the smallest V8 heap each completes in) and under workerd (wall time per request through Wrangler's dev server, which bounds the CPU time from above). Measured on Oct 7, 2026 (4-core arm64):

| Case                                                      | Body    | Node CPU | Smallest heap | workerd wall |
| --------------------------------------------------------- | ------- | -------- | ------------- | ------------ |
| Golden fixtures (largest: `budget-overflow`)              | 1–7 KiB | 1–4 ms   | ≤ 16 MB       | 8–10 ms      |
| Largest compile input (4 MiB, every adapter)              | 4.0 MiB | 148 ms   | ≤ 24 MB       | 151 ms       |
| Parse-back, 1 MiB files (the API's `MaxObservedBytes`)    | 1.8 MiB | 168 ms   | ≤ 24 MB       | 185 ms       |
| Parse-back, 2 MiB files (the body limit), every cite gone | 3.3 MiB | 328 ms   | ≤ 32 MB       | 322 ms       |

Against the Workers Paid limits: 128 MB of memory per isolate, and the 5 s `limits.cpu_ms` set in `wrangler.jsonc` (the platform allows up to 5 minutes). The Worker bundle is 64 KiB (19 KiB gzipped).

```bash
pnpm --filter @memaxlabs/compiler build && pnpm --filter @memaxlabs/compile-service build
node packages/compile-service/scripts/measure.mjs
```

## Deployment

CI deploys the Worker with `wrangler deploy` (`.github/workflows/deploy-cloudflare.yml`): staging on every push to `main`, production from the `prod` branch, each before the API that calls it, with `COMPILE_SERVICE_TOKEN` uploaded from Doppler alongside the new version. The Worker is reached only at its custom domain (`routes` in `wrangler.jsonc`; `workers_dev` and preview URLs are off). The API and the worker find it through `COMPILE_SERVICE_URL` in their Fly tomls.

Rollback: `pnpm exec wrangler rollback --env production` in this directory returns the Worker to its previous version.

To self-host it, build the image from the repository root (it builds `packages/compiler` too):

```bash
docker build -f packages/compile-service/Dockerfile .
docker run -p 8080:8080 -e COMPILE_SERVICE_TOKEN=… <image>
```

The image is `node:24-alpine` with the built service and the compiler (`pnpm deploy --prod`). The tests run on Node 22 and later.

## Development

```bash
pnpm --filter @memaxlabs/compiler build            # the service imports the built compiler
pnpm --filter @memaxlabs/compile-service build
pnpm --filter @memaxlabs/compile-service test      # the same contract against node:http and against the Worker under workerd
pnpm --filter @memaxlabs/compile-service lint
PORT=8090 pnpm --filter @memaxlabs/compile-service start                     # the Node server
pnpm --filter @memaxlabs/compile-service dev:cf --port 8788 --var COMPILE_SERVICE_TOKEN:dev-token   # the Worker under workerd
pnpm --filter @memaxlabs/compile-service build:cf  # bundle the Worker into .wrangler/dist and print its size
```

The Go side has its own tests: `internal/compile` runs the coordinator against an in-process fake, and the integration tests spawn this service with Node (they build the two packages first when `node_modules` exists, and skip without Node) and a random token. Point them at the Worker instead with `MEMAX_TEST_COMPILE_SERVICE_URL=http://127.0.0.1:8788 MEMAX_TEST_COMPILE_SERVICE_TOKEN=dev-token`.

## License

AGPL-3.0, like the API server and worker it is deployed with. See [LICENSE](LICENSE).

Why AGPL and not Apache-2.0 like the compiler: the service is part of the hosted Memax backend, not something other software links against. It has no use outside the server that calls it, and it carries nothing reusable of its own: the compiler is the reusable part, and it stays Apache-2.0 so the CLI, editors and other tools can embed it directly. Keeping the backend under one license (server, worker, compile service) keeps the self-hosted edition's terms simple: every network service it runs is AGPL. The licensing rule holds: this AGPL package depends on the Apache-2.0 compiler, and nothing Apache-2.0 depends on it.
