"use client";

import { useEffect, useState } from "react";

export type ScrollDirection = "up" | "down" | "idle";

export interface ScrollDirectionState {
  direction: ScrollDirection;
  atTop: boolean;
}

/**
 * Page scroll direction with rAF throttling and a small hysteresis
 * threshold so tiny wheel ticks don't flip state back and forth. Used by
 * the desktop docked bar and mobile dock-aligned bar to mimic Apple-
 * style reveal on scroll-up / hide on scroll-down. `atTop` flips true
 * inside `topPadding` so the bar always reveals near the top of the
 * page.
 *
 * Listens on BOTH `window` and the desktop shell's inner scroll
 * container (`<main>` with `overflow-y-auto`). The mobile shell uses
 * body scroll (window.scrollY changes); the desktop shell scrolls
 * inside `<main>` (window.scrollY stays 0 but `<main>.scrollTop`
 * changes). Without listening to both, scroll-hide silently broke on
 * desktop after shell-v2 introduced the inner-main scroll container
 * (plan 26 phase 4 fix: ScanRestButton never appeared on desktop).
 *
 * Touchmove fallback: pages without scrollable content (mobile brain
 * tab, short memory detail pages) never fire scroll events. We
 * additionally listen to `touchstart`/`touchmove` on window and infer
 * direction from finger movement so the bar still hides on swipe-up
 * and reveals on swipe-down. Scroll events, when they do fire, still
 * win.
 */
export function useScrollDirection({
  threshold = 8,
  topPadding = 32,
}: { threshold?: number; topPadding?: number } = {}): ScrollDirectionState {
  const [direction, setDirection] = useState<ScrollDirection>("idle");
  const [atTop, setAtTop] = useState(true);

  useEffect(() => {
    if (typeof window === "undefined") return;

    // Resolve scroll position from the actual scrolled element. Desktop
    // shell-v2 puts overflow-y-auto on <main>, mobile uses body scroll.
    // Reading scrollTop from the EVENT TARGET (in scroll handlers) or
    // from a known scrollable element each tick avoids the mount-order
    // race that comes with capturing a DOM ref at hook-init.
    const getScrollTop = (target: EventTarget | null): number => {
      if (target instanceof HTMLElement) return target.scrollTop;
      // Document/window scroll → fall back to window.scrollY (or
      // documentElement.scrollTop on older browsers).
      return window.scrollY || document.documentElement.scrollTop || 0;
    };

    // Best-effort initial source: any scrollable [data-scroll-root]
    // (desktop main) → its scrollTop, otherwise window.
    const initialSource = document.querySelector(
      "[data-scroll-root]",
    ) as HTMLElement | null;
    let lastY =
      initialSource && initialSource.scrollHeight > initialSource.clientHeight
        ? initialSource.scrollTop
        : window.scrollY;
    let lastSource: EventTarget | null = null;
    let ticking = false;
    let lastEventTarget: EventTarget | null = null;
    let touchLastY: number | null = null;
    // atTop hysteresis: enter atTop at scrollY ≤ topPadding (32px),
    // exit atTop at scrollY > topPadding + threshold (32+8=40px). The
    // 8px buffer prevents transparent/glass flap on small scroll
    // bounces around 32px when consumers like MobileTopBar key visual
    // state directly off `atTop`. Codex Phase 26 final review.
    let atTopState = lastY <= topPadding;

    const update = () => {
      const y = getScrollTop(lastEventTarget);
      // Reset baseline when the active scroll source changes —
      // comparing scrollTop across unrelated containers (e.g., desktop
      // main vs window) would briefly produce a wrong direction. Codex
      // Phase 26 final review.
      if (lastEventTarget !== lastSource) {
        lastY = y;
        lastSource = lastEventTarget;
      }
      const delta = y - lastY;
      if (Math.abs(delta) > threshold) {
        setDirection(delta > 0 ? "down" : "up");
        lastY = y;
      }
      // Hysteresis: enter atTop at <=topPadding, exit at >topPadding+threshold.
      const nextAtTop = atTopState
        ? y <= topPadding + threshold
        : y <= topPadding;
      if (nextAtTop !== atTopState) {
        atTopState = nextAtTop;
        setAtTop(nextAtTop);
      }
      ticking = false;
    };

    // Predicate: which scroll events count toward bar hide / atTop?
    // Capture-phase document listener picks up EVERY scrollable
    // descendant — popovers, dropdowns, internal lists, the editor,
    // hub-switcher menu, etc. Without this filter, scrolling inside any
    // of those would flip global bar hide state, mobile top-bar glass,
    // etc. We accept only:
    //   - window / document (mobile body scroll)
    //   - the desktop scroll root (`<main data-scroll-root>`) OR any
    //     ancestor of it that explicitly opts in via the same attribute
    // Codex Phase 26 session-batch review (Medium 1).
    const isPageLevelTarget = (target: EventTarget | null): boolean => {
      if (target === window || target === document) return true;
      if (
        target instanceof HTMLElement &&
        target.hasAttribute("data-scroll-root")
      ) {
        return true;
      }
      return false;
    };

    const onScroll = (e: Event) => {
      if (!isPageLevelTarget(e.target)) return;
      lastEventTarget = e.target;
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(update);
    };

    const onTouchStart = (e: TouchEvent) => {
      const t = e.touches[0];
      if (!t) return;
      touchLastY = t.clientY;
    };

    const onTouchMove = (e: TouchEvent) => {
      if (touchLastY === null) return;
      const t = e.touches[0];
      if (!t) return;
      // Finger moving up (clientY decreases) means the user wants to see
      // content below — treat as "down" so the bar hides. Swipe down is
      // "up" so the bar reveals.
      const delta = touchLastY - t.clientY;
      if (Math.abs(delta) > threshold) {
        setDirection(delta > 0 ? "down" : "up");
        touchLastY = t.clientY;
      }
    };

    const onTouchEnd = () => {
      touchLastY = null;
    };

    setAtTop(lastY <= topPadding);
    // CAPTURE-PHASE scroll listener at document — `scroll` events don't
    // bubble, but capture-phase delegation catches them at the document
    // root regardless of which element scrolled. This handles both
    // window scroll (mobile body) and inner-main scroll (desktop's
    // overflow-y-auto container) with a SINGLE listener — no need to
    // query the DOM at hook-init time, no race with React mount order.
    document.addEventListener("scroll", onScroll, {
      passive: true,
      capture: true,
    });
    window.addEventListener("touchstart", onTouchStart, { passive: true });
    window.addEventListener("touchmove", onTouchMove, { passive: true });
    window.addEventListener("touchend", onTouchEnd, { passive: true });
    window.addEventListener("touchcancel", onTouchEnd, { passive: true });
    return () => {
      document.removeEventListener("scroll", onScroll, { capture: true });
      window.removeEventListener("touchstart", onTouchStart);
      window.removeEventListener("touchmove", onTouchMove);
      window.removeEventListener("touchend", onTouchEnd);
      window.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [threshold, topPadding]);

  return { direction, atTop };
}
