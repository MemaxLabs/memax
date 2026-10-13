/**
 * The places of a V2 space and their URLs (plan §6.3, HANDOFF §6). The
 * rail order is Today, Review, Briefs, Memories, Handoffs, Agents; a
 * team space adds Decisions after Memories. Activity and Search have a
 * route but no rail item; Dream's editions are reached from Today, and
 * light it.
 */
import type { SpaceKind } from "./data/types";

/** URL segments under /[space]/ that this build serves. */
export const PLACE_ROUTES = [
  "today",
  "review",
  "brief",
  "memories",
  "decisions",
  "handoffs",
  "agents",
  "dream",
  "activity",
  "search",
] as const;

export type PlaceRoute = (typeof PLACE_ROUTES)[number];

/** The rail's ids (NavPlace in @memaxlabs/ledger): "briefs" is the Brief's place. */
export type RailPlace =
  | "today"
  | "review"
  | "briefs"
  | "memories"
  | "decisions"
  | "handoffs"
  | "agents";

const RAIL_ROUTE: Record<RailPlace, PlaceRoute> = {
  today: "today",
  review: "review",
  briefs: "brief",
  memories: "memories",
  decisions: "decisions",
  handoffs: "handoffs",
  agents: "agents",
};

export function isPlaceRoute(
  segment: string | undefined,
): segment is PlaceRoute {
  return (PLACE_ROUTES as readonly string[]).includes(segment ?? "");
}

/** The rail, in order, for a space of this kind. */
export function railPlaces(kind: SpaceKind): RailPlace[] {
  const places: RailPlace[] = ["today", "review", "briefs", "memories"];
  if (kind === "team") places.push("decisions");
  places.push("handoffs", "agents");
  return places;
}

export function routeOfRail(place: RailPlace): PlaceRoute {
  return RAIL_ROUTE[place];
}

/** Which rail item a route lights up; Activity and Search light none, an edition Today. */
export function railOfRoute(
  route: PlaceRoute | undefined,
): RailPlace | undefined {
  if (!route || route === "activity" || route === "search") return undefined;
  if (route === "dream") return "today";
  return route === "brief" ? "briefs" : route;
}

/** An edition's page: /memax-v2/dream/214, or the latest at /memax-v2/dream. */
export function editionHref(space: string, n?: number): string {
  const base = placeHref(space, "dream");
  return n === undefined ? base : `${base}/${n}`;
}

/** Whether a space has this place: Decisions is a team space's. */
export function spaceHasPlace(kind: SpaceKind, route: PlaceRoute): boolean {
  return route !== "decisions" || kind === "team";
}

export function placeHref(space: string, route: PlaceRoute): string {
  return `/${encodeURIComponent(space)}/${route}`;
}

/**
 * Search across spaces, from a space: /memax-v2/search?q=deploy. The
 * space is where the frame stays; the search covers every space.
 */
export function searchHref(space: string, query?: string): string {
  const base = placeHref(space, "search");
  const q = query?.trim();
  return q ? `${base}?q=${encodeURIComponent(q)}` : base;
}

/**
 * Where a space still on V1 opens in V1: its memories, under V1's hub
 * routes (/h/<slug>/memories), which name a person's own personal space
 * "personal". A V1 page, so following it is a full load into V1.
 */
export function v1SpaceHref(space: { slug: string; kind: SpaceKind }): string {
  const slug = space.kind === "personal" ? "personal" : space.slug;
  return `/h/${encodeURIComponent(slug)}/memories`;
}

/**
 * Where a decision gate opens: Review, with its card selected. A gate
 * that ended still opens there, saying how it ended.
 */
export function gateHref(space: string, ref: string): string {
  return `${placeHref(space, "review")}?gate=${encodeURIComponent(ref)}`;
}

/**
 * Where ⌘1–⌘9 and the switcher land: the same place in the other space,
 * or its Today when it hasn't got one.
 */
export function switchHref(
  target: { slug: string; kind: SpaceKind },
  current: PlaceRoute | undefined,
): string {
  const route =
    current && spaceHasPlace(target.kind, current) ? current : "today";
  return placeHref(target.slug, route);
}

export const SETTINGS_HREF = "/settings/account";
