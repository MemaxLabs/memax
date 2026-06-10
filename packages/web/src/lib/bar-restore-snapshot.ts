import type { BarPhase, MobileComposeState } from "@/lib/bar-interaction";

const KEY = "memax-bar-restore";

/**
 * Snapshot of the bar's user-visible state at the moment of a
 * result-tap navigation. Persisted in sessionStorage so it survives
 * iOS Safari bfcache / remount between peel-for-nav and return-to-origin.
 *
 * Only holds what can't be rederived:
 *   - `value`, `recallQuery`, `phase`, `activeNavigationIndex` — input state
 *   - `routeTopicDismissed` — the "chip was dismissed on this topic route"
 *     override that route-derived scope can't express on its own
 *   - `isBarExpanded`, `mobileComposeState` — expanded chrome state
 *
 * Recall result rows are *not* snapshotted — React Query rehydrates
 * them from the `["recall", query, hubId, topicId]` cache key.
 */
export interface BarRestoreSnapshot {
  pathname: string;
  value: string;
  recallQuery: string;
  phase: BarPhase;
  activeNavigationIndex: number | null;
  routeTopicDismissed: boolean;
  isBarExpanded: boolean;
  mobileComposeState: MobileComposeState;
  savedAt: number;
}

/** Discard snapshots older than this (prevents stale restore surprises). */
const SNAPSHOT_TTL_MS = 10 * 60 * 1000;

export function saveBarRestoreSnapshot(snapshot: BarRestoreSnapshot): void {
  if (typeof globalThis.sessionStorage === "undefined") return;
  try {
    sessionStorage.setItem(KEY, JSON.stringify(snapshot));
  } catch {
    // sessionStorage can throw in private mode / quota-exceeded; snapshot
    // is a nice-to-have, not a correctness requirement.
  }
}

/**
 * Non-destructive peek. Returns the snapshot if its origin matches
 * `pathname`, else null. Does NOT remove the snapshot from storage —
 * callers must use `consumeBarRestoreSnapshot` to finalize.
 *
 * Used by the `useReducer` initializer so React Strict Mode's double-
 * invocation of lazy init doesn't destructively eat the snapshot before
 * the mount effect can actually apply it. Peek is idempotent.
 */
export function peekBarRestoreSnapshot(
  pathname: string,
): BarRestoreSnapshot | null {
  if (typeof globalThis.sessionStorage === "undefined") return null;
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return null;
  let parsed: BarRestoreSnapshot;
  try {
    parsed = JSON.parse(raw) as BarRestoreSnapshot;
  } catch {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (!parsed || typeof parsed.pathname !== "string") {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (
    typeof parsed.savedAt !== "number" ||
    Date.now() - parsed.savedAt > SNAPSHOT_TTL_MS
  ) {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (parsed.pathname !== pathname) return null;
  return parsed;
}

/**
 * Read the snapshot and return it ONLY if its origin matches `pathname`
 * (i.e. the user has returned). Any match — forward or back — consumes
 * the snapshot so it can't fire twice.
 *
 * TTL'd: old snapshots silently evaporate.
 */
export function consumeBarRestoreSnapshot(
  pathname: string,
): BarRestoreSnapshot | null {
  if (typeof globalThis.sessionStorage === "undefined") return null;
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return null;
  let parsed: BarRestoreSnapshot;
  try {
    parsed = JSON.parse(raw) as BarRestoreSnapshot;
  } catch {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (!parsed || typeof parsed.pathname !== "string") {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (
    typeof parsed.savedAt !== "number" ||
    Date.now() - parsed.savedAt > SNAPSHOT_TTL_MS
  ) {
    sessionStorage.removeItem(KEY);
    return null;
  }
  if (parsed.pathname !== pathname) return null;
  sessionStorage.removeItem(KEY);
  return parsed;
}

export function clearBarRestoreSnapshot(): void {
  if (typeof globalThis.sessionStorage === "undefined") return;
  sessionStorage.removeItem(KEY);
}
