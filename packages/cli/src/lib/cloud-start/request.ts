// The cloud start's one request (cloud warm-start design §3.3, §3.6), over
// node:http and node:https, with one deadline for all of it: the proxy's
// tunnel, TLS, the request and the answer. Sandboxes reach the internet
// through their egress proxy (HTTPS_PROXY), which Node's own modules don't
// read, so a CONNECT tunnel is opened by hand when one is set. It never
// throws: anything but a 2xx answer is a reason, and the caller prints
// nothing. The key goes into one header and nowhere else.
import {
  request as httpRequest,
  type ClientRequest,
  type IncomingMessage,
} from "node:http";
import { request as httpsRequest } from "node:https";
import type { Socket } from "node:net";
import { connect as tlsConnect } from "node:tls";

/** What the request came to. */
export type StartOutcome =
  | { ok: true; status: number; data: unknown }
  | { ok: false; reason: string; status?: number };

export interface StartRequest {
  url: URL;
  /** The start key; "" sends no Authorization (a proxy may attach one). */
  key: string;
  body: string;
  headers: Record<string, string>;
  deadlineMs: number;
  env: Record<string, string | undefined>;
}

const MAX_ANSWER = 2 * 1024 * 1024;

/** The proxy for url, from the environment, unless NO_PROXY spares it. */
export function proxyFor(
  url: URL,
  env: Record<string, string | undefined>,
): URL | null {
  const raw =
    url.protocol === "https:"
      ? (env.HTTPS_PROXY ?? env.https_proxy)
      : (env.HTTP_PROXY ?? env.http_proxy);
  if (!raw?.trim()) return null;
  const host = url.hostname.toLowerCase();
  const port = url.port || (url.protocol === "https:" ? "443" : "80");
  for (const entry of (env.NO_PROXY ?? env.no_proxy ?? "").split(/[\s,]+/)) {
    const e = entry.trim().toLowerCase();
    if (!e) continue;
    if (e === "*") return null;
    const [h, p] = e.startsWith("[") ? [e, undefined] : e.split(":");
    if (p && p !== port) continue;
    const bare = h.replace(/^\*?\./, "");
    if (host === bare || host.endsWith(`.${bare}`)) return null;
  }
  try {
    const p = new URL(raw.includes("://") ? raw : `http://${raw}`);
    return p.protocol === "http:" || p.protocol === "https:" ? p : null;
  } catch {
    return null;
  }
}

function proxyAuth(proxy: URL): Record<string, string> {
  if (!proxy.username) return {};
  const creds = `${decodeURIComponent(proxy.username)}:${decodeURIComponent(proxy.password)}`;
  return {
    "Proxy-Authorization": `Basic ${Buffer.from(creds).toString("base64")}`,
  };
}

/** Sends the start; resolves once, whatever happens. */
export function postStart(r: StartRequest): Promise<StartOutcome> {
  return new Promise((resolve) => {
    const open: Array<ClientRequest | Socket> = [];
    let settled = false;
    const done = (o: StartOutcome) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      for (const x of open) x.destroy();
      resolve(o);
    };
    const timer = setTimeout(
      () => done({ ok: false, reason: `no answer within ${r.deadlineMs} ms` }),
      r.deadlineMs,
    );
    const fail = (what: string) => (err: Error) =>
      done({
        ok: false,
        reason: `${what}: ${(err as NodeJS.ErrnoException).code ?? err.message}`,
      });

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
      "Content-Length": String(Buffer.byteLength(r.body)),
      ...r.headers,
    };
    if (r.key) headers.Authorization = `Bearer ${r.key}`;
    const https = r.url.protocol === "https:";
    const port = Number(r.url.port) || (https ? 443 : 80);
    const path = `${r.url.pathname}${r.url.search}`;

    const answer = (res: IncomingMessage) => {
      const chunks: Buffer[] = [];
      let size = 0;
      res.on("data", (c: Buffer) => {
        size += c.length;
        if (size > MAX_ANSWER) {
          done({ ok: false, reason: "the answer is too large" });
          return;
        }
        chunks.push(c);
      });
      res.on("error", fail("reading the answer"));
      res.on("end", () => {
        const status = res.statusCode ?? 0;
        let body: unknown;
        try {
          body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
        } catch {
          body = undefined;
        }
        const env = body as { data?: unknown; error?: { code?: unknown } };
        if (status < 200 || status >= 300) {
          const code =
            typeof env?.error?.code === "string" ? ` ${env.error.code}` : "";
          done({
            ok: false,
            status,
            reason: `the API answered ${status}${code}`,
          });
          return;
        }
        if (!env || typeof env !== "object" || !("data" in env)) {
          done({ ok: false, status, reason: "the answer isn't Memax's" });
          return;
        }
        done({ ok: true, status, data: env.data });
      });
    };
    const send = (req: ClientRequest) => {
      open.push(req);
      req.on("response", answer);
      req.on("error", fail("the request"));
      req.end(r.body);
    };

    const proxy = proxyFor(r.url, r.env);
    if (!proxy) {
      send(
        (https ? httpsRequest : httpRequest)({
          method: "POST",
          hostname: r.url.hostname,
          port,
          path,
          headers,
          agent: false,
        }),
      );
      return;
    }
    const proxyPort =
      Number(proxy.port) || (proxy.protocol === "https:" ? 443 : 80);
    if (!https) {
      // Plain HTTP through a proxy: the absolute URL, to the proxy.
      send(
        (proxy.protocol === "https:" ? httpsRequest : httpRequest)({
          method: "POST",
          hostname: proxy.hostname,
          port: proxyPort,
          path: r.url.href,
          headers: { ...headers, Host: r.url.host, ...proxyAuth(proxy) },
          agent: false,
        }),
      );
      return;
    }
    // HTTPS through a proxy: a CONNECT tunnel, then TLS to the API inside it.
    const tunnel = (proxy.protocol === "https:" ? httpsRequest : httpRequest)({
      method: "CONNECT",
      hostname: proxy.hostname,
      port: proxyPort,
      path: `${r.url.hostname}:${port}`,
      headers: { Host: `${r.url.hostname}:${port}`, ...proxyAuth(proxy) },
      agent: false,
    });
    open.push(tunnel);
    tunnel.on("error", fail("the proxy"));
    tunnel.on("connect", (res: IncomingMessage, socket: Socket) => {
      open.push(socket);
      if (res.statusCode !== 200) {
        done({ ok: false, reason: `the proxy answered ${res.statusCode}` });
        return;
      }
      const tls = tlsConnect({ socket, servername: r.url.hostname });
      open.push(tls);
      tls.on("error", fail("TLS"));
      send(
        httpsRequest({
          method: "POST",
          hostname: r.url.hostname,
          port,
          path,
          headers,
          agent: false,
          createConnection: () => tls,
        }),
      );
    });
    tunnel.end();
  });
}
