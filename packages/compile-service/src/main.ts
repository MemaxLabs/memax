/**
 * Entry point: `node dist/main.js`.
 *
 *   PORT            port to listen on (default 8080; 0 picks a free one)
 *   HOST            address to bind (default "::", every IPv6 and IPv4
 *                   address; Fly's private network is IPv6)
 *   MAX_BODY_BYTES  largest accepted body (default 4 MiB)
 *   SHUTDOWN_GRACE_MS  how long in-flight requests get on SIGTERM (default 10 s)
 */
import { jsonLogger } from "./log.js";
import { DEFAULT_MAX_BODY, createService } from "./server.js";

const log = jsonLogger();
const port = intFrom("PORT", 8080, 0);
const host = process.env.HOST?.trim() || "::";
const grace = intFrom("SHUTDOWN_GRACE_MS", 10_000);
const service = createService({
  maxBodyBytes: intFrom("MAX_BODY_BYTES", DEFAULT_MAX_BODY),
  log,
});

try {
  const addr = await service.listen(port, host);
  log.log("info", "listening", {
    address: addr.address,
    port: addr.port,
    node: process.version,
  });
} catch (err) {
  log.log("error", "listen failed", {
    error: err instanceof Error ? err.message : String(err),
    host,
    port,
  });
  process.exit(1);
}

let stopping = false;
for (const signal of ["SIGTERM", "SIGINT"] as const) {
  process.on(signal, () => {
    if (stopping) return;
    stopping = true;
    log.log("info", "draining", { signal, grace_ms: grace });
    service
      .close(grace)
      .then(() => {
        log.log("info", "stopped", {});
        process.exit(0);
      })
      .catch((err: unknown) => {
        log.log("error", "shutdown failed", {
          error: err instanceof Error ? err.message : String(err),
        });
        process.exit(1);
      });
  });
}

function intFrom(name: string, fallback: number, min = 1): number {
  const raw = process.env[name];
  if (raw === undefined || raw.trim() === "") return fallback;
  const n = Number(raw);
  if (!Number.isInteger(n) || n < min) {
    log.log("error", "invalid setting", { name, value: raw });
    process.exit(1);
  }
  return n;
}
