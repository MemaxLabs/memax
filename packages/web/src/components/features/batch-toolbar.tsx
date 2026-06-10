"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { pluralize, useLocale } from "@/i18n";
import { useSelection } from "@/contexts/selection-context";
import {
  signalBatchToolbarActive,
  signalBatchToolbarInactive,
} from "@/lib/batch-active";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { DestinationPicker } from "@/components/features/destination-picker";
import { useDestructiveAction } from "@/hooks/use-destructive-action";
import {
  FolderInput,
  Trash2,
  Download,
  Copy,
  Check,
  X,
  Loader2,
  ChevronLeft,
  CheckCheck,
} from "lucide-react";

interface BatchToolbarProps {
  /**
   * Called with selected IDs when batch forget is confirmed. Must
   * return a boolean so the destructive confirmation card can stay
   * mounted on failure — `true` closes the card and runs normal
   * success side effects, `false` keeps the confirm buttons visible
   * so the user can retry or cancel. Paired with the widened
   * useDestructiveAction contract that treats explicit false as
   * expected failure (see f8d334b4).
   */
  onBatchForget: (ids: string[]) => Promise<boolean>;
  /**
   * Called with selected IDs + target.
   * `destinationName` is the display string the picker rendered for the
   * chosen row so the success toast matches what the user clicked.
   */
  onBatchMove: (
    ids: string[],
    target: {
      topicId?: string;
      hubId?: string;
      destinationName?: string;
    },
  ) => void;
  /** Called with selected IDs to copy context */
  onCopyContext: (ids: string[]) => void;
  /** Called with selected IDs to export */
  onExport?: (ids: string[]) => void;
  /** Whether a batch mutation is in progress */
  pending?: boolean;
  /** Pending label for the active batch mutation */
  pendingLabel?: string;
  /** Compact mode (icons only, for mobile) */
  compact?: boolean;
}

/**
 * Shell state machine — which body renders inside the stable outer
 * container. `confirm` and `pending` own the whole shell (no anchor
 * row); `actions` and `picker` share the anchor row and only swap
 * the trailing body region. This is the "one container, one morph"
 * contract the batch UX ships on; slice 1 moved `picker` from an
 * external `absolute bottom-full` overlay INTO the shell so it
 * morphs alongside the other states.
 */
type BatchMode = "actions" | "picker" | "confirm" | "pending";

