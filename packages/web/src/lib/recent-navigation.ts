import {
  isBrainViewRoute,
  isMemoriesRoute,
  isTopicRoute,
  isPulseRoute,
} from "./route-helpers";

const OPEN_RECENT_ONCE_KEY = "memax-open-recent-once";
const SURFACE_TRANSITION_KEY = "memax-surface-transition";

export type SurfaceTransitionKind =
  | "hub-switch"
  | "brain-to-memory"
  | "memory-to-brain";

export interface SurfaceTransitionRequest {
  kind: SurfaceTransitionKind;
  hubName?: string;
  hubBadgeLabel?: string;
  hubAccent?: string;
  hubKind?: "personal" | "team";
  minDurationMs?: number;
  maxDurationMs?: number;
  waitFor?: Promise<unknown>;
}

type LiveSurfaceTransitionListener = (
  request: SurfaceTransitionRequest | null,
) => void;

type AppSurface = "brain" | "memory" | null;

let liveSurfaceTransition: SurfaceTransitionRequest | null = null;
const liveSurfaceTransitionListeners = new Set<LiveSurfaceTransitionListener>();

function emitLiveSurfaceTransition() {
  liveSurfaceTransitionListeners.forEach((listener) => {
    listener(liveSurfaceTransition);
  });
}

function getAppSurface(pathname: string): AppSurface {
  // Brain surface — v1 `/home` AND v2 `/brain` (both mount BrainView).
  if (isBrainViewRoute(pathname)) return "brain";
  // Memory-side surface — v1 `/memories[/...]` + v2 `/h/<slug>/memories[/...]`
  // + topic detail under both shells + the pulse board (`/h/<slug>/pulse`
  // and its bare `/pulse` forwarder). Pulse
  // belongs here per the bar's view derivation; keep these in sync with
  // bar-context.tsx so brain↔memory transitions fire consistently.
  if (
    isMemoriesRoute(pathname) ||
    isTopicRoute(pathname) ||
    isPulseRoute(pathname)
  ) {
    return "memory";
  }
  return null;
}

export function requestRecentExpandOnArrival(hubId: string) {
  if (typeof globalThis.sessionStorage === "undefined") return;
  sessionStorage.setItem(OPEN_RECENT_ONCE_KEY, hubId);
}

export function consumeRecentExpandOnArrival(hubId?: string): boolean {
  if (!hubId || typeof globalThis.sessionStorage === "undefined") return false;
  const requestedHubId = sessionStorage.getItem(OPEN_RECENT_ONCE_KEY);
  if (requestedHubId !== hubId) return false;
  sessionStorage.removeItem(OPEN_RECENT_ONCE_KEY);
  return true;
}

export function requestHubSwitchTransition() {
  requestSurfaceTransition({ kind: "hub-switch" });
}

export function requestNamedHubSwitchTransition({
  hubName,
  hubBadgeLabel,
  hubAccent,
  hubKind,
}: {
  hubName: string;
  hubBadgeLabel?: string;
  hubAccent?: string;
  hubKind?: "personal" | "team";
}) {
  requestSurfaceTransition({
    kind: "hub-switch",
    hubName,
    hubBadgeLabel,
    hubAccent,
    hubKind,
  });
}

export function startLiveSurfaceTransition(request: SurfaceTransitionRequest) {
  liveSurfaceTransition = request;
  emitLiveSurfaceTransition();
}

/**
 * True while `request` is still the CURRENT live transition. Timer
 * callbacks captured by a superseded effect run use this to tell
 * whether they're acting on their own transition or someone else's.
 */
export function isLiveSurfaceTransition(
  request: SurfaceTransitionRequest,
): boolean {
  return liveSurfaceTransition === request;
}

export function clearLiveSurfaceTransition(request?: SurfaceTransitionRequest) {
  if (!liveSurfaceTransition) return;
  // Identity check (adversarial review, 2026-09-14): a hub switch
  // superseded mid-warm leaves an uncancellable `.finally(finish)` on
  // its warm promise; when that settles it schedules orphan timers
  // that would otherwise blind-clear whatever transition is live NOW
  // — hiding a later, still-in-flight switch's overlay early. Callers
  // pass the request their run belongs to; a stale caller no-ops.
  if (request && liveSurfaceTransition !== request) return;
  liveSurfaceTransition = null;
  emitLiveSurfaceTransition();
}

export function subscribeLiveSurfaceTransition(
  listener: LiveSurfaceTransitionListener,
) {
  liveSurfaceTransitionListeners.add(listener);
  listener(liveSurfaceTransition);
  return () => {
    liveSurfaceTransitionListeners.delete(listener);
  };
}

export function requestSurfaceTransition(request: SurfaceTransitionRequest) {
  if (typeof globalThis.sessionStorage === "undefined") return;
  sessionStorage.setItem(SURFACE_TRANSITION_KEY, JSON.stringify(request));
}

export function consumeSurfaceTransition(): SurfaceTransitionRequest | null {
  if (typeof globalThis.sessionStorage === "undefined") return null;
  const raw = sessionStorage.getItem(SURFACE_TRANSITION_KEY);
  if (!raw) return null;
  sessionStorage.removeItem(SURFACE_TRANSITION_KEY);
  try {
    const requested = JSON.parse(raw) as SurfaceTransitionRequest;
    if (
      requested.kind === "hub-switch" ||
      requested.kind === "brain-to-memory" ||
      requested.kind === "memory-to-brain"
    ) {
      return requested;
    }
    return null;
  } catch {
    if (
      raw === "hub-switch" ||
      raw === "brain-to-memory" ||
      raw === "memory-to-brain"
    ) {
      return { kind: raw };
    }
    return null;
  }
}

export function requestSurfaceTransitionForNavigation(
  currentPathname: string,
  targetPathname: string,
) {
  const from = getAppSurface(currentPathname);
  const to = getAppSurface(targetPathname);
  if (from === "brain" && to === "memory") {
    requestSurfaceTransition({ kind: "brain-to-memory" });
  } else if (from === "memory" && to === "brain") {
    requestSurfaceTransition({ kind: "memory-to-brain" });
  }
}
