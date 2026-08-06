"use client";

/**
 * ScanRestButton — floating signature-tinted glass FAB at the bottom-
 * right thumb-zone. Persistent re-entry to the global bar AFTER the
 * user has scrolled the bar off-screen.
 *
 * Plan 26 phase 4 — replaced the prior "always visible" pattern with
 * scroll-hide-aware visibility. Was: visible whenever bar overlay
 * isn't open. Now: visible only when the bar has been hidden via
 * scroll (`barScrollHidden`) AND the overlay isn't already open.
 * The FAB is the user's clear "where did the bar go" signal — when
 * the bar is in its docked rest state, the FAB stays out of the way.
 *
 * Click semantics:
 *   - Calls `openBar()` to focus the bar input AND clears the
 *     `barScrollHidden` flag so the bar reveals from its scroll-hide
 *     state simultaneously.
 *
 * Visual:
 *   - 44×44 hit target (iOS HIG / WCAG 2.5.5).
 *   - `glass-panel` material — the same shared recipe the desktop
 *     LeftRail, PinnedSecondaryPanel, and MobileTopBar use. One glass
 *     language across all shell chrome (plan 26 phase 7).
 *   - 8% --signature background tint composed onto the glass — same
 *     "memax-branded glass" treatment as the bar's signature accents.
 *     Per design system §0, --signature is reserved for AI moments;
 *     opening the universal capture/recall surface qualifies.
 *   - Margin from corner: `right-8 bottom-8` desktop, `right-6
 *     bottom-28` mobile (above the dock). Never hugs the viewport
 *     edge per user spec.
 *   - Safe-area-inset-bottom honored on mobile so the button doesn't
 *     tuck under the iPhone home indicator.
 */

import { Search } from "lucide-react";
import { motion, useReducedMotion } from "framer-motion";
import { usePathname } from "next/navigation";
import { useBar } from "@/contexts/bar-context";
import { useShellState } from "@/contexts/shell-state-context";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useLocale } from "@/i18n";
import { MOBILE_DOCK_BOTTOM_GAP_PX } from "@/lib/layout";
import { isChatSurfaceRoute } from "@/lib/route-helpers";

export function ScanRestButton() {
  const { openBar, barOverlayOpen } = useBar();
  const { barScrollHidden, setBarScrollHidden, barShown } = useShellState();
  const { t } = useLocale();
  const reduceMotion = useReducedMotion();
  const isMobile = useIsMobile();
  const pathname = usePathname();
  // Visible when the bar is hidden for ANY reason — scrolled off-screen
  // (sticky scroll-hide) OR route-hidden (e.g., /agents, /pulse where
  // the bar isn't inline). The FAB is the persistent re-entry on every
  // route except those where the bar is already open as an overlay.
  // Plan 26 follow-up.
  //
  // Phase 3.7c chat surface — suppressed entirely on /brain and
  // descendants. The chat composer is the input on those routes;
  // surfacing a FAB that re-opens the bar would invite users into a
  // recall/ask flow that competes with the active chat session.
  const onChatSurface = isChatSurfaceRoute(pathname ?? "");
  const visible =
    !onChatSurface && (barScrollHidden || !barShown) && !barOverlayOpen;
  // Solid signature surface — was glass-panel + light tint, user
  // wanted the inverted treatment: solid purple bg, white icon,
  // white outline. Reads as a confident primary action vs the
  // previous "tinted glass affordance". Plan 26 follow-up.
  //
  // Mobile bottom-aligned with where the bar would sit: the bar's
  // mobile rest position parks at MOBILE_DOCK_BOTTOM_GAP_PX above
  // viewport bottom (+ safe-area inset). FAB matches so when the
  // user taps and the bar reveals, the FAB doesn't visually "jump
  // up" to a different anchor.
  //
  // Split shell still: outer motion.div for opacity (no transform),
  // inner motion.button for scale (transform). Avoids the
  // backdrop-filter+transform conflict pattern.
  return (
    <motion.div
      className={`z-shell-bar fixed h-12 w-12 rounded-full overflow-hidden ${
        isMobile ? "right-6" : "right-8 bottom-8"
      }`}
      initial={false}
      animate={{ opacity: visible ? 1 : 0 }}
      // Chat-surface routes (/brain) hide instantly so a stale FAB
      // opacity from a prior route can't tween out over the chat
      // composer's first paint (matches the GlobalBar's instant-hide
      // path in app-shell-client).
      transition={
        onChatSurface || reduceMotion
          ? { duration: 0 }
          : { type: "spring", stiffness: 700, damping: 38, mass: 0.4 }
      }
      style={{
        bottom: isMobile
          ? `calc(${MOBILE_DOCK_BOTTOM_GAP_PX}px + env(safe-area-inset-bottom))`
          : undefined,
        background: "var(--signature)",
        // White rim — 2px outline + subtle ambient shadow for depth
        // against the page. The `box-shadow` second layer gives a
        // soft drop so the FAB reads as elevated, not pasted-on.
        boxShadow:
          "0 0 0 2px color-mix(in srgb, white 90%, var(--signature)), 0 8px 24px color-mix(in srgb, var(--signature) 40%, transparent), 0 2px 6px oklch(0 0 0 / 0.08)",
        pointerEvents: visible ? "auto" : "none",
      }}
    >
      <motion.button
        type="button"
        onClick={() => {
          setBarScrollHidden(false);
          openBar();
        }}
        aria-label={t.nav.openBar}
        className="relative flex h-full w-full items-center justify-center cursor-pointer"
        initial={false}
        animate={{ scale: visible ? 1 : 0.85 }}
        // Inner scale aligns with the wrapper opacity transition: chat
        // surfaces hide instantly so a stale 1.0 scale can't tween
        // down to 0.85 across the chat composer's first paint, even
        // though pointer-events are off (codex review pass 1).
        transition={
          onChatSurface || reduceMotion
            ? { duration: 0 }
            : { type: "spring", stiffness: 700, damping: 38, mass: 0.4 }
        }
      >
        <Search className="h-5 w-5 text-white" strokeWidth={2.4} />
      </motion.button>
    </motion.div>
  );
}
