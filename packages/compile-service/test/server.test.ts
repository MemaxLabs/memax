import { request } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import { toPointer } from "../src/errors.js";
import { createHandler } from "../src/handler.js";
import { jsonLogger, type Logger } from "../src/log.js";
import { createService, type Service } from "../src/server.js";
import { TOKEN, contract, demoInput, post, type Config } from "./suite.js";

function memoryLogger(): Logger & { text(): string } {
  const lines: string[] = [];
  const logger = jsonLogger((line) => lines.push(line));
  return { log: logger.log, text: () => lines.join("") };
}

let running: Service[] = [];

async function start(opts: Parameters<typeof createService>[0] = {}) {
  const log = memoryLogger();
  const service = createService({ log, ...opts });
  const addr = await service.listen(0, "127.0.0.1");
  running.push(service);
  return { service, logs: log.text, base: `http://127.0.0.1:${addr.port}` };
}

async function stopAll() {
  await Promise.all(running.map((s) => s.close(100)));
  running = [];
}

// The shared contract, against node:http with a token.
contract(
  "node",
  (config: Config) => start({ token: TOKEN, ...config }),
  stopAll,
);

describe("node: the server", () => {
  afterEach(stopAll);

  it("serves without a token when none is set, as on a private network", async () => {
    const { base } = await start();
    const res = await fetch(base + "/compile", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(demoInput),
    });
    expect(res.status).toBe(200);
  });

  it("reuses a keep-alive connection after refusing a body unread", async () => {
    const { base } = await start({ token: TOKEN });
    const url = new URL(base);
    const statuses: number[] = [];
    const agent = new (await import("node:http")).Agent({
      keepAlive: true,
      maxSockets: 1,
    });
    const send = (auth: boolean) =>
      new Promise<void>((resolve, reject) => {
        const body = JSON.stringify(demoInput);
        const req = request(
          {
            host: url.hostname,
            port: url.port,
            path: "/compile",
            method: "POST",
            agent,
            headers: {
              "content-type": "application/json",
              "content-length": Buffer.byteLength(body),
              ...(auth ? { authorization: `Bearer ${TOKEN}` } : {}),
            },
          },
          (res) => {
            statuses.push(res.statusCode ?? 0);
            res.resume();
            res.on("end", resolve);
          },
        );
        req.on("error", reject);
        req.end(body);
      });
    await send(false);
    await send(true);
    agent.destroy();
    expect(statuses).toEqual([401, 200]);
  });

  it("finishes in-flight requests on close, and answers 503 while draining", async () => {
    const { base, service } = await start({ token: TOKEN });
    const url = new URL(base + "/compile");
    const slow = new Promise<number>((resolve, reject) => {
      const req = request(
        {
          host: url.hostname,
          port: url.port,
          path: url.pathname,
          method: "POST",
          headers: {
            "content-type": "application/json",
            authorization: `Bearer ${TOKEN}`,
          },
        },
        (res) => {
          res.resume();
          resolve(res.statusCode ?? 0);
        },
      );
      req.on("error", reject);
      const body = JSON.stringify(demoInput);
      req.write(body.slice(0, 10));
      setTimeout(() => req.end(body.slice(10)), 150);
    });
    await new Promise((r) => setTimeout(r, 50));
    const closing = service.close(5_000);
    expect(service.draining()).toBe(true);
    expect(await slow).toBe(200);
    await closing;
    await expect(fetch(base + "/health")).rejects.toThrow();
    running = running.filter((s) => s !== service);
  });

  it("answers a request that arrives while draining with 503", async () => {
    let draining = false;
    const handler = createHandler({ token: TOKEN, draining: () => draining });
    draining = true;
    const res = await handler(
      new Request("http://localhost/compile", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          authorization: `Bearer ${TOKEN}`,
        },
        body: "{}",
      }),
    );
    expect(res.status).toBe(503);
    const health = await handler(new Request("http://localhost/health"));
    expect(health.status).toBe(503);
    expect(((await health.json()) as { status: string }).status).toBe(
      "draining",
    );
  });

  it("checks the token before the body: an unauthenticated request is never read", async () => {
    const { base, logs } = await start({ token: TOKEN });
    const res = await post(base, "/compile", demoInput, {
      authorization: "Bearer nope",
      "x-request-id": "req-unread",
    });
    expect(res.status).toBe(401);
    const line = logs()
      .split("\n")
      .find((l) => l.includes("req-unread"));
    expect(JSON.parse(line ?? "{}")).toMatchObject({
      status: 401,
      bytes_in: 0,
    });
  });
});

describe("toPointer", () => {
  it.each([
    ["input", ""],
    ["version", "/version"],
    ["memories[3].state", "/memories/3/state"],
    [
      "brief.sections[0].items[2].cites[1]",
      "/brief/sections/0/items/2/cites/1",
    ],
    ["targets", "/targets"],
  ])("%s → %s", (path, pointer) => {
    expect(toPointer(path)).toBe(pointer);
  });
});
