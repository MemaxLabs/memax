"use client";

import type { Translations } from "@/i18n/locales/en";

export const DEFAULT_PERSONAL_HUB_NAME = "Memory Hub";

interface HubDisplayInput {
  name?: string | null;
  icon?: string | null;
  hub_type?: string | null;
  hubType?: string | null;
}

interface HubDisplayViewer {
  name?: string | null;
  displayName?: string | null;
}

function getHubType(hub: HubDisplayInput) {
  return hub.hub_type ?? hub.hubType ?? "";
}

function normalize(value?: string | null) {
  return value?.trim() ?? "";
}

const graphemeSegmenter =
  typeof Intl !== "undefined" && "Segmenter" in Intl
    ? new Intl.Segmenter(undefined, { granularity: "grapheme" })
    : null;

function firstDisplayGlyph(value?: string | null) {
  const normalized = normalize(value);
  if (!normalized) return "";
  if (graphemeSegmenter) {
    for (const part of graphemeSegmenter.segment(normalized)) {
      return part.segment;
    }
  }
  return Array.from(normalized)[0] ?? "";
}

function legacyPersonalHubNames(viewer?: HubDisplayViewer) {
  const names = new Set<string>();
  const viewerName = normalize(viewer?.name);
  const viewerDisplayName = normalize(viewer?.displayName);
  if (viewerName) names.add(`${viewerName}'s Hub`);
  if (viewerDisplayName) names.add(`${viewerDisplayName}'s Hub`);
  return names;
}

export function getHubDisplayName(
  hub: HubDisplayInput,
  t: Translations,
  viewer?: HubDisplayViewer,
) {
  const hubType = getHubType(hub);
  const hubName = normalize(hub.name);
  if (hubType !== "personal") return hubName;
  if (!hubName) return t.hubs.defaultPersonalHubName;
  if (hubName === DEFAULT_PERSONAL_HUB_NAME)
    return t.hubs.defaultPersonalHubName;
  if (legacyPersonalHubNames(viewer).has(hubName)) {
    return t.hubs.defaultPersonalHubName;
  }
  return hubName;
}

export function getHubDisplayInitial(
  hub: HubDisplayInput,
  t: Translations,
  viewer?: HubDisplayViewer,
) {
  const icon = normalize(hub.icon);
  if (icon) return icon;
  const label = getHubDisplayName(hub, t, viewer);
  const glyph = firstDisplayGlyph(label);
  return glyph ? glyph.toLocaleUpperCase() : "?";
}
