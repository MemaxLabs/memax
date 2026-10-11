/**
 * V1 / V2 UI gating: the one place that says which URLs belong to the
 * V2 Ledger UI (plan §6.3) and who may open them.
 *
 * Whether a person sees V2 is a per-person flag the API decides
 * (internal/v2ui: a member of a space on the V2 record, an operator's
 * choice, or the signup date; plan 25 E1). The browser carries it as the
 * `memax_ui=v2` cookie, a routing hint only: the web app's server sets or
 * clears it from the flag whenever a session starts and on every profile
 * read (/api/auth/me), and clears it at sign-out (lib/bff). In dev-fixture
 * builds /dev/ui?v=2|1 sets and clears it by hand.
 *
 * Requests for V2 paths without the hint are redirected to the V1 home,
 * except that a signed-in browser is sent once to /signin?next= to read
 * its flag again (a hint can be missing because the flag turned on after
 * the session started, say when `memax init` made the person's first
 * space): the sign-in page reads the profile, which sets the hint, and
 * goes on to `next` for a person with the flag or to V1's home for one
 * without. The other way round, a browser with the hint that opens V1's
 * home (/home, or a signed-in browser's / and /login, which V1 sends
 * there) goes to its V2 landing instead (V2_LANDING_PATH), so a person
 * whose flag turned on while they were in V1 reaches V2 the next time
 * they open the app. A short-lived marker (UI_RECHECK_COOKIE) makes each
 * of those trips at most once a minute, so a hint that can't be set
 * never loops. Every other V1 path (a hub, V1's settings, the admin
 * pages) is never touched. src/proxy.ts applies the decision, and its
 * static `config.matcher` must cover every path listed here
 * (proxy.test.ts checks that).
 *
 * Pure and dependency-free: it runs in the proxy on every matched
 * request.
 */

export const UI_COOKIE = "memax_ui";
export const UI_COOKIE_V2 = "v2";

/**
 * The re-check marker: set by the proxy when it sends a browser to
 * /signin to read its flag again (a signed-in one without the hint on a
 * V2 path, or one with the hint on V1's home), for UI_RECHECK_SECONDS.
 * While it is there, a V2 path without the hint goes to the V1 home, and
 * V1's home opens as it is. Not a secret, and nothing else reads it.
 */
export const UI_RECHECK_COOKIE = "memax_ui_check";
export const UI_RECHECK_SECONDS = 60;

/** Top-level areas that are V2 in full, with any sub-path (§6.3). */
const V2_AREAS = new Set(["signin", "device", "unsubscribe", "setup", "join"]);

/**
 * V2 areas that open without the opt-in. The memax CLI sends anyone to
 * /device to confirm its sign-in code (RFC 8628), and V1 has no such
 * page, so a person who never opted in must reach it; signing in on the
 * way there goes through /signin (and /signin/callback). Dream's morning
 * email links to /unsubscribe, which needs no sign-in at all. None shows
 * a space: once signed in, a browser without the opt-in lands back in V1.
 */
const V2_OPEN_AREAS = new Set(["signin", "device", "unsubscribe"]);

/**
 * OAuthConsent, the Ledger page where a person lets an outside agent (an
 * MCP client) connect: /oauth/authorize. It opens for every browser, like
 * /signin and /device: the API sends everyone there (V1 people too) from
 * an agent's OAuth request, cookie or not, and the person signs in on the
 * way if they need to.
 *
 * V1's consent page (/oauth/consent, in the (v1) tree) is retired: every
 * browser that lands there, a link from before the move, goes on to the
 * Ledger page with the same query (V1_PAGES_WITH_V2). The paths differ
 * because the root layouts can't share one.
 */
const OAUTH_CONSENT_V2 = "/oauth/authorize";

/**
 * Retired V1 pages and the V2 page that took each over with the same
 * query: every browser goes on to the V2 one, query and all.
 */
const V1_PAGES_WITH_V2: ReadonlyMap<string, string> = new Map([
  ["/oauth/consent", OAUTH_CONSENT_V2],
]);

function isOAuthConsentV2(segments: string[]): boolean {
  return segments[0] === "oauth" && segments[1] === "authorize";
}

/** Whether a V2 path opens for every browser (V2_OPEN_AREAS, OAuthConsent). */
export function isOpenV2Path(pathname: string): boolean {
  const segments = pathname.split("/").filter(Boolean);
  const first = segments[0];
  if (isOAuthConsentV2(segments)) return true;
  return first !== undefined && V2_OPEN_AREAS.has(first);
}

/** The V2 page that took over a retired V1 page, if any. */
export function v2PageFor(pathname: string): string | null {
  const segments = pathname.split("/").filter(Boolean);
  return V1_PAGES_WITH_V2.get(`/${segments.join("/")}`) ?? null;
}

