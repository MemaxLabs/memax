"use client";

import { useEffect, useLayoutEffect, useSyncExternalStore } from "react";

/**
 * Bottom-chrome occupancy — who owns the bottom edge of the phone
 * screen right now. The mobile dock and any bottom-anchored composer
 * (chat composer, ✦ bar in a non-docked state) share the same strip of
 * pixels, and stacking them by z-index only decides who paints over
 * whom, never who should be there. So: a composer that mounts at the
 * bottom REGISTERS here, and the dock hides while anything is
 * registered. The dock's route rule (「/brain/sessions/*」) stays as a
 * first-paint guard for full-thread routes; this store covers what a
 * route cannot tell apart — a draft composer on plain /brain.
 */
let occupants = 0;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}
const getSnapshot = () => occupants > 0;
const getServerSnapshot = () => false;

/** True while at least one bottom composer is mounted. */
export function useBottomChromeOccupied(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

// Layout effect so the dock hides in the same frame the composer
// commits — a passive effect would let one frame paint both.
const useIsoLayoutEffect =
  typeof window === "undefined" ? useEffect : useLayoutEffect;

/**
 * Register this component as owning the bottom edge while `active` is
 * true. Unregisters on unmount or when `active` flips false.
 */
export function useOccupyBottomChrome(active: boolean) {
  useIsoLayoutEffect(() => {
    if (!active) return;
    occupants += 1;
    emit();
    return () => {
      occupants -= 1;
      emit();
    };
  }, [active]);
}
