/**
 * The Node server: node:http around the web-standard handler (handler.ts),
 * which the Workers entry (worker.ts) uses too. This file adds what only a
 * long-running process needs: socket timeouts for slow clients, and a
 * drain on shutdown so a deploy loses no request. Run it for local
 * development, the Go integration tests, CI and self-hosting.
 */
import type { IncomingMessage, Server } from "node:http";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";

import { createHandler, DEFAULT_MAX_BODY } from "./handler.js";
import { silentLogger, type Logger } from "./log.js";

export { DEFAULT_MAX_BODY };

export interface ServiceOptions {
  /** Largest accepted request body, in bytes. Default 4 MiB. */
  maxBodyBytes?: number;
  /** How long a request may take end to end, in ms. Default 15 s. */
  requestTimeoutMs?: number;
  /**
   * The bearer token every route but GET /health requires. Unset or empty
   * serves without one, as on a private network.
   */
  token?: string;
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

export function createService(opts: ServiceOptions = {}): Service {
  const log = opts.log ?? silentLogger;
  let draining = false;
  let inFlight = 0;
  let idle: (() => void) | null = null;
  const handler = createHandler({
    maxBodyBytes: opts.maxBodyBytes ?? DEFAULT_MAX_BODY,
    token: opts.token,
    draining: () => draining,
    log,
    now: opts.now,
  });

  const server = createServer({ keepAliveTimeout: 65_000 }, (req, res) => {
    inFlight++;
    res.on("close", () => {
      inFlight--;
      if (inFlight === 0 && idle) idle();
    });
    const body = requestBody(req);
    const request = new Request(`http://localhost${req.url ?? "/"}`, {
      method: req.method,
      headers: requestHeaders(req),
      body: req.method === "GET" || req.method === "HEAD" ? null : body.stream,
      duplex: "half",
    } as RequestInit);

    handler(request)
      .then(async (response) => {
        const payload = Buffer.from(await response.arrayBuffer());
        const headers: Record<string, string | number> = {
          "content-length": payload.length,
        };
        response.headers.forEach((value, name) => (headers[name] = value));
        // An over-limit body is left unread, so the connection can't be
        // reused; and a draining server lets every connection go.
        if (response.status === 413 || draining) headers.connection = "close";
        res.writeHead(response.status, headers);
        res.end(payload);
        // Whatever the handler didn't read is discarded, so a keep-alive
        // connection is ready for its next request.
        body.discard();
      })
      .catch(() => {
        // The handler answers every error itself; this is a write that
        // failed on a socket the client already closed.
        res.destroy();
      });
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

function requestHeaders(req: IncomingMessage): Headers {
  const headers = new Headers();
  for (const [name, value] of Object.entries(req.headers)) {
    if (value === undefined) continue;
    for (const v of Array.isArray(value) ? value : [value])
      headers.append(name, v);
  }
  return headers;
}

/**
 * The request body as a web stream that reads only when the handler
 * asks (an unauthenticated or refused request is never buffered), and
 * `discard`, which drains whatever is left without closing the socket.
 */
function requestBody(req: IncomingMessage): {
  stream: ReadableStream<Uint8Array>;
  discard(): void;
} {
  let settled = false;
  const discard = () => {
    settled = true;
    req.removeAllListeners("data");
    req.resume();
  };
  const stream = new ReadableStream<Uint8Array>(
    {
      start(controller) {
        req.on("data", (chunk: Buffer) => {
          if (settled) return;
          controller.enqueue(new Uint8Array(chunk));
          req.pause();
        });
        req.on("end", () => {
          if (settled) return;
          settled = true;
          controller.close();
        });
        req.on("error", (err) => {
          if (settled) return;
          settled = true;
          controller.error(err);
        });
        req.pause();
      },
      pull() {
        req.resume();
      },
      cancel: discard,
    },
    // Nothing is read ahead: pull runs only when the handler reads.
    { highWaterMark: 0 },
  );
  return { stream, discard };
}
