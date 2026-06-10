"use client";

/**
 * `<BottomSheet>` — mobile bottom-sheet primitive (plan 22 §5.2).
 *
 * Slide-up sheet anchored to the bottom of the viewport. Drag the
 * grabber down (or below the velocity/offset threshold) to dismiss.
 * Snap points let the sheet rest at multiple heights without forcing
 * a full-screen open.
 *
 * Used by:
 *   - mobile hub switcher in `<MobileTopBar>`
 *   - future mobile compose hub-picker
 *   - future mobile attachment preview
 *
 * Internals:
 *   - base-ui `<Dialog>` for the modal lifecycle (focus trap, Esc,
 *     click-outside dismissal via the backdrop).
 *   - framer-motion drag handle on the popup with bounded drag and
 *     velocity-aware close threshold.
 *   - `touch-action: pan-y` on the popup so vertical drag works
 *     without scroll-jacking horizontal page gestures.
 *   - reduced-motion fallback: opacity-only fade, no transform
 *     (handled via `useReducedMotion`).
 *
 * Snap points: `["50%", "90%"]` by default. Caller can override with
 * any subset of `["25%", "50%", "75%", "90%"]`. The sheet opens at
 * the FIRST snap point and the user can drag between them; dragging
 * below the lowest snap point + a 200px/s velocity threshold closes.
 */

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { motion, useReducedMotion, type PanInfo } from "framer-motion";

export type BottomSheetSnapPoint = "25%" | "50%" | "75%" | "90%";

interface BottomSheetProps {
  open: boolean;
  onClose: () => void;
  /** Optional sheet header — appears above the content with a divider. */
  title?: ReactNode;
  children: ReactNode;
  /**
   * Heights the sheet snaps between. The sheet opens at the first
   * entry; the user can drag up to taller entries or down to dismiss.
   * Defaults to `["50%", "90%"]`.
   */
  snapPoints?: BottomSheetSnapPoint[];
  /** Accessible label for screen readers. Required by base-ui Dialog. */
  ariaLabel: string;
}

const DEFAULT_SNAP_POINTS: BottomSheetSnapPoint[] = ["50%", "90%"];
const CLOSE_VELOCITY_PX_PER_S = 200;
const CLOSE_OFFSET_PX = 80;

function snapPointToPx(snap: BottomSheetSnapPoint, viewportHeight: number) {
  const pct = parseInt(snap, 10) / 100;
  return Math.round(viewportHeight * pct);
}

export function BottomSheet({
  open,
  onClose,
  title,
  children,
  snapPoints = DEFAULT_SNAP_POINTS,
  ariaLabel,
}: BottomSheetProps) {
  const reduceMotion = useReducedMotion();
  // Capture viewport height once on open so snap-point math is stable
  // across keyboard appearances (which would otherwise shrink innerHeight
  // mid-drag and shift snap targets).
  const [viewportHeight, setViewportHeight] = useState(0);
  useEffect(() => {
    if (!open) return;
    const update = () => setViewportHeight(window.innerHeight);
    update();
    return () => {};
  }, [open]);

  // Pixel heights derived from snap points — sorted ascending so the
  // first entry is the smallest/initial.
  const snapHeightsPx = snapPoints
    .map((s) => snapPointToPx(s, viewportHeight))
    .sort((a, b) => a - b);
  const initialHeight = snapHeightsPx[0] ?? 0;

  // Tracks the currently-resting snap height. Defaults to the smallest
  // (initial) snap point; user drag can promote to a larger snap.
  const [restingHeight, setRestingHeight] = useState(initialHeight);
  useEffect(() => {
    if (open) setRestingHeight(initialHeight);
  }, [open, initialHeight]);

  const dragStartHeightRef = useRef(restingHeight);

  const handleDragEnd = (_: unknown, info: PanInfo) => {
    // Below the lowest snap + downward velocity → close.
    const dragDistance = info.offset.y;
    if (
      dragDistance > CLOSE_OFFSET_PX &&
      info.velocity.y > CLOSE_VELOCITY_PX_PER_S
    ) {
      onClose();
      return;
    }
    // Otherwise: snap to the nearest snap point above/below the
    // dragged height. `dragDistance > 0` is downward (smaller height);
    // `< 0` is upward (taller height).
    const targetHeight = dragStartHeightRef.current - dragDistance;
    const nearest = snapHeightsPx.reduce((best, h) =>
      Math.abs(h - targetHeight) < Math.abs(best - targetHeight) ? h : best,
    );
    setRestingHeight(nearest);
  };

  return (
    <Dialog.Root open={open} onOpenChange={(next) => (next ? null : onClose())}>
      <Dialog.Portal>
        {/* No backdrop-filter on the scrim — kitchen mobile invariant
            allows at most ONE backdrop-filter on mobile, and the sheet
            itself owns one via .glass-dropdown. Stacking two iOS Safari
            blur passes is a perf hit. Codex cadence-review fix. */}
        <Dialog.Backdrop className="fixed inset-0 z-modal bg-foreground/12" />
        <Dialog.Popup
          aria-label={ariaLabel}
          className="z-modal fixed bottom-0 left-0 right-0 outline-none"
        >
          {/* The base-ui Popup is a positioning shell; the actual
              draggable surface is the motion.div nested inside. We
              don't merge them via a `render` prop because base-ui's
              Popup typing for `onDragEnd` (React DragEvent) conflicts
              with framer-motion's PanInfo signature. Nesting keeps
              both contracts clean. */}
          <motion.div
            // glass-dropdown matches the rest of the chrome family
            // (mobile drawer, secondary panel, popovers) — single
            // glass language across all overlay surfaces. Plan 26
            // follow-up after user "filter dropdown is correct"
            // baseline.
            className="glass-dropdown backdrop-blur-sm rounded-t-2xl flex flex-col"
            drag={reduceMotion ? false : "y"}
            dragConstraints={{ top: 0, bottom: 0 }}
            dragElastic={0.1}
            onDragStart={() => {
              dragStartHeightRef.current = restingHeight;
            }}
            onDragEnd={handleDragEnd}
            animate={{ height: restingHeight }}
            initial={{ height: initialHeight }}
            transition={
              reduceMotion
                ? { duration: 0 }
                : // Snappier spring matching the FAB↔bar morph (plan 26
                  // critical change). Settles in ~120ms with zero
                  // overshoot — feels instant on mobile where every
                  // ms of perceived lag reads as sluggish.
                  {
                    type: "spring",
                    stiffness: 700,
                    damping: 38,
                    mass: 0.4,
                  }
            }
            style={{
              touchAction: "pan-y",
              maxHeight: "90vh",
            }}
          >
            {/* Grabber — the drag handle visual. Centered, narrow pill. */}
            <div className="flex justify-center py-2 shrink-0">
              <div
                aria-hidden
                className="h-1 w-10 rounded-full bg-foreground/15"
              />
            </div>
            {title ? (
              <header className="px-5 pb-3 shrink-0 border-b border-border/40">
                <div className="text-[15px] font-semibold text-foreground">
                  {title}
                </div>
              </header>
            ) : null}
            <div className="flex-1 overflow-y-auto overscroll-contain px-5 py-4">
              {children}
            </div>
          </motion.div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
