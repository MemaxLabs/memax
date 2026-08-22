"use client";

/**
 * `<MemoryDetail variant="route" | "modal" />` — plan 21 phase 1's
 * unified memory-detail entry point. One component, two variants.
 *
 * Variant semantics:
 *   - `route`  → caller is the canonical permalink page
 *                (`/h/<slug>/memories/<id>`). Renders the full reading
 *                surface inline with sticky-header chrome owned by
 *                `<MemoryDetailView>` itself.
 *   - `modal`  → caller is a transient inspect dialog opened from a
 *                grid click. Wraps the same body in a base-ui
 *                `<Dialog>` with a centered glass-panel and an Esc /
 *                click-outside dismiss.
 *
 * Both variants render the IDENTICAL set of sections — title, body,
 * "memax sees this as," source, lifecycle, tags (plan 21 §3.1) — so
 * a copy-link from the modal to the route lands on the same content.
 *
 * Why a thin variant wrapper instead of a from-scratch decomposition:
 * the existing `<MemoryDetailView>` already implements the section
 * cluster the plan calls for (with phase 1a having moved tags to the
 * bottom). Phase 1b/3 add Tiptap edit mode + finer sub-component
 * extraction (MemoryDetailHeader, ClassificationPanel, ...); those
 * land incrementally without breaking the variant API committed
 * here. Memory-modal.tsx will swap to `variant="modal"` in a follow-
 * up so the existing modal flow doesn't churn in the same commit.
 *
 * Recall-time tracking: `<MemoryDetailView>` already mounts the
 * dwell-gated `useTrackMemoryAccessed(memoryId)` (plan 21 phase 0)
 * once per id, so both variants get tracking via the inner view
 * with no double-fire. Server-side SetNX dedup is the safety net
 * for the unlikely two-instance race.
 */