export function BatchToolbar({
  onBatchForget,
  onBatchMove,
  onCopyContext,
  onExport,
  pending = false,
  pendingLabel,
  compact: compactProp,
}: BatchToolbarProps) {
  const isMobile = useIsMobile();
  const compact = compactProp ?? isMobile;
  const { t } = useLocale();
  const { selected, exit, selectAll, visibleCount } = useSelection();
  const forgetAction = useDestructiveAction<"forget">();
  const [showPicker, setShowPicker] = useState(false);
  const [copied, setCopied] = useState(false);

  // Focus restoration + outside-click dismissal refs. shellRef is the
  // whole morphing container; moveButtonRef is the trigger we return
  // focus to after the picker closes (the ActionRow unmounts while
  // picker mode is active, so we cache the ref while it's alive and
  // restore on next commit when it re-mounts).
  const shellRef = useRef<HTMLDivElement>(null);
  const moveButtonRef = useRef<HTMLButtonElement>(null);
  const prevModeRef = useRef<BatchMode>("actions");

  const count = selected.size;
  const isVisible = count > 0;

  // Signal GlobalBar to hide when batch toolbar is showing (Set-based, idempotent)
  useEffect(() => {
    if (isVisible) {
      const id = signalBatchToolbarActive();
      return () => signalBatchToolbarInactive(id);
    }
  }, [isVisible]);

  const isForgetRunning = forgetAction.isRunning("forget");
  const effectivePending = pending || isForgetRunning;
  const mode: BatchMode =
    forgetAction.isConfirming("forget") && !isForgetRunning
      ? "confirm"
      : effectivePending
        ? "pending"
        : showPicker
          ? "picker"
          : "actions";

  // Picker-open concerns: Escape to close, outside-click to close, and
  // focus restoration to the Move button when picker → actions. All
  // attach only while mode === "picker" so they don't run during
  // confirm / pending / actions states.
  //
  // Keydown listens on the SHELL element in bubble phase. DrillDownTree
  // owns its own one-level-back Esc inside the picker; it calls
  // stopPropagation when it consumes the key, which prevents the event
  // from reaching the shell. Only root-level Escape (not handled by
  // any nested navigator) bubbles up to us here, and we stopPropagation
  // so SelectionProvider's document-level Esc → exit() doesn't fire
  // and clear the selection out from under the toolbar.
  useEffect(() => {
    if (mode !== "picker") return;
    const shell = shellRef.current;
    if (!shell) return;
    const onDocPointerDown = (e: PointerEvent) => {
      const target = e.target as Node | null;
      if (target && shell.contains(target)) return;
      setShowPicker(false);
    };
    const onShellKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      setShowPicker(false);
    };
    document.addEventListener("pointerdown", onDocPointerDown);
    shell.addEventListener("keydown", onShellKey);
    return () => {
      document.removeEventListener("pointerdown", onDocPointerDown);
      shell.removeEventListener("keydown", onShellKey);
    };
  }, [mode]);

  useEffect(() => {
    if (prevModeRef.current === "picker" && mode === "actions") {
      // ActionRow just re-mounted; moveButtonRef is valid on the next
      // frame. preventScroll avoids a viewport jump when the caller
      // was scrolled mid-picker.
      const raf = requestAnimationFrame(() => {
        moveButtonRef.current?.focus({ preventScroll: true });
      });
      return () => cancelAnimationFrame(raf);
    }
    prevModeRef.current = mode;
  }, [mode]);

  if (!isVisible || typeof document === "undefined") return null;

  const ids = [...selected];

  const handleCopy = () => {
    onCopyContext(ids);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // ── Shell chrome ──
  // Every non-confirm state uses .glass-toolbar — same material family
  // as .glass-bar (65% fill + lg blur) so the toolbar and the muted
  // command bar behind it read as kin. Confirm mode overlays a
  // destructive tint on top of the glass (no change to the underlying
  // material; the destructive color is an additive signal, not a
  // replacement chrome).
  //
  // Geometry is mode-driven: picker expands vertically (responsive
  // width up to 420px, capped height); everything else is a horizontal
  // strip at min-h-[52px].
  // `pointer-events-auto` + `animate-content-ready` are on the SAME
  // element as `.glass-toolbar`. Keeping the fade-in opacity on the
  // glass element itself means the only ancestor between this div and
  // the viewport is the fixed portal wrapper (no opacity / transform /
  // filter), so backdrop-filter actually samples page content instead
  // of an empty compositing layer trapped by an animated parent.
  // Mirrors the bar's pattern from layout.tsx: "The glass only samples
  // page content when backdrop-filter lives on the outermost fixed
  // element with no transformed ancestors."
  //
  // `backdrop-blur-lg` is paired with `glass-toolbar` deliberately: the
  // class owns bg + rim + shadow + radius + saturate/contrast (via
  // -webkit-backdrop-filter), but Lightning CSS strips the standard
  // `backdrop-filter` property from raw custom-CSS in some builds.
  // Pairing a Tailwind utility guarantees the standard property emits
  // so the blur actually resolves cross-browser. Same pattern the bar
  // uses (`glass-bar backdrop-blur-lg`) and popover (`glass-dropdown
  // backdrop-blur-sm`) — see 1e340aff for the original finding.
  const commonShellClass =
    "pointer-events-auto animate-content-ready backdrop-blur-lg";
  const shellClass =
    mode === "picker"
      ? // Responsive width: full available width up to 420px so narrow
        // viewports (≥320px with the portal's px-4 gutters = 288px
        // effective) don't get forced-overflow. Height capped at
        // max-h-[380px] so the anchor row (≈52px) + hint row (≈44px)
        // + list region always fit; the list gets an explicit
        // listHeight below so it owns its scroll region.
        `${commonShellClass} glass-toolbar flex flex-col rounded-surface w-full max-w-[420px] max-h-[380px] overflow-hidden`
      : mode === "confirm"
        ? // Destructive overlay on top of the glass-toolbar material.
          // Tint via inline style (additive) so we don't fork the
          // recipe; border/shadow/inset-rim continue to come from the
          // class.
          `${commonShellClass} glass-toolbar flex items-center gap-2 rounded-surface px-4 min-h-[52px]`
        : `${commonShellClass} glass-toolbar flex items-center gap-2 rounded-surface px-4 min-h-[52px]`;
  const shellStyle =
    mode === "confirm"
      ? {
          // Layered on top of .glass-toolbar's material via a
          // translucent destructive tint. Border picks up the
          // destructive color; shadow stays from the class.
          background: "oklch(from var(--destructive) l c h / 0.08)",
          border: "1px solid oklch(from var(--destructive) l c h / 0.15)",
        }
      : undefined;

  const handlePickerSelectTopic = (
    topicId: string,
    hubId?: string,
    destinationName?: string,
  ) => {
    onBatchMove(ids, { topicId, hubId, destinationName });
    setShowPicker(false);
  };
  const handlePickerSelectHub = (hubId: string, destinationName?: string) => {
    onBatchMove(ids, { hubId, destinationName });
    setShowPicker(false);
  };

  const shell = (
    <div ref={shellRef} className={shellClass} style={shellStyle}>
      {mode === "confirm" && (
        <>
          <span className="text-[13px] text-fg-2 flex-1">
            {pluralize(t.batch.forgetConfirmOne, t.batch.forgetConfirm, count)}
          </span>
          <button
            type="button"
            onClick={() =>
              void forgetAction.run("forget", () => onBatchForget(ids))
            }
            className="text-[13px] min-h-11 px-4 py-2 rounded-chrome cursor-pointer transition-colors"
            style={{
              background: "oklch(from var(--destructive) l c h / 0.12)",
              color: "oklch(from var(--destructive) l c h / 0.8)",
            }}
          >
            {t.forget.button}
          </button>
          <button
            type="button"
            onClick={() => forgetAction.cancel("forget")}
            className="text-[13px] min-h-11 px-4 py-2 rounded-chrome cursor-pointer text-fg-2 hover:text-fg-1 transition-colors"
          >
            {t.forget.keep}
          </button>
        </>
      )}

      {mode === "pending" && (
        <>
          <Loader2 className="h-3.5 w-3.5 text-fg-3 animate-spin" />
          <span className="text-[13px] text-fg-2">
            {isForgetRunning
              ? pluralize(t.batch.forgettingOne, t.batch.forgetting, count)
              : (pendingLabel ??
                pluralize(t.batch.movingOne, t.batch.moving, count))}
          </span>
        </>
      )}

      {(mode === "actions" || mode === "picker") && (
        <>
          {/* Anchor row — count + close. Stable across actions/picker
              modes; unmounts only when the shell switches to confirm
              or pending (which own the whole body). In picker mode,
              sits as the top strip above the scrollable picker list. */}
          <div
            className={
              mode === "picker"
                ? "flex items-center gap-0.5 px-1.5 min-h-[52px] shrink-0 border-b border-border/40"
                : "flex items-center gap-0.5 px-1.5 min-h-[52px] w-full"
            }
            // in picker mode the shell has no px-4, so anchor row owns
            // its own horizontal padding via px-1.5 (already present)
          >
            <button
              type="button"
              onClick={exit}
              className="flex items-center gap-1.5 min-h-11 pl-3 pr-2.5 rounded-chrome cursor-pointer transition-colors hover:bg-surface-2"
              title={t.batch.done}
            >
              <span
                className="text-[13px] font-semibold tabular-nums"
                style={{ color: "var(--signature)" }}
              >
                {count}
              </span>
              <X className="h-3.5 w-3.5 text-fg-3" />
            </button>

            <div className="w-px h-5 bg-border/40 mx-0.5" />

            {mode === "actions" ? (
              <>
                {/* Select all — Cmd/Ctrl+A shortcut also wired in
                    selection-context. Only shown in actions mode; hidden
                    during picker/confirm/pending. Disabled when every
                    visible row is already selected (nothing to add). */}
                <button
                  type="button"
                  onClick={selectAll}
                  disabled={
                    effectivePending ||
                    (visibleCount > 0 && count >= visibleCount)
                  }
                  className="flex items-center gap-1.5 min-h-11 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 hover:bg-surface-2 disabled:cursor-default disabled:text-fg-4 disabled:hover:bg-transparent"
                  title={t.batch.selectAll}
                >
                  <CheckCheck className="h-4 w-4" />
                  {!compact && (
                    <span className="text-[13px]">{t.batch.selectAll}</span>
                  )}
                </button>

                {/* Move */}
                <button
                  ref={moveButtonRef}
                  type="button"
                  onClick={() => setShowPicker(true)}
                  disabled={effectivePending}
                  className="flex items-center gap-1.5 min-h-11 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 hover:bg-surface-2 disabled:cursor-default disabled:text-fg-4 disabled:hover:bg-transparent"
                  title={t.batch.move}
                >
                  <FolderInput className="h-4 w-4" />
                  {!compact && (
                    <span className="text-[13px]">{t.batch.move}</span>
                  )}
                </button>

                {/* Copy context */}
                <button
                  type="button"
                  onClick={handleCopy}
                  disabled={effectivePending}
                  className="flex items-center gap-1.5 min-h-11 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 hover:bg-surface-2 disabled:cursor-default disabled:text-fg-4 disabled:hover:bg-transparent"
                  title={t.batch.copy}
                >
                  {copied ? (
                    <Check
                      className="h-4 w-4"
                      style={{ color: "oklch(0.72 0.19 145)" }}
                    />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                  {!compact && (
                    <span className="text-[13px]">
                      {copied ? t.batch.copied : t.batch.copy}
                    </span>
                  )}
                </button>

                {/* Export */}
                {onExport && (
                  <button
                    type="button"
                    onClick={() => onExport(ids)}
                    className="flex items-center gap-1.5 min-h-11 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 hover:bg-surface-2"
                    title={t.batch.export}
                  >
                    <Download className="h-4 w-4" />
                    {!compact && (
                      <span className="text-[13px]">{t.batch.export}</span>
                    )}
                  </button>
                )}

                {/* Forget */}
                <button
                  type="button"
                  onClick={() => forgetAction.start("forget")}
                  disabled={effectivePending}
                  className="flex items-center gap-1.5 min-h-11 px-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-destructive/70 hover:bg-destructive/5 disabled:cursor-default disabled:text-fg-4 disabled:hover:bg-transparent"
                  title={t.batch.forget}
                >
                  <Trash2 className="h-4 w-4" />
                  {!compact && (
                    <span className="text-[13px]">{t.batch.forget}</span>
                  )}
                </button>
              </>
            ) : (
              // Picker mode — back button returns to the action row
              // WITHOUT exiting batch mode. The count × button above
              // exits entirely (clear + exit, Gmail/Mail parity).
              // DrillDownTree owns nested back-between-levels; this
              // back is specifically "exit picker, keep selection".
              <button
                type="button"
                onClick={() => setShowPicker(false)}
                aria-label={t.batch.pickerBack}
                className="flex items-center gap-1.5 min-h-11 pl-2 pr-3 rounded-chrome cursor-pointer transition-colors text-fg-2 hover:text-fg-1 hover:bg-surface-2"
              >
                <ChevronLeft className="h-3.5 w-3.5 text-fg-3" />
                <span className="text-[13px]">{t.batch.move}</span>
              </button>
            )}
          </div>

          {mode === "picker" && (
            <ClickableBody
              // Listens on this inner element so picker clicks don't
              // trigger the outside-click handler attached at document.
              onPointerDownCapture={(e) => e.stopPropagation()}
            >
              {/* Explicit listHeight so DrillDownTree's internal
                  overflow-y-auto region owns its own scroll instead
                  of growing to content and getting clipped by the
                  shell's overflow-hidden. 380 (shell max-h) - 52
                  (anchor row) - 44 (DestinationPicker hint row) ≈
                  284px of list space; 260 keeps a little breathing
                  room. Without this, long topic trees either clip or
                  the page behind the picker inherits touch-scroll. */}
              <DestinationPicker
                variant="plain"
                listHeight={260}
                onSelectTopic={handlePickerSelectTopic}
                onSelectHub={handlePickerSelectHub}
                onClose={() => setShowPicker(false)}
              />
            </ClickableBody>
          )}
        </>
      )}
    </div>
  );

  return createPortal(
    <div className="fixed inset-0 z-modal pointer-events-none">
      <div
        className="absolute bottom-0 left-0 right-0 flex justify-center py-3 px-4 pointer-events-none"
        style={{
          // Stack above the muted bar. --bar-dock-offset is set by
          // layout.tsx when the bar is docked at the bottom; resolves
          // to 0 when the bar is elsewhere (brain centered / engaged
          // lifted) so the toolbar keeps its native bottom anchor.
          // 12px gap + device safe-area always applied.
          paddingBottom:
            "calc(var(--bar-dock-offset, 0px) + 12px + env(safe-area-inset-bottom))",
          transition: "padding-bottom 200ms cubic-bezier(0.16, 1, 0.3, 1)",
        }}
      >
        {shell}
      </div>
    </div>,
    document.body,
  );
}

/**
 * ClickableBody — thin wrapper around the in-shell picker body that
 * flex-1/overflow-hiddens so DestinationPicker's internal scrolling
 * bounds to the shell's max-height and that stops pointerdown from
 * propagating to the document-level outside-click handler. Kept
 * inline so its existence is obvious at the call site; the scoping
 * is the important part.
 */
function ClickableBody({
  children,
  onPointerDownCapture,
}: {
  children: React.ReactNode;
  onPointerDownCapture: React.PointerEventHandler<HTMLDivElement>;
}) {
  return (
    <div
      onPointerDownCapture={onPointerDownCapture}
      className="flex-1 overflow-hidden"
    >
      {children}
    </div>
  );
}
