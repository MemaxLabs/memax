// The daemon's HTTP client against a real node:http server.
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { Memax } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { lightFetch } from "../../src/lib/daemon/http.js";

let server: Server;
let url = "";
let handler: (
  req: import("node:http").IncomingMessage,
  res: import("node:http").ServerResponse,
  body: string,
) => void;
const sockets = new Set<string>();

beforeEach(async () => {
  sockets.clear();
  server = createServer((req, res) => {
    sockets.add(`${req.socket.remotePort}`);
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => handler(req, res, body));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});
afterEach(async () => {
  server.closeAllConnections();
  await new Promise((r) => server.close(r));
});

describe("lightFetch", () => {
  it("sends method, headers and body, and reads status, headers and text", async () => {
    handler = (req, res, body) => {
      res.writeHead(201, {
        "Content-Type": "application/json",
        "Retry-After": "7",
        "X-Echo": req.headers["x-test"],
      });
      res.end(
        JSON.stringify({
          method: req.method,
          body,
          length: req.headers["content-length"],
        }),
      );
    };
    const res = await lightFetch(`${url}/x`, {
      method: "POST",
      headers: { "X-Test": "a", "Content-Type": "application/json" },
      body: '{"é":1}',
    });
    expect(res.status).toBe(201);
    expect(res.ok).toBe(true);
    expect(res.headers.get("retry-after")).toBe("7");
    expect(res.headers.get("X-Echo")).toBe("a");
    expect(res.headers.get("missing")).toBeNull();
    expect(JSON.parse(await res.text())).toEqual({
      method: "POST",
      body: '{"é":1}',
      length: "8",
    });
  });

  it("keeps one connection alive across requests", async () => {
    handler = (_req, res) => res.end("{}");
    for (let i = 0; i < 5; i++) await (await lightFetch(`${url}/`)).text();
    expect(sockets.size).toBe(1);
  });

  it("sends again once when the server closed a kept-alive connection", async () => {
    let n = 0;
    handler = (req, res) => {
      n++;
      if (n === 2) {
        req.socket.destroy(); // dropped just as the reused socket sent
        return;
      }
      res.end('{"data":{"items":[]}}');
    };
    await (await lightFetch(`${url}/`)).text();
    const res = await lightFetch(`${url}/`);
    expect(res.status).toBe(200);
    expect(n).toBe(3);
  });

  it("aborts with the signal's reason, and fails fast when nobody listens", async () => {
    handler = () => {}; // never answers
    await expect(
      lightFetch(`${url}/`, { signal: AbortSignal.timeout(50) }),
    ).rejects.toMatchObject({ name: "TimeoutError" });
    await expect(lightFetch("http://127.0.0.1:9/")).rejects.toMatchObject({
      code: "ECONNREFUSED",
    });
    await expect(lightFetch("ftp://x/")).rejects.toThrow(
      /unsupported URL scheme/,
    );
  });

  it("carries memax-sdk's envelope and errors", async () => {
    handler = (req, res) => {
      if (req.url === "/v2/spaces") {
        res.writeHead(200, { "Content-Type": "application/json" });
        return res.end(
          JSON.stringify({ data: { items: [{ id: "s", slug: "memax-v2" }] } }),
        );
      }
      res.writeHead(429, {
        "Content-Type": "application/json",
        "Retry-After": "3",
      });
      res.end(
        JSON.stringify({
          error: { code: "rate_limited", message: "Slow down." },
        }),
      );
    };
    const memax = new Memax({
      apiUrl: url,
      apiKey: "k",
      fetch: lightFetch,
      maxRetries: 0,
    });
    expect((await memax.v2.spaces.list()).items[0].slug).toBe("memax-v2");
    await expect(memax.v2.targets.list("x")).rejects.toMatchObject({
      status: 429,
      code: "rate_limited",
      retryAfterSeconds: 3,
    });
    const down = new Memax({
      apiUrl: "http://127.0.0.1:9",
      apiKey: "k",
      fetch: lightFetch,
      maxRetries: 0,
    });
    await expect(down.v2.spaces.list()).rejects.toMatchObject({
      code: "network_error",
    });
  });
});
