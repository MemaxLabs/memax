/**
 * The web session's cookies (a backend-for-frontend; THREAT_MODEL.md in
 * packages/server/internal/websurface has why). Only the web app's server
 * reads them: /api/auth/* sets and clears them, /api/proxy attaches the
 * access token to what it forwards. Page script never sees a token.
 *
 * - The access token and the refresh token are HttpOnly, Secure and
 *   SameSite=Strict, named with the `__Host-` prefix: host-only (no
 *   Domain), Path=/ and Secure, so a sibling subdomain can't set or
 *   shadow them. Strict works because only same-origin fetches from the
 *   app's own pages read them; a cross-site navigation into the app reads
 *   nothing here (the presence marker below routes it).
 * - While an operator impersonates someone, their own session waits in the
 *   `original` pair, and a readable marker names them for the banner.
 * - The session-presence marker (lib/session-presence.ts) is readable and
 *   SameSite=Lax, holds no secret ("1"), and lets the middleware and the
 *   frame's layout tell a signed-in browser apart before any script runs.
 *
 * Over plain http (local development, the Playwright and workerd runs on
 * localhost) the names drop the prefix and the cookies drop Secure, which
 * the prefix and plain http don't allow. Each mode reads only its own
 * names, so a plain cookie never stands in for a secure one.
 */

import { SESSION_PRESENCE_COOKIE } from "@/lib/session-presence";

export { SESSION_PRESENCE_COOKIE };

/** The readable marker naming the operator while they impersonate someone. */
export const IMPERSONATING_COOKIE = "memax_impersonating";

export interface CookieNames {
  access: string;
  refresh: string;
  originalAccess: string;
  originalRefresh: string;
}

const SECURE_NAMES: CookieNames = {
  access: "__Host-memax_session",
  refresh: "__Host-memax_refresh",
  originalAccess: "__Host-memax_original_session",
  originalRefresh: "__Host-memax_original_refresh",
};

const PLAIN_NAMES: CookieNames = {
  access: "memax_session",
  refresh: "memax_refresh",
  originalAccess: "memax_original_session",
  originalRefresh: "memax_original_refresh",
};

/** A session lives 30 days from sign-in unless the API says otherwise. */
const DEFAULT_SESSION_SECONDS = 30 * 24 * 60 * 60;

/** Whether the browser reached us over https (directly or behind a proxy). */
export function isSecureRequest(req: Request): boolean {
  if (new URL(req.url).protocol === "https:") return true;
  const proto = req.headers.get("x-forwarded-proto")?.split(",")[0]?.trim();
  return proto === "https";
}

export function cookieNames(secure: boolean): CookieNames {
  return secure ? SECURE_NAMES : PLAIN_NAMES;
}

/** The request's cookies, by name. */
export function readCookies(req: Request): Map<string, string> {
  const out = new Map<string, string>();
  for (const part of (req.headers.get("cookie") ?? "").split(";")) {
    const eq = part.indexOf("=");
    if (eq < 0) continue;
    const name = part.slice(0, eq).trim();
    if (!name || out.has(name)) continue;
    const raw = part.slice(eq + 1).trim();
    try {
      out.set(name, decodeURIComponent(raw));
    } catch {
      out.set(name, raw);
    }
  }
  return out;
}

interface CookieOptions {
  maxAge: number;
  /** Default true. */
  httpOnly?: boolean;
  /** Default Strict. */
  sameSite?: "Strict" | "Lax";
}

/** A Set-Cookie value. */
export function serializeCookie(
  name: string,
  value: string,
  secure: boolean,
  opts: CookieOptions,
): string {
  const parts = [
    `${name}=${encodeURIComponent(value)}`,
    "Path=/",
    `Max-Age=${Math.max(0, Math.floor(opts.maxAge))}`,
    `SameSite=${opts.sameSite ?? "Strict"}`,
  ];
  if (opts.httpOnly !== false) parts.push("HttpOnly");
  if (secure) parts.push("Secure");
  return parts.join("; ");
}

/** A Set-Cookie value that deletes the cookie. */
export function expireCookie(
  name: string,
  secure: boolean,
  opts: Omit<CookieOptions, "maxAge"> = {},
): string {
  return serializeCookie(name, "", secure, { ...opts, maxAge: 0 });
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  refresh_expires_in?: number;
}

/** How long the access cookie lives: a minute short of the token. */
export function accessMaxAge(expiresIn: number): number {
  return Math.max(30, expiresIn - 60);
}

/** The cookies a signed-in (or refreshed) session sets. */
export function sessionCookies(secure: boolean, t: TokenPair): string[] {
  const names = cookieNames(secure);
  const sessionSeconds = t.refresh_expires_in || DEFAULT_SESSION_SECONDS;
  return [
    serializeCookie(names.access, t.access_token, secure, {
      maxAge: accessMaxAge(t.expires_in),
    }),
    serializeCookie(names.refresh, t.refresh_token, secure, {
      maxAge: sessionSeconds,
    }),
    presenceCookie(secure, sessionSeconds),
  ];
}

/** The readable "this browser has a session" marker. */
export function presenceCookie(secure: boolean, maxAge: number): string {
  return serializeCookie(SESSION_PRESENCE_COOKIE, "1", secure, {
    maxAge,
    httpOnly: false,
    sameSite: "Lax",
  });
}

/** Every cookie of the session, deleted: signed out. */
export function clearedSessionCookies(secure: boolean): string[] {
  const names = cookieNames(secure);
  return [
    expireCookie(names.access, secure),
    expireCookie(names.refresh, secure),
    expireCookie(names.originalAccess, secure),
    expireCookie(names.originalRefresh, secure),
    expireCookie(SESSION_PRESENCE_COOKIE, secure, {
      httpOnly: false,
      sameSite: "Lax",
    }),
    expireCookie(IMPERSONATING_COOKIE, secure, { httpOnly: false }),
  ];
}

/** A response with these Set-Cookie headers added. */
export function withCookies<T extends Response>(res: T, cookies: string[]): T {
  for (const c of cookies) res.headers.append("set-cookie", c);
  return res;
}