/**
 * Areas where V1 keeps the bare path: /settings is V1's settings page,
 * while /settings/{plan,account,keys,…} are V2.
 */
const V2_SUBPATH_AREAS = new Set(["settings"]);

/**
 * What can follow a space slug, /[space]/<place>/…: the six places,
 * Decisions for team spaces, and the detail routes (§6.3). A bare
 * /[space] is not a V2 page: with memax_ui=v2 it redirects to its Today
 * (decideUiGate), and without it a mistyped V1 URL still gets V1's 404.
 */
export const V2_SPACE_PLACES = [
  "today",
  "review",
  "brief",
  "memories",
  "handoffs",
  "agents",
  "decisions",
  "dream",
  "activity",
  "search",
  "settings",
] as const;
const SPACE_PLACES = new Set<string>(V2_SPACE_PLACES);

/**
 * First path segments a space slug may never take, because space URLs
 * live at the root (§6.3). Every top-level route of either UI, the
 * public site, public/ folders and a few product words. Enforced when a
 * space is created (Phase 1, server side; a migration renames colliding
 * V1 hub slugs). ui-gate.test.ts checks that every top-level route in
 * src/app is listed.
 *
 * This is about URL collisions only. The V1 hub-name rules (e.g. "team"
 * is too vague for a hub) live with the server and still apply.
 */
export const RESERVED_SPACE_SLUGS: ReadonlySet<string> = new Set([
  // The plan's list (§6.3)
  "settings",
  "setup",
  "signin",
  "device",
  "unsubscribe",
  "join",
  "oauth",
  "pricing",
  "security",
  "docs",
  "admin",
  "api",
  "h",
  // V1 top-level routes, until cutover
  "agents",
  "auth",
  "brain",
  "discover",
  "dreams",
  "home",
  "inbox",
  "invite",
  "login",
  "memories",
  "privacy",
  "pulse",
  "register",
  "share",
  "terms",
  "waitlist",
  // Dev fixtures and toggles
  "dev",
  // public/ folders and static paths
  "images",
  "assets",
  "static",
  "public",
  // Product and account words we may route later
  "account",
  "billing",
  "blog",
  "changelog",
  "download",
  "help",
  "legal",
  "logout",
  "mcp",
  "signout",
  "signup",
  "status",
  "support",
  "v1",
  "v2",
]);

/** Lowercase letters, digits and inner hyphens, 1–50 characters. */
const SPACE_SLUG_RE = /^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$/;

/** Whether `slug` is shaped like a space slug and not reserved. */
export function isSpaceSlug(slug: string): boolean {
  return SPACE_SLUG_RE.test(slug) && !RESERVED_SPACE_SLUGS.has(slug);
}

/** Whether a request path belongs to the V2 Ledger UI. */
export function isV2Path(pathname: string): boolean {
  const segments = pathname.split("/").filter(Boolean);
  const [first, second] = segments;
  if (first === undefined) return false; // "/" stays V1 until cutover
  if (V2_AREAS.has(first)) return true;
  if (isOAuthConsentV2(segments)) return true;
  if (V2_SUBPATH_AREAS.has(first)) return segments.length > 1;
  if (first === "dev") return second === "ledger";
  return second !== undefined && SPACE_PLACES.has(second) && isSpaceSlug(first);
}

/** Whether the `memax_ui` cookie value opts this browser into V2. */
export function hasV2Opt(uiCookie: string | undefined): boolean {
  return uiCookie === UI_COOKIE_V2;
}

/**
 * Where V1 starts: the /home entry resolver for a browser with a
 * session, the landing page otherwise (the same split as the proxy's
 * session fast path, so the redirect takes one hop).
 */
export function v1HomePath(hasSession: boolean): string {
  return hasSession ? "/home" : "/";
}

/**
 * V1's signed-out entry pages: a browser with a session skips them for
 * V1's home, unless the request carries a query (/login?returnTo=…, an
 * OAuth error), which is flow state the home knows nothing about. The
 * proxy's session fast path; /register isn't one, since an invite link
 * (/register?invite=…) must reach its page.
 */
export const V1_ENTRY_PATHS: ReadonlySet<string> = new Set(["/", "/login"]);

/**
 * Where a person who sees V2 starts instead of V1's home: the sign-in
 * page, which signs them in if they need to and otherwise lands them by
 * their V2 UI flag (lib/v2/onboarding/routes.ts: FirstRun, an import in
 * progress, the switch of a space still on V1, or a space's Today). The
 * profile read it lands with also keeps the hint in step, so a stale
 * hint ends there: someone without the flag goes on to V1's home.
 */
export const V2_LANDING_PATH = "/signin";

/**
 * Whether a request opens V1's home: /home itself, or an entry page a
 * signed-in browser skips for it (V1_ENTRY_PATHS).
 */
