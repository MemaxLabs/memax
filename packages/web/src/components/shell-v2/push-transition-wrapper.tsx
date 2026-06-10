"use client";

/**
 * `<PushTransitionWrapper>` — plan 22 P0-4 + plan 26 retune.
 *
 * Pass-through wrapper that handles forward-push scroll-to-top, with
 * NO transition on tab/route switches. Plan 26 follow-up — user wants
 * tab switching instant; any animation here adds perceived lag.
 *
 * Was: opacity cross-fade via AnimatePresence (FAST 150ms).
 * Now: render children directly. Routes that need a loading state
 * should provide a `loading.tsx` segment file (Next 16 App Router
 * convention) so Next renders the placeholder while the new route
 * resolves; this wrapper stays out of the way.
 *
 * Forward push still scrolls body to top so the entering route lands
 * at its top edge instead of inheriting the outgoing route's scroll
 * position. Back/neutral preserve scroll for restoration.
 */

import { useEffect, type ReactNode } from "react";
import { usePathname } from "next/navigation";
import { useNavigationDirection } from "@/contexts/navigation-direction";

export function PushTransitionWrapper({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const direction = useNavigationDirection();

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (direction !== "forward") return;
    window.scrollTo({ top: 0, behavior: "auto" });
  }, [pathname, direction]);

  return <>{children}</>;
}
