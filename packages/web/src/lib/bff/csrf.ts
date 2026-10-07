/**
 * Cross-site request forgery, for every route that acts with the session's
 * cookies (/api/proxy and /api/auth/*). Two layers, either enough alone
 * against another site:
 *
 * 1. The session cookies are SameSite=Strict (lib/bff/cookies.ts), so a
 *    browser never sends them with a request another site starts.
 * 2. Fetch Metadata: a state-changing request must say it came from this
 *    origin (`Sec-Fetch-Site: same-origin`, which every current browser
 *    sends and page script can't set); a browser too old to send it must
 *    send an `Origin` that is ours. A request from a sibling subdomain
 *    (`same-site`), which SameSite doesn't stop, is refused too, and so
 *    is any request another site starts, reads included.
 *
 * Why not a double-submit token: it needs every fetch in both web trees to
 * carry it, and it adds nothing against another site that these two don't
 * already stop. None of this is a defence against script running on our
 * own pages (XSS), which acts as the page does; the BFF's point there is
 * that such script can no longer take the tokens away.
 */

import { APP_URL } from "@/lib/urls";

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

/** The origins this app is served from, as the browser sees them. */
function ourOrigins(req: Request): Set<string> {
  const url = new URL(req.url);
  const origins = new Set([url.origin, new URL(APP_URL).origin]);
  const host = req.headers.get("x-forwarded-host")?.split(",")[0]?.trim();
  if (host) {
    const proto =
      req.headers.get("x-forwarded-proto")?.split(",")[0]?.trim() ||
      url.protocol.replace(":", "");
    origins.add(`${proto}://${host}`);
  }
  return origins;
}

/**
 * Why the request is refused, or null when it may go on. `safe` reads may
 * come from a navigation (`none`: a typed URL, a bookmark, a download link
 * opened directly); anything else must come from our own pages.
 */
export function csrfProblem(req: Request): string | null {
  const site = req.headers.get("sec-fetch-site");
  const safe = SAFE_METHODS.has(req.method.toUpperCase());
  if (site) {
    if (site === "same-origin") return null;
    if (site === "none" && safe) return null;
    return `sec-fetch-site ${site}`;
  }
  const origin = req.headers.get("origin");
  if (origin) {
    return ourOrigins(req).has(origin) ? null : "origin mismatch";
  }
  // Neither header: a browser sends Origin with every state-changing
  // request, so this isn't one of ours. Reads carry no risk.
  return safe ? null : "no origin";
}

/** The 403 a refused request gets, in the API's envelope. */
export function csrfRefusal(req: Request): Response | null {
  const problem = csrfProblem(req);
  if (!problem) return null;
  return Response.json(
    {
      error: {
        code: "csrf_refused",
        message:
          "This request didn't come from a Memax page, so it wasn't sent. Reload memax.app and try again.",
      },
    },
    { status: 403, headers: { "cache-control": "no-store" } },
  );
}
