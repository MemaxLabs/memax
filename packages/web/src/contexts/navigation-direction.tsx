"use client";

/**
 * NavigationDirectionProvider — plan 22 P0-3.
 *
 * Tracks whether the current SPA navigation is "forward" (push), "back"
 * (popstate), or "neutral" (replace). Exposes via `useNavigationDirection()`
 * so route wrappers (e.g. `<PushTransitionWrapper>`) can pick the right
 * slide direction.
 *
 * Mechanism: patches `history.pushState` / `history.replaceState` ONCE
 * at provider mount, stamping a monotonically-increasing index into
 * `history.state.__memaxNavIndex`. On `popstate`, compares the new
 * state's index to the current index — higher = forward (rare; only
 * if a script forwarded), lower = back (the common case from browser
 * back, iOS swipe-back, or router.back).
 *
 * Why patch instead of `router.events`: Next App Router doesn't ship
 * a router-events emitter. The patch is the only way to detect
 * pushState calls (which Next's router internals trigger) without
 * polling pathname changes (which can't distinguish forward vs
 * replace).
 *
 * **External-store source of truth (plan 26 final fix).** Direction
 * lives in a module-scoped variable + Set<subscriber>; consumers read
 * it via `useSyncExternalStore`. The patched pushState/replaceState
 * functions write directly to that variable and notify subscribers,
 * which means NO setState fires from inside the patch. Next 16's
 * router calls history.pushState from inside its own
 * useInsertionEffect; React 19 raises "useInsertionEffect must not
 * schedule updates" if we invoke a React state setter while still on
 * that call stack — even when wrapped in setTimeout, React's
 * scheduling tracker can still attribute the update back to the
 * insertion-effect scope. The external-store pattern bypasses
 * React's render lifecycle entirely.
 */

import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";

const NAV_INDEX_KEY = "__memaxNavIndex";

type HistoryStateWithIndex = { [NAV_INDEX_KEY]?: number } & Record<
  string,
  unknown
>;

export type NavigationDirection = "forward" | "back" | "neutral";

// ─── Module-scoped store ────────────────────────────────────────────────

let currentDirection: NavigationDirection = "neutral";
const subscribers = new Set<() => void>();

function setStoreDirection(next: NavigationDirection) {
  if (currentDirection === next) return;
  currentDirection = next;
  // Notify subscribers in a microtask so consumers schedule their re-
  // renders OUTSIDE the current call stack — which (when the patch is
  // invoked from Next's router useInsertionEffect) is the very thing
  // we're trying to escape.
  queueMicrotask(() => {
    for (const fn of subscribers) fn();
  });
}

function subscribe(fn: () => void): () => void {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

function getSnapshot(): NavigationDirection {
  return currentDirection;
}

function getServerSnapshot(): NavigationDirection {
  return "neutral";
}

// ─── React surface ─────────────────────────────────────────────────────

interface NavigationDirectionContextValue {
  direction: NavigationDirection;
}

const NavigationDirectionContext =
  createContext<NavigationDirectionContextValue>({
    direction: "neutral",
  });

export function NavigationDirectionProvider({
  children,
}: {
  children: ReactNode;
}) {
  const direction = useSyncExternalStore(
    subscribe,
    getSnapshot,
    getServerSnapshot,
  );
  // Live current-index lives in a ref so the popstate listener (whose
  // closure captures it once via `[]` deps) can compare against the
  // up-to-date value. State copy is intentionally not used here —
  // closing over `currentIndex` from useState would freeze it at 0
  // and back navigations would never classify as "back" (codex H1).
  const currentIndexRef = useRef(0);

  useEffect(() => {
    if (typeof window === "undefined") return;

    // Capture originals so we can restore on cleanup. Each provider
    // mount patches; cleanup restores. Strict-mode double-mount is
    // safe: the second patch reads the FIRST patch as "original," but
    // cleanup unwinds in reverse so the originals come back in order.
    const origPush = history.pushState;
    const origReplace = history.replaceState;

    function nextIndex(state: HistoryStateWithIndex | null): number {
      const currentIdx =
        (history.state as HistoryStateWithIndex | null)?.[NAV_INDEX_KEY] ?? 0;
      const stateIdx = state?.[NAV_INDEX_KEY];
      // If the caller passed an index already (e.g. seed below), respect it.
      // Otherwise increment current.
      return typeof stateIdx === "number" ? stateIdx : currentIdx + 1;
    }

    history.pushState = function patchedPush(state, title, url) {
      const stateIn = state as HistoryStateWithIndex | null;
      const idx = nextIndex(stateIn);
      const next: HistoryStateWithIndex = {
        ...(stateIn ?? {}),
        [NAV_INDEX_KEY]: idx,
      };
      // Direct module-scope write — no React setState, no useInsertionEffect
      // warning. Subscribers are notified in a microtask (see
      // setStoreDirection).
      setStoreDirection("forward");
      currentIndexRef.current = idx;
      return origPush.call(history, next, title, url);
    };

    history.replaceState = function patchedReplace(state, title, url) {
      // Replace keeps the current index — neutral (no slide animation).
      const stateIn = state as HistoryStateWithIndex | null;
      const idx =
        (history.state as HistoryStateWithIndex | null)?.[NAV_INDEX_KEY] ?? 0;
      const merged: HistoryStateWithIndex = {
        ...(stateIn ?? {}),
        [NAV_INDEX_KEY]: idx,
      };
      setStoreDirection("neutral");
      currentIndexRef.current = idx;
      return origReplace.call(history, merged, title, url);
    };

    const onPopState = (e: PopStateEvent) => {
      const newIdx =
        (e.state as HistoryStateWithIndex | null)?.[NAV_INDEX_KEY] ?? 0;
      const prevIdx = currentIndexRef.current;
      if (newIdx > prevIdx) setStoreDirection("forward");
      else if (newIdx < prevIdx) setStoreDirection("back");
      // newIdx === prevIdx: leave direction unchanged (no animation).
      currentIndexRef.current = newIdx;
    };
    window.addEventListener("popstate", onPopState);

    // Seed the initial entry with index 0 if not stamped already.
    // Uses the ORIGINAL replaceState (not the patched one) so the
    // seed itself doesn't fire a "neutral" direction transition.
    if (
      (history.state as HistoryStateWithIndex | null)?.[NAV_INDEX_KEY] == null
    ) {
      origReplace.call(
        history,
        { ...(history.state ?? {}), [NAV_INDEX_KEY]: 0 },
        "",
        location.href,
      );
    }
    currentIndexRef.current =
      (history.state as HistoryStateWithIndex | null)?.[NAV_INDEX_KEY] ?? 0;

    return () => {
      history.pushState = origPush;
      history.replaceState = origReplace;
      window.removeEventListener("popstate", onPopState);
      // Reset the module-scoped store so a fresh provider mount (strict-
      // mode remount, layout swap, etc.) doesn't read stale "forward"/
      // "back" via getSnapshot. Codex Phase 26 session-batch review.
      setStoreDirection("neutral");
    };
  }, []);

  // Context value is just `{ direction }` for backwards compat with
  // useNavigationDirection(); the actual subscription is via
  // useSyncExternalStore above.
  return (
    <NavigationDirectionContext.Provider value={{ direction }}>
      {children}
    </NavigationDirectionContext.Provider>
  );
}

export function useNavigationDirection(): NavigationDirection {
  return useContext(NavigationDirectionContext).direction;
}
