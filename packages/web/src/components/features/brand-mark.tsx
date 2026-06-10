"use client";

import Link from "next/link";
import { ChevronsRight } from "lucide-react";
import { useTreePanel } from "@/components/features/topic/topic-tree-panel";
import { useLocale } from "@/i18n";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";

/**
 * BrandMark — top-left main chrome (mobile-only after plan 24 phase
 * 4b deletion of v1 desktop). An Apple-style squircle with the memax
 * icon. Icon-only at rest; on hover the glyph cross-fades to a
 * `ChevronsRight` cue and `click` calls `pin()` to open the tree
 * drawer. When the tree drawer expands (pinned or drag-session), this
 * floating instance FADES OUT (opacity 0 + pointer-events none) so
 * the drawer's slide-in doesn't visually collide with the floating
 * icon beneath it.
 *
 * The legacy `"panel-header"` context (memax wordmark inside the v1
 * PinnedTreeSidebar header) was deleted alongside PinnedTreeSidebar
 * in plan 24 phase 4b.
 *
 * Surface tokens (floating squircle):
 *   - `--radius-chrome` (14px, ~39% radius at 36×36) — the memax chrome
 *     squircle standard. Single source of truth in globals.css; every
 *     chrome-sized interactive tile (BrandMark, panel close buttons,
 *     inbox trigger, etc.) consumes this token so they can't drift.
 *   - Background `var(--card)` — the kitchen's base surface token.
 *     Auto-flips across themes: paper-white (#fff) in light, gently
 *     elevated dark (oklch 0.2) in dark mode. Lighter than the prior
 *     8%-foreground tint; reads as a bright Apple-style app tile in
 *     light mode while still reading as an elevated surface in dark.
 *   - Glyph `var(--foreground)` at 0.85 opacity — same tone as the
 *     wordmark that sits beside it in the panel header, keeping the
 *     pair visually unified.
 *   - Inner-rim + soft drop shadow preserve the iOS Liquid Glass DNA:
 *     `inset 0 1px 0 oklch(1 0 0 / 0.08), 0 1px 3px oklch(0 0 0 / 0.08)`.
 *
 * Refero 2026 sweep (Raycast, Cursor, Anthropic, Polar, Linear, Notion):
 *   - Product-icon tiles are solid, not translucent.
 *   - Chrome palette stays neutral; brand color only appears on inner
 *     affordances (hub badges, send buttons, etc.) — never on the
 *     identity tile itself.
 *   - Sidebar headers show wordmark only; icon lives in the top chrome
 *     when the sidebar is closed.
 */
export function BrandMark({
  context,
  isMobile,
}: {
  /**
   * `"floating"` is the only supported context after plan 24 phase 4b.
   * Kept as a discriminated union so future contexts (e.g. a v2 mobile
   * drawer header) can be added without breaking call sites.
   */
  context: "floating";
  isMobile?: boolean;
}) {
  const { t } = useLocale();
  const { isPinned, pin, isDragSessionOpen } = useTreePanel();
  const mobile = !!isMobile;
  const panelExpanded = isPinned || isDragSessionOpen;

  void context;

  // ── floating: squircle icon. Auto-hides when panel expands. ─────────

  // Light `var(--card)` fill — paper-white Apple-style tile in light
  // mode, gently-elevated dark surface in dark mode. Brighter than the
  // earlier 8%-foreground tint (user: "too dark, light is better").
  // Curve `rounded-surface` (39% radius on 36px) is the memax chrome-
  // squircle standard; other chrome surfaces should unify to this.
  const squircleStyle = {
    background: "var(--card)",
    boxShadow:
      "inset 0 1px 0 oklch(1 0 0 / 0.08), 0 1px 3px oklch(0 0 0 / 0.08)",
  } as const;

  const squircleClass =
    "relative flex h-10 w-10 items-center justify-center rounded-chrome cursor-pointer transition-transform active:scale-[0.96]";

  const glyph = (
    <span
      aria-hidden
      className="h-5 w-5"
      style={{
        backgroundColor: "var(--foreground)",
        opacity: 0.85,
        maskImage: "url(/images/memax-icon.svg)",
        maskSize: "contain",
        maskRepeat: "no-repeat",
        maskPosition: "center",
        WebkitMaskImage: "url(/images/memax-icon.svg)",
        WebkitMaskSize: "contain",
        WebkitMaskRepeat: "no-repeat",
        WebkitMaskPosition: "center",
      }}
    />
  );

  // Outer wrapper handles the panel-takeover fade (opacity + pointer-
  // events). When the panel expands the floating brand gracefully fades
  // out so the panel's own fade-in doesn't overlap with a still-visible
  // floating tile underneath.
  const takeoverStyle = {
    opacity: panelExpanded && !mobile ? 0 : 1,
    pointerEvents: (panelExpanded && !mobile
      ? "none"
      : "auto") as React.CSSProperties["pointerEvents"],
    transition: `opacity ${FAST}s ${EASE}`,
  } as const;

  // Mobile OR desktop with panel already expanded (shouldn't happen in
  // practice — floating is hidden — but render path is safe): plain /home
  // link, no chevron cue.
  if (mobile) {
    return (
      <Link
        href="/home"
        aria-label={t.topics.brandHome}
        className={squircleClass}
        style={{ ...squircleStyle, ...takeoverStyle }}
      >
        {glyph}
      </Link>
    );
  }

  // Desktop closed: button that opens the panel. Group-hover cross-fade
  // memax glyph ↔ chevrons cue.
  return (
    <button
      type="button"
      onClick={pin}
      aria-label={t.topics.treeOpen}
      title={t.topics.treeOpen}
      className={`group ${squircleClass}`}
      style={{ ...squircleStyle, ...takeoverStyle }}
      aria-hidden={panelExpanded}
      tabIndex={panelExpanded ? -1 : 0}
    >
      <span className="absolute inset-0 flex items-center justify-center transition-opacity duration-150 group-hover:opacity-0">
        {glyph}
      </span>
      <span
        className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity duration-150 group-hover:opacity-100"
        aria-hidden
      >
        <ChevronsRight
          className="h-4.5 w-4.5"
          strokeWidth={1.8}
          style={{ color: "var(--foreground)", opacity: 0.85 }}
        />
      </span>
    </button>
  );
}