export function opensV1Home({
  pathname,
  search,
  hasSession,
}: {
  pathname: string;
  search: string;
  hasSession: boolean;
}): boolean {
  if (pathname === v1HomePath(true)) return true;
  return hasSession && search === "" && V1_ENTRY_PATHS.has(pathname);
}

/**
 * The dev toggle, /dev/ui?v=2|1[&next=/path]: which UI to switch to and
 * where to land. `next` must be a same-origin path; anything else falls
 * back to the default for that UI. Returns null for a missing or
 * unknown `v`.
 */
export function parseUiToggle(
  params: URLSearchParams,
): { ui: "v1" | "v2"; next: string } | null {
  const v = params.get("v");
  if (v !== "1" && v !== "2") return null;
  const ui = v === "2" ? "v2" : "v1";
  const next = params.get("next");
  const safe =
    next !== null &&
    next.startsWith("/") &&
    !next.startsWith("//") &&
    !next.includes("\\");
  return {
    ui,
    next: safe ? next : ui === "v2" ? "/dev/ledger/tokens" : "/",
  };
}

/** The space a bare /[space] names, or null for any other path. */
export function bareSpaceSlug(pathname: string): string | null {
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length !== 1) return null;
  return isSpaceSlug(segments[0]) ? segments[0] : null;
}

/**
 * Whether a path (a `next`, query and all) is a V2 page that needs the
 * opt-in: a V2 path that isn't open to every browser. After signing in, a
 * person without the V2 UI flag never goes to one (onboarding/routes.ts).
 */
export function needsV2Opt(path: string): boolean {
  const pathname = path.split(/[?#]/, 1)[0] ?? "";
  return isV2Path(pathname) && !isOpenV2Path(pathname);
}

export type UiGateDecision =
  | { action: "continue" }
  | {
      action: "redirect";
      pathname: string;
      /** The query string goes along (V1_PAGES_WITH_V2). */
      keepQuery?: boolean;
      /** The redirect's own query string, "?…" (the re-check's `next`). */
      search?: string;
      /** Set the re-check marker (UI_RECHECK_COOKIE) on the redirect. */
      recheck?: boolean;
    };

export function decideUiGate({
  pathname,
  search = "",
  uiCookie,
  hasSession,
  rechecked = false,
}: {
  pathname: string;
  /** The request's query string, "?…" or "". */
  search?: string;
  uiCookie: string | undefined;
  hasSession: boolean;
  /** The re-check marker is there: this browser was re-checked a moment ago. */
  rechecked?: boolean;
}): UiGateDecision {
  const v2Page = v2PageFor(pathname);
  if (v2Page !== null) {
    return { action: "redirect", pathname: v2Page, keepQuery: true };
  }
  const bare = bareSpaceSlug(pathname);
  if (bare !== null) {
    // A space opens on its Today, for V2 browsers only.
    return hasV2Opt(uiCookie)
      ? { action: "redirect", pathname: `/${bare}/today` }
      : { action: "continue" };
  }
  if (hasV2Opt(uiCookie) && opensV1Home({ pathname, search, hasSession })) {
    // V1's home, for a person who sees V2: their V2 landing, which reads
    // the flag again on the way. Re-checked a moment ago, the flag didn't
    // land them in V2 after all (or the hint can't be cleared), so V1's
    // home opens as it is, never a loop. The marker goes on signed out
    // too: the sign-in page then asks them to sign in, and lands them by
    // their flag after.
    return rechecked
      ? { action: "continue" }
      : { action: "redirect", pathname: V2_LANDING_PATH, recheck: true };
  }
  if (!isV2Path(pathname) || hasV2Opt(uiCookie) || isOpenV2Path(pathname)) {
    return { action: "continue" };
  }
  // Without the hint, the sign-in page decides: signed in, it reads the
  // flag again and comes back here for a person with it; signed out, the
  // person signs in and lands by their flag (V1 people on V1's home), so a
  // V2 link from an email or the CLI never strands an alpha person in V1.
  // Never for the dev fixtures, which aren't anyone's page.
  const dev = pathname.split("/").filter(Boolean)[0] === "dev";
  if (!dev && (!hasSession || !rechecked)) {
    // The page's own query goes along; the router's (_rsc, on a client
    // navigation's fetch) doesn't.
    const query = new URLSearchParams(search);
    query.delete("_rsc");
    const own = query.toString();
    const next = new URLSearchParams({
      next: `${pathname}${own ? `?${own}` : ""}`,
    });
    return {
      action: "redirect",
      pathname: "/signin",
      search: `?${next.toString()}`,
      recheck: hasSession,
    };
  }
  return { action: "redirect", pathname: v1HomePath(hasSession) };
}
