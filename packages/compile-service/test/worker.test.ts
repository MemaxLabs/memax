/**
 * The Workers entry (src/worker.ts) under workerd, Cloudflare's runtime,
 * through Wrangler's local dev server: the same contract as the Node
 * server, plus what only the Worker does (it refuses to serve at all
 * without a token, since it has a public URL).
 */
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { unstable_startWorker } from "wrangler";
import { describe, expect, it } from "vitest";

import { TOKEN, contract, demoInput, type Config } from "./suite.js";

const here = dirname(fileURLToPath(import.meta.url));
const config = join(here, "../wrangler.jsonc");

type Worker = Awaited<ReturnType<typeof unstable_startWorker>>;

const workers = new Map<string, Promise<{ worker: Worker; base: string }>>();
let logs: string[] = [];

/** One workerd instance per configuration, shared by the tests. */
function startWorker(vars: Record<string, string>) {
  const key = JSON.stringify(vars);
  let started = workers.get(key);
  if (!started) {
    started = (async () => {
      const worker = await unstable_startWorker({
        config,
        bindings: Object.fromEntries(
          Object.entries(vars).map(([name, value]) => [
            name,
            name === "COMPILE_SERVICE_TOKEN"
              ? { type: "secret_text" as const, value }
              : { type: "plain_text" as const, value },
          ]),
        ),
        dev: {
          server: { hostname: "127.0.0.1", port: 0 },
          inspector: false,
          watch: false,
          persist: false,
          logLevel: "none",
          structuredLogsHandler: (log: { message: string }) =>
            logs.push(log.message),
        },
      });
      await worker.ready;
      const url = await worker.url;
      return { worker, base: url.origin };
    })();
    workers.set(key, started);
  }
  return started;
}

async function stopAll() {
  const all = await Promise.all(workers.values());
  workers.clear();
  logs = [];
  await Promise.all(all.map(({ worker }) => worker.dispose()));
}

contract(
  "workerd",
  async (cfg: Config) => {
    const vars: Record<string, string> = { COMPILE_SERVICE_TOKEN: TOKEN };
    if (cfg.maxBodyBytes) vars.MAX_BODY_BYTES = String(cfg.maxBodyBytes);
    const { base } = await startWorker(vars);
    return { base, logs: () => logs.join("\n") };
  },
  stopAll,
);

describe("workerd: the Worker", () => {
  it("refuses every route but health when it has no token", async () => {
    const { base } = await startWorker({});
    const res = await fetch(base + "/compile", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(demoInput),
    });
    expect(res.status).toBe(503);
    const body = (await res.json()) as Record<string, any>;
    expect(body.error.code).toBe("unavailable");
    expect(body.error.message).toMatch(/COMPILE_SERVICE_TOKEN/);
    expect((await fetch(base + "/health")).status).toBe(200);
  }, 60_000);
});
