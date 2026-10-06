# @memaxlabs/compile-service

The stateless compile service: [`@memaxlabs/compiler`](../compiler) over HTTP. The Go API server and worker call it on Memax's private network to compile a Space's targets (AGENTS.md, the CLAUDE.md shim, scoped Cursor rules, the ChatGPT copy-out) and to read hand edits back (plan 25 §5.7). It takes JSON in and returns JSON out. It holds no state, reads no database and has no credentials.

It is a thin wrapper on purpose. The compiler is the one implementation of rendering and parse-back, shared with the CLI and CI, so the server never reimplements it in Go.

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

Other errors: `400 invalid_json`, `404 not_found`, `405 method_not_allowed` (with `Allow`), `413 too_large`, `415 unsupported_media_type` (send `application/json`), `500 internal_error`, and `503 unavailable` for a request that arrives while the service drains. `X-Request-Id` is echoed back, or generated.

## Behaviour

- **Limits.** A body may be at most `MAX_BODY_BYTES` (4 MiB): a declared `Content-Length` over it is refused before reading, and a streamed body is cut off at the limit. A request has 15 s end to end and 10 s for its headers.
- **Logs.** One JSON object per line on stdout: route, status, sizes, duration and request ID. Never a request's content: compile inputs hold memory statements, and parse-back requests hold whole files.
- **Shutdown.** On `SIGTERM` (or `SIGINT`) the service stops accepting connections, answers health with `503`, gives in-flight requests `SHUTDOWN_GRACE_MS` (10 s) to finish, then closes every connection and exits. Fly's `kill_timeout` is 15 s.
- **Dependencies.** None at runtime besides the compiler, which has none either. It is built on `node:http`.

| Variable            | Default   | Meaning                                                           |
| ------------------- | --------- | ----------------------------------------------------------------- |
| `PORT`              | `8080`    | Port to listen on                                                 |
| `HOST`              | `::`      | Address to bind; `::` takes IPv6 (Fly's private network) and IPv4 |
| `MAX_BODY_BYTES`    | `4194304` | Largest accepted body                                             |
| `SHUTDOWN_GRACE_MS` | `10000`   | How long in-flight requests get on shutdown                       |

## Deployment

Internal only, on Fly. The tomls in [`fly/`](fly) have no `[http_service]` and no `[[services]]`, so Fly's proxy never routes public traffic to it: the server and worker reach it at `http://memax-compile-staging.internal:8080` (staging) and `http://memax-compile-production.internal:8080` (production), which is `COMPILE_SERVICE_URL` in their tomls. Don't allocate public IPs for these apps.

```bash
# From the repository root: the image builds packages/compiler too.
fly deploy . -c packages/compile-service/fly/fly.compile.staging.toml
fly deploy . -c packages/compile-service/fly/fly.compile.production.toml
fly scale count 2 -a memax-compile-production   # once, for HA
```

The image is `node:24-alpine` with the built service and the compiler (`pnpm deploy --prod`). The tests run on Node 22 and later.

## Development

```bash
pnpm --filter @memaxlabs/compiler build            # the service imports the built compiler
pnpm --filter @memaxlabs/compile-service build
pnpm --filter @memaxlabs/compile-service test      # vitest, against a real server on a random port
pnpm --filter @memaxlabs/compile-service lint
PORT=8090 pnpm --filter @memaxlabs/compile-service start
```

The Go side has its own tests: `internal/compile` runs the coordinator against an in-process fake, and one integration test spawns this service with Node (it builds the two packages first when `node_modules` exists, and skips without Node).

## License

AGPL-3.0, like the API server and worker it is deployed with. See [LICENSE](LICENSE).

Why AGPL and not Apache-2.0 like the compiler: the service is part of the hosted Memax backend, not something other software links against. It has no use outside the server that calls it, and it carries nothing reusable of its own: the compiler is the reusable part, and it stays Apache-2.0 so the CLI, editors and other tools can embed it directly. Keeping the backend under one license (server, worker, compile service) keeps the self-hosted edition's terms simple: every network service it runs is AGPL. The licensing rule holds: this AGPL package depends on the Apache-2.0 compiler, and nothing Apache-2.0 depends on it.
