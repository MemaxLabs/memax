"use client";

/**
 * ShellStateProvider — UI state for shell-v2 chrome.
 *
 * Two flavors of state, intentionally co-located so both flavors share
 * one provider instance and one re-render frontier:
 *
 * **Persisted user preferences** (localStorage, `memax_shell_state`):
 *   1. `collapsed` — left rail collapsed (56px) vs expanded (196px).
 *      Default: true (collapsed). Most users want the rail compact
 *      unless they're actively navigating; expansion is the explicit
 *      gesture (hovering the brand mark or clicking the toggle).
 *   2. `secondaryHidden` — per-tab visibility of the secondary panel
 *      (currently only Memories has one; the API stays per-tab so
 *      future tabs with secondaries can opt in without re-shaping).
 *      Default: false (visible). The topic explorer is the core
 *      navigation surface inside Memories; hiding it is the explicit
 *      gesture (close chevron or click the active rail tab again —
 *      Notion / Linear convention).
 *
 * **Transient session state** (NOT persisted, route + scroll ephemera):
 *   3. `barScrollHidden` — sticky scroll-hide flag for the global bar
 *      (plan 26 phase 4). Resets on reload by design.
 *   4. `barShown` — whether the bar is currently inline on the route.
 *      Mirrors `shouldShow` from app-shell-client so ScanRestButton
 *      can surface as the persistent re-entry on bar-less routes.
 *   5. `isHydrated` — true once the persisted state has been read from
 *      localStorage. Components that animate the rail width or panel
 *      slide read this to skip the first-paint animation.
 *
 * Storage strategy:
 *   - Single localStorage key (`memax_shell_state`) holds the JSON-encoded
 *     state. One write per change.
 *   - Hydration is corruption-resistant: a malformed payload (parse
 *     failure, missing fields, invalid types) falls back to defaults
 *     silently. localStorage outages (private mode, quota) are also
 *     tolerated — the session keeps working in-memory; preferences just
 *     don't persist across reloads.
 *   - SSR-safe: the initializer reads `typeof window === "undefined"`
 *     before touching localStorage; server renders use defaults.
 *
 * Provider placement: mounted at the v2 shell root (above the rail and
 * the secondary panel). When v2 ships behind a cookie flag, the v1 shell
 * never instantiates this provider — it's a v2-only concept.
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { SHELL_TABS, type ShellTabId } from "@/components/shell-v2/shell-tabs";

// Re-export for callers that already import from this module.
export type { ShellTabId };

interface ShellStateValue {
  collapsed: boolean;
  toggleCollapsed: () => void;
  setCollapsed: (next: boolean) => void;

  /** Per-tab visibility preference for the secondary panel. */
  secondaryHidden: Record<ShellTabId, boolean>;
  /**
   * Toggle the secondary panel for a specific tab. Used by the rail's
   * "click active tab again to toggle" affordance and by the close
   * chevron inside the panel header.
   */
  toggleSecondary: (tab: ShellTabId) => void;
  setSecondaryHidden: (tab: ShellTabId, hidden: boolean) => void;

  /**
   * `true` once the persisted state has been read from localStorage on
   * the client. Server renders + the first client render expose `false`
   * so React hydration matches; the second client render flips to
   * `true` and reflects the persisted state. Components that animate
   * the rail width or panel slide MUST read this flag to skip the
   * initial enter animation when hydrated state differs from defaults
   * — without this gate the rail visibly snaps from collapsed (default)
   * to expanded (persisted) on every page load for users who prefer
   * the expanded shell.
   */
  isHydrated: boolean;

  /**
   * Sticky scroll-hide state for the global bar. `true` once the user
   * has scrolled the bar off-screen on a scrollable surface; resets
   * to `false` when the user reaches the top OR explicitly opens the
   * bar via the FAB. Lives here (rather than BarContext or local to
   * any single component) so both the bar's hide rendering and the
   * FAB-style ScanRestButton can react from one source. Plan 26
   * phase 4 — was duplicated between two components which would have
   * been racy.
   */
  barScrollHidden: boolean;
  setBarScrollHidden: (next: boolean) => void;
  /**
   * Whether the bar is currently inline on the route (vs route-hidden
   * because the route doesn't host a bar surface — e.g., /agents,
   * /pulse in some configurations). When `false`, the bar's glass
   * material is skipped (no floating blur rectangle) and ScanRestButton
   * surfaces as the persistent re-entry. Plan 26 follow-up: previously
   * the bar's outer glass-bar class always rendered, which on bar-less
   * routes (view==="none" without overlay) painted a blurry strip at
   * the bottom that wasn't interactable.
   */
  barShown: boolean;
  setBarShown: (next: boolean) => void;
}

const STORAGE_KEY = "memax_shell_state";

interface PersistedState {
  collapsed: boolean;
  secondaryHidden: Record<ShellTabId, boolean>;
}

// Defaults derived from SHELL_TABS so adding a new tab in shell-tabs.ts
// is a one-line change — secondaryHidden auto-grows with no map fiddling.
const DEFAULT_SECONDARY_HIDDEN: Record<ShellTabId, boolean> =
  Object.fromEntries(SHELL_TABS.map((t) => [t.id, false])) as Record<
    ShellTabId,
    boolean
  >;

const DEFAULT_STATE: PersistedState = {
  collapsed: true,
  secondaryHidden: DEFAULT_SECONDARY_HIDDEN,
};

const SHELL_TAB_IDS = new Set<ShellTabId>(SHELL_TABS.map((t) => t.id));

