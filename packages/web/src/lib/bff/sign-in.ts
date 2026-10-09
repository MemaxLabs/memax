import { MemaxError } from "memax-sdk";
import { decodeClaims } from "./claims";
import {
  clearedSessionCookies,
  DEFAULT_SESSION_SECONDS,
  sessionCookies,
  uiCookie,
  withCookies,
} from "./cookies";
import { apiClient, errorResponse, readSession, revokeToken } from "./session";

/**
 * Starts the web session for a sign-in's one-time code (GitHub, Google,
 * the email code, or a passkey): the web app's server trades the code for
 * the session, its tokens go into HttpOnly cookies and never reach the
 * page, and the answer says only that it worked, what kind of session it
 * is and which web UI the person sees (`ui`). A session this browser had
 * before is signed out, and the V2 UI hint (memax_ui) is set or cleared
 * from the person's own flag: the next person on a shared browser never
 * inherits the last one's. An answer without the flag counts as V1.
 */
export async function sessionFromCode(
  req: Request,
  code: string,
): Promise<Response> {
  const previous = readSession(req);
  try {
    const tokens = await apiClient(req, { clientInfo: true }).auth.exchangeCode(
      code,
    );
    if (!tokens?.access_token || !tokens.refresh_token) {
      return errorResponse(
        502,
        "invalid_response",
        "The sign-in didn't complete.",
      );
    }
    // Sign the browser's previous session out, if it had one, so a second
    // sign-in doesn't leave the first one alive.
    if (previous.refresh) await revokeToken(req, previous.refresh);
    const claims = decodeClaims(tokens.access_token);
    const ui = tokens.ui === "v2" ? "v2" : "v1";
    const res = Response.json(
      { data: { signed_in: true, surface: claims?.surface ?? null, ui } },
      { headers: { "cache-control": "no-store" } },
    );
    return withCookies(res, [
      ...clearedSessionCookies(previous.secure),
      ...sessionCookies(previous.secure, tokens),
      uiCookie(
        previous.secure,
        ui,
        tokens.refresh_expires_in || DEFAULT_SESSION_SECONDS,
      ),
    ]);
  } catch (error) {
    return apiError(error);
  }
}

/** An error the API answered, passed on; anything else is unreachable. */
export function apiError(error: unknown): Response {
  if (error instanceof MemaxError && error.status > 0) {
    return errorResponse(error.status, error.code, error.message);
  }
  return errorResponse(502, "network_error", "Could not reach memax API.");
}
