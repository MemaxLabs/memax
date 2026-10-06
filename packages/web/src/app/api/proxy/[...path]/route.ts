import { createHash, createHmac, randomBytes } from "node:crypto";
import { NextResponse } from "next/server";
import { API_URL } from "@/lib/urls";

// Web-surface signing (packages/server/internal/websurface has the full
// design and threat model). The API records a person's keep as made on
// the web (assurance human_web) only when both hold: the session was
// issued to the web app (its token's `surface` claim, set by the server
// at login), and this proxy signed the request with WEB_SURFACE_SECRET,
// which only the web deployment and the API know. A CLI login sent
// through this proxy is not signed, and the API wouldn't accept it as the
// web if it were. The secret is read once, when the server starts.
const SURFACE_SECRET = (process.env.WEB_SURFACE_SECRET ?? "").trim();
const SURFACE_VERSION = "v1";

// Request headers forwarded to the API. Anything else the browser sends,
// the X-Memax-Surface-* headers included, is dropped.
const FORWARDED_REQUEST_HEADERS = [
  "authorization",
  "content-type",
  "accept",
  "x-hub-id",
  "x-timezone",
  "idempotency-key",
  "if-match",
];

// Response headers passed back to the browser.
const FORWARDED_RESPONSE_HEADERS = [
  "content-type",
  "cache-control",
  "content-disposition",
  "x-memax-warning",
  "etag",
  "idempotent-replayed",
  "retry-after",
];

function buildUpstreamURL(req: Request, path: string[]): URL {
  const url = new URL(req.url);
  const upstream = new URL(`${API_URL}/${path.join("/")}`);
  upstream.search = url.search;
  return upstream;
}

function buildUpstreamHeaders(req: Request): Headers {
  const headers = new Headers();
  for (const name of FORWARDED_REQUEST_HEADERS) {
    const value = req.headers.get(name);
    if (value) headers.set(name, value);
  }
  return headers;
}

/**
 * The user id of a session issued to the web app, read from the bearer
 * token's payload, or null for anything else: API keys, CLI sessions,
 * agent and impersonation tokens. The proxy doesn't verify the token; the
 * API does, and it also checks that the signed user is the token's.
 */
function webSessionUserID(authorization: string | null): string | null {
  const token = authorization?.match(/^Bearer\s+(\S+)$/)?.[1];
  if (!token || token.startsWith("mxk_")) return null;
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const claims = JSON.parse(
      Buffer.from(parts[1] ?? "", "base64url").toString("utf8"),
    ) as { sub?: unknown; surface?: unknown };
    if (claims.surface !== "web") return null;
    return typeof claims.sub === "string" && claims.sub !== ""
      ? claims.sub
      : null;
  } catch {
    return null;
  }
}

/**
 * The X-Memax-Surface-* headers for one request: an HMAC-SHA256 over the
 * method, the request target (path and query, as sent), a timestamp, a
 * fresh nonce, the user id, the Idempotency-Key and If-Match headers and
 * the SHA-256 of the body. Must match websurface.Sign in the API.
 */
function surfaceHeaders(input: {
  method: string;
  target: string;
  userID: string;
  idempotencyKey: string;
  ifMatch: string;
  body: string;
}): Record<string, string> {
  const timestamp = Math.floor(Date.now() / 1000).toString();
  const nonce = randomBytes(16).toString("base64url");
  const bodyHash = createHash("sha256")
    .update(input.body, "utf8")
    .digest("hex");
  const canonical = [
    `memax-web-surface/${SURFACE_VERSION}`,
    input.method.toUpperCase(),
    input.target,
    timestamp,
    nonce,
    input.userID,
    input.idempotencyKey,
    input.ifMatch,
    bodyHash,
  ].join("\n");
  const signature = createHmac("sha256", SURFACE_SECRET)
    .update(canonical, "utf8")
    .digest("base64url");
  return {
    "x-memax-surface": "web",
    "x-memax-surface-timestamp": timestamp,
    "x-memax-surface-nonce": nonce,
    "x-memax-surface-user": input.userID,
    "x-memax-surface-signature": `${SURFACE_VERSION}=${signature}`,
  };
}

async function proxy(req: Request, path: string[]): Promise<Response> {
  try {
    const upstreamURL = buildUpstreamURL(req, path);
    const headers = buildUpstreamHeaders(req);
    const body =
      req.method === "GET" || req.method === "HEAD"
        ? undefined
        : await req.text();

    // Only /v2 reads the signature, and only a web session gets one.
    const userID =
      SURFACE_SECRET && path[0] === "v2"
        ? webSessionUserID(headers.get("authorization"))
        : null;
    if (userID) {
      const signed = surfaceHeaders({
        method: req.method,
        target: upstreamURL.pathname + upstreamURL.search,
        userID,
        idempotencyKey: headers.get("idempotency-key") ?? "",
        ifMatch: headers.get("if-match") ?? "",
        body: body ?? "",
      });
      for (const [name, value] of Object.entries(signed)) {
        headers.set(name, value);
      }
    }

    const upstream = await fetch(upstreamURL.toString(), {
      method: req.method,
      headers,
      body,
      cache: "no-store",
    });

    const responseHeaders = new Headers();
    for (const name of FORWARDED_RESPONSE_HEADERS) {
      const value = upstream.headers.get(name);
      if (value) responseHeaders.set(name, value);
    }

    return new Response(upstream.body, {
      status: upstream.status,
      headers: responseHeaders,
    });
  } catch {
    return NextResponse.json(
      {
        error: {
          code: "network_error",
          message: "Could not reach memax API.",
        },
      },
      { status: 502 },
    );
  }
}

export async function GET(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function POST(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function PATCH(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function DELETE(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}