/**
 * Per-tab boolean validation. A persisted payload may have:
 *   - missing tab keys (older state, before a new tab landed) → fill from
 *     default
 *   - unknown tab keys (older state, after a tab was renamed) → drop
 *   - non-boolean values for existing tabs (e.g., a string from a manual
 *     edit) → fall back to default for that tab
 *
 * Returns a fresh, well-typed object every time so persistence writes
 * the canonical shape back next time the value is mutated.
 */
function sanitizeSecondaryHidden(raw: unknown): Record<ShellTabId, boolean> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return { ...DEFAULT_SECONDARY_HIDDEN };
  }
  const out: Record<ShellTabId, boolean> = { ...DEFAULT_SECONDARY_HIDDEN };
  for (const tabId of SHELL_TAB_IDS) {
    const value = (raw as Record<string, unknown>)[tabId];
    if (typeof value === "boolean") {
      out[tabId] = value;
    }
  }
  return out;
}

function parsePersistedState(raw: string | null): PersistedState {
  if (!raw) return DEFAULT_STATE;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return DEFAULT_STATE;
    const obj = parsed as Partial<PersistedState>;
    const collapsed =
      typeof obj.collapsed === "boolean"
        ? obj.collapsed
        : DEFAULT_STATE.collapsed;
    const secondaryHidden = sanitizeSecondaryHidden(obj.secondaryHidden);
    return { collapsed, secondaryHidden };
  } catch {
    return DEFAULT_STATE;
  }
}

function readPersistedState(): PersistedState {
  if (typeof window === "undefined") return DEFAULT_STATE;
  try {
    return parsePersistedState(localStorage.getItem(STORAGE_KEY));
  } catch {
    // localStorage outage (private mode, quota, security policy) — defaults.
    return DEFAULT_STATE;
  }
}

function persistState(next: PersistedState) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // localStorage unavailable — tolerate silently. State stays in-memory
    // for the session.
  }
}

const ShellStateContext = createContext<ShellStateValue | null>(null);

export function ShellStateProvider({ children }: { children: ReactNode }) {
  // First render uses defaults so server-rendered HTML and client's first
  // render match — required for React hydration. We read localStorage in
  // an effect, then update state. Components that need to skip the
  // first-paint animation read `isHydrated` (false during SSR + first
  // client paint, true after).
  const [state, setState] = useState<PersistedState>(DEFAULT_STATE);
  const [isHydrated, setIsHydrated] = useState(false);
  // Bar scroll-hide is intentionally NOT persisted (session-scoped).
  // Reloading the page should always start with the bar visible.
  const [barScrollHidden, setBarScrollHidden] = useState(false);
  // Default `true` — most routes host the bar inline. App-shell-client
  // flips it to `false` on routes that don't (e.g., /agents).
  const [barShown, setBarShown] = useState(true);

  // Read localStorage exactly once on mount. We deliberately don't put
  // `state` in the dependency array — this is a one-shot hydration, not
  // a sync loop.
  useEffect(() => {
    const persisted = readPersistedState();
    setState(persisted);
    setIsHydrated(true);
    // We re-canonicalize the localStorage shape on hydration: if the
    // payload had unknown keys or invalid types, sanitizeSecondaryHidden
    // already cleaned them up. Persist the canonical shape so the next
    // load is a clean JSON.parse.
    persistState(persisted);
  }, []);

  // Persist subsequent mutations. Skip writes before hydration so we
  // don't overwrite real persisted state with defaults on the first
  // render pass (the hydration effect above handles the initial write).
  useEffect(() => {
    if (!isHydrated) return;
    persistState(state);
  }, [state, isHydrated]);

  const toggleCollapsed = useCallback(() => {
    setState((prev) => ({ ...prev, collapsed: !prev.collapsed }));
  }, []);

  const setCollapsed = useCallback((next: boolean) => {
    setState((prev) =>
      prev.collapsed === next ? prev : { ...prev, collapsed: next },
    );
  }, []);

  const toggleSecondary = useCallback((tab: ShellTabId) => {
    setState((prev) => ({
      ...prev,
      secondaryHidden: {
        ...prev.secondaryHidden,
        [tab]: !prev.secondaryHidden[tab],
      },
    }));
  }, []);

  const setSecondaryHidden = useCallback((tab: ShellTabId, hidden: boolean) => {
    setState((prev) => {
      if (prev.secondaryHidden[tab] === hidden) return prev;
      return {
        ...prev,
        secondaryHidden: { ...prev.secondaryHidden, [tab]: hidden },
      };
    });
  }, []);

  const value = useMemo<ShellStateValue>(
    () => ({
      collapsed: state.collapsed,
      toggleCollapsed,
      setCollapsed,
      secondaryHidden: state.secondaryHidden,
      toggleSecondary,
      setSecondaryHidden,
      isHydrated,
      barScrollHidden,
      setBarScrollHidden,
      barShown,
      setBarShown,
    }),
    [
      state,
      isHydrated,
      toggleCollapsed,
      setCollapsed,
      toggleSecondary,
      setSecondaryHidden,
      barScrollHidden,
      barShown,
    ],
  );

  return (
    <ShellStateContext.Provider value={value}>
      {children}
    </ShellStateContext.Provider>
  );
}

/**
 * Hook for shell-v2 chrome to read + mutate UI state.
 * Throws if used outside a `<ShellStateProvider>`.
 */
export function useShellState(): ShellStateValue {
  const value = useContext(ShellStateContext);
  if (!value) {
    throw new Error(
      "useShellState must be used inside a <ShellStateProvider>. v2 chrome " +
        "components require this provider mounted at the shell root.",
    );
  }
  return value;
}
