"use client";

import { useSyncExternalStore } from "react";

/** A phone (HANDOFF §7 "Responsive": below 640px). */
const NARROW = "(max-width: 639px)";

function subscribe(change: () => void) {
  const query = window.matchMedia?.(NARROW);
  query?.addEventListener("change", change);
  return () => query?.removeEventListener("change", change);
}

/**
 * Whether the page is phone-wide, for components that change shape there
 * (a memory row's receipt moves under its words: MemoryRow `stacked`).
 * The server renders the desktop shape; the client follows the window.
 */
export function useNarrow(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia?.(NARROW).matches ?? false,
    () => false,
  );
}
