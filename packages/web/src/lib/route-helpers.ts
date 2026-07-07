/**
 * Route helpers — single source of truth for route-shape detection and
 * URL construction across shell-v1 and shell-v2.
 *
 * Two route families coexist during the migration:
 *
 *   v1 (legacy):       /memories                  /memories/topics/:id
 *                      /memories/:id              /inbox
 *   v2 (shell-v2):     /h/:slug/memories          /h/:slug/topics/:id
 *                      /h/:slug/memories/:id      /inbox  (unchanged)
 *
 * Predicates (`isMemoriesRoute`, `isTopicRoute`, ...) match BOTH shapes
 * so consumers don't have to branch on shell version. Path builders take
 * an explicit `slug | null` argument; `null` selects v1 shape, a string
 * selects v2.
 *
 * `getHubSlugForPath` returns the slug for v2 routes and `null` for v1.
 * Callers in shell-v2 should resolve the slug to a hub UUID via
 * `resolveHubFromSlug` from `./hub-from-slug.ts` before issuing API
 * calls — the slug is a routing identity, the UUID is the data identity.
 */

// ── Regexes ─────────────────────────────────────────────────────────────

// Top-level prefixes. Match either the bare path or a path with a
// trailing segment so consumers don't accidentally match "/memoriesfoo".
const V1_MEMORIES_PREFIX_RE = /^\/memories(\/|$)/;
const V2_MEMORIES_PREFIX_RE = /^\/h\/[^/]+\/memories(\/|$)/;
const V1_INBOX_PREFIX_RE = /^\/inbox(\/|$)/;

// Topic detail. v1: /memories/topics/:id  v2: /h/:slug/topics/:id
const V1_TOPIC_RE = /^\/memories\/topics\/([^/]+)\/?$/;
const V2_TOPIC_RE = /^\/h\/[^/]+\/topics\/([^/]+)\/?$/;

// Memory detail. v1: /memories/:id  v2: /h/:slug/memories/:id
// Reserved sibling segments under /memories are NOT memory ids.
const V1_MEMORY_DETAIL_RE = /^\/memories\/([^/]+)\/?$/;
const V2_MEMORY_DETAIL_RE = /^\/h\/[^/]+\/memories\/([^/]+)\/?$/;
const RESERVED_MEMORY_SEGMENTS = new Set(["topics", "new"]);

// Hub slug extraction. The `[^/]+` accepts UUID-shape, alias `personal`,
// or any other slug-shape — validation happens in `resolveHubFromSlug`.
const V2_HUB_SLUG_RE = /^\/h\/([^/]+)(\/|$)/;

// ── Predicates ──────────────────────────────────────────────────────────

export function isMemoriesRoute(pathname: string): boolean {
  return (
    V1_MEMORIES_PREFIX_RE.test(pathname) || V2_MEMORIES_PREFIX_RE.test(pathname)
  );
}

/**
 * Strict memories OVERVIEW predicate — matches `/memories[/]` (v1) and
 * `/h/<slug>/memories[/]` (v2), but NOT nested detail / topic routes.
 *
 * `isMemoriesRoute` is the loose form (covers any descendant); use this
 * one when chrome is scoped to the overview surface only — e.g.,
 * banner chrome that hides under topic / memory detail.
 */
export function isMemoriesOverviewRoute(pathname: string): boolean {
  return (
    /^\/memories\/?$/.test(pathname) ||
    /^\/h\/[^/]+\/memories\/?$/.test(pathname)
  );
}

/**
 * Brain surface predicate — `/brain[/...]` only.
 *
 * `/home` is deliberately NOT a brain-view route: it is the neutral
 * entry resolver (see app/(app)/home/page.tsx). While it decides
 * between `/brain` and the memories overview, the shell must render
 * neutral chrome — no bar, no active rail tab — so classifying it as
 * brain here would repaint the exact wrong-surface flash the resolver
 * exists to remove.
 */
export function isBrainViewRoute(pathname: string): boolean {
  return pathname === "/brain" || pathname.startsWith("/brain/");
}

/**
 * isChatSurfaceRoute — true on routes where the new chat surface
 * (Phase 3.7c) takes over the page. Today that's `/brain[/...]`
 * which renders <ChatBrainView />. The chat surface owns its own
 * composer, so the global bar + ScanRestButton FAB are suppressed
 * on these routes (option A from the chat-integration plan): no
 * competing text inputs, no FAB redirecting users away from chat.
 *
 * Kept distinct from `isBrainViewRoute` so the legacy "brain view"
 * affordances in bar-context (drag-and-drop file capture, "/" slash
 * commands, recent-navigation tracking) keep working in case a
 * future surface wants to reuse the brain-view bucket without
 * inheriting the chat-surface suppression.
 */
export function isChatSurfaceRoute(pathname: string): boolean {
  return pathname === "/brain" || pathname.startsWith("/brain/");
}

/**
 * isChatSessionRoute — true ONLY on the active-session detail
 * route `/brain/sessions/<id>`. Drives mobile chrome adjustments:
 *   - MobileTopBar swaps its leading Menu icon for a Back arrow.
 *   - MobileDock is hidden so the conversation owns the full
 *     viewport (industry pattern: ChatGPT / Claude.ai mobile hide
 *     global nav inside threads).
 *
 * The sessions LIST at `/brain` is NOT a session route — that page
 * keeps the dock + drawer affordances.
 */
