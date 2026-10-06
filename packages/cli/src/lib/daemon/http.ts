// The daemon's HTTP client: the slice of `fetch` memax-sdk uses, over
// node:http and node:https with one long-lived keep-alive connection.
//
// Why not the built-in fetch: undici parses HTTP in a WebAssembly module
// and keeps an idle connection for only 4 s, so a daemon that polls every
// 5 s would carry that module and pay a TLS handshake on every poll. This
// keeps the connection for a minute and uses Node's native parser.
import {
  Agent as HttpAgent,
  request as httpRequest,
  type IncomingMessage,
} from "node:http";
import { Agent as HttpsAgent, request as httpsRequest } from "node:https";

const KEEP_ALIVE_MS = 60_000;
const MAX_BODY = 16 * 1024 * 1024;

const agents = {
  "http:": new HttpAgent({
    keepAlive: true,
    maxSockets: 4,
    timeout: KEEP_ALIVE_MS,
  }),
  "https:": new HttpsAgent({
    keepAlive: true,
    maxSockets: 4,
    timeout: KEEP_ALIVE_MS,
  }),
};

/** What memax-sdk reads from a response. */
class LightResponse {
  constructor(
    readonly status: number,
    private readonly raw: IncomingMessage["headers"],
    private readonly body: string,
  ) {}
  get ok(): boolean {
    return this.status >= 200 && this.status < 300;
  }
  readonly headers = {
    get: (name: string): string | null => {
      const v = this.raw[name.toLowerCase()];
      return v === undefined ? null : Array.isArray(v) ? v.join(", ") : v;
    },
  };
  async text(): Promise<string> {
    return this.body;
  }
  async json(): Promise<unknown> {
    return JSON.parse(this.body);
  }
}

function abortError(signal: AbortSignal): Error {
  const reason = signal.reason as Error | undefined;
  if (
    reason instanceof Error &&
    (reason.name === "AbortError" || reason.name === "TimeoutError")
  )
    return reason;
  const err = new Error("The operation was aborted");
  err.name = "AbortError";
  return err;
}

export const lightFetch = ((
  input: string | URL,
  init: RequestInit = {},
): Promise<LightResponse> => {
  const url = new URL(String(input));
  const send =
    url.protocol === "https:"
      ? httpsRequest
      : url.protocol === "http:"
        ? httpRequest
        : null;
  if (!send)
    return Promise.reject(
      new TypeError(`unsupported URL scheme: ${url.protocol}`),
    );
  const signal = init.signal ?? undefined;
  if (signal?.aborted) return Promise.reject(abortError(signal));
  // memax-sdk sends a plain object and a string body. (The global Headers
  // class would load undici, which is what this avoids.)
  if (
    init.body !== undefined &&
    init.body !== null &&
    typeof init.body !== "string"
  ) {
    return Promise.reject(new TypeError("lightFetch sends string bodies only"));
  }
  const body = init.body ?? undefined;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(
    (init.headers ?? {}) as Record<string, string>,
  ))
    out[k.toLowerCase()] = String(v);
  if (body !== undefined)
    out["content-length"] = String(Buffer.byteLength(body));
  const agent = agents[url.protocol as "http:" | "https:"];
  const attempt = (retry: boolean): Promise<LightResponse> =>
    once(send, url, init.method ?? "GET", out, body, agent, signal).catch(
      (err: { reused?: boolean }) => {
        // The server closed a kept-alive connection just as it was reused:
        // send once more on a fresh one (commands carry Idempotency-Keys).
        if (retry && err.reused && !signal?.aborted) return attempt(false);
        throw err;
      },
    );
  return attempt(true);
}) as unknown as typeof globalThis.fetch;

function once(
  send: typeof httpRequest,
  url: URL,
  method: string,
  headers: Record<string, string>,
  body: string | undefined,
  agent: HttpAgent,
  signal: AbortSignal | undefined,
): Promise<LightResponse> {
  return new Promise((resolve, reject) => {
    const req = send(url, { method, headers, agent }, (res) => {
      const chunks: Buffer[] = [];
      let size = 0;
      res.on("data", (c: Buffer) => {
        size += c.length;
        if (size > MAX_BODY) {
          req.destroy(new Error("response too large"));
          return;
        }
        chunks.push(c);
      });
      res.on("end", () =>
        resolve(
          new LightResponse(
            res.statusCode ?? 0,
            res.headers,
            Buffer.concat(chunks).toString("utf8"),
          ),
        ),
      );
      res.on("error", reject);
    });
    const onAbort = () => req.destroy(abortError(signal!));
    signal?.addEventListener("abort", onAbort, { once: true });
    req.on("close", () => signal?.removeEventListener("abort", onAbort));
    req.on("error", (err: Error & { reused?: boolean }) => {
      if (signal?.aborted) return reject(abortError(signal));
      err.reused =
        req.reusedSocket &&
        (err as NodeJS.ErrnoException).code === "ECONNRESET";
      reject(err);
    });
    req.end(body);
  });
}
