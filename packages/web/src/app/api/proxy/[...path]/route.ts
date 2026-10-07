import { createHash, createHmac, randomBytes } from "node:crypto";
import { NextResponse } from "next/server";
import { webSessionUser } from "@/lib/bff/claims";
import { withCookies } from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import {
  endSession,
  ensureAccess,
  errorResponse,
  readSession,
  revokeToken,
  surfaceSecret,
  type Session,
} from "@/lib/bff/session";
import { API_URL } from "@/lib/urls";

// The browser's way to the API, and the only holder of its session (a
// backend-for-frontend; packages/server/internal/websurface has the design
// and threat model). The access token is read from its HttpOnly cookie
// here, never from the browser: an Authorization header the page sends is
// dropped. When the token is missing or about to expire the proxy
// refreshes it (single-flight, lib/bff/session.ts), and once more if the
// API refuses it; the new cookies ride on the response.
//
// Web-surface signing: the API records a person's keep as made on the web
// (assurance human_web) only when both hold: the session was issued to the
// web app (its token's `surface` claim, set by the server at login), and
// this proxy signed the request with WEB_SURFACE_SECRET, which only the web
// deployment and the API know. A CLI token in a forged cookie is not
// signed, and the API wouldn't accept it as the web if it were.
const SURFACE_VERSION = "v1";

// Request headers forwarded to the API. Anything else the browser sends
// (Authorization, cookies, the X-Memax-Surface-* and X-Memax-Client-*
// headers) is dropped. X-Memax-Passkey is a passkey's answer to the
// re-check the API asked for (403 needs_passkey): self-authenticating
// (signed by the authenticator over a challenge bound to the person, the
// session and this exact request), so the proxy passes it through as is
// and the surface signature doesn't need to cover it.
const FORWARDED_REQUEST_HEADERS = [
  "content-type",
  "accept",
  "x-hub-id",
  "x-timezone",
  "idempotency-key",
  "if-match",
  "x-memax-passkey",
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
  "x-memax-export-receipt",
];

// The API's token endpoints. Page script has no tokens to send them, and
// must never be handed one: signing in, refreshing, impersonating and
// signing out go through /api/auth/*, and the OAuth server is for the CLI
// and MCP clients, which call the API directly.
const BLOCKED = [
  /^v1\/auth\/(refresh|exchange|impersonate)(\/|$)/,
  /^oauth\/(token|revoke|device_authorization|register)(\/|$)/,
];

// The email code's verify answers tokens when the code was requested
// without a redirect (the CLI's way). The web app always asks with one, so
// it gets a one-time code to exchange (/api/auth/exchange); tokens that
// come back anyway are signed out, never handed to the page.
const EMAIL_VERIFY = "v1/auth/email/verify";

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
 * The X-Memax-Surface-* headers for one request: an HMAC-SHA256 over the
 * method, the request target (path and query, as sent), a timestamp, a
 * fresh nonce, the user id, the Idempotency-Key and If-Match headers and
 * the SHA-256 of the body. Must match websurface.Sign in the API.
 */
function surfaceHeaders(input: {
  secret: string;
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
  const signature = createHmac("sha256", input.secret)
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

/** Sends the request upstream with the session's current access token. */
async function send(
  req: Request,
  path: string[],
  session: Session,
  body: string | undefined,
): Promise<Response> {
  const upstreamURL = buildUpstreamURL(req, path);
  const headers = buildUpstreamHeaders(req);
  if (session.access) {
    headers.set("authorization", `Bearer ${session.access}`);
  }
  // Only /v2 reads the signature, and only a web session gets one.
  const secret = surfaceSecret();
  const userID =
    secret && path[0] === "v2" ? webSessionUser(session.access) : null;
  if (userID) {
    const signed = surfaceHeaders({
      secret,
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
  // The browser going away aborts the upstream call too, so a stream
  // (Ask) stops on the API, and its model call with it.
  return fetch(upstreamURL.toString(), {
    method: req.method,
    headers,
    body,
    cache: "no-store",
    signal: req.signal,
  });
}

function passBack(upstream: Response, body?: BodyInit | null): Response {
  const responseHeaders = new Headers();
  for (const name of FORWARDED_RESPONSE_HEADERS) {
    const value = upstream.headers.get(name);
    if (value) responseHeaders.set(name, value);
  }
  return new Response(body === undefined ? upstream.body : body, {
    status: upstream.status,
    headers: responseHeaders,
  });
}

/**
 * Tokens the email code's verify answered (a request made without a
 * redirect) are signed out and kept from the page.
 */
async function scrubTokens(
  req: Request,
  upstream: Response,
): Promise<Response> {
  const text = await upstream.text();
  let refresh: string | undefined;
  try {
    const parsed = JSON.parse(text) as {
      data?: { access_token?: unknown; refresh_token?: unknown };
    };
    if (parsed.data?.access_token || parsed.data?.refresh_token) {
      refresh =
        typeof parsed.data.refresh_token === "string"
          ? parsed.data.refresh_token
          : "";
    }
  } catch {
    // Not JSON: nothing to scrub.
  }
  if (refresh === undefined) return passBack(upstream, text);
  await revokeToken(req, refresh);
  return errorResponse(
    400,
    "redirect_required",
    "Ask for a new code from this page to sign in.",
  );
}

async function proxy(req: Request, path: string[]): Promise<Response> {
  const refused = csrfRefusal(req);
  if (refused) return refused;
  const joined = path.join("/");
  if (BLOCKED.some((re) => re.test(joined))) {
    return errorResponse(
      404,
      "not_found",
      "The web app signs in and out through /api/auth, not the API proxy.",
    );
  }
  const session = readSession(req);
  try {
    const body =
      req.method === "GET" || req.method === "HEAD"
        ? undefined
        : await req.text();

    const state = await ensureAccess(session, req);
    if (state === "unavailable" && !session.access) {
      return errorResponse(502, "network_error", "Could not reach memax API.");
    }
    let upstream = await send(req, path, session, body);
    // The API refused the token (expired early, its secret changed):
    // refresh once and try again. Still refused with a fresh token, the
    // session is over.
    if (upstream.status === 401 && session.refresh) {
      const retry = await ensureAccess(session, req, { force: true });
      if (retry === "ok") {
        await upstream.body?.cancel();
        upstream = await send(req, path, session, body);
        if (upstream.status === 401) endSession(session);
      }
    }
    const res =
      joined === EMAIL_VERIFY && upstream.ok
        ? await scrubTokens(req, upstream)
        : passBack(upstream);
    return withCookies(res, session.setCookies);
  } catch {
    return withCookies(
      NextResponse.json(
        {
          error: {
            code: "network_error",
            message: "Could not reach memax API.",
          },
        },
        { status: 502 },
      ),
      session.setCookies,
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