import { Dialog } from "@base-ui/react/dialog";
import { ArrowUpRight, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { MemoryDetailView } from "@/components/features/memory-detail/memory-detail-view";
import { useRouter } from "next/navigation";
import { useLocale } from "@/i18n";

interface RouteVariantProps {
  variant: "route";
  memoryId: string;
}

interface ModalVariantProps {
  variant: "modal";
  memoryId: string;
  /** Required for modal variant — Esc / click-outside / close-button trigger. */
  onClose: () => void;
}

interface PanelVariantProps {
  variant: "panel";
  memoryId: string;
  /** Right-anchored side panel, opened via Next.js intercepted route
   *  from a grid click. Closing pops the intercept (router.back), so
   *  the user returns to the list with the panel sliding out. */
  onClose: () => void;
  /** Full-page route for the same memory — surfaced as a "↗ Open full
   *  page" affordance inside the panel (Linear / Notion pattern).
   *  Lets the user promote a quick-peek into the focused reading
   *  surface when they want it. */
  fullPagePath: string;
}

type Props = RouteVariantProps | ModalVariantProps | PanelVariantProps;

export function MemoryDetail(props: Props) {
  if (props.variant === "modal") {
    return (
      <MemoryDetailModalShell
        memoryId={props.memoryId}
        onClose={props.onClose}
      />
    );
  }
  if (props.variant === "panel") {
    return (
      <MemoryDetailPanelShell
        memoryId={props.memoryId}
        onClose={props.onClose}
        fullPagePath={props.fullPagePath}
      />
    );
  }
  return <MemoryDetailView memoryId={props.memoryId} />;
}

/**
 * MemoryDetailPanelShell — right-anchored slide-in panel for
 * grid-click "peek" navigation. Industry 2026 pattern: Linear's
 * issue panel, Notion's side peek, Vercel's deployment panel.
 *
 * Behavior:
 *   - Slide-in from the right via base-ui Dialog + a CSS-only
 *     translate transition. Backdrop is transparent (no dim) so the
 *     underlying list stays visually present — the panel reads as
 *     "alongside" the list, not "modal-on-top-of-content".
 *   - Esc / backdrop-click / X all close → router.back() so the URL
 *     pops cleanly and the intercept unwinds.
 *   - "Open full page" link (↗) navigates to the canonical
 *     /h/<slug>/memories/<id> route. Same content, different surface
 *     for a focused read.
 *   - Width caps at min(640px, 50vw) so the list stays visible on
 *     wider viewports. On narrow viewports the panel takes ~50% and
 *     the list still gets the rest.
 */
// Match the panel's transition-duration so close-anim ends BEFORE
// router.back() unmounts the route. Keeping these in sync in one
// place avoids the slide-out flickering if the className changes
// in the future.
const PANEL_TRANSITION_MS = 300;

function MemoryDetailPanelShell({
  memoryId,
  onClose,
  fullPagePath,
}: {
  memoryId: string;
  onClose: () => void;
  fullPagePath: string;
}) {
  const { t } = useLocale();
  const router = useRouter();
  // Controlled open with a mount-time false→true flip so base-ui
  // sees an actual opening transition and applies data-starting-style
  // (the translate-x-full → 0 slide-in). Mounting with open=true
  // straight from useState skips the starting-style frame in some
  // React 18+ commit phases — the popup commits already at the
  // resting position with no transform to interpolate from. Flipping
  // in useLayoutEffect guarantees one render at open=false (data-
  // starting-style attaches with translate-x-full) before we commit
  // the transition to open=true (data-starting-style detaches, CSS
  // animates the transform back to 0). Same React subtree, just a
  // proper opening-frame.
  //
  // On exit, `onClose` (router.back) tears down the subtree
  // immediately; if we wired it straight to X / Esc / backdrop,
  // data-ending-style would never get a frame. Instead: flip open
  // false → base-ui sets data-ending-style → panel slides out over
  // PANEL_TRANSITION_MS → then we call onClose to pop the route.
  // Browser/router back-button still hits the intercept correctly
  // because that path doesn't go through our handler.
  //
  // closingRef + timerRef guard against:
  //   a) Repeated close events (rapid X + Esc) stacking up multiple
  //      router.back() calls and over-popping history.
  //   b) The user triggering native back AFTER closeWithExit but
  //      BEFORE the timeout fires, leading to a stale timer that
  //      pops history again post-unmount.
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    // Schedule on the next frame so the false-open render commits
    // and paints with data-starting-style + translate-x-full applied
    // before we flip to true. requestAnimationFrame is enough — the
    // browser paints between rAF and the next commit, giving the
    // starting style a real frame to attach to.
    const id = requestAnimationFrame(() => setOpen(true));
    return () => cancelAnimationFrame(id);
  }, []);
  const closingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const closeWithExit = () => {
    if (closingRef.current) return; // first close wins
    closingRef.current = true;
    setOpen(false);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      onClose();
    }, PANEL_TRANSITION_MS);
  };
  // "Open full page" — animate the slide-out FIRST so the user sees
  // the panel leave gracefully, THEN soft-navigate to the /full child
  // route. Pushing the memory's own URL would be a no-op (the
  // intercept already owns it) and the old escape — a hard
  // window.location.assign — made this click AND every subsequent
  // browser-back a full document reload. `(.)memories/[id]` matches
  // exactly one segment, so `<memory>/full` is NOT intercepted: it
  // renders the canonical full view inline, client-side, and back
  // from it soft-returns to where the user was.
  const openFullPage = () => {
    if (closingRef.current) return;
    closingRef.current = true;
    setOpen(false);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      router.push(`${fullPagePath}/full`);
    }, PANEL_TRANSITION_MS);
  };
  // Prefetch the escape route while the panel is open so the swap
  // lands the moment the slide-out finishes instead of paying the
  // chunk download inside the transition window.
  useEffect(() => {
    router.prefetch(`${fullPagePath}/full`);
  }, [router, fullPagePath]);
  useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, []);
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) closeWithExit();
      }}
    >
      <Dialog.Portal>
        {/* Translucent backdrop — softer than the modal's
            foreground/12. Lets the list show through so the panel
            reads as a side-by-side reading surface, not a modal
            takeover. */}
        <Dialog.Backdrop className="fixed inset-0 z-modal bg-foreground/[0.04]" />
        <Dialog.Popup
          aria-label={t.memoryDetail.panelAria}
          // Slide-in animation: data-starting-style sets initial
          // translate-x-full BEFORE the open frame; data-ending-style
          // re-applies it when open flips back to false (the
          // controlled close path above). Base-ui maps these to
          // `data-starting-style` / `data-ending-style` attrs.
          //
          // Glass styling: we don't use the .glass-panel utility here
          // because that class includes a full border-radius + 4-side
          // box-shadow that look wrong on an edge-anchored panel
          // (rounded right edge floating against the viewport, top/
          // bottom shadow clipping). Instead we compose the same
          // tokens (--glass-fill-72, --glass-filter-md,
          // --glass-border-hairline, --glass-inset-highlight) with
          // panel-appropriate geometry: only the left edge is
          // rounded, only a left-side hairline + edge-anchored
          // shadow show. Net result is the SAME material as
          // .glass-panel (matching blur character + fill opacity) but
          // shaped for a side panel.
          style={{
            background: "var(--glass-fill-72)",
            backdropFilter: "var(--glass-filter-md)",
            WebkitBackdropFilter: "var(--glass-filter-md)",
            borderLeft: "1px solid var(--glass-border-hairline)",
            boxShadow:
              "inset 1px 0 0 var(--glass-inset-highlight), -8px 0 24px -6px oklch(0 0 0 / 0.10), -2px 0 8px -2px oklch(0 0 0 / 0.06)",
          }}
          className={[
            "z-modal fixed right-0 top-0 h-dvh",
            // Width: full on mobile (takeover), capped at min(640px,
            // 50vw) on tablet+ so the underlying view stays visible.
            "w-full md:w-[min(640px,50vw)]",
            // Only the left edge is rounded (panel hugs the right
            // viewport edge). md+ adds a subtle rounded-l so it
            // reads as a glass panel rather than a slab.
            "md:rounded-l-2xl outline-none overflow-hidden flex flex-col",
            "data-[starting-style]:translate-x-full",
            "data-[ending-style]:translate-x-full",
            "transition-transform duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]",
          ].join(" ")}
        >
          <div className="flex items-center justify-between px-4 py-3 border-b border-border/10 shrink-0">
            {/*
              Animated escape to the full page: triggers the slide-out
              first so the panel leaves gracefully, then hard-navs.
              Hard nav is required because soft nav to the same memory
              URL re-triggers the @panel/(.)memories/[id] intercept
              (the very thing we're trying to leave). The animation
              guard turns a flashing reload into a clean transition.
            */}
            <button
              type="button"
              onClick={openFullPage}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[12px] text-fg-3 hover:text-fg-1 hover:bg-foreground/5 transition-colors cursor-pointer"
              title={t.memoryDetail.openFullPage}
            >
              <ArrowUpRight className="h-3.5 w-3.5" aria-hidden />
              <span>{t.memoryDetail.openFullPage}</span>
            </button>
            <button
              type="button"
              onClick={closeWithExit}
              aria-label={t.memoryDetail.closePanel}
              className="rounded-chrome p-1.5 text-fg-3 hover:bg-foreground/5 hover:text-fg-2 transition-colors cursor-pointer"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto">
            <MemoryDetailView memoryId={memoryId} onBack={closeWithExit} />
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function MemoryDetailModalShell({
  memoryId,
  onClose,
}: {
  memoryId: string;
  onClose: () => void;
}) {
  const { t } = useLocale();
  // No tracker mount here — the inner <MemoryDetailView> already
  // calls useTrackMemoryAccessed(id) which dwell-fires per the
  // plan-21 phase-0 contract. Adding a second mount would have
  // both timers race + both call markSeen + trackAccessed; the
  // sessionStorage + server SetNX dedup catch it but the cleaner
  // shape is single-mount.
  return (
    <Dialog.Root open onOpenChange={(next) => (next ? null : onClose())}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-modal bg-foreground/12 backdrop-blur-sm" />
        <Dialog.Popup
          aria-label={t.note.actions.menu}
          className="glass-panel z-modal fixed left-1/2 top-1/2 w-[min(960px,92vw)] max-h-[90vh] -translate-x-1/2 -translate-y-1/2 rounded-surface outline-none overflow-hidden flex flex-col"
        >
          <button
            type="button"
            onClick={onClose}
            aria-label={t.composeModal.closeAria}
            className="absolute right-4 top-4 z-10 rounded-chrome p-1.5 text-fg-3 hover:bg-foreground/5 hover:text-fg-2 transition-colors cursor-pointer"
          >
            <X className="h-4 w-4" />
          </button>
          <div className="flex-1 overflow-y-auto">
            {/* `onBack={onClose}` so the inner view's back arrow
                dismisses the dialog instead of running router.back()
                (the route-variant default). Codex 5.5 M-finding. */}
            <MemoryDetailView memoryId={memoryId} onBack={onClose} />
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
