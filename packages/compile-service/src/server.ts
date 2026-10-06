/**
 * The HTTP server: node:http and nothing else. It is reachable only on
 * Memax's private network (Fly's 6PN, `memax-compile*.internal`), so it has
 * no auth of its own; what it guards against is malformed and oversized
 * input, slow clients, and losing requests on deploy.
 */
import { randomUUID } from "node:crypto";
import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";

import { HttpError, type ErrorBody } from "./errors.js";
import { silentLogger, type Logger } from "./log.js";
import { compileRoute, healthRoute, parseBackRoute } from "./routes.js";

export interface ServiceOptions {
  /** Largest accepted request body, in bytes. Default 4 MiB. */
  maxBodyBytes?: number;
  /** How long a request may take end to end, in ms. Default 15 s. */
  requestTimeoutMs?: number;
  log?: Logger;
  now?: () => number;
}

export interface Service {
  server: Server;
  /** Starts listening; resolves with the bound address. */
  listen(port: number, host?: string): Promise<AddressInfo>;
  /**
   * Stops taking requests, answers health with 503, lets in-flight
   * requests finish for up to `graceMs`, then closes every connection.
   */
  close(graceMs?: number): Promise<void>;
  draining(): boolean;
}

export const DEFAULT_MAX_BODY = 4 << 20;

type Route = (body: unknown) => unknown;

const POST_ROUTES: Record<string, Route> = {
  "/compile": compileRoute,
  "/parse-back": parseBackRoute,
};

export function createService(opts: ServiceOptions = {}): Service {
  const maxBody = opts.maxBodyBytes ?? DEFAULT_MAX_BODY;
  const log = opts.log ?? silentLogger;
  const now = opts.now ?? Date.now;
  const startedAt = now();
  let draining = false;
  let inFlight = 0;
  let idle: (() => void) | null = null;

  const server = createServer({ keepAliveTimeout: 65_000 }, (req, res) => {
    inFlight++;
    const started = now();
    const requestId = headerValue(req, "x-request-id") ?? randomUUID();
    res.setHeader("x-request-id", requestId);
    let bytesIn = 0;

    const finish = (status: number, body: unknown, close = false) => {
      const payload = JSON.stringify(body);
      res.writeHead(status, {
        "content-type": "application/json; charset=utf-8",
        "content-length": Buffer.byteLength(payload),
        ...(close || draining ? { connection: "close" } : {}),
      });
      res.end(payload);
      log.log(
        status >= 500 ? "error" : status >= 400 ? "warn" : "info",
        "request",
        {
          request_id: requestId,
          method: req.method ?? "",
          path: path(req),
          status,
          duration_ms: now() - started,
          bytes_in: bytesIn,
          bytes_out: Buffer.byteLength(payload),
        },
      );
    };
    res.on("close", () => {
      inFlight--;
      if (inFlight === 0 && idle) idle();
    });

    handle(req)
      .then(({ status, body }) => finish(status, body))
      .catch((err: unknown) => {
        if (err instanceof HttpError) {
          finish(err.status, err.body(), err.status === 413);
          return;
        }
        log.log("error", "unhandled error", {
          request_id: requestId,
          path: path(req),
          error: err instanceof Error ? err.name : "unknown",
        });
        const body: ErrorBody = {
          error: {
            code: "internal_error",
            message: "The compile service failed. Try again.",
          },
        };
        finish(500, body);
      });

    async function handle(
      req: IncomingMessage,
    ): Promise<{ status: number; body: unknown }> {
      const p = path(req);
      if (p === "/health") {
        if (req.method !== "GET" && req.method !== "HEAD")
          throw methodNotAllowed("GET");
        return {
          status: draining ? 503 : 200,
          body: healthRoute(draining, startedAt, now()),
        };
      }
      const route = POST_ROUTES[p];
      if (!route) {
        throw new HttpError(
          404,
          "not_found",
          `There is no ${p} here. Use POST /compile, POST /parse-back or GET /health.`,
        );
      }
      if (req.method !== "POST") throw methodNotAllowed("POST");
      if (draining) {
        throw new HttpError(
          503,
          "unavailable",
          "The compile service is shutting down. Retry on another machine.",
        );
      }
      const type = (headerValue(req, "content-type") ?? "")
        .split(";")[0]
        .trim()
        .toLowerCase();
      if (type !== "application/json") {
        throw new HttpError(
          415,
          "unsupported_media_type",
          "Send the body as application/json.",
        );
      }
      const raw = await readBody(req, maxBody, (n) => (bytesIn = n));
      let body: unknown;
      try {
        body = JSON.parse(raw);
      } catch (err) {
        throw new HttpError(400, "invalid_json", "The body isn't valid JSON.", [
          {
            instancePath: "",
            message: err instanceof Error ? err.message : "invalid JSON",
          },
        ]);
      }
      return { status: 200, body: route(body) };
    }

    function methodNotAllowed(allow: string): HttpError {
      res.setHeader("allow", allow);
      return new HttpError(
        405,
        "method_not_allowed",
        `Use ${allow} ${path(req)}.`,
      );
    }
  });

  server.requestTimeout = opts.requestTimeoutMs ?? 15_000;
  server.headersTimeout = Math.min(server.requestTimeout, 10_000);

  return {
    server,
    draining: () => draining,
    listen(port, host) {
      return new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, host, () => {
          server.off("error", reject);
          resolve(server.address() as AddressInfo);
        });
      });
    },
    async close(graceMs = 10_000) {
      draining = true;
      const closed = new Promise<void>((resolve) =>
        server.close(() => resolve()),
      );
      server.closeIdleConnections();
      if (inFlight > 0) {
        await Promise.race([
          new Promise<void>((resolve) => (idle = resolve)),
          new Promise<void>((resolve) => setTimeout(resolve, graceMs).unref()),
        ]);
      }
      server.closeAllConnections();
      await closed;
    },
  };
}

/** Reads a body up to `max` bytes; a larger one is 413. */
function readBody(
  req: IncomingMessage,
  max: number,
  progress: (n: number) => void,
): Promise<string> {
  const declared = Number(headerValue(req, "content-length") ?? "NaN");
  if (Number.isFinite(declared) && declared > max) {
    req.resume();
    return Promise.reject(tooLarge(max));
  }
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let size = 0;
    req.on("data", (chunk: Buffer) => {
      size += chunk.length;
      progress(size);
      if (size > max) {
        req.removeAllListeners("data");
        req.resume();
        reject(tooLarge(max));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function tooLarge(max: number): HttpError {
  return new HttpError(413, "too_large", `The body is over ${max} bytes.`);
}

function path(req: IncomingMessage): string {
  const url = req.url ?? "/";
  const q = url.indexOf("?");
  return q < 0 ? url : url.slice(0, q);
}

function headerValue(req: IncomingMessage, name: string): string | undefined {
  const v = req.headers[name];
  return Array.isArray(v) ? v[0] : v;
}
