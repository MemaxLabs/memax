"use client";

import { useSyncExternalStore } from "react";
import type { PlaceRoute } from "@/lib/v2/places";

/**
 * A phone: below 640px the rail gives way to the top and bottom bars
 * (plan §6.6, MobileToday.png). CSS decides the layout; this is for the
 * few behaviours a phone changes (a gate answers on one tap, the swipe
 * hint shows), never for what renders on the server.
 */
export const PHONE_QUERY = "(max-width: 639px)";

/** The query, where the browser has matchMedia (jsdom doesn't). */
function phoneQuery(): MediaQueryList | null {
  return typeof window.matchMedia === "function"
    ? window.matchMedia(PHONE_QUERY)
    : null;
}

function subscribe(listener: () => void) {
  const query = phoneQuery();
  query?.addEventListener("change", listener);
  return () => query?.removeEventListener("change", listener);
}

/** Whether the viewport is a phone's; false on the server and before hydration. */
export function usePhone(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => phoneQuery()?.matches ?? false,
    () => false,
  );
}

/**
 * Whether a page draws its own phone header (a back link and its title)
 * in place of the space's top bar: Review and its cards (MobileReview,
 * MobileGate), and a memory's page (MobileMemory). `detail` is whether
 * the path goes past the place (/memories/M-0219; /review/M-0431/compare
 * keeps the top bar).
 */
export function ownsPhoneHeader(
  route: PlaceRoute | undefined,
  detail: boolean,
): boolean {
  return detail ? route === "memories" : route === "review";
}
