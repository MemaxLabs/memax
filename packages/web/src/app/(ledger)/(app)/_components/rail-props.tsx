"use client";

import { Menu } from "@base-ui/react/menu";
import type { NavItem, NavRailProps } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { statusLabel, statusState } from "@/lib/v2/copy";
import {
  waitingOnYou,
  type SpaceOverview,
  type SpaceSummary,
  type Viewer,
} from "@/lib/v2/data/types";
import { useKeycap } from "@/lib/v2/keymap/react";
import {
  placeHref,
  railOfRoute,
  railPlaces,
  routeOfRail,
  SETTINGS_HREF,
  type PlaceRoute,
} from "@/lib/v2/places";
import { useAgentName, useCount } from "../_lib/frame-copy";
import { useOverlays } from "../_lib/overlays";

/**
 * The NavRail's props for a space (HANDOFF §6): the places in order
 * with real links, Review's ochre count and Handoffs' plain one, the
 * switcher as a Base UI Menu trigger, ⌘K, settings, the person and the
 * status line.
 */
export function useRailProps({
  space,
  overview,
  route,
  inSettings,
  viewer,
}: {
  space: SpaceSummary;
  overview: SpaceOverview | undefined;
  route: PlaceRoute | undefined;
  inSettings: boolean;
  viewer: Viewer | null;
}): NavRailProps {
  const { t } = useLocale();
  const copy = t.ledger.app;
  const { openCommand } = useOverlays();
  const askShortcut = useKeycap("command.open");
  const agentName = useAgentName();
  const reviewLabel = useCount(
    copy.frame.reviewCountOne,
    copy.frame.reviewCount,
  );
  const handoffLabel = useCount(
    copy.frame.handoffCountOne,
    copy.frame.handoffCount,
  );

  const items: NavItem[] = railPlaces(space.kind).map((place) => {
    const item: NavItem = {
      id: place,
      href: placeHref(space.slug, routeOfRail(place)),
    };
    // Review's memories and the questions agents wait on, both in Review.
    const waiting = overview ? waitingOnYou(overview) : 0;
    if (place === "review" && waiting) {
      item.count = waiting;
      item.tone = "pending";
      item.countLabel = reviewLabel(waiting);
    }
    if (place === "handoffs" && overview?.openHandoffs) {
      item.count = overview.openHandoffs;
      item.countLabel = handoffLabel(overview.openHandoffs);
    }
    return item;
  });

  return {
    items,
    active: inSettings ? undefined : railOfRoute(route),
    space: space.name,
    spaceKind: copy.frame.spaceKind[space.kind],
    spaceRender: (
      <Menu.Trigger aria-label={`${space.name}, ${copy.frame.switchSpace}`} />
    ),
    onAskClick: () => openCommand("ask"),
    askShortcut,
    settingsHref: SETTINGS_HREF,
    settingsActive: inSettings,
    person: viewer?.initials ?? "",
    personName: undefined,
    status: overview
      ? {
          state: statusState(overview.status),
          label: statusLabel(copy, overview.status, agentName),
        }
      : { state: "working", label: copy.frame.loading },
  };
}
