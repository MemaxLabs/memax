"use client";

import {
  useEffect,
  useRef,
  useState,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { StateMark } from "@memaxlabs/ledger";
import styles from "./swipe.module.css";

/** How far a card goes before letting go decides it: 30% of its width, at most 120px. */
export function swipeThreshold(width: number): number {
  // A card not laid out yet takes the most.
  return width > 0 ? Math.min(width * 0.3, 120) : 120;
}

/** A flick decides from this far (px), at this speed (px/ms). */
const FLICK_DISTANCE = 48;
const FLICK_SPEED = 0.6;

/** How far a finger moves before the drag picks a direction. */
const SLOP = 10;

/** How long after a drag the browser's click for it may still arrive. */
const CLICK_AFTER_DRAG_MS = 600;

export type SwipeDecision = "keep" | "reject";

/** What letting go at `dx` after `ms` decides, if anything. */
export function swipeDecision(
  dx: number,
  ms: number,
  width: number,
): SwipeDecision | null {
  const speed = dx / Math.max(ms, 1);
  const line = swipeThreshold(width);
  if (dx >= line || (dx >= FLICK_DISTANCE && speed >= FLICK_SPEED)) {
    return "keep";
  }
  if (dx <= -line || (dx <= -FLICK_DISTANCE && speed <= -FLICK_SPEED)) {
    return "reject";
  }
  return null;
}

interface Drag {
  id: number;
  x: number;
  y: number;
  at: number;
  width: number;
  /** null until the finger has moved past the slop; then whether it's sideways. */
  sideways: boolean | null;
}

/**
 * MobileReview.png's swipe: a proposal follows the finger sideways, and
 * letting go past the line (or a flick) decides it: right keeps, left
 * rejects. `onKeep` and `onReject` are Review's own Keep and Reject, the
 * commands the buttons and K and X send, with the same intent keys, so a
 * swipe is never a different decision. The buttons stay on the card
 * (WCAG 2.5.1): a swipe is a shortcut, never the only way.
 *
 * Touch and pen only (a mouse has the buttons and the keys). A drag that
 * starts vertically is the page's scroll (touch-action: pan-y), and one
 * that starts in a field is typing. While a card can't be decided by a
 * swipe (`enabled` false: being edited, already kept, a viewer's, a
 * conflict whose Keep replaces a decision), it doesn't move.
 */
export function Swipe({
  enabled,
  onKeep,
  onReject,
  keepLabel,
  rejectLabel,
  children,
}: {
  enabled: boolean;
  onKeep: () => void;
  onReject: () => void;
  keepLabel: string;
  rejectLabel: string;
  children: ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const drag = useRef<Drag | null>(null);
  /** Until when (event time) a click is the tail of a drag, not a press. */
  const swallowUntil = useRef(-Infinity);
  const [dx, setDx] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [line, setLine] = useState(Infinity);

  // A sideways drag is the card's, not the page's: its touchmoves are
  // cancelled, so the browser never makes a scroll or a fling of it. A
  // fling it made would take the next tap as the fling's stop and send no
  // click, so the toast's Undo, tapped at once, did nothing. React listens
  // to touchmove passively, hence the listener here.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const onTouchMove = (event: TouchEvent) => {
      if (drag.current?.sideways && event.cancelable) event.preventDefault();
    };
    el.addEventListener("touchmove", onTouchMove, { passive: false });
    return () => el.removeEventListener("touchmove", onTouchMove);
  }, []);

  const reset = () => {
    drag.current = null;
    setDragging(false);
    setDx(0);
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    swallowUntil.current = -Infinity;
    if (!enabled || event.pointerType === "mouse" || !event.isPrimary) return;
    const target = event.target as Element;
    if (target.closest("input, textarea, select, [contenteditable='true']")) {
      return;
    }
    drag.current = {
      id: event.pointerId,
      x: event.clientX,
      y: event.clientY,
      at: event.timeStamp,
      width: box.current?.offsetWidth ?? 0,
      sideways: null,
    };
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || event.pointerId !== d.id) return;
    const mx = event.clientX - d.x;
    const my = event.clientY - d.y;
    if (d.sideways === null) {
      if (Math.abs(mx) < SLOP && Math.abs(my) < SLOP) return;
      d.sideways = Math.abs(mx) > Math.abs(my);
      if (!d.sideways) {
        // A scroll: the browser has it.
        drag.current = null;
        return;
      }
      box.current?.setPointerCapture?.(event.pointerId);
      setLine(swipeThreshold(d.width));
      setDragging(true);
    }
    setDx(mx);
  };

  const onPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || event.pointerId !== d.id) return;
    if (!d.sideways) {
      drag.current = null;
      return;
    }
    const decided = swipeDecision(
      event.clientX - d.x,
      event.timeStamp - d.at,
      d.width,
    );
    // A drag that started on a button wasn't a press of it: the click the
    // browser may send after it is swallowed.
    swallowUntil.current = event.timeStamp + CLICK_AFTER_DRAG_MS;
    reset();
    if (decided === "keep") onKeep();
    else if (decided === "reject") onReject();
  };

  const onClickCapture = (event: MouseEvent<HTMLDivElement>) => {
    // Only a pointer's click (detail ≥ 1), and only right after a drag:
    // Enter or Space on a focused button (detail 0) always goes through.
    if (event.detail === 0 || event.timeStamp > swallowUntil.current) return;
    swallowUntil.current = -Infinity;
    event.preventDefault();
    event.stopPropagation();
  };

  const toward = dx >= line ? "keep" : dx <= -line ? "reject" : null;

  return (
    <div
      ref={box}
      className={[
        styles.swipe,
        enabled ? styles.enabled : "",
        dragging ? styles.dragging : "",
      ].join(" ")}
      style={dx !== 0 ? { transform: `translateX(${dx}px)` } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={reset}
      onLostPointerCapture={(event) => {
        // Only the card's own capture ending: the element under the finger
        // held a touch's implicit capture until the card took it, and that
        // loss bubbles here too.
        if (event.target === box.current && drag.current?.sideways) reset();
      }}
      onClickCapture={onClickCapture}
      data-toward={toward ?? undefined}
    >
      {children}
      {/* What letting go would do, in words; the toast and the card say what did. */}
      {dragging ? (
        <span
          className={`${styles.cue} ${dx > 0 ? styles.cueKeep : styles.cueReject}`}
          data-on={toward !== null || undefined}
          aria-hidden="true"
        >
          {dx > 0 ? (
            <StateMark state="kept" label={keepLabel} />
          ) : (
            <span className={styles.rejectWord}>{rejectLabel}</span>
          )}
        </span>
      ) : null}
    </div>
  );
}
