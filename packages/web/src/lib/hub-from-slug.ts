/**
 * Resolve the URL `[slug]` segment in `/h/[slug]/...` routes to a hub UUID.
 *
 * The resolver is purely client-side — no server endpoint needed. It uses
 * the `useAuth().hubs` list (already populated on app load) and these
 * rules, applied in order:
 *
 *   1. Literal `personal` → the user's own personal hub.
 *      Personal hubs are auto-created at signup, owned by the user, and
 *      contain only the owner as a member. So `hubs.find(h.hub.hub_type ===
 *      "personal")` returns the caller's own personal hub deterministically.
 *
 *   2. UUID-shaped slug → first try `hub.id` match (deep-link / SDK
 *      consumer / agent path), then fall back to `hub.slug` match.
 *      The fallback matters because production assigns
 *      `personal_hub.slug = user.id` (auth.go:1735), so a personal hub's
 *      slug field is itself UUID-shaped. Without the fallback, pasting
 *      a user-id into the URL would 404 even though it could match the
 *      personal hub's slug. ID compare is case-insensitive (UUIDs are
 *      hex-case-insensitive per RFC 4122 §3) — slug compare stays
 *      case-sensitive (slugs are URL identity, case carries meaning).
 *
 *   3. Otherwise → match by `hub.slug`.
 *      Team hub slugs are globally unique (baseline migration enforces
 *      `UNIQUE(slug)` on the `hubs` table), so this resolves
 *      deterministically across users in different orgs.
 *
 * Returns `null` when no match — caller should render a 404 / "hub not
 * found" route. Never throws.
 *
 * NOTE: this never resolves to a hub the caller isn't a member of.
 * `useAuth().hubs` only contains the caller's hubs, so a UUID or slug
 * pointing at someone else's private space yields null — no info leak.
 */

// 8-4-4-4-12 hex with hyphens, RFC 4122 shape. Lowercase or uppercase.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isUUIDLike(value: string): boolean {
  return UUID_RE.test(value);
}

/**
 * Structural shape accepted by the resolver. Loose typing for `hub_type`
 * (string instead of `HubType` literal union) so callers can pass
 * either the SDK's `HubWithRole` OR the web app's older
 * `lib/auth.tsx#HubWithRole` (which has `hub_type: string`). The
 * resolver only compares against the literal `"personal"`, so any
 * other string value routes through the slug field.
 */
export interface HubWithRoleShape {
  hub: {
    id: string;
    slug: string;
    hub_type: string;
  };
}

export interface HubFromSlugInput {
  slug: string;
  hubs: HubWithRoleShape[];
}

/**
 * Resolve the slug to a hub UUID. Returns null when no hub matches.
 *
 * Inputs are read-only and never mutated. Pure function — safe to call
 * inside a render or selector.
 */
export function resolveHubFromSlug({
  slug,
  hubs,
}: HubFromSlugInput): string | null {
  if (!slug) return null;

  if (slug === "personal") {
    const personal = hubs.find((entry) => entry.hub.hub_type === "personal");
    return personal?.hub.id ?? null;
  }

  // UUID-shaped slug: try ID match first (case-insensitive per RFC 4122),
  // then fall through to slug match. Personal hubs have UUID-shaped slugs
  // (slug = user.id), so a UUID-shaped URL segment may be either a real
  // hub ID OR a personal-hub slug — both must resolve.
  if (isUUIDLike(slug)) {
    const slugLower = slug.toLowerCase();
    const byId = hubs.find((entry) => entry.hub.id.toLowerCase() === slugLower);
    if (byId) return byId.hub.id;
    // fall through to slug match
  }

  const bySlug = hubs.find((entry) => entry.hub.slug === slug);
  return bySlug?.hub.id ?? null;
}

/**
 * Inverse helper: given a hub, produce the canonical URL slug for routing.
 *
 *   - The caller's own personal hub → "personal" (the alias)
 *   - Any other hub → its `slug` field
 *
 * Used by hub-switcher, breadcrumbs, copy-link affordances, and anywhere
 * else that needs to render a route URL for a hub.
 *
 * `currentUserId` is passed explicitly so the helper stays pure (no
 * useAuth() inside). When the caller's own personal hub matches, we
 * return "personal" — for OTHER users' hubs (impossible today since
 * personal hubs have a single member, but kept correct for forward-
 * compatibility) the helper returns the literal slug.
 */
export function hubRouteSlug(
  // Loose typing for hub_type accepts both the SDK's `HubType` literal
  // union and the web app's older `Hub.hub_type: string` shape. We only
  // compare against the literal `"personal"`; any other value (including
  // future hub types) routes through the slug field.
  hub: {
    id: string;
    slug: string;
    hub_type: string;
    owner_id: string;
  },
  currentUserId: string,
): string {
  if (hub.hub_type === "personal" && hub.owner_id === currentUserId) {
    return "personal";
  }
  return hub.slug;
}
