import { clearedSessionCookies, withCookies } from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import { readSession, revokeToken } from "@/lib/bff/session";

/**
 * Signing out: the session is revoked on the API (RFC 7009, with its
 * refresh token, so it ends everywhere it was copied, not just here), and
 * every session cookie is cleared, an operator's own session waiting
 * behind an impersonation included. The cookies are cleared even when the
 * API can't be reached; `revoked: false` says the session then lasts until
 * it expires, or until it is signed out from Settings.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;
  const session = readSession(req);
  const results = await Promise.all([
    revokeToken(req, session.refresh ?? session.access),
    revokeToken(req, session.originalRefresh),
  ]);
  const res = Response.json(
    { data: { signed_out: true, revoked: results.every(Boolean) } },
    { headers: { "cache-control": "no-store" } },
  );
  return withCookies(res, clearedSessionCookies(session.secure));
}
