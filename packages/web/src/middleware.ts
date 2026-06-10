/**
 * Next.js middleware — legacy path normalization.
 *
 * Plan 24 phase 4b retired v1 across the board. Legacy `/memories`
 * paths now unconditionally redirect to their v2 equivalents so URLs
 * stay canonical:
 *
 *   /memories                 → /h/personal/memories
 *   /memories/topics/:id      → /h/personal/topics/:id
 *   /memories/:id             (unchanged — see below)
 *   /inbox[/...]              (unchanged — inbox is shared)
 *   /brain, /agents, …        (unchanged — not hub-scoped)
 *
 * `personal` is the safe default redirect target because every user
 * has a personal hub (auto-created at signup). Last-active-hub-aware
 * redirect is a future polish that needs a server-readable hub
 * preference.
 *
 * Memory detail (`/memories/:id`) is intentionally NOT redirected: the
 * hub identity for a memory is encoded in its data (the memory belongs
 * to a single hub), not in the URL. Redirecting blindly to
 * `/h/personal/...` for a team-hub memory would force `<V2HubRoute>`
 * to switch the active hub to personal, after which the memory's
 * content still loads (memory id is global) but the URL + chrome lie
 * about which hub the memory belongs to. In-app navigation already
 * emits hub-aware paths via `buildMemoryDetailPath(currentHubSlug,
 * id)`, so users never reach `/memories/<id>` from inside the app;
 * this case only matters for old shared/email links, where rendering
 * at the v1 path under v2 chrome is correct.
 */

import { NextRequest, NextResponse } from "next/server";

// Path predicates — minimal regex set (don't import lib/route-helpers
// because middleware runs in the Edge runtime which has a tighter
// import surface and we want this file to be self-contained / cheap).
const V1_MEMORIES_OVERVIEW_RE = /^\/memories\/?$/;
const V1_MEMORIES_TOPIC_RE = /^\/memories\/topics\/([^/]+)\/?$/;

function v1ToV2Path(pathname: string): string | null {
  // /memories or /memories/ — the overview
  if (V1_MEMORIES_OVERVIEW_RE.test(pathname)) {
    return "/h/personal/memories";
  }
  // /memories/topics/<id> — topic detail
  const topicMatch = pathname.match(V1_MEMORIES_TOPIC_RE);
  if (topicMatch?.[1]) {
    return `/h/personal/topics/${topicMatch[1]}`;
  }
  // /memories/<id> — intentionally not redirected; see header docstring.
  return null;
}

export function middleware(request: NextRequest) {
  const target = v1ToV2Path(request.nextUrl.pathname);
  if (!target) return NextResponse.next();

  // Build a fresh URL with the target pathname instead of cloning +
  // mutating; cloning preserved Next's internal trailing-slash hint
  // from the original request and the response location ended up with
  // an unwanted trailing slash for `/memories/`.
  const redirectUrl = new URL(
    `${target}${request.nextUrl.search}`,
    request.nextUrl.origin,
  );
  return NextResponse.redirect(redirectUrl);
}

// Match ONLY the legacy /memories tree. Other paths (/h/*, /brain,
// /agents, /inbox, /api/*, _next, etc.) skip the middleware entirely
// so it stays cheap.
export const config = {
  matcher: ["/memories", "/memories/:path*"],
};
