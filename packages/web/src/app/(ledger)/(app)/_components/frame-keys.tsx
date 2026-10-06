"use client";

import { useRouter } from "next/navigation";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useHotkey } from "@/lib/v2/keymap/react";
import { placeHref, switchHref, type PlaceRoute } from "@/lib/v2/places";
import { useOverlays } from "../_lib/overlays";

/**
 * The frame's own bindings, in the app scope: ⌘K, G T / G R / G B /
 * G M, ⌘1–⌘9 and `?`. Pages and layers add theirs in their own scopes.
 */
export function FrameKeys({
  space,
  spaces,
  route,
}: {
  space: SpaceSummary | undefined;
  spaces: SpaceSummary[];
  route: PlaceRoute | undefined;
}) {
  const router = useRouter();
  const { openCommand, setKeysOpen } = useOverlays();

  const go = (to: PlaceRoute) => {
    if (space) router.push(placeHref(space.slug, to));
  };

  useHotkey("command.open", () => openCommand("ask"));
  useHotkey("help.keys", () => setKeysOpen(true));
  useHotkey("go.today", () => go("today"), { enabled: Boolean(space) });
  useHotkey("go.review", () => go("review"), { enabled: Boolean(space) });
  useHotkey("go.brief", () => go("brief"), { enabled: Boolean(space) });
  useHotkey("go.memories", () => go("memories"), { enabled: Boolean(space) });
  useHotkey("space.switch", (_, { index }) => {
    const target = spaces[index];
    // No space there: leave ⌘n to the browser.
    if (!target) return false;
    if (target.slug !== space?.slug) router.push(switchHref(target, route));
  });
  return null;
}
