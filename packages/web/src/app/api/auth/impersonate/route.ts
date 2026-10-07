import { MemaxError } from "memax-sdk";
import {
  accessMaxAge,
  expireCookie,
  IMPERSONATING_COOKIE,
  serializeCookie,
  withCookies,
} from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import {
  apiClient,
  ensureAccess,
  errorResponse,
  readSession,
} from "@/lib/bff/session";

/** How long the operator's own session waits behind an impersonation. */
const ORIGINAL_SECONDS = 30 * 24 * 60 * 60;

/**
 * An operator with dev access acting as another person, to debug as them
 * (POST starts, DELETE stops). The impersonation token is access-only and
 * lasts an hour; it takes the access cookie, and the operator's own
 * refresh token waits, HttpOnly like the rest, until they stop. The page
 * reads only a marker with the operator's name, for the banner. None of
 * it is a token the page can see.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;

  let body: { user_id?: string; email?: string; original_name?: string };
  try {
    body = (await req.json()) as typeof body;
  } catch {
    return errorResponse(400, "invalid_request", "Invalid JSON.");
  }
  const session = readSession(req);
  if (session.originalRefresh) {
    return errorResponse(
      409,
      "already_impersonating",
      "Already impersonating. Stop the current session first.",
    );
  }
  if ((await ensureAccess(session, req)) !== "ok" || !session.refresh) {
    return withCookies(
      errorResponse(401, "unauthorized", "Sign in again to impersonate."),
      session.setCookies,
    );
  }
  try {
    const result = await apiClient(req, {
      access: session.access,
    }).auth.impersonate({ userId: body.user_id, email: body.email });
    const { names, secure } = session;
    const res = Response.json(
      { data: { impersonated: true, target_id: result.target_id } },
      { headers: { "cache-control": "no-store" } },
    );
    return withCookies(res, [
      ...session.setCookies,
      serializeCookie(names.originalRefresh, session.refresh, secure, {
        maxAge: ORIGINAL_SECONDS,
      }),
      serializeCookie(names.access, result.access_token, secure, {
        maxAge: accessMaxAge(result.expires_in),
      }),
      expireCookie(names.refresh, secure),
      serializeCookie(
        IMPERSONATING_COOKIE,
        (body.original_name ?? "").slice(0, 100),
        secure,
        { maxAge: ORIGINAL_SECONDS, httpOnly: false },
      ),
    ]);
  } catch (error) {
    if (error instanceof MemaxError && error.status > 0) {
      return withCookies(
        errorResponse(error.status, error.code, error.message),
        session.setCookies,
      );
    }
    return errorResponse(502, "network_error", "Could not reach API.");
  }
}

/**
 * Stops impersonating: the operator's own session comes back (its access
 * token is refreshed on the next request).
 */
export async function DELETE(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;
  const { names, secure, originalRefresh } = readSession(req);
  const cookies = [
    expireCookie(names.access, secure),
    expireCookie(names.originalRefresh, secure),
    expireCookie(IMPERSONATING_COOKIE, secure, { httpOnly: false }),
  ];
  if (originalRefresh) {
    cookies.push(
      serializeCookie(names.refresh, originalRefresh, secure, {
        maxAge: ORIGINAL_SECONDS,
      }),
    );
  }
  return withCookies(
    Response.json(
      { data: { restored: Boolean(originalRefresh) } },
      { headers: { "cache-control": "no-store" } },
    ),
    cookies,
  );
}
