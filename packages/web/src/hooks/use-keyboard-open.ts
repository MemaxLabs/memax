"use client";

/**
 * useKeyboardOpen — detects whether the virtual keyboard is open.
 *
 * Compares visualViewport.height against its initial value (captured at
 * mount, before any keyboard). When the height drops by >150px, keyboard
 * is open. Resets baseline on orientation change.
 *
 * Scroll restoration: iOS Safari auto-scrolls to keep the focused input
 * visible when the keyboard opens. When the keyboard closes, Safari does
 * NOT restore the original scroll position — content can end up under
 * fixed headers. This hook saves scrollY on keyboard open and restores it
 * on keyboard close (industry standard: Telegram Web, Linear mobile).
 *
 * Cross-platform:
 * - iOS Safari: visual viewport shrinks, layout viewport stays
 * - Chrome Android: both shrink, but vv.height always reflects the
 *   current visible area regardless of interactive-widget mode
 * - 150px threshold: keyboards are >200px, URL bar changes are <100px
 *
 * Usage:
 * - MobileDock: hides when keyboard is open (native tab bar pattern)
 * - GlobalBar: switches from bottom-docked to keyboard-adjacent positioning
 */

import { useEffect, useRef, useState } from "react";

export function useKeyboardOpen(): boolean {
  const [open, setOpen] = useState(false);
  const baselineRef = useRef(0);
  const savedScrollRef = useRef(0);

  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    baselineRef.current = vv.height;

    function onResize() {
      if (!vv) return;
      const drop = baselineRef.current - vv.height;
      setOpen((prev) => {
        const isOpen = drop > 150;

        // Keyboard opening → save scroll position
        if (isOpen && !prev) {
          savedScrollRef.current = window.scrollY;
        }

        // Keyboard closing → restore scroll position (iOS Safari fix)
        if (!isOpen && prev) {
          requestAnimationFrame(() => {
            window.scrollTo(0, savedScrollRef.current);
          });
        }

        return prev === isOpen ? prev : isOpen;
      });
    }

    function onOrientationChange() {
      requestAnimationFrame(() => {
        if (vv) baselineRef.current = vv.height;
      });
    }

    vv.addEventListener("resize", onResize);
    window.addEventListener("orientationchange", onOrientationChange);
    return () => {
      vv.removeEventListener("resize", onResize);
      window.removeEventListener("orientationchange", onOrientationChange);
    };
  }, []);

  return open;
}
