"use client";

// ═══════════════════════════════════════════════
// TopicChip — the universal topic-reference chip.
//
// One component. Three surfaces (all rendered by the SAME primitive):
//   1. Memory row breadcrumb (collapsed "Parent › Leaf" path)
//   2. Memory detail dream-history from/to (single topic)
//   3. Hover card from/to (single topic)
//
// State is pure color + one prefix glyph:
//   neutral → bg-surface-1 / text-fg-3
//   dream   → bg-signature/10 / text-signature + leading ✦
//
// Topic identity is ALWAYS preserved — the ✦ in dream state is added
// BEFORE the topic icon, never replacing it. The caller decides when
// to pass dream tone (e.g. memory.lifecycle.pending_dream_action).
//
// Props are a discriminated union so breadcrumb-mode vs single-mode
// are mutually exclusive at compile time, plus a dev-only runtime
// assertion for JS callers that escape the union via dynamic spread.
//
// This primitive lives in @memaxlabs/ui so both production surfaces
// (packages/web) AND the kitchen design-review deck (packages/ui/app/
// dev/kitchen) render EXACTLY the same component. No mock chip — if
// the kitchen demo looks right, the shipped UI looks right.
//
// Breadcrumb mode expects already-collapsed input: `visibleSegments`
// + `ellipsis` — the caller applies its own collapsing policy (see
// packages/web/src/lib/topic-label.ts for the web adapter). Keeping
// the collapsing policy outside the primitive avoids coupling the
// design system to web-specific data shapes.
//
// Topic cards DO NOT use this component. They carry their own
// DeltaChip (aggregate signal, lighter chrome). This component is
// strictly for "this chip refers to a topic entity."
// ═══════════════════════════════════════════════

import type { ReactNode } from "react";
import { ChevronRight, CircleOff } from "lucide-react";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "./hover-card";
import { TopicIcon } from "./topic-icon";

export type TopicChipTone = "neutral" | "dream";

/**
 * Intent styling for neutral-tone chips. Dream tone overrides these.
 * Kept as a string union in the design system so web can map its
 * domain-specific intent vocabulary to these tokens.
 */
export type TopicChipIntent = "placed" | "proposed" | "contested" | "archived";

/**
 * A single segment in a breadcrumb path. Must be pre-collapsed by the
 * caller — this primitive renders whatever segments it's given and
 * optionally prepends an ellipsis when `ellipsis` is true.
 */
export interface TopicChipSegment {
  id?: string;
  name: string;
  /** Lucide icon name — resolved via TopicIcon. Only the last segment's
   *  icon is rendered as the leading chip glyph. */
  icon?: string;
}

interface TopicChipSharedProps {
  tone?: TopicChipTone;
  intent?: TopicChipIntent;
  onClick?: () => void;
  /** When non-null, chip mounts as a HoverCardTrigger showing this
   *  content on hover/focus. NON-modal — no backdrop, no focus trap,
   *  click-through navigation intact. Caller is responsible for gating
   *  mobile-off; mobile should pass `undefined`. */
  popover?: ReactNode;
  title?: string;
  className?: string;
}

interface TopicChipBreadcrumbProps extends TopicChipSharedProps {
  /** Pre-collapsed visible segments. Typically 1-2 entries. */
  visibleSegments: TopicChipSegment[];
  /** When true, render a leading "…" to signal that segments were
   *  collapsed from a deeper path. */
  ellipsis?: boolean;
  label?: never;
  icon?: never;
  kind?: never;
}

interface TopicChipSingleProps extends TopicChipSharedProps {
  kind?: "topic" | "unassigned";
  label: string;
  /** Lucide icon name string. Ignored when kind="unassigned" (which
   *  renders CircleOff unconditionally). */
  icon?: string;
  visibleSegments?: never;
  ellipsis?: never;
}

export type TopicChipProps = TopicChipBreadcrumbProps | TopicChipSingleProps;

