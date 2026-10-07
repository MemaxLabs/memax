import { MemaxError } from "memax-sdk";
import { decodeClaims } from "@/lib/bff/claims";
import { presenceCookie, uiCookieFor, withCookies } from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import {
  apiClient,
  endSession,
  ensureAccess,
  errorResponse,
  readSession,
} from "@/lib/bff/session";

/** How long a re-planted presence marker lasts: a session's longest life. */
const PRESENCE_SECONDS = 30 * 24 * 60 * 60;

/**
 * Who is signed in, for the page: the profile (with `ui`, the web UI the
 * person sees, which also keeps the memax_ui routing hint in step), and
 * what the session is (`session.surface`: "web" for a sign-in on the web
 * app, which with the proxy's signature is what keeps as a person on the
 * web; and whether an operator is impersonating, in which case the hint
 * follows the person being impersonated). The page never sees the tokens.
 * A browser without a session, or whose session ended, gets 401 and its
 * cookies cleared (the hint stays: only a sign-in or a sign-out changes
 * it then); while impersonating, an expired impersonation keeps the
 * operator's own session for /api/auth/impersonate to restore.
 */
export async function GET(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;
  const session = readSession(req);
  const state = await ensureAccess(session, req);
  if (state === "unavailable" && !session.access) {
    return errorResponse(502, "network_error", "Could not reach memax API.");
  }
  if (!session.access) {
    if (session.originalRefresh) {
      return errorResponse(
        401,
        "impersonation_expired",
        "The impersonation ended. Stop impersonating to return to your account.",
      );
    }
    if (!session.ended) endSession(session);
    return withCookies(
      errorResponse(401, "unauthorized", "Sign in to Memax."),
      session.setCookies,
    );
  }
  const me = async () => apiClient(req, { access: session.access }).auth.me();
  try {
    let profile;
    try {
      profile = await me();
    } catch (error) {
      if (
        !(error instanceof MemaxError && error.status === 401) ||
        !session.refresh
      ) {
        throw error;
      }
      // Refused: refresh once and ask again.
      if ((await ensureAccess(session, req, { force: true })) !== "ok") {
        throw error;
      }
      profile = await me();
    }
    const claims = decodeClaims(session.access);
    // A verified session (re)plants the presence marker the middleware's
    // fast path reads.
    if (!session.setCookies.length) {
      session.setCookies.push(presenceCookie(session.secure, PRESENCE_SECONDS));
    }
    // The V2 UI hint follows the person's flag (an operator changed it, a
    // space switched to V2 or back, `memax init` made their first space):
    // set or cleared when it disagrees, so the proxy routes the next load.
    session.setCookies.push(
      ...uiCookieFor(req, session.secure, profile.ui, PRESENCE_SECONDS),
    );
    const res = Response.json(
      {
        data: {
          ...profile,
          session: {
            surface: claims?.surface ?? null,
            impersonating: Boolean(claims?.impersonator_id),
          },
        },
      },
      { headers: { "cache-control": "no-store" } },
    );
    return withCookies(res, session.setCookies);
  } catch (error) {
    if (error instanceof MemaxError && error.status === 401) {
      if (!session.originalRefresh && !session.ended) endSession(session);
      return withCookies(
        errorResponse(401, "unauthorized", "Sign in to Memax."),
        session.setCookies,
      );
    }
    if (error instanceof MemaxError && error.status > 0) {
      return withCookies(
        errorResponse(error.status, error.code, error.message),
        session.setCookies,
      );
    }
    return withCookies(
      errorResponse(502, "network_error", "Could not reach memax API."),
      session.setCookies,
    );
  }
}
