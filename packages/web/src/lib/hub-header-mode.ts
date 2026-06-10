"use client";

import type { Settings as SDKSettings } from "memax-sdk";

export type HubHeaderMode = NonNullable<SDKSettings["hub_header_aurora_mode"]>;

/**
 * Minimum hub shape needed to resolve an aurora mode. Kept structural so
 * both the SDK `Hub` and the web-local auth context `Hub` (whose hub_type
 * is declared as `string`) satisfy it without a cast.
 */
interface HubLike {
  hub_type: string;
  header_aurora_mode?: string;
}

const HUB_HEADER_MODE_STORAGE_KEY = "memax_ui_hub_header_aurora_mode";
const DEFAULT_MODE: HubHeaderMode = "signature";

function isValidHubHeaderMode(value: unknown): value is HubHeaderMode {
  return value === "none" || value === "signature" || value === "time";
}

export function getStoredHubHeaderMode(): HubHeaderMode | undefined {
  if (typeof window === "undefined") return undefined;
  const value = window.localStorage.getItem(HUB_HEADER_MODE_STORAGE_KEY);
  return isValidHubHeaderMode(value) ? value : undefined;
}

export function setStoredHubHeaderMode(mode: HubHeaderMode | undefined) {
  if (typeof window === "undefined") return;
  if (!mode) {
    window.localStorage.removeItem(HUB_HEADER_MODE_STORAGE_KEY);
    return;
  }
  window.localStorage.setItem(HUB_HEADER_MODE_STORAGE_KEY, mode);
}

export function applyStoredHubHeaderMode<T extends SDKSettings>(
  settings: T,
): T {
  const stored = getStoredHubHeaderMode();
  if (!stored) return settings;
  return {
    ...settings,
    hub_header_aurora_mode: stored,
  };
}

/**
 * Resolve the aurora mode for a given hub. Personal hubs fall back to the
 * user's settings value (1 user ↔ 1 personal hub). Team hubs read the
 * per-hub override stored on the Hub row; empty/missing means "signature".
 * Returns the default when no hub is supplied (e.g. pre-hydration render).
 */
export function resolveHubHeaderMode(
  hub: HubLike | null | undefined,
  settings: Pick<SDKSettings, "hub_header_aurora_mode"> | null | undefined,
): HubHeaderMode {
  if (!hub) {
    // Unresolved hub (pre-hydration render): return the neutral default
    // rather than reading user settings. Otherwise a team-hub page flash-
    // renders with the viewer's personal aurora mode for a beat before
    // the per-hub value lands.
    return DEFAULT_MODE;
  }
  if (hub.hub_type === "personal") {
    const fromSettings = settings?.hub_header_aurora_mode;
    return isValidHubHeaderMode(fromSettings) ? fromSettings : DEFAULT_MODE;
  }
  const fromHub = hub.header_aurora_mode;
  return isValidHubHeaderMode(fromHub) ? fromHub : DEFAULT_MODE;
}