export function TopicChip(props: TopicChipProps) {
  if (process.env.NODE_ENV !== "production") {
    const hasSegments =
      "visibleSegments" in props && props.visibleSegments != null;
    const hasLabel = "label" in props && props.label != null;
    if (hasSegments && hasLabel) {
      // eslint-disable-next-line no-console
      console.error(
        "TopicChip: `visibleSegments` and `label` are mutually exclusive; pass exactly one.",
      );
    }
    if (!hasSegments && !hasLabel) {
      // eslint-disable-next-line no-console
      console.error(
        "TopicChip: one of `visibleSegments` or `label` is required.",
      );
    }
  }

  const {
    tone = "neutral",
    intent,
    onClick,
    popover,
    title,
    className,
  } = props;
  const isDream = tone === "dream";

  const intentTextClass = isDream
    ? ""
    : intent === "contested"
      ? "text-fg-2"
      : intent === "archived"
        ? "text-fg-4 line-through"
        : "text-fg-3";

  const toneBgClass = isDream ? "font-medium" : "bg-surface-1";
  const rootClass = `inline-flex max-w-50 shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] ${toneBgClass} ${intentTextClass} ${className ?? ""}`;
  const rootStyle: React.CSSProperties = isDream
    ? {
        background: "oklch(from var(--signature) l c h / 0.1)",
        color: "var(--signature)",
        lineHeight: "1.25",
      }
    : { lineHeight: "1.25" };

  const chipContent = (
    <>
      {isDream && (
        <span className="text-[10px] leading-none shrink-0" aria-hidden>
          ✦
        </span>
      )}
      {renderIdentity(props)}
    </>
  );

  const resolvedTitle =
    title ??
    ("visibleSegments" in props && props.visibleSegments
      ? props.visibleSegments.map((s) => s.name).join(" / ")
      : "label" in props
        ? props.label
        : undefined);

  const chipNode = onClick ? (
    <button
      type="button"
      className={`${rootClass} cursor-pointer transition-colors hover:bg-surface-2 focus-visible:bg-surface-2 outline-none`}
      style={rootStyle}
      title={resolvedTitle}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
    >
      {chipContent}
    </button>
  ) : (
    <span className={rootClass} style={rootStyle} title={resolvedTitle}>
      {chipContent}
    </span>
  );

  if (popover) {
    return (
      <HoverCard>
        <HoverCardTrigger render={chipNode as React.ReactElement} />
        <HoverCardContent side="top" align="start" sideOffset={6}>
          {popover}
        </HoverCardContent>
      </HoverCard>
    );
  }

  return chipNode;
}

function renderIdentity(props: TopicChipProps): ReactNode {
  if ("visibleSegments" in props && props.visibleSegments) {
    const { visibleSegments: visible, ellipsis } = props;
    const leafIcon = visible[visible.length - 1]?.icon;
    return (
      <>
        {leafIcon && (
          <TopicIcon
            name={leafIcon}
            className="h-2.5 w-2.5 shrink-0 text-fg-4"
          />
        )}
        {ellipsis && <span className="text-fg-4">…</span>}
        {visible.map((segment, index) => (
          <span
            key={`${segment.id ?? segment.name}-${index}`}
            className="flex min-w-0 items-center gap-1 truncate"
          >
            {index > 0 && (
              <ChevronRight className="h-2.5 w-2.5 shrink-0 text-fg-4" />
            )}
            <span className="truncate">{segment.name}</span>
          </span>
        ))}
      </>
    );
  }

  if ("label" in props) {
    const { label, icon, kind = "topic" } = props;
    return (
      <>
        {kind === "unassigned" ? (
          <CircleOff className="h-2.5 w-2.5 shrink-0 text-fg-4" />
        ) : icon ? (
          <TopicIcon name={icon} className="h-2.5 w-2.5 shrink-0 text-fg-4" />
        ) : null}
        <span className="truncate">{label}</span>
      </>
    );
  }

  return null;
}
