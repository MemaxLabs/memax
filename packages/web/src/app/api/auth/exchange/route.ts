import { MemaxError } from "memax-sdk";
import { decodeClaims } from "@/lib/bff/claims";
import {
  clearedSessionCookies,
  sessionCookies,
  withCookies,
} from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import {
  apiClient,
  errorResponse,
  readSession,
  revokeToken,
} from "@/lib/bff/session";

/**
 * Signing in: the one-time code the API redirected to the web app (after
 * GitHub, Google or the email code) is traded here, by the web app's
 * server, for the session. Its tokens go into HttpOnly cookies and never
 * reach the page; the answer says only that it worked and what kind of
 * session it is. A session this browser had before is signed out.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;

  let code: string | undefined;
  try {
    code = ((await req.json()) as { code?: string }).code;
  } catch {
    // Answered below.
  }
  if (!code) return errorResponse(400, "invalid_request", "Missing code.");

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
    if (error instanceof MemaxError && error.status > 0) {
      return errorResponse(error.status, error.code, error.message);
    }
    return errorResponse(502, "network_error", "Could not reach memax API.");
  }
}
