"use client";

import { useMemo, useRef, useState, type PointerEvent } from "react";
import { motion, type Transition } from "framer-motion";
import { X } from "lucide-react";
import { useBar } from "@/contexts/bar-context";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { useLocale } from "@/i18n";
import { ExpandSearchResults } from "./expand-search-results";
import { MOBILE_OVERLAY_CONTENT_TOP } from "@/lib/layout";
import { useVisualViewportRect } from "@/hooks/use-visual-viewport-rect";

// Northstar (kitchen 38e3): mobile compose is ONE solid surface. Apple Notes,
// Linear, Raycast all converge on this — when you enter compose, the page goes
// away. No scrim, no transparency, no backdrop-filter.
//
// Motion: opacity fade only on entry/exit. No y-slide, no delayed content
// fade — the user explicitly asked for no heavy slide animations on mobile.
// Drag-to-dismiss is a user gesture (not an entrance animation) and stays.
//
// Perf invariant: zero backdrop-filter on mobile compose. Solid var(--background)
// surface means paint-only, no full-viewport blur pass.

const DISMISS_OFFSET_PX = 96;
const DISMISS_VELOCITY_PX_S = 720;
const RUBBER_BAND_RESIST = 0.55;
const MAX_PULL_PX = 160;
// Minimum downward Y-delta before the drag-to-dismiss gesture activates.
// Below this threshold the pointer is NOT captured, so tap events propagate
// to child result rows and fire click handlers. Fixes the "tap does
// nothing" bug on mobile/narrow-desktop where pointer capture on every
// pointerdown was hijacking clicks.
const DRAG_ACTIVATION_PX = 6;

