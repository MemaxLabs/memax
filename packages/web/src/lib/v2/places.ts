/**
 * The places of a V2 space and their URLs (plan §6.3, HANDOFF §6). The
 * rail order is Today, Review, Briefs, Memories, Handoffs, Agents; a
 * team space adds Decisions after Memories. Activity has a route but no
 * rail item.
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
  "activity",
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

/** Which rail item a route lights up; Activity lights none. */
export function railOfRoute(
  route: PlaceRoute | undefined,
): RailPlace | undefined {
  if (!route || route === "activity") return undefined;
  return route === "brief" ? "briefs" : route;
}

/** Whether a space has this place: Decisions is a team space's. */
export function spaceHasPlace(kind: SpaceKind, route: PlaceRoute): boolean {
  return route !== "decisions" || kind === "team";
}

export function placeHref(space: string, route: PlaceRoute): string {
  return `/${encodeURIComponent(space)}/${route}`;
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
