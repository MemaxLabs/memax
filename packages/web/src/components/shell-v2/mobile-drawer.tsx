"use client";

/**
 * `<MobileDrawer>` — left-edge slide-in nav drawer for mobile (plan
 * 22 P0-2). Renders the same tab targets the desktop LeftRail does
 * (Brain / Memories / Inbox / Agents) plus the user-avatar
 * SettingsPanel trigger.
 *
 * A11y: wrapped in `<Dialog.Root>` from base-ui so the open drawer
 * gets a real focus trap, focus restore on close, and Esc handling
 * for free. The motion.aside lives INSIDE `Dialog.Popup` so its
 * drag interaction stays on a framer-motion element (Popup's typing
 * for `onDragEnd` collides with framer's PanInfo signature). To keep
 * the slide-out animation alive after the request to close, we use a
 * two-phase close: an inner `displayed` state stays true while the
 * exit animation runs, and AnimatePresence's `onExitComplete` calls
 * `onClose()` — which then flips the parent's `open` and unmounts
 * the Dialog. Without this handoff, base-ui sees `open=false`
 * immediately and tears down the Portal before framer can run
 * `exit={{ x: "-100%" }}` (codex p3 re-review L1).
 *
 * Drag-to-close contract:
 *   - Only mounts the drag handler when the drawer is OPEN. When
 *     closed, no handler exists — iOS Safari's native left-edge
 *     swipe-back gesture works untouched (P0-2 spec: do NOT hijack
 *     left-edge gestures when closed). The Dialog.Root is fully
 *     unmounted via `open=false`, so even the Popup container is
 *     gone — no chance of an event listener catching a closed-state
 *     edge gesture.
 *   - Threshold: whichever fires first — drag offset > min(100px,
 *     drawerWidth*0.33) OR drag velocity > 400px/s leftward. Velocity
 *     gives a fast flick the satisfaction of closing without a long
 *     drag; offset handles slow deliberate close.
 *   - `touch-action: pan-y` so vertical scrolls inside the drawer
 *     work without being captured by the drag handler.
 *
 * SSR-safety: `window.innerWidth` is read inside `useLayoutEffect`,
 * not at render time. `drawerWidth = 0` until measurement; rendering
 * is gated on `drawerWidth > 0` so no pre-measure flash.
 */

import { useEffect, useLayoutEffect, useState, type ReactNode } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { AnimatePresence, motion } from "framer-motion";
import { X } from "lucide-react";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { useLocale } from "@/i18n";

const DRAWER_WIDTH_PCT = 0.78;
const CLOSE_VELOCITY_THRESHOLD_PX_PER_S = 400;
const MAX_OFFSET_THRESHOLD_PX = 100;

interface MobileDrawerProps {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}

