/**
 * The compile service as one web-standard function, `Request → Response`.
 * The Node server (server.ts, node:http) and the Workers entry (worker.ts)
 * both call it, so they answer every request identically.
 *
 * It guards against malformed and oversized input and, when a token is
 * set, against callers without it: every route but GET /health then needs
 * `Authorization: Bearer <COMPILE_SERVICE_TOKEN>`, checked before the body
 * is read and compared in constant time. On Cloudflare the service has a
 * public URL, so the Worker refuses to serve without a token at all.
 */
import { HttpError, type ErrorBody } from "./errors.js";
import { silentLogger, type Logger } from "./log.js";
import { compileRoute, healthRoute, parseBackRoute } from "./routes.js";

export const DEFAULT_MAX_BODY = 4 << 20;

export interface HandlerOptions {
  /** Largest accepted request body, in bytes. Default 4 MiB. */
  maxBodyBytes?: number;
  /**
   * The bearer token every route but GET /health requires. Empty or unset
   * means none (the private network, local development, the Go tests).
   */
  token?: string;
  /**
   * Refuse every route but GET /health when no token is set, instead of
   * serving without one. The Worker sets this: it has a public URL.
   */
  requireToken?: boolean;
  /** Whether the service is shutting down (the Node server's drain). */
  draining?: () => boolean;
  log?: Logger;
  now?: () => number;
}

export type Handler = (request: Request) => Promise<Response>;

type Route = (body: unknown) => unknown;

const POST_ROUTES: Record<string, Route> = {
  "/compile": compileRoute,
  "/parse-back": parseBackRoute,
};

const encoder = new TextEncoder();

export function createHandler(opts: HandlerOptions = {}): Handler {
  const maxBody = opts.maxBodyBytes ?? DEFAULT_MAX_BODY;
  const log = opts.log ?? silentLogger;
  const now = opts.now ?? Date.now;
  const draining = opts.draining ?? (() => false);
  const token = opts.token?.trim() ?? "";
  const startedAt = now();
  // The token's digest, computed once: requests compare digests, so the
  // comparison takes the same time whatever the token or the guess.
  let tokenDigest: Promise<Uint8Array> | null = null;

  return async (request) => {
    const started = now();
    const requestId = request.headers.get("x-request-id") ?? randomId();
    const path = pathOf(request.url);
    const method = request.method.toUpperCase();
    const extra: Record<string, string> = {};
    let bytesIn = 0;
    let status: number;
    let body: unknown;

    try {
      ({ status, body } = await handle());
    } catch (err) {
      if (err instanceof HttpError) {
        status = err.status;
        body = err.body();
      } else {
        log.log("error", "unhandled error", {
          request_id: requestId,
          path,
          error: err instanceof Error ? err.name : "unknown",
        });
        status = 500;
        body = {
          error: {
            code: "internal_error",
            message: "The compile service failed. Try again.",
          },
        } satisfies ErrorBody;
      }
    }

    const payload = JSON.stringify(body);
    const bytesOut = encoder.encode(payload).length;
    // Never a header (the token is one) and never a body: compile inputs
    // hold memory statements, and parse-back requests hold whole files.
    log.log(
      status >= 500 ? "error" : status >= 400 ? "warn" : "info",
      "request",
      {
        request_id: requestId,
        method,
        path,
        status,
        duration_ms: now() - started,
        bytes_in: bytesIn,
        bytes_out: bytesOut,
      },
    );
    // A HEAD answer keeps its body here; both runtimes drop it on the wire.
    return new Response(payload, {
      status,
      headers: {
        "content-type": "application/json; charset=utf-8",
        "x-request-id": requestId,
        ...extra,
      },
    });

    async function handle(): Promise<{ status: number; body: unknown }> {
      if (path === "/health") {
        if (method !== "GET" && method !== "HEAD")
          throw methodNotAllowed("GET");
        return {
          status: draining() ? 503 : 200,
          body: healthRoute(draining(), startedAt, now()),
        };
      }
      await authorize();
      const route = POST_ROUTES[path];
      if (!route) {
        throw new HttpError(
          404,
          "not_found",
          `There is no ${path} here. Use POST /compile, POST /parse-back or GET /health.`,
        );
      }
      if (method !== "POST") throw methodNotAllowed("POST");
      if (draining()) {
        throw new HttpError(
          503,
          "unavailable",
          "The compile service is shutting down. Retry on another machine.",
        );
      }
      const type = (request.headers.get("content-type") ?? "")
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
      const raw = await readBody(request, maxBody, (n) => (bytesIn = n));
      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch (err) {
        throw new HttpError(400, "invalid_json", "The body isn't valid JSON.", [
          {
            instancePath: "",
            message: err instanceof Error ? err.message : "invalid JSON",
          },
        ]);
      }
      return { status: 200, body: route(parsed) };
    }

    async function authorize(): Promise<void> {
      if (token === "") {
        if (!opts.requireToken) return;
        throw new HttpError(
          503,
          "unavailable",
          "The compile service has no COMPILE_SERVICE_TOKEN set, so it refuses every request but GET /health.",
        );
      }
      tokenDigest ??= sha256(token);
      const presented = bearer(request.headers.get("authorization"));
      const [want, got] = await Promise.all([tokenDigest, sha256(presented)]);
      let diff = presented === "" ? 1 : 0;
      for (let i = 0; i < want.length; i++) diff |= want[i] ^ got[i];
      if (diff !== 0) {
        extra["www-authenticate"] = 'Bearer realm="memax-compile"';
        throw new HttpError(
          401,
          "unauthorized",
          "Send the compile service token as Authorization: Bearer <token>.",
        );
      }
    }

    function methodNotAllowed(allow: string): HttpError {
      extra.allow = allow;
      return new HttpError(405, "method_not_allowed", `Use ${allow} ${path}.`);
    }
  };
}

