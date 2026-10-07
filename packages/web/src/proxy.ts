/**
 * Next.js proxy (Next 16's name for middleware): V1/V2 UI gating plus
 * V1 legacy path normalization.
 *
 * V2 gating (see lib/ui-gate.ts, the single definition of the V2 path
 * set): requests for V2 Ledger paths without the `memax_ui=v2` cookie
 * redirect to the V1 home. Every other path falls through to the V1
 * rules below unchanged.
 *
 * Plan 24 phase 4b retired v1 across the board. Legacy `/memories`
 * paths now unconditionally redirect to their v2 equivalents so URLs
 * stay canonical:
 *
 *   /memories                 → /h/personal/memories
 *   /memories/topics/:id      → /h/personal/topics/:id
 *   /memories/:id             (unchanged — see below)
 *   /pulse                    (unchanged — a client-side forwarder to
 *                              /h/<active-slug>/pulse; the active hub
 *                              isn't server-readable, so redirecting
 *                              here could only guess `personal`)
 *   /brain, /agents, …        (unchanged — not hub-scoped)
 *
 * (Plan 24's "v1"/"v2" are the old hub-shell versions, not the Memax
 * V2 Ledger UI gated above.)
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
import { decideUiGate, UI_COOKIE } from "@/lib/ui-gate";

// Path predicates — minimal regex set. Keep this file cheap: it runs
// before every matched request. (lib/ui-gate is pure and
// dependency-free for the same reason.)
const V1_MEMORIES_OVERVIEW_RE = /^\/memories\/?$/;
const V1_MEMORIES_TOPIC_RE = /^\/memories\/topics\/([^/]+)\/?$/;

// Session-presence fast path (see lib/session-presence.ts). Name is
// duplicated here rather than imported to keep the proxy
// self-contained; lib/session-presence.ts and proxy.test.ts both pin
// it.
//
// /register is deliberately NOT in this set: invite links arrive as
// /register?invite=TOKEN and a redirect would swallow the token
// before the page can capture it — a stale presence cookie must never
// cost someone their invite. The register page itself forwards
// already-signed-in users onward.
const SESSION_PRESENCE_COOKIE = "memax_session_presence";
const SIGNED_OUT_ENTRY_PATHS = new Set(["/", "/login"]);

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

export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const hasSession =
    request.cookies.get(SESSION_PRESENCE_COOKIE)?.value === "1";

  // V2 gating first: a V2 path without the opt-in cookie goes to the
  // V1 home. Query strings are dropped; they belong to the V2 route.
  const gate = decideUiGate({
    pathname,
    uiCookie: request.cookies.get(UI_COOKIE)?.value,
    hasSession,
  });
  if (gate.action === "redirect") {
    return NextResponse.redirect(
      new URL(gate.pathname, request.nextUrl.origin),
    );
  }

  // Silent session restore. When the session-presence cookie says this
  // browser already has a session (see lib/session-presence.ts), the
  // signed-out entry surfaces redirect straight to the `/home` entry
  // resolver: a previously-signed-in user who lands on `/` or taps
  // "Sign in" never sees the marketing page or login form — they land
  // in the app in one hop with zero credential re-entry. The cookie
  // carries no secret; if it is ever stale, the app shell's auth init
  // fails, clears it, and bounces to /login exactly once (no loop).
  // Requests carrying a query string skip the fast path entirely —
  // /login?returnTo=…, OAuth error params, etc. encode flow state the
  // /home resolver knows nothing about; redirecting would discard it.
  if (
    SIGNED_OUT_ENTRY_PATHS.has(pathname) &&
    request.nextUrl.search === "" &&
    hasSession
  ) {
    return NextResponse.redirect(new URL("/home", request.nextUrl.origin));
  }

  const target = v1ToV2Path(pathname);
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

// Static on purpose (Next reads it at build time, so it can't import
// lib/ui-gate). It matches the V1 paths above plus every V2 path in
// lib/ui-gate.ts; proxy.test.ts fails if the two drift. Everything
// else (/h/*, /brain, /agents, /pulse, /register, /api/*, _next, …)
// skips the proxy entirely so it stays cheap.
//
// The space pattern lists lib/ui-gate's V2_SPACE_PLACES. The bare
// /[space] pattern (redirected to its Today) leaves out every reserved
// slug in lib/ui-gate's RESERVED_SPACE_SLUGS, so V1's own one-segment
// routes (/home, /brain, /settings…) still skip the proxy.
export const config = {
  matcher: [
    // V1: signed-out entry surfaces and the legacy /memories tree
    "/",
    "/login",
    "/memories",
    "/memories/:path*",
    // V2 Ledger areas
    "/signin/:path*",
    "/device/:path*",
    "/unsubscribe/:path*",
    "/setup/:path*",
    "/join/:path*",
    "/settings/:path+",
    "/dev/ledger/:path*",
    // V2 spaces: /[space]/<place>/…
    "/:space/:place(today|review|brief|memories|handoffs|agents|decisions|dream|activity|search|settings)/:path*",
    // A bare /[space], which opens on its Today
    "/:space((?!(?:account|admin|agents|api|assets|auth|billing|blog|brain|changelog|dev|device|discover|docs|download|dreams|h|help|home|images|inbox|invite|join|legal|login|logout|mcp|memories|oauth|pricing|privacy|public|pulse|register|security|settings|setup|share|signin|signout|signup|static|status|support|terms|v1|v2|waitlist)(?:/|$))[a-z0-9][a-z0-9-]*)",
  ],
};
