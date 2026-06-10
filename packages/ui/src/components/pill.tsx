"use client";

import * as React from "react";
import { ChevronDown, Plus, X } from "lucide-react";
import { cn } from "../utils";

/**
 * Pill — the canonical chip primitive for memax.
 *
 * One visual language, four semantic variants, three sizes.
 * Replaces the previously-divergent hub-identity-chip, topic-pills,
 * memory-row hub label, and ui/tag inline styles.
 *
 * Variants (what the pill is FOR):
 *   - select  — represents a selected value, click opens a menu. Auto-chevron.
 *   - remove  — shows a value with an inline × that clears it.
 *   - add     — empty-state dashed pill, click to assign a value. Auto-plus.
 *   - static  — read-only label. No interaction.
 *
 * Sizes (pick by the weight of surrounding text):
 *   - lg (h-9, text-[14px]) — comfortable. Matches 14px list/content weight.
 *                             Use when the pill is a primary element next to
 *                             14px text (e.g. top-bar hub switcher trigger
 *                             that sits beside its own dropdown list items).
 *   - md (h-7, text-[12px]) — default chip. Topic selectors, inline chips,
 *                             most content-page pills.
 *   - sm (h-5, text-[11px]) — ultra-dense metadata. Memory row labels.
 *
 * Icons auto-size to the pill (via `[&_svg:not([class*='size-'])]:size-X` on
 * each size class — same pattern as ui/button.tsx). Pass `icon={<Lightbulb />}`
 * without a size class and it renders at the correct size for the pill.
 *
 * Composition — use `pillClass()` when a wrapper (PopoverTrigger, Link, etc.)
 * needs to adopt pill styling directly instead of nesting a <Pill>:
 *
 *   <PopoverTrigger className={pillClass({ variant: "select" })}>
 *     <TopicIcon />
 *     <span className="truncate">{topic.name}</span>
 *     <ChevronDown className="shrink-0 text-fg-4" />
 *   </PopoverTrigger>
 *
 * Live demos + migration map: kitchen section 19, "Pill — canonical chip".
 */

export type PillVariant = "select" | "remove" | "add" | "static";
export type PillSize = "sm" | "md" | "lg";

const BASE =
  "inline-flex items-center rounded-lg transition-colors whitespace-nowrap max-w-full outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 [&_svg]:shrink-0 [&_svg]:pointer-events-none";

const SIZE: Record<PillSize, string> = {
  sm: "h-5 gap-1 px-1.5 text-[11px] [&_svg:not([class*='size-'])]:size-3",
  md: "h-7 gap-1.5 px-2.5 text-[12px] [&_svg:not([class*='size-'])]:size-3.5",
  lg: "h-9 gap-2 px-3 text-[14px] [&_svg:not([class*='size-'])]:size-4",
};

const VARIANT: Record<PillVariant, string> = {
  select:
    "border border-border/60 bg-surface-1 text-fg-2 cursor-pointer hover:bg-surface-2 hover:text-fg-1 hover:border-border",
  remove: "border border-border/60 bg-surface-1 text-fg-2",
  add: "border border-dashed border-border/70 bg-transparent text-fg-3 cursor-pointer hover:text-fg-2 hover:border-border",
  static: "border border-transparent bg-surface-1 text-fg-3",
};

const DISABLED = "opacity-50 cursor-not-allowed pointer-events-none";

export interface PillClassOptions {
  variant?: PillVariant;
  size?: PillSize;
  disabled?: boolean;
  className?: string;
}

export function pillClass(opts: PillClassOptions = {}): string {
  const { variant = "static", size = "md", disabled, className } = opts;
  return cn(
    BASE,
    SIZE[size],
    VARIANT[variant],
    disabled && DISABLED,
    className,
  );
}

export interface PillProps {
  variant?: PillVariant;
  size?: PillSize;
  /** Leading slot — icon, avatar, or color dot. Sized automatically to match. */
  icon?: React.ReactNode;
  /** Click handler for select/add variants. */
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  /** Clear handler for remove variant — renders the inline × button. */
  onRemove?: () => void;
  /** aria-label for the × button (remove variant). Defaults to "Remove". */
  removeLabel?: string;
  disabled?: boolean;
  className?: string;
  title?: string;
  "aria-label"?: string;
  children: React.ReactNode;
}

export function Pill({
  variant = "static",
  size = "md",
  icon,
  onClick,
  onRemove,
  removeLabel = "Remove",
  disabled,
  className,
  title,
  children,
  ...aria
}: PillProps) {
  const leading =
    variant === "add" && !icon ? (
      <Plus aria-hidden />
    ) : icon ? (
      <span
        className="inline-flex shrink-0 items-center justify-center"
        aria-hidden
      >
        {icon}
      </span>
    ) : null;

  // min-w-0 is required for truncate to take effect inside a flex container.
  const label = <span className="min-w-0 truncate">{children}</span>;

  if (variant === "remove") {
    return (
      <span
        className={pillClass({ variant, size, className })}
        title={title}
        {...aria}
      >
        {leading}
        {label}
        {onRemove && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            className="shrink-0 -mr-0.5 rounded-full p-0.5 text-fg-4 transition-colors hover:text-fg-2 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            aria-label={removeLabel}
          >
            <X />
          </button>
        )}
      </span>
    );
  }

  if (variant === "static") {
    return (
      <span
        className={pillClass({ variant, size, className })}
        title={title}
        {...aria}
      >
        {leading}
        {label}
      </span>
    );
  }

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      title={title}
      className={pillClass({ variant, size, disabled, className })}
      {...aria}
    >
      {leading}
      {label}
      {variant === "select" && (
        <ChevronDown className="text-fg-4" aria-hidden />
      )}
    </button>
  );
}
