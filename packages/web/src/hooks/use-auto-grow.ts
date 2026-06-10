"use client";

import {
  useCallback,
  useLayoutEffect,
  useEffect,
  useRef,
  type RefObject,
} from "react";

/**
 * Single threshold for "content has wrapped past one visual line." With
 * a 24px line-height + 24px min height, a clearly-wrapped textarea
 * reports scrollHeight >= 48 — well above 40. A single line reports 24.
 */
export const EXPAND_THRESHOLD = 40;

/**
 * One-way auto-grow signal. Returns true iff scrollHeight indicates the
 * textarea has clearly wrapped past one line.
 *
 * Why one-way (no collapse): collapse from `scrollHeight` is fundamentally
 * circular. The compose grid widens the input *because* we expanded it,
 * which drops scrollHeight back to ~24 in the wider layout — which used
 * to read as "back to one line, collapse." Collapsing then narrows the
 * input, content wraps again, scrollHeight jumps back to 48 → expand.
 * That feedback loop produced the wrap-edge flicker. The fix: never
 * collapse from a width-dependent measurement. Collapse is a separate,
 * explicit signal owned by the consumer — `bar-context.tsx#handleChange`
 * collapses on the empty-value transition (the only per-keystroke signal
 * that monotonically drops wrap risk to zero); other collapse intents
 * (Esc, peel-back, blur, route change) are discrete bar-machine
 * transitions and don't go through this hook.
 */
export function shouldAutoExpand(scrollH: number): boolean {
  return scrollH > EXPAND_THRESHOLD;
}

/**
 * Cross-browser auto-growing textarea hook.
 *
 * Uses scrollHeight measurement — same approach as ChatGPT, Discord, Linear.
 * react-textarea-autosize pattern: measure with height=auto, set explicit px.
 *
 * NO CSS transition on the textarea element — transitions interfere with
 * the auto→measure→set pattern. Smooth animation should be applied to a
 * CONTAINER element (e.g., framer-motion layout or CSS transition on a wrapper).
 *
 * Two distinct re-measure paths, with different responsibilities:
 *   · `useLayoutEffect` on value change — re-measure HEIGHT and, if not
 *     already expanded, call `onExpand`. This is the only path that can
 *     trigger expansion.
 *   · `ResizeObserver` on width change — re-measure HEIGHT only. Never
 *     touches expansion. This is the load-bearing fix for the wrap-edge
 *     feedback loop: a width change caused by our own expansion would
 *     otherwise immediately re-decide expansion and flicker.
 */
export function useAutoGrow(
  ref: RefObject<HTMLTextAreaElement | null>,
  value: string,
  maxHeight: number,
  isExpanded: boolean,
  onExpand?: () => void,
): void {
  const prevWidth = useRef(0);

  const measureHeight = useCallback((): number | null => {
    const el = ref.current;
    if (!el) return null;
    el.style.height = "auto";
    const scrollH = el.scrollHeight;
    const clamped = Math.min(scrollH, maxHeight);
    el.style.height = `${clamped}px`;
    el.style.overflowY = scrollH > maxHeight ? "auto" : "hidden";
    return scrollH;
  }, [ref, maxHeight]);

  // Re-measure on value change. Only this path can trigger expansion.
  useLayoutEffect(() => {
    const scrollH = measureHeight();
    if (scrollH == null) return;
    if (ref.current) prevWidth.current = ref.current.clientWidth;
    if (!isExpanded && shouldAutoExpand(scrollH)) {
      onExpand?.();
    }
  }, [ref, value, isExpanded, onExpand, measureHeight]);

  // Re-measure on WIDTH change (grid morph, window resize). Height-only —
  // width changes must NEVER feed back into the expansion decision, or
  // the wrap-edge flicker returns.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      const newWidth = el.clientWidth;
      if (newWidth !== prevWidth.current) {
        prevWidth.current = newWidth;
        measureHeight();
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref, measureHeight]);
}