/**
 * Reads a body up to `max` bytes; a larger one is 413, whether its
 * Content-Length says so up front or it streams past the limit.
 */
async function readBody(
  request: Request,
  max: number,
  progress: (n: number) => void,
): Promise<string> {
  const declared = Number(request.headers.get("content-length") ?? "NaN");
  if (Number.isFinite(declared) && declared > max) throw tooLarge(max);
  if (!request.body) return "";
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    progress(size);
    if (size > max) {
      await discard(reader, max);
      throw tooLarge(max);
    }
    chunks.push(value);
  }
  const all = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    all.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(all);
}

/**
 * Reads what is left of an over-limit body and throws it away, up to
 * three more limits' worth, then cancels: a caller (it has the token)
 * that streamed a little too much gets its 413 on a connection it can
 * reuse, and one that keeps streaming is cut off. Cancelling at once
 * would be cheaper, but a cancelled body leaves the connection behind it
 * unusable (workerd's dev proxy then fails the next request).
 */
async function discard(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  max: number,
): Promise<void> {
  let left = 3 * max;
  while (left > 0) {
    const { done, value } = await reader.read();
    if (done) return;
    left -= value.byteLength;
  }
  await reader.cancel().catch(() => {});
}

function tooLarge(max: number): HttpError {
  return new HttpError(413, "too_large", `The body is over ${max} bytes.`);
}

/** The path of a request URL, without its query. */
function pathOf(url: string): string {
  try {
    return new URL(url).pathname;
  } catch {
    return "/";
  }
}

/** The token in an `Authorization: Bearer <token>` header, or "". */
function bearer(header: string | null): string {
  return /^Bearer +(\S+) *$/i.exec(header ?? "")?.[1] ?? "";
}

async function sha256(text: string): Promise<Uint8Array> {
  return new Uint8Array(
    await crypto.subtle.digest("SHA-256", encoder.encode(text)),
  );
}

function randomId(): string {
  return crypto.randomUUID();
}
