import { MemaxError } from "memax-sdk";
import { decodeClaims } from "./claims";
import { clearedSessionCookies, sessionCookies, withCookies } from "./cookies";
import { apiClient, errorResponse, readSession, revokeToken } from "./session";

/**
 * Starts the web session for a sign-in's one-time code (GitHub, Google,
 * the email code, or a passkey): the web app's server trades the code for
 * the session, its tokens go into HttpOnly cookies and never reach the
 * page, and the answer says only that it worked and what kind of session
 * it is. A session this browser had before is signed out.
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
    const res = Response.json(
      { data: { signed_in: true, surface: claims?.surface ?? null } },
      { headers: { "cache-control": "no-store" } },
    );
    return withCookies(res, [
      ...clearedSessionCookies(previous.secure),
      ...sessionCookies(previous.secure, tokens),
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
