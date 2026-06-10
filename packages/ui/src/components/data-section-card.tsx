"use client";

import type { LucideIcon } from "lucide-react";
import { ContentError } from "./content-error";
import { SectionHeader } from "./section-header";
import { Skeleton } from "./skeleton";

export type CardPhase =
  | "loading"
  | "loaded"
  | "empty"
  | "filtered-empty"
  | "error";

export type DataSectionVariant = "card" | "plain" | "glass";

export interface ErrorCopy {
  title: string;
  detail?: string;
  retryLabel: string;
  onRetry: () => void;
}

export interface EmptyCopy {
  title: string;
  hint?: string;
}

export interface FilteredEmptyCopy extends EmptyCopy {
  clearLabel: string;
  onClear: () => void;
}

export interface DataSectionCardProps {
  icon?: LucideIcon;
  label?: string;
  trailing?: React.ReactNode;
  phase: CardPhase;
  isPlaceholderData?: boolean;
  skeleton?: React.ReactNode;
  errorCopy?: ErrorCopy;
  emptyCopy?: EmptyCopy;
  filteredEmptyCopy?: FilteredEmptyCopy;
  children?: React.ReactNode;
  /**
   * Outer frame:
   * - "card" (default): rounded-surface border bg-card outer chrome; SectionHeader
   *    renders with its bottom divider + inner padding. Form-style panels.
   * - "plain": no outer wrapper; SectionHeader renders in plain form on
   *    --background; error/empty/skeleton states render without card outline.
   *    For borderless content lists (Recent feed, topic memory list, recall).
   * - "glass": rounded-surface chrome-family glass (`.glass-card` — backdrop
   *    blur + subtle rim + no drop shadow). Narrowly scoped to the memory
   *    detail page; shares its recipe with the top-right chrome capsule and
   *    hub transition pill. Header behaves like "card" (divider + padding).
   *
   * Row content inside `children` is unchanged across variants — this prop
   * only toggles the frame.
   */
  variant?: DataSectionVariant;
}

export function DataCardSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div>
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className={`px-4 py-2.5 space-y-1.5 ${i > 0 ? "border-t border-border/30" : ""}`}
        >
          <div className="flex items-center gap-1.5">
            <Skeleton className="h-3 w-3 shrink-0" />
            <Skeleton
              className="h-3"
              style={{ width: `${28 + ((i * 13) % 14)}%` }}
            />
            <Skeleton className="ml-auto h-3 w-8" />
          </div>
          <Skeleton
            className="h-3.5"
            style={{ width: `${62 + ((i * 11) % 24)}%` }}
          />
          <Skeleton
            className="h-3"
            style={{ width: `${74 + ((i * 7) % 18)}%` }}
          />
        </div>
      ))}
    </div>
  );
}

export function DataSectionCard({
  icon: Icon,
  label,
  trailing,
  phase,
  isPlaceholderData = false,
  skeleton,
  errorCopy,
  emptyCopy,
  filteredEmptyCopy,
  children,
  variant = "card",
}: DataSectionCardProps) {
  const bodyOpacity = isPlaceholderData ? 0.72 : 1;
  const isPlain = variant === "plain";
  const isGlass = variant === "glass";

  // Plain variant: bare --background, no outer chrome, SectionHeader in
  // plain form, empty/filtered-empty states render with px-1 alignment so
  // they line up with row leading-edge content. Glass variant skips the
  // inline bg/border style (the .glass-card class provides both) and
  // otherwise mirrors the card frame behaviour.
  const wrapperClass = isPlain
    ? ""
    : isGlass
      ? "glass-card overflow-hidden rounded-surface"
      : "overflow-hidden rounded-surface";
  const wrapperStyle: React.CSSProperties | undefined =
    isPlain || isGlass
      ? undefined
      : { border: "1px solid var(--border)", background: "var(--card)" };

  // Empty/filtered-empty padding mirrors the variant's header padding so
  // copy aligns with the label vertically on its left edge.
  const statePadding = isPlain ? "px-1 py-3" : "px-4 py-4";

  // SectionHeader supports only "card" | "plain"; glass uses the card
  // header treatment (divider + padding) since its content behaves like
  // a framed card.
  const headerVariant = isGlass ? "card" : variant;

  return (
    <div className={wrapperClass} style={wrapperStyle}>
      <SectionHeader
        icon={Icon}
        label={label}
        trailing={trailing}
        variant={headerVariant}
      />

      <div
        style={{
          opacity: bodyOpacity,
          transition: "opacity 200ms cubic-bezier(0.16, 1, 0.3, 1)",
        }}
      >
        {phase === "loading" && (skeleton ?? <DataCardSkeleton />)}

        {phase === "loaded" && <div>{children}</div>}

        {phase === "empty" && emptyCopy && (
          <div className={statePadding}>
            <p className="text-[14px] text-fg-2">{emptyCopy.title}</p>
            {emptyCopy.hint && (
              <p className="mt-1 text-[12px] text-fg-3">{emptyCopy.hint}</p>
            )}
          </div>
        )}

        {phase === "filtered-empty" && filteredEmptyCopy && (
          <div className={statePadding}>
            <p className="text-[14px] text-fg-2">{filteredEmptyCopy.title}</p>
            {filteredEmptyCopy.hint && (
              <p className="mt-1 text-[12px] text-fg-3">
                {filteredEmptyCopy.hint}
              </p>
            )}
            <button
              onClick={filteredEmptyCopy.onClear}
              className="mt-3 cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              {filteredEmptyCopy.clearLabel}
            </button>
          </div>
        )}

        {phase === "error" && errorCopy && (
          <ContentError
            plain
            message={errorCopy.title}
            detail={errorCopy.detail}
            retryLabel={errorCopy.retryLabel}
            onRetry={errorCopy.onRetry}
          />
        )}
      </div>
    </div>
  );
}
