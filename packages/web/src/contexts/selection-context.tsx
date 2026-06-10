"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

interface SelectionState {
  /** Whether selection mode is active */
  active: boolean;
  /** Set of selected memory IDs */
  selected: Set<string>;
  /** Enter selection mode (optionally selecting one ID immediately) */
  enter: (initialId?: string) => void;
  /** Exit selection mode and clear all */
  exit: () => void;
  /** Toggle a single ID. Also sets the shift-click anchor. */
  toggle: (id: string) => void;
  /** Shift-click range-select anchored on the last non-shift toggle.
   *  Falls back to plain toggle when there's no anchor or the ids can't
   *  be located in the registered visible list. */
  shiftToggle: (id: string) => void;
  /** Select a range (used internally + for legacy consumers). */
  selectRange: (ids: string[]) => void;
  /** Select every id registered via `setVisibleIds`. No-op when !active. */
  selectAll: () => void;
  /** Clear selection without exiting mode. Prefer `exit()` for UI
   *  affordances that mean "I'm done" (e.g. ✕ buttons) — an empty
   *  selected set while `active` is still true produces a ghost batch
   *  mode where next row-click silently starts a new selection. */
  clear: () => void;
  /** Register the ordered list of currently-visible ids. Drives
   *  `selectAll()` and `shiftToggle()` range resolution. Side-effect:
   *  reconciles `selected` with the new universe — hidden ids are
   *  dropped. If reconciliation empties the selection, batch mode
   *  auto-exits to avoid a ghost "active with 0 selected" state
   *  (codex-review 2026-04-21). */
  setVisibleIds: (ids: string[]) => void;
  /** Reactive count of currently-registered visible ids. Exposed so
   *  the toolbar can disable the "Select all" button when everything
   *  is already selected without reading a ref mid-render. */
  visibleCount: number;
}

const SelectionContext = createContext<SelectionState | null>(null);

export function SelectionProvider({ children }: { children: React.ReactNode }) {
  const [active, setActive] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // Shift-click anchor — set on every plain toggle / shiftToggle.
  // Not reactive state: reading it inside shiftToggle doesn't need a
  // re-render to propagate.
  const lastToggledIdRef = useRef<string | null>(null);
  // Ordered universe of currently-rendered ids, registered by the
  // surface consumer via `setVisibleIds()`. Ref-based to avoid
  // re-rendering the whole provider on every data refresh.
  const visibleIdsRef = useRef<string[]>([]);
  // Reactive mirror of the count only (not the ids). Lets the toolbar
  // disable "Select all" when everything visible is already selected,
  // without forcing the whole provider to re-render on every id
  // identity change. Updated from setVisibleIds below.
  const [visibleCount, setVisibleCount] = useState(0);

  const enter = useCallback((initialId?: string) => {
    setActive(true);
    if (initialId) {
      setSelected(new Set([initialId]));
      lastToggledIdRef.current = initialId;
    }
  }, []);

  const exit = useCallback(() => {
    setActive(false);
    setSelected(new Set());
    lastToggledIdRef.current = null;
  }, []);

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    lastToggledIdRef.current = id;
  }, []);

  const selectRange = useCallback((ids: string[]) => {
    setSelected((prev) => {
      const next = new Set(prev);
      for (const id of ids) next.add(id);
      return next;
    });
  }, []);

  const shiftToggle = useCallback((id: string) => {
    const anchor = lastToggledIdRef.current;
    const visible = visibleIdsRef.current;
    // No anchor, or either id missing from the visible universe →
    // fall back to plain toggle. This keeps shift-click meaningful
    // even when the anchored item has been filtered out.
    if (!anchor || anchor === id) {
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
      lastToggledIdRef.current = id;
      return;
    }
    const anchorIdx = visible.indexOf(anchor);
    const targetIdx = visible.indexOf(id);
    if (anchorIdx < 0 || targetIdx < 0) {
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
      lastToggledIdRef.current = id;
      return;
    }
    const [lo, hi] =
      anchorIdx < targetIdx ? [anchorIdx, targetIdx] : [targetIdx, anchorIdx];
    const slice = visible.slice(lo, hi + 1);
    setSelected((prev) => {
      const next = new Set(prev);
      for (const candidate of slice) next.add(candidate);
      return next;
    });
    // Anchor advances to the shift-click target so consecutive
    // shift-clicks extend from the last target (matches Gmail /
    // Finder shift-click convention).
    lastToggledIdRef.current = id;
  }, []);

  const selectAll = useCallback(() => {
    // Guard at the provider so callsites don't need to remember it
    // (codex-review v2 2026-04-21).
    if (!active) return;
    const visible = visibleIdsRef.current;
    if (visible.length === 0) return;
    setSelected(new Set(visible));
  }, [active]);

  const clear = useCallback(() => {
    setSelected(new Set());
    lastToggledIdRef.current = null;
  }, []);

  const setVisibleIds = useCallback(
    (ids: string[]) => {
      visibleIdsRef.current = ids;
      setVisibleCount((prev) => (prev === ids.length ? prev : ids.length));
      if (!active) return;
      // Reconcile `selected` with the new universe. Keep selections
      // whose id is still visible; drop the rest. If the result is
      // empty, auto-exit batch mode to avoid a ghost "active + 0
      // selected" state (codex-review v2 2026-04-21).
      const universe = new Set(ids);
      setSelected((prev) => {
        if (prev.size === 0) return prev;
        const next = new Set<string>();
        for (const id of prev) if (universe.has(id)) next.add(id);
        if (next.size === 0) {
          // Defer to microtask to avoid `setState during render`
          // warnings if a caller wires setVisibleIds in a render path
          // (expected path is a useEffect, but be defensive).
          queueMicrotask(() => setActive(false));
          lastToggledIdRef.current = null;
          return next;
        }
        if (next.size === prev.size) return prev;
        return next;
      });
    },
    [active],
  );

  // Esc exits selection mode
  useEffect(() => {
    if (!active) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        exit();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [active, exit]);

  // Cmd/Ctrl+A — select all visible ids while batch mode is active.
  // Guarded against editable / text-selection contexts so we don't
  // steal the native select-all in inputs, textareas, or elements
  // carrying a live text selection.
  useEffect(() => {
    if (!active) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== "a") return;
      if (!(e.metaKey || e.ctrlKey)) return;
      if (e.shiftKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || target.isContentEditable) {
          return;
        }
      }
      const sel = window.getSelection();
      if (sel && sel.toString().length > 0) return;
      if (visibleIdsRef.current.length === 0) return;
      e.preventDefault();
      setSelected(new Set(visibleIdsRef.current));
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [active]);

  const value = useMemo(
    () => ({
      active,
      selected,
      enter,
      exit,
      toggle,
      shiftToggle,
      selectRange,
      selectAll,
      clear,
      setVisibleIds,
      visibleCount,
    }),
    [
      active,
      selected,
      enter,
      exit,
      toggle,
      shiftToggle,
      selectRange,
      selectAll,
      clear,
      setVisibleIds,
      visibleCount,
    ],
  );

  return (
    <SelectionContext.Provider value={value}>
      {children}
    </SelectionContext.Provider>
  );
}

export function useSelection(): SelectionState {
  const ctx = useContext(SelectionContext);
  if (!ctx) {
    // Graceful fallback — selection not available on this surface
    return {
      active: false,
      selected: new Set(),
      enter: () => {},
      exit: () => {},
      toggle: () => {},
      shiftToggle: () => {},
      selectRange: () => {},
      selectAll: () => {},
      clear: () => {},
      setVisibleIds: () => {},
      visibleCount: 0,
    };
  }
  return ctx;
}