export function isChatSessionRoute(pathname: string): boolean {
  return pathname.startsWith("/brain/sessions/");
}

export function isInboxRoute(pathname: string): boolean {
  return V1_INBOX_PREFIX_RE.test(pathname);
}

export function isTopicRoute(pathname: string): boolean {
  return V1_TOPIC_RE.test(pathname) || V2_TOPIC_RE.test(pathname);
}

export function isMemoryDetailRoute(pathname: string): boolean {
  // Topic and "new" routes are nested under /memories — exclude them.
  const id =
    pathname.match(V1_MEMORY_DETAIL_RE)?.[1] ??
    pathname.match(V2_MEMORY_DETAIL_RE)?.[1];
  return Boolean(id && !RESERVED_MEMORY_SEGMENTS.has(id));
}

/**
 * Bar-context "memory surface" predicate. The bar renders its
 * memory-surface chrome (drop targets, focus-to-engage docking) on
 * memories routes AND on /inbox (per route-aware bar setup at
 * bar-context.tsx:368). Topic and memory-detail nested routes count.
 *
 * v1 topic routes (`/memories/topics/:id`) match isMemoriesRoute via
 * the `/memories` prefix. v2 topic routes (`/h/:slug/topics/:id`) are a
 * sibling of memories, so they need the explicit isTopicRoute branch
 * — without it, the bar would derive view="none" on v2 topic pages and
 * lose drop-target / topic-route-aware chip behavior.
 */
export function isBarMemorySurfaceRoute(pathname: string): boolean {
  return (
    isMemoriesRoute(pathname) ||
    isTopicRoute(pathname) ||
    isInboxRoute(pathname)
  );
}

// ── Extractors ──────────────────────────────────────────────────────────

/** Memory id from a path, or `undefined` if the path isn't a memory detail. */
export function getMemoryIdForPath(pathname: string): string | undefined {
  const id =
    pathname.match(V1_MEMORY_DETAIL_RE)?.[1] ??
    pathname.match(V2_MEMORY_DETAIL_RE)?.[1];
  if (!id || RESERVED_MEMORY_SEGMENTS.has(id)) return undefined;
  return id;
}

/** Topic id from a path (topic-detail routes only), or `undefined`. */
export function getTopicIdForPath(pathname: string): string | undefined {
  return (
    pathname.match(V1_TOPIC_RE)?.[1] ??
    pathname.match(V2_TOPIC_RE)?.[1] ??
    undefined
  );
}

/**
 * The active topic id for highlighting in the tree. On topic detail
 * routes this is the topic in the URL. On memory detail routes the
 * caller passes the memory's `topic_id` (looked up from the memory data)
 * because the URL doesn't carry it.
 */
export function getActiveTopicIdForPath(input: {
  pathname: string;
  memoryTopicId?: string | null;
}): string | undefined {
  const topicId = getTopicIdForPath(input.pathname);
  if (topicId) return topicId;
  if (getMemoryIdForPath(input.pathname)) {
    return input.memoryTopicId || undefined;
  }
  return undefined;
}

/**
 * The hub slug from a v2 path. Returns `null` for v1 routes (which
 * don't carry a slug — hub identity comes from the active-hub context).
 */
export function getHubSlugForPath(pathname: string): string | null {
  return pathname.match(V2_HUB_SLUG_RE)?.[1] ?? null;
}

// ── Path builders ───────────────────────────────────────────────────────

/**
 * Build a memories-overview path. `slug = null` selects v1 (`/memories`),
 * a string selects v2 (`/h/<slug>/memories`).
 *
 * Most callers should pass the active-hub slug from
 * `hubRouteSlug(activeHub.hub, currentUserId)`; passing `null` is the
 * fallback for shell-v1 callers OR when the active hub isn't loaded yet.
 */
export function buildMemoriesPath(slug: string | null): string {
  return slug ? `/h/${slug}/memories` : "/memories";
}

export function buildTopicPath(slug: string | null, topicId: string): string {
  return slug ? `/h/${slug}/topics/${topicId}` : `/memories/topics/${topicId}`;
}

export function buildMemoryDetailPath(
  slug: string | null,
  memoryId: string,
): string {
  return slug ? `/h/${slug}/memories/${memoryId}` : `/memories/${memoryId}`;
}

// ── Route → shell-v2 tab ────────────────────────────────────────────────

import type { ShellTabId } from "@/components/shell-v2/shell-tabs";

/**
 * Resolve a pathname to the shell-v2 tab that should be highlighted in
 * `<LeftRail>`. Returns `null` for routes that don't belong to any tab
 * (e.g., `/`, `/login`) — the rail renders no active state in that case.
 *
 * Matches both v1 and v2 path shapes so the rail works regardless of
 * whether the caller is on a legacy `/memories` route or a new
 * `/h/<slug>/memories` route.
 */
export function getShellTabForPath(pathname: string): ShellTabId | null {
  // /brain plus descendants. `/home` intentionally returns null — it
  // is the neutral entry resolver and the rail must show no active
  // tab while it decides between /brain and the memories overview.
  if (isBrainViewRoute(pathname)) return "brain";
  // /agents and any descendant
  if (pathname === "/agents" || pathname.startsWith("/agents/"))
    return "agents";
  // /inbox and any descendant
  if (isInboxRoute(pathname)) return "inbox";
  // memories overview, memory detail, topic detail (v1 + v2)
  if (isMemoriesRoute(pathname) || isTopicRoute(pathname)) return "memories";
  return null;
}
