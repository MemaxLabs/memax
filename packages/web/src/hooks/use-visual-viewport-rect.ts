"use client";

import { useEffect, useState } from "react";

interface VisualViewportRect {
  top: number;
  height: number;
}

/**
 * Mirrors the visible viewport rectangle for mobile keyboard-safe fixed
 * surfaces. iOS Safari keeps the layout viewport full height and only updates
 * `window.visualViewport`, so fullscreen/mirror overlays must anchor to that
 * rect instead of `inset: 0`.
 */
export function useVisualViewportRect(): VisualViewportRect | null {
  const [rect, setRect] = useState<VisualViewportRect | null>(null);

  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    const update = () => {
      setRect({
        top: vv.offsetTop,
        height: vv.height,
      });
    };

    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    window.addEventListener("orientationchange", update);

    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
      window.removeEventListener("orientationchange", update);
    };
  }, []);

  return rect;
}
