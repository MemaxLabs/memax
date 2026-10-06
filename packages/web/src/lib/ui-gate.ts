/**
 * V1 / V2 UI gating: the one place that says which URLs belong to the
 * V2 Ledger UI (plan §6.3) and who may open them.
 *
 * A browser opts into V2 with the `memax_ui=v2` cookie (dev: visit
 * /dev/ui?v=2). Requests for V2 paths without it are redirected to the
 * V1 home; V1 paths are never touched. src/proxy.ts applies the
 * decision, and its static `config.matcher` must cover every V2 path
 * listed here (proxy.test.ts checks that).
 *
 * Pure and dependency-free: it runs in the proxy on every matched
 * request.
 */

export const UI_COOKIE = "memax_ui";
export const UI_COOKIE_V2 = "v2";

/** Top-level areas that are V2 in full, with any sub-path (§6.3). */
const V2_AREAS = new Set(["signin", "device", "setup", "join"]);

/**
 * Areas where V1 keeps the bare path: /settings is V1's settings page,
 * while /settings/{plan,account,keys,…} are V2.
 */
const V2_SUBPATH_AREAS = new Set(["settings"]);

/**
 * What can follow a space slug, /[space]/<place>/…: the six places,
 * Decisions for team spaces, and the detail routes (§6.3). A bare
 * /[space] is not a V2 route yet, so a mistyped V1 URL still gets V1's
 * 404 rather than a redirect.
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

export type UiGateDecision =
  | { action: "continue" }
  | { action: "redirect"; pathname: string };

export function decideUiGate({
  pathname,
  uiCookie,
  hasSession,
}: {
  pathname: string;
  uiCookie: string | undefined;
  hasSession: boolean;
}): UiGateDecision {
  if (!isV2Path(pathname) || hasV2Opt(uiCookie)) {
    return { action: "continue" };
  }
  return { action: "redirect", pathname: v1HomePath(hasSession) };
}
