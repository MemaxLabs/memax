import { createHash } from "node:crypto";
import { Memax, MemaxError, type AuthTokenPair } from "memax-sdk";
import { API_URL } from "@/lib/urls";
import { usable } from "./claims";
import { clientInfoHeaders } from "./client-info";
import {
  clearedSessionCookies,
  cookieNames,
  isSecureRequest,
  readCookies,
  sessionCookies,
  type CookieNames,
} from "./cookies";

/**
 * The web session as the web app's server holds it (a BFF). The tokens
 * live in HttpOnly cookies (lib/bff/cookies.ts); this reads them, refreshes
 * the access token when it is missing or about to expire, and says which
 * cookies the response must set or clear. Refresh tokens rotate on every
 * use (internal/sessions in the API), so a refresh is single-flight per
 * refresh token in each server process; refreshes that race across
 * processes (other Workers isolates, other tabs) are answered with the same
 * next token by the API's grace window.
 */

/** WEB_SURFACE_SECRET: signs /v2 requests and where a browser is. */
export function surfaceSecret(): string {
  return (process.env.WEB_SURFACE_SECRET ?? "").trim();
}

export interface Session {
  secure: boolean;
  names: CookieNames;
  access?: string;
  refresh?: string;
  /** The operator's own session, waiting while they impersonate someone. */
  originalRefresh?: string;
  /** Set-Cookie values the response must carry. */
  setCookies: string[];
  /** The refresh token was refused: the session is over, its cookies cleared. */
  ended: boolean;
}

export function readSession(req: Request): Session {
  const secure = isSecureRequest(req);
  const names = cookieNames(secure);
  const jar = readCookies(req);
  return {
    secure,
    names,
    access: jar.get(names.access) || undefined,
    refresh: jar.get(names.refresh) || undefined,
    originalRefresh: jar.get(names.originalRefresh) || undefined,
    setCookies: [],
    ended: false,
  };
}

/**
 * The SDK on the API, for one request. Sign-ins and refreshes carry where
 * the browser is (clientInfo), signed, for the sessions list.
 */
export function apiClient(
  req: Request,
  opts: { clientInfo?: boolean; access?: string } = {},
): Memax {
  const headers = opts.clientInfo
    ? clientInfoHeaders(req, surfaceSecret())
    : {};
  const access = opts.access;
  return new Memax({
    apiUrl: API_URL,
    maxRetries: 0,
    // The web server answers the browser at once; it never waits out a 429.
    rateLimitRetries: 0,
    headers,
    auth: access
      ? async () => ({ Authorization: `Bearer ${access}` })
      : undefined,
  });
}

export type RefreshOutcome =
  | { ok: true; tokens: AuthTokenPair }
  | {
      ok: false;
      ended: boolean;
      status: number;
      code: string;
      message: string;
    };

/** How long a refresh's answer is shared with requests that arrive after it. */
const SHARE_MS = 10_000;
const inflight = new Map<
  string,
  { at: number; promise: Promise<RefreshOutcome> }
>();

/**
 * Trades the refresh token for a new pair, once per token per process: a
 * second request with the same token, while the first is in flight or just
 * after, gets the same answer instead of presenting the token again.
 */
export function refreshTokens(
  refresh: string,
  req: Request,
): Promise<RefreshOutcome> {
  const key = createHash("sha256").update(refresh).digest("base64url");
  const now = Date.now();
  const hit = inflight.get(key);
  if (hit && now - hit.at < SHARE_MS) return hit.promise;
  if (inflight.size > 1000) {
    for (const [k, v] of inflight) {
      if (now - v.at >= SHARE_MS) inflight.delete(k);
    }
  }
  const promise = (async (): Promise<RefreshOutcome> => {
    try {
      const tokens = await apiClient(req, { clientInfo: true }).auth.refresh(
        refresh,
      );
      if (!tokens?.access_token || !tokens.refresh_token) {
        return {
          ok: false,
          ended: false,
          status: 502,
          code: "invalid_response",
          message: "The API answered a refresh without tokens.",
        };
      }
      return { ok: true, tokens };
    } catch (err) {
      if (err instanceof MemaxError && err.status > 0) {
        // 400 and 401: the token is no good (signed out, expired, reused).
        const ended = err.status === 400 || err.status === 401;
        return {
          ok: false,
          ended,
          status: err.status,
          code: err.code,
          message: err.message,
        };
      }
      return {
        ok: false,
        ended: false,
        status: 502,
        code: "network_error",
        message: "Could not reach memax API.",
      };
    }
  })();
  inflight.set(key, { at: now, promise });
  // A failure that didn't end the session (the API unreachable) may be
  // tried again at once.
  void promise.then((r) => {
    if (!r.ok && !r.ended) inflight.delete(key);
  });
  return promise;
}

/** Forgets every shared refresh (tests). */
export function resetRefreshes(): void {
  inflight.clear();
}

export type AccessState = "ok" | "none" | "ended" | "unavailable";

/**
 * Makes sure the session has an access token that works: the cookie's
 * while it has more than 30 seconds left (unless `force`, after the API
 * refused it), else a refreshed one. "none": no session; "ended": the
 * refresh token was refused, and the cookies are being cleared;
 * "unavailable": the API couldn't be reached, and the cookies stay.
 */
export async function ensureAccess(
  session: Session,
  req: Request,
  opts: { force?: boolean } = {},
): Promise<AccessState> {
  if (!opts.force && usable(session.access)) return "ok";
  if (!session.refresh) {
    return !opts.force && usable(session.access, Date.now(), 0) ? "ok" : "none";
  }
  const outcome = await refreshTokens(session.refresh, req);
  if (outcome.ok) {
    session.access = outcome.tokens.access_token;
    session.refresh = outcome.tokens.refresh_token;
    session.setCookies.push(...sessionCookies(session.secure, outcome.tokens));
    return "ok";
  }
  if (outcome.ended) {
    endSession(session);
    return "ended";
  }
  return "unavailable";
}

/** Clears the session's cookies on the response. */
export function endSession(session: Session): void {
  session.access = undefined;
  session.refresh = undefined;
  session.ended = true;
  session.setCookies.push(...clearedSessionCookies(session.secure));
}

/** Signs a session out on the API (RFC 7009); false when it couldn't. */
export async function revokeToken(
  req: Request,
  token: string | undefined,
): Promise<boolean> {
  if (!token) return true;
  try {
    await apiClient(req).auth.revoke(token);
    return true;
  } catch {
    return false;
  }
}

/** A JSON error in the API's envelope. */
export function errorResponse(
  status: number,
  code: string,
  message: string,
): Response {
  return Response.json(
    { error: { code, message } },
    { status, headers: { "cache-control": "no-store" } },
  );
}
