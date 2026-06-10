"use client";

import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export type SectionHeaderVariant = "card" | "plain";

interface SectionHeaderProps {
  /** Optional leading icon — small (h-3.5 w-3.5) and tertiary. */
  icon?: LucideIcon;
  /** Section title. Sentence case, never uppercase. */
  label?: string;
  /** Trailing slot for counts, toggles, menus, or actions. Right-aligned. */
  trailing?: ReactNode;
  /**
   * Frame context:
   * - "card": sits inside DataSectionCard chrome → own bottom divider,
   *    inner px-4 py-3 padding.
   * - "plain": sits on bare --background for borderless content lists →
   *    no divider, tighter px-1 so the label aligns with row leading edge
   *    at the content's own horizontal rhythm.
   * Defaults to "card" for back-compat.
   */
  variant?: SectionHeaderVariant;
}

/**
 * SectionHeader — the single header row used by every labeled panel
 * (DataSectionCard inline cards + Inbox glass popover + borderless
 * content lists). Keeps [icon] label [flex-1] [trailing] consistent
 * across every surface so two panels rendered side-by-side always feel
 * like one family.
 *
 * Outer chrome (rounded / border / background / blur) stays with the
 * caller because that is a genuine architectural difference — inline
 * solid cards vs glass popovers vs borderless lists — not duplication.
 */
export function SectionHeader({
  icon: Icon,
  label,
  trailing,
  variant = "card",
}: SectionHeaderProps) {
  // Plain headers sit on bare --background above borderless lists — the
  // header/first-row rhythm is owned by a bottom margin on the header
  // itself (mb-2) so skeleton and loaded states share the same geometry
  // and there is no visible shift at the skeleton→loaded transition.
  // Card variant keeps its own bottom divider and needs no external gap.
  const framing =
    variant === "plain" ? "px-1 mb-2" : "border-b border-border/30 px-4 py-3";
  return (
    <div className={`flex items-center gap-2 ${framing}`}>
      {Icon && <Icon className="h-3.5 w-3.5 shrink-0 text-fg-3" />}
      {label && (
        <span className="text-[14px] font-semibold text-fg-2">{label}</span>
      )}
      <div className="flex-1" />
      {trailing}
    </div>
  );
}