export function MobileBarSurface() {
  const {
    isMobile,
    setMobileComposeActive,
    value,
    inputRef,
    sendKind,
    recallQuery,
    interaction,
    hideBar,
  } = useBar();
  const { t } = useLocale();
  const viewportRect = useVisualViewportRect();

  const dragStartY = useRef<number | null>(null);
  const dragStartTime = useRef<number>(0);
  const activePointerId = useRef<number | null>(null);
  const [pullOffset, setPullOffset] = useState(0);
  const [isDragging, setIsDragging] = useState(false);

  const mirrorText = useMemo(
    () => value.trim() || recallQuery,
    [recallQuery, value],
  );

  if (!isMobile || interaction.mobileComposeState !== "mirror") return null;

  const dismissToDock = () => {
    inputRef.current?.blur();
    setMobileComposeActive(false);
  };

  const fade: Transition = { duration: FAST, ease: EASE };

  // Spring-back after drag release (kitchen: 0.25s settle). CSS transform on
  // the result list keeps the gesture crisp regardless of framer state.
  const settleTransition = `transform 0.25s cubic-bezier(${EASE.join(", ")})`;

  const clearDragState = () => {
    dragStartY.current = null;
    dragStartTime.current = 0;
    activePointerId.current = null;
    setIsDragging(false);
    setPullOffset(0);
  };

  const releasePointerCaptureIfHeld = (event: PointerEvent<HTMLDivElement>) => {
    const pointerId = activePointerId.current;
    if (pointerId === null) return;
    if (
      event.currentTarget.hasPointerCapture &&
      event.currentTarget.hasPointerCapture(pointerId)
    ) {
      event.currentTarget.releasePointerCapture(pointerId);
    }
  };

  // ─── Drag handlers — only arm when scrollTop === 0 ───────────────────────

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.scrollTop > 0) return;
    // Multi-touch guard: if a gesture is already in flight with a
    // different pointer, ignore this one. Otherwise a second finger
    // could overwrite the tracked pointer mid-gesture and corrupt the
    // drag/tap decision (codex-review v3 M3).
    if (
      activePointerId.current !== null &&
      activePointerId.current !== event.pointerId
    ) {
      return;
    }
    // Arm the gesture but DON'T capture yet — capturing on every
    // pointerdown stole click events from child result rows (tap-to-open
    // was broken). We only capture once the user has moved far enough
    // downward to look like a drag (see onPointerMove).
    activePointerId.current = event.pointerId;
    dragStartY.current = event.clientY;
    dragStartTime.current = event.timeStamp;
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (dragStartY.current === null) return;
    // Multi-touch guard: only process events for the pointer we armed.
    if (
      activePointerId.current !== null &&
      event.pointerId !== activePointerId.current
    ) {
      return;
    }
    const raw = event.clientY - dragStartY.current;
    if (raw <= 0) {
      if (pullOffset !== 0) setPullOffset(0);
      return;
    }
    // Promote to a drag gesture once past the activation threshold.
    // This is when we capture the pointer — subsequent pointer events
    // route to us so the rubber-band stays smooth even if the finger
    // slides over child elements. Taps (pointerup before threshold)
    // never reach this branch, so child onClick fires normally.
    if (!isDragging && raw >= DRAG_ACTIVATION_PX) {
      if (event.currentTarget.setPointerCapture) {
        event.currentTarget.setPointerCapture(event.pointerId);
      }
      setIsDragging(true);
    }
    if (raw < DRAG_ACTIVATION_PX) return;
    setPullOffset(Math.min(raw * RUBBER_BAND_RESIST, MAX_PULL_PX));
  };

  const endDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (dragStartY.current === null) {
      setIsDragging(false);
      return;
    }
    // Multi-touch guard: ignore pointerup from a different pointer than
    // the one we armed.
    if (
      activePointerId.current !== null &&
      event.pointerId !== activePointerId.current
    ) {
      return;
    }
    // If the gesture never promoted to a drag (isDragging stayed false),
    // this was a tap — let the synthesized click reach the child row.
    // We just clear state without dismissing.
    if (!isDragging) {
      dragStartY.current = null;
      activePointerId.current = null;
      return;
    }
    const elapsedSec = Math.max(
      0.001,
      (event.timeStamp - dragStartTime.current) / 1000,
    );
    const velocity = pullOffset / elapsedSec;
    dragStartY.current = null;

    if (pullOffset > DISMISS_OFFSET_PX || velocity > DISMISS_VELOCITY_PX_S) {
      releasePointerCaptureIfHeld(event);
      activePointerId.current = null;
      setIsDragging(false);
      // Also hideBar so barOverlayOpen flips false — otherwise
      // shouldShow stays true on FAB-state routes and the FAB stays
      // hidden, stranding the user. Mirrors the X button path.
      // Codex critical-change review.
      dismissToDock();
      hideBar();
      return;
    }
    releasePointerCaptureIfHeld(event);
    activePointerId.current = null;
    setIsDragging(false);
    setPullOffset(0);
  };

  const cancelDrag = (event: PointerEvent<HTMLDivElement>) => {
    releasePointerCaptureIfHeld(event);
    clearDragState();
  };

  const onLostPointerCapture = () => {
    clearDragState();
  };

  return (
    <motion.div
      className="fixed left-0 right-0 z-bar-notif overflow-y-auto overscroll-contain"
      style={{
        top: viewportRect?.top ?? 0,
        height: viewportRect ? `${viewportRect.height}px` : "100dvh",
        background: "var(--background)",
        touchAction: "pan-y",
      }}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={fade}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={cancelDrag}
      onLostPointerCapture={onLostPointerCapture}
    >
      {/* Mirror header — PINNED to top of the scroll container so iOS
          keyboard opens, visual-viewport resizes, and content scroll never
          push the "asking / dump or ask…" row out of view. The sticky top
          is relative to this scroll parent (the motion.div), so it stays
          anchored to the visible viewport top regardless of keyboard state. */}
      <div
        className="sticky top-0 z-10 border-b border-border/30 px-4 pb-4"
        style={{
          paddingTop: MOBILE_OVERLAY_CONTENT_TOP,
          background: "var(--background)",
        }}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <p className="mb-1 text-[10px] uppercase tracking-[0.14em] text-fg-3">
              {sendKind === "remember"
                ? t.bar.mobileDumping
                : t.bar.mobileAsking}
            </p>
            <p className="text-[24px] font-medium leading-tight text-fg-1">
              {mirrorText || t.bar.placeholder.default}
            </p>
          </div>
          {/* Visible close affordance — swipe-down dismissal still
              works (96px / 720px·s gesture below) but isn't
              discoverable, especially the first time the user opens
              the bar via FAB on mobile. Plan 26 critical change.
              Tap clears the mirror surface AND closes the bar overlay
              so the user lands back on the FAB rest state. */}
          <button
            type="button"
            onClick={() => {
              dismissToDock();
              hideBar();
            }}
            aria-label={t.composeModal.closeAria}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1 cursor-pointer"
          >
            <X className="h-4 w-4" strokeWidth={2} />
          </button>
        </div>
      </div>

      {/* Results wrapper — drag transform applies here so the pinned
          mirror header stays put during rubber-band pulls. */}
      <div
        style={{
          paddingBottom: "calc(160px + var(--safe-bottom, 0px))",
          transform: `translate3d(0, ${pullOffset}px, 0)`,
          transition: isDragging ? "none" : settleTransition,
          willChange: isDragging ? "transform" : undefined,
        }}
      >
        <ExpandSearchResults />
      </div>
    </motion.div>
  );
}