export function MobileDrawer({ open, onClose, children }: MobileDrawerProps) {
  const { t } = useLocale();
  const [drawerWidth, setDrawerWidth] = useState(0);
  // `displayed` mirrors `open` but lags during exit so AnimatePresence
  // can run the slide-out before Dialog.Root unmounts the Portal.
  // Goes true synchronously when open flips on, false after exit
  // completes (or immediately if never opened).
  const [displayed, setDisplayed] = useState(open);

  useLayoutEffect(() => {
    const update = () =>
      setDrawerWidth(Math.round(window.innerWidth * DRAWER_WIDTH_PCT));
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

  // Open synchronously; let `onExitComplete` clear `displayed` on
  // close so the exit animation can play.
  useEffect(() => {
    if (open) setDisplayed(true);
  }, [open]);

  // Whichever fires first — small offset OR fast velocity. The
  // capped offset (100px max) means short drags still close on big
  // screens where 33% is much wider; velocity covers the flick case.
  const closeOffsetThreshold = Math.min(
    MAX_OFFSET_THRESHOLD_PX,
    Math.round(drawerWidth * 0.33),
  );

  // Defer mount until measurement lands — avoids painting the drawer
  // at width 0 (collapsed) before the matchMedia + resize listener
  // can settle. Dialog.Root open keeps the Portal mounted while
  // either the parent's `open` is true OR our exit animation is
  // still running (`displayed`).
  const dialogOpen = (open || displayed) && drawerWidth > 0;

  return (
    <Dialog.Root
      open={dialogOpen}
      onOpenChange={(next) => {
        // Esc + backdrop press both flow through here. The parent
        // updates `open=false`; the AnimatePresence below catches
        // the resulting unmount on `motion.aside` and runs the exit
        // animation. We only forward to onClose — the exit-complete
        // callback handles the actual `displayed` flip.
        if (!next) onClose();
      }}
    >
      <Dialog.Portal>
        {/* Both scrim and panel live inside the same AnimatePresence
            so their enter/exit run on the same timeline. Dropping
            base-ui's Dialog.Backdrop in favor of a sibling motion.div
            is what keeps them in step — the previous data-state
            backdrop only flipped data-ending-style after `displayed`
            cleared, which fired AFTER the panel exit completed
            (codex p3 drawer-anim L1). With AnimatePresence both
            children unmount on `open=false` and animate exit
            together. The scrim still pressureses on click via
            onClick → onClose; Esc + focus trap remain handled by
            Dialog.Root above. */}
        <Dialog.Popup
          aria-label={t.nav.primary}
          className="z-modal fixed inset-0 outline-none"
        >
          <AnimatePresence
            onExitComplete={() => {
              // Exit done → drop `displayed` so Dialog.Root unmounts
              // the Portal. Without this the Portal stays mounted
              // forever once the user opens the drawer once.
              setDisplayed(false);
            }}
          >
            {open && (
              <div key="drawer-stack">
                <motion.div
                  // Light scrim — was bg-foreground/40 which darkened
                  // the page content behind the drawer enough that the
                  // glass-dropdown's backdrop-filter sampled a darkened
                  // background and the whole drawer read gray-tinted.
                  // 12% gives enough click-area dimming for affordance
                  // without polluting the glass sample. Plan 26 follow-
                  // up after switching the drawer material to glass-
                  // dropdown.
                  className="absolute inset-0 bg-foreground/12"
                  onClick={onClose}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={{ opacity: 0 }}
                  transition={{ duration: FAST, ease: EASE }}
                  aria-hidden
                />
                <motion.aside
                  // Use the same .glass-dropdown recipe the mobile filter
                  // dropdown uses (user-validated as the correct glass
                  // visual). Was glass-panel which read denser + lacked
                  // the same blur character. Plan 26 follow-up.
                  className="glass-dropdown backdrop-blur-sm absolute inset-y-0 left-0 flex flex-col"
                  style={{
                    width: drawerWidth,
                    touchAction: "pan-y",
                  }}
                  initial={{ x: "-100%" }}
                  animate={{ x: 0 }}
                  exit={{ x: "-100%" }}
                  // Aggressive snap — drawer is large, every ms of
                  // perceived lag reads as sluggish on mobile. Spring
                  // bumped to stiffness=1100, damping=44, mass=0.3 →
                  // settles in ~80ms with no overshoot. User: "mobile
                  // side panel animation is too slow" (twice).
                  transition={{
                    type: "spring",
                    stiffness: 1100,
                    damping: 44,
                    mass: 0.3,
                  }}
                  drag="x"
                  dragConstraints={{ left: -drawerWidth, right: 0 }}
                  dragElastic={0.05}
                  onDragEnd={(_, info) => {
                    const closedByOffset =
                      info.offset.x < -closeOffsetThreshold;
                    const closedByVelocity =
                      info.velocity.x < -CLOSE_VELOCITY_THRESHOLD_PX_PER_S;
                    if (closedByOffset || closedByVelocity) onClose();
                  }}
                >
                  {/* Drawer header — close button on the right so the
                      affordance pair is symmetric with the hamburger
                      that opened it. */}
                  <div className="flex h-12 items-center justify-end px-3 shrink-0">
                    <button
                      type="button"
                      onClick={onClose}
                      aria-label={t.nav.hideSecondaryPanel}
                      className="flex h-8 w-8 items-center justify-center rounded-md text-fg-3 cursor-pointer transition-colors hover:bg-foreground/6 hover:text-fg-2"
                    >
                      <X className="h-4 w-4" strokeWidth={2} />
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto overscroll-contain">
                    {children}
                  </div>
                </motion.aside>
              </div>
            )}
          </AnimatePresence>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
