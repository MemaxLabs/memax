"use client";

/**
 * Unified MemoryRow — single component, surface-driven rendering.
 *
 * Kitchen spec: section 01 (Topics).
 * Replaces: FreshMemoryRow (topic-grid), TopicMemoryRow (topic-detail), MemoryRow (memory-view).
 *
 * Attribution tiers (indicator slot):
 *   1. Agent capture (source_agent) → Bot icon + agent name
 *   2. Team human (author_name + team hub) → Avatar + name
 *   3. Personal default → Content-type icon or colored dot
 *
 * Surface presets control what's shown:
 *   recent  → attribution + summary + hub label + copy
 *   inbox   → minimal: title + trailing actor identity (non-self only) + age
 *   topic   → summary + trailing actor identity (non-self only), no source/count
 *   list    → source + count + summary
 *   recall  → summary + hub label
 */

import {
  useState,
  useCallback,
  useEffect,
  useRef,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
  Copy,
  Check,
  GripVertical,
} from "lucide-react";
import { MemaxStar } from "@/components/features/memax-star";
import { AgentInlineIdentity } from "@/components/features/agent-inline-identity";
import { useAuth } from "@/lib/auth";
import { useSelection } from "@/contexts/selection-context";
import { formatAge } from "@/lib/format-age";
import { formatMemoryMarkdown } from "@/lib/format-context";
import { sanitizeSummary, sanitizeTitle } from "@/lib/sanitize-metadata";
import {
  linkedAgentAttributionCopy,
  standaloneAgentAttributionText,
} from "@/lib/memory-attribution-copy";
import { HighlightText, MarkdownSnippet, Pill } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { queryClient } from "@/lib/query-client";
import { isTextSelectionActiveIn } from "@/lib/text-selection";
import { getMemoryQueryOptions, type Memory } from "@/hooks/use-memories";
import {
  resolveMemoryRowHubType,
  resolveMemoryRowPresentation,
  type MemoryRowSurface,
} from "@/lib/memory-row-presentation";
import type { TopicLabelData } from "@/lib/topic-label";
import { TopicChip } from "@/components/features/topic/topic-chip";
import { DreamActionPopoverBody } from "@/components/features/dream-action-popover-body";
import { MemoryCardLayout } from "@/components/features/memory-card/memory-card-layout";
import type { DreamActionRef } from "memax-sdk";

interface MemoryRowProps {
  memory: Memory;
  surface: MemoryRowSurface;
  topicLabel?: TopicLabelData;
  showDivider?: boolean;
  onClick?: () => void;
  isNew?: boolean;
  // Search support (list surface)
  searchHighlight?: string;
  dimmed?: boolean; // opacity-25 when search doesn't match
  // Drag support (topic surface)
  dragRef?: (node: HTMLElement | null) => void;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  dragAttributes?: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  dragListeners?: any;
  isDragging?: boolean;
  /**
   * Card preset only: the active-hub id. When the memory's `hub_id`
   * differs, the card surfaces a cross-hub label. Row surfaces ignore
   * this prop. Forwards to `<MemoryCardLayout>` directly when
   * `surface === "card"`.
   */
  currentHubId?: string;
}

const IDENTITY_SIZE = {
  sm: {
    shell: "h-3.5 w-3.5 text-[10px]",
    icon: "h-3.5 w-3.5",
  },
  md: {
    shell: "h-5 w-5 text-[10px]",
    icon: "h-5 w-5",
  },
} as const;

const TRAILING_ACTOR_AVATAR =
  "h-[18px] w-[18px] rounded-full text-[9px] shrink-0";

function isRichContentType(contentType: string) {
  return (
    contentType === "pdf" || contentType === "image" || contentType === "link"
  );
}

function renderContentTypeIcon(
  contentType: string,
  size: keyof typeof IDENTITY_SIZE,
) {
  if (!isRichContentType(contentType)) return null;
  const className = `${IDENTITY_SIZE[size].icon} shrink-0 text-fg-3`;
  if (contentType === "image") return <ImageIcon className={className} />;
  if (contentType === "link") return <LinkIcon className={className} />;
  return <FileText className={className} />;
}

// TopicLabelChip (row surface adapter) — thin wrapper over the shared
// TopicChip primitive. Keeps the existing row-level prop shape
// (TopicLabelData) while delegating all rendering + tone + interaction
// to TopicChip. When the row has a pending dream action and the viewer
// is on desktop, a DreamActionPopoverBody is threaded through as hover
// content. Mobile intentionally skips the popover — tap on a mobile
// tinted chip still navigates to the topic; the detail page's dream
// history is the mobile equivalent for "why did this move?".
function TopicLabelChip({
  topicLabel,
  tone = "neutral",
  dreamAction,
  isMobile,
}: {
  topicLabel: TopicLabelData;
  tone?: "neutral" | "dream";
  dreamAction?: DreamActionRef | null;
  isMobile?: boolean;
}) {
  const popover =
    !isMobile && dreamAction ? (
      <DreamActionPopoverBody action={dreamAction} />
    ) : undefined;
  return (
    <TopicChip
      segments={topicLabel.path}
      tone={tone}
      intent={topicLabel.intent}
      onClick={topicLabel.onClick}
      popover={popover}
    />
  );
}

export function MemoryRow({
  memory: m,
  surface,
  topicLabel,
  showDivider = false,
  onClick,
  isNew = false,
  searchHighlight,
  dimmed = false,
  dragRef,
  dragAttributes,
  dragListeners,
  isDragging = false,
  currentHubId,
}: MemoryRowProps) {
  // Card preset short-circuits to a dedicated column-layout component
  // (see `memory-card-layout.tsx`). The branch sits below the JSX prep
  // — all hook calls run unconditionally to satisfy Rules-of-Hooks
  // even when the same component instance flips surface modes.
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const isMobile = useIsMobile();
  const { hubs, user } = useAuth();
  const selection = useSelection();
  const [rowCopied, setRowCopied] = useState(false);
  const [isResolvingIndicator, setIsResolvingIndicator] = useState(false);
  const [isExitingSelected, setIsExitingSelected] = useState(false);
  const isSelected = selection.active && selection.selected.has(m.id);
  const previousState = useRef(m.state);
  const previousSelectionActive = useRef(selection.active);
  const previousSelected = useRef(isSelected);
  const prefetchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Defense-in-depth: sanitize title in case pre-validation bad data remains
  // If sanitizeTitle rejects the title (returns ""), show a localized fallback
  // — do NOT fall back to the original bad title, which defeats the guard.
  const cleanTitle =
    sanitizeTitle(m.title) || (m.title ? t.toast.untitled : "");

  // Hub info — for hub label in right meta
  const hubInfo = hubs.find((h) => h.hub.id === m.hub_id);
  const hubLabel = m.hub_name || hubInfo?.hub.name;
  const isTeamHub =
    resolveMemoryRowHubType(m, hubInfo?.hub.hub_type) === "team";
  const copyRow = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      navigator.clipboard.writeText(formatMemoryMarkdown(m)).then(() => {
        setRowCopied(true);
        setTimeout(() => setRowCopied(false), 2000);
      });
    },
    [m],
  );
  const schedulePrefetch = useCallback(() => {
    if (isMobile) return;
    if (prefetchTimerRef.current) return;
    prefetchTimerRef.current = setTimeout(() => {
      prefetchTimerRef.current = null;
      void queryClient.prefetchQuery(getMemoryQueryOptions(m.id));
    }, 80);
  }, [isMobile, m.id]);
  const cancelPrefetch = useCallback(() => {
    if (!prefetchTimerRef.current) return;
    clearTimeout(prefetchTimerRef.current);
    prefetchTimerRef.current = null;
  }, []);

  useEffect(() => cancelPrefetch, [cancelPrefetch]);

  // Surface-specific visibility
  const showSource = false; // Removed — source_agent attribution replaces transport label
  const showCount = false;
  // Mobile fresh row groups the topic pin against the title (reading order:
  // title → where it landed → who → when). Other non-stacked surfaces
  // (topic / inbox / recall) keep the historical order where the topic pin
  // sits right before time. Kept surface-local rather than in the
  // presentation resolver because this is pure DOM ordering, not a
  // visibility contract.
  const isMobileCompactRecent = isMobile && surface === "recent";
  const presentation = resolveMemoryRowPresentation(
    m,
    {
      surface,
      isMobile,
      currentUserId: user?.id,
      isTeamHub,
    },
    { hasTopicLabel: !!topicLabel },
  );
  const {
    attribution,
    isProcessing,
    hasTeamAuthor,
    showSummary,
    showCopy,
    showHubLabel,
    showInlineContext,
    useStackedRecent,
    showTrailingContentMeta,
    leadingIdentity,
    trailingActor,
  } = presentation;
  const agentDisplayName = attribution.agentDisplayName;
  const AgentIcon = attribution.agentIdentity?.icon;
  const linkedCopy = linkedAgentAttributionCopy(attribution, t);
  const selectionSpring =
    "all 0.25s var(--ease-spring, cubic-bezier(0.34, 1.56, 0.64, 1))";

  const teamAgentLabel =
    hasTeamAuthor && attribution.hasAgent
      ? standaloneAgentAttributionText(attribution, t, interpolate, "regular")
      : null;
  const attributionLabel = hasTeamAuthor
    ? isMobile
      ? t.attribution.pushed
      : m.author_name
        ? attribution.hasAgent
          ? {
              author: m.author_name,
              kind: attribution.isHumanRequestedAgent
                ? ("saved_with_agent" as const)
                : ("captured_by_agent" as const),
            }
          : `${m.author_name} ${t.attribution.pushed}`
        : teamAgentLabel || t.attribution.pushed
    : attribution.hasAgent
      ? !isMobile
        ? standaloneAgentAttributionText(attribution, t, interpolate, "regular")
        : standaloneAgentAttributionText(attribution, t, interpolate, "compact")
      : attribution.isOwnMemory && !attribution.hasAgent
        ? isMobile
          ? t.attribution.you
          : t.attribution.youPushed
        : null;
  const inlineAttributionLabel = hasTeamAuthor
    ? !isMobile && m.author_name
      ? attribution.hasAgent
        ? {
            author: m.author_name,
            kind: attribution.isHumanRequestedAgent
              ? ("saved_with_agent" as const)
              : ("captured_by_agent" as const),
          }
        : `${m.author_name} ${t.attribution.pushed}`
      : teamAgentLabel || t.attribution.pushed
    : attribution.hasAgent
      ? !isMobile
        ? standaloneAgentAttributionText(attribution, t, interpolate, "regular")
        : standaloneAgentAttributionText(attribution, t, interpolate, "compact")
      : null;
  const identitySize = surface === "inbox" ? "sm" : "md";
  const contentTypeMeta = isRichContentType(m.content_type)
    ? renderContentTypeIcon(m.content_type, "sm")
    : null;
  const trailingContentMeta = showTrailingContentMeta ? contentTypeMeta : null;
  const renderAuthorAvatar = (className: string) =>
    m.author_avatar_url ? (
      <img
        src={m.author_avatar_url}
        alt=""
        className={className}
        title={m.author_name ?? undefined}
      />
    ) : (
      <div
        className={`${className} flex items-center justify-center font-medium text-foreground/55`}
        style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
        title={m.author_name ?? undefined}
      >
        {m.author_name?.[0]?.toUpperCase()}
      </div>
    );
  const renderAgentIdentity = (
    containerClass: string,
    iconClass: string,
    title?: string,
  ) =>
    attribution.agentIconEmoji ? (
      <span
        className={`${containerClass} flex items-center justify-center leading-none`}
        title={title}
      >
        {attribution.agentIconEmoji}
      </span>
    ) : AgentIcon ? (
      <span
        className={`${containerClass} flex items-center justify-center`}
        title={title}
      >
        <AgentIcon
          className={iconClass}
          style={{ color: attribution.agentIdentity?.color }}
          strokeWidth={1.8}
        />
      </span>
    ) : (
      <div
        className={`${containerClass} rounded-lg flex items-center justify-center font-medium text-fg-2`}
        style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
        title={title}
      >
        {agentDisplayName[0]?.toUpperCase() ?? "A"}
      </div>
    );
  const renderedTrailingActor =
    trailingActor === "author"
      ? renderAuthorAvatar(TRAILING_ACTOR_AVATAR)
      : trailingActor === "agent"
        ? renderAgentIdentity(
            TRAILING_ACTOR_AVATAR,
            "h-[18px] w-[18px] shrink-0",
            agentDisplayName,
          )
        : null;
  const renderAttributionText = (
    value:
      | string
      | {
          author: string;
          kind: "saved_with_agent" | "captured_by_agent";
        }
      | null,
  ) => {
    if (!value) return null;
    if (typeof value === "string") {
      return value;
    }
    return (
      <>
        <span>{value.author}</span>
        <span className="text-fg-4">·</span>
        <span>{linkedCopy.prefix}</span>
        <AgentInlineIdentity attribution={attribution} />
        {linkedCopy.suffix ? <span>{linkedCopy.suffix}</span> : null}
      </>
    );
  };

  const renderLeadingIdentity = () => {
    if (leadingIdentity === "processing") {
      return (
        <span className="flex h-5 w-5 shrink-0 items-center justify-center">
          <MemaxStar
            className="text-[12px] state-slow-breathe"
            style={{ color: "var(--signature)" }}
          />
        </span>
      );
    }
    if (leadingIdentity === "author") {
      return renderAuthorAvatar(
        `${IDENTITY_SIZE[identitySize].icon} rounded-full shrink-0`,
      );
    }
    if (leadingIdentity === "agent") {
      return renderAgentIdentity(
        `${IDENTITY_SIZE[identitySize].shell} shrink-0`,
        `${IDENTITY_SIZE[identitySize].icon} shrink-0`,
      );
    }
    if (leadingIdentity === "none") {
      return null;
    }
    return renderContentTypeIcon(m.content_type, identitySize);
  };

  const baseIndicator = (
    <span className="flex h-5 w-5 shrink-0 items-center justify-center">
      {renderLeadingIdentity()}
    </span>
  );

  const showSelectionShell = selection.active || isExitingSelected;
  const showSelectedMarker = isSelected || isExitingSelected;
  const shouldReserveIndicatorSlot =
    leadingIdentity !== "none" || showSelectionShell || isResolvingIndicator;

  const indicator = (
    <span
      className="relative flex h-5 w-5 shrink-0 items-center justify-center"
      style={{ transition: selectionSpring }}
    >
      <span
        className="absolute inset-0 flex items-center justify-center"
        style={{
          opacity: showSelectionShell ? 0 : 1,
          transform: showSelectionShell ? "scale(0.72)" : "scale(1)",
          transition: selectionSpring,
        }}
      >
        {baseIndicator}
      </span>
      <span
        className="absolute inset-0 flex items-center justify-center"
        style={{
          opacity: showSelectionShell ? 1 : 0,
          transform: showSelectionShell ? "scale(1)" : "scale(0.72)",
          transition: selectionSpring,
        }}
      >
        <span className="relative flex h-5 w-5 items-center justify-center">
          <span
            className="absolute inset-0 flex items-center justify-center"
            style={{
              opacity: showSelectedMarker ? 0 : 1,
              transform: showSelectedMarker ? "scale(0.72)" : "scale(1)",
              transition: selectionSpring,
            }}
          >
            <span
              className="h-2.5 w-2.5 rounded-full"
              style={{
                border: "1.5px solid var(--fg-3)",
                transition: selectionSpring,
              }}
            />
          </span>
          <span
            className="absolute inset-0 flex items-center justify-center"
            style={{
              opacity: showSelectedMarker ? 1 : 0,
              transform: showSelectedMarker ? "scale(1)" : "scale(0.72)",
              transition: selectionSpring,
            }}
          >
            <Check
              className="h-3 w-3"
              style={{ color: "var(--signature)" }}
              strokeWidth={2.5}
            />
          </span>
        </span>
      </span>
    </span>
  );

  useEffect(() => {
    const wasProcessing = previousState.current === "processing";
    if (wasProcessing && !isProcessing) {
      setIsResolvingIndicator(true);
      const timer = setTimeout(() => setIsResolvingIndicator(false), 320);
      previousState.current = m.state;
      return () => clearTimeout(timer);
    }
    previousState.current = m.state;
  }, [isProcessing, m.state]);

  useEffect(() => {
    if (
      previousSelectionActive.current &&
      previousSelected.current &&
      !selection.active
    ) {
      setIsExitingSelected(true);
      const timer = setTimeout(() => setIsExitingSelected(false), 220);
      previousSelectionActive.current = selection.active;
      previousSelected.current = isSelected;
      return () => clearTimeout(timer);
    }

    if (selection.active) {
      setIsExitingSelected(false);
    }

    previousSelectionActive.current = selection.active;
    previousSelected.current = isSelected;
  }, [isSelected, selection.active]);

  const rawIndicator =
    !shouldReserveIndicatorSlot ? null : !showSelectionShell &&
      isResolvingIndicator ? (
      <span className="state-indicator-morph">
        <span className="state-indicator-morph__glow" aria-hidden="true" />
        <span className="state-indicator-morph__star" aria-hidden="true">
          <MemaxStar
            className="text-[12px]"
            style={{ color: "var(--signature)" }}
          />
        </span>
        <span className="state-indicator-morph__icon" aria-hidden="true">
          {indicator}
        </span>
      </span>
    ) : (
      indicator
    );

  // Freshness halo — radial signature glow behind the indicator shell.
  // Pointer-events: none so it cannot intercept grip / row pointer
  // events (DnD + selection stay intact). State machine owned by
  // memory-row-presentation.ts via resolveHaloState (pure age-based,
  // no server state). When halo is "off", no extra DOM is rendered.
  const haloState = presentation.lifecycle.halo;
  // Freshness halo is a team-hub signal — in personal hubs every memory
  // is yours, so "who added this just now" isn't information worth
  // surfacing. Suppress in select mode too so the checkbox reads clean.
  const suppressHalo = showSelectionShell || !isTeamHub;
  const displayedIndicator =
    haloState === "off" || suppressHalo || !rawIndicator ? (
      rawIndicator
    ) : (
      <span className="relative inline-flex items-center justify-center">
        <span
          aria-hidden
          className={`absolute pointer-events-none rounded-full ${
            haloState === "breathing" ? "state-slow-breathe" : ""
          }`}
          style={{
            inset: -8,
            background:
              "radial-gradient(circle, oklch(from var(--signature) l c h / 0.22) 0%, transparent 68%)",
          }}
        />
        <span className="relative z-10 inline-flex">{rawIndicator}</span>
      </span>
    );

  // ── Context label — compact inline mode only (recall/list). ──
  const contextLabel = !showInlineContext ? null : hasTeamAuthor &&
    m.author_name ? (
    <span className="inline-flex shrink-0 items-center gap-1 text-[13px] text-foreground/40">
      {renderAttributionText(inlineAttributionLabel)}
    </span>
  ) : attribution.hasAgent ? (
    <span className="text-[13px] text-foreground/40 shrink-0">
      {renderAttributionText(inlineAttributionLabel)}
    </span>
  ) : null;

  // Long-press: 500ms hold enters selection mode with this memory.
  // Gesture ownership (Codex review):
  //   - Desktop → click navigates, drag handle (grip) on hover initiates
  //     drag-to-topic, Select button on the topic grid header enters
  //     selection mode. No long-press on desktop.
  //   - Mobile → long-press enters selection mode (there is no drag handle
  //     and no row-level Select affordance). No drag-to-tree on mobile
  //     (mobile tree is a bottom sheet; drag UX there is out of scope).
  // Running long-press on desktop would fight with pointer-level drag
  // activation on the grip AND with ordinary pointer interactions like
  // text selection, which is why it's mobile-only here.
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const longPressTriggered = useRef(false);

  const onPointerDown = useCallback(() => {
    if (!isMobile) return;
    longPressTriggered.current = false;
    longPressTimer.current = setTimeout(() => {
      longPressTriggered.current = true;
      if (!selection.active) {
        selection.enter(m.id);
      }
    }, 500);
  }, [isMobile, selection, m.id]);

  const cancelLongPress = useCallback(() => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  }, []);

  const handleClick = useCallback(
    (event: ReactMouseEvent<HTMLDivElement>) => {
      // If long-press fired, don't also trigger click
      if (longPressTriggered.current) {
        longPressTriggered.current = false;
        return;
      }
      // Suppress row activation when the click is the end of a drag-
      // select inside this row — users copying the title don't want to
      // get yanked to the detail page OR to have batch-select toggle
      // the row on the mouseup. The guard runs regardless of
      // selection-mode so both "navigate" and "toggle" paths respect
      // an in-flight text selection.
      if (isTextSelectionActiveIn(event.currentTarget)) {
        return;
      }
      if (selection.active) {
        if (event.shiftKey) selection.shiftToggle(m.id);
        else selection.toggle(m.id);
      } else {
        onClick?.();
      }
    },
    [selection, m.id, onClick],
  );

  // `dragRef` being provided is our source of truth for "this row is
  // draggable on this surface" (Codex review: use dragRef as the signal,
  // not dragListeners truthiness). DraggableMemoryRow passes dragRef on
  // desktop only, never on mobile — so the grip below only renders on
  // desktop, which is the correct gesture-ownership boundary.
  const isDraggable = !!dragRef;

  // Card preset delegates to a dedicated column-layout component. The
  // branch sits AFTER all hooks so Rules-of-Hooks invariants hold even
  // when callers re-render with a different `surface`. The unused row
  // hooks above (selection, prefetch, longPress) fire but their
  // results are discarded.
  if (surface === "card") {
    return (
      <MemoryCardLayout
        memory={m}
        topicLabel={topicLabel}
        currentHubId={currentHubId}
        onClick={onClick}
        isNew={isNew}
      />
    );
  }

  return (
    <div
      ref={dragRef}
      {...dragAttributes}
      onClick={handleClick}
      onPointerEnter={schedulePrefetch}
      onPointerDown={onPointerDown}
      onPointerUp={cancelLongPress}
      onPointerLeave={() => {
        cancelLongPress();
        cancelPrefetch();
      }}
      onPointerCancel={cancelLongPress}
      role="button"
      tabIndex={0}
      onFocus={schedulePrefetch}
      onBlur={cancelPrefetch}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onClick?.();
      }}
      className={`memory-row group relative w-full text-left px-4 py-2.5 cursor-pointer transition-opacity duration-150 ${
        isSelected ? "memory-row--selected" : ""
      } ${showDivider ? "border-t border-border/30" : ""} ${
        // Opacity precedence (codex-review 2026-04-21):
        //   drag (strongest) > search-dim > batch-dim > normal.
        // Mutually exclusive ternary so class order doesn't decide the
        // final visual. Batch-dim is the lightest "quieting" layer —
        // unselected rows fade slightly to focus attention on selected
        // ones without losing interactivity.
        isDragging
          ? "opacity-30"
          : dimmed
            ? "opacity-25"
            : selection.active && !isSelected
              ? "opacity-60"
              : ""
      } ${isNew ? "state-memory-arrive" : ""}`}
    >
      {/* Desktop-only drag handle (Codex review).
          Gesture-ownership split: the row root still holds `ref={dragRef}`
          so dnd-kit's drag overlay uses the row's bounding box, but only
          the grip carries the activator listeners — so pointer gestures
          on the row body (click, text-select, mobile long-press) do not
          fight with drag activation. stopPropagation on click prevents
          an accidental row-navigate when the user clicks the grip itself.
          Absolute-positioned so toggling the grip's visibility on hover
          never reflows the row. */}
      {isDraggable && (
        <span
          {...dragListeners}
          onClick={(e) => e.stopPropagation()}
          className="absolute left-1 top-1/2 -translate-y-1/2 p-0.5 text-fg-4 opacity-0 group-hover:opacity-100 transition-opacity cursor-grab active:cursor-grabbing"
          role="button"
          tabIndex={-1}
          aria-label={t.topics.dragHandleAria.replace("{name}", cleanTitle)}
        >
          <GripVertical className="h-3 w-3" />
        </span>
      )}
      {useStackedRecent ? (
        <>
          <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
            {displayedIndicator}
            {attributionLabel ? (
              <span className="inline-flex min-w-0 items-center gap-1 text-foreground/40">
                {renderAttributionText(attributionLabel)}
              </span>
            ) : null}
            <span className="ml-auto flex items-center gap-1.5 text-foreground/40">
              {contentTypeMeta}
              <span className="tabular-nums">
                {formatAge(m.created_at, t, interpolate)}
              </span>
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="text-[14px] text-fg-1 truncate flex-1 min-w-0">
              {searchHighlight ? (
                <HighlightText text={cleanTitle} highlight={searchHighlight} />
              ) : (
                cleanTitle
              )}
            </span>
            {topicLabel && (
              <TopicLabelChip
                topicLabel={topicLabel}
                tone={presentation.lifecycle.breadcrumbTone}
                dreamAction={presentation.lifecycle.dreamAction}
                isMobile={isMobile}
              />
            )}
            {showCopy && (
              <button
                onClick={copyRow}
                className="text-fg-4 hover:text-fg-3 transition-colors cursor-pointer shrink-0"
              >
                {rowCopied ? (
                  <Check className="h-3 w-3" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </button>
            )}
          </div>
        </>
      ) : (
        <div className="flex items-center gap-2">
          {displayedIndicator}
          {contextLabel}
          <span className="text-[14px] text-fg-1 truncate flex-1 min-w-0">
            {searchHighlight ? (
              <HighlightText text={cleanTitle} highlight={searchHighlight} />
            ) : (
              cleanTitle
            )}
          </span>
          <div
            className="flex items-center gap-1.5 ml-auto shrink-0"
            data-testid="memory-row-right-cluster"
          >
            {isProcessing ? (
              <span
                className="text-[13px] shrink-0 state-slow-breathe"
                style={{ color: "var(--signature)" }}
              >
                {t.memoryView.remembering}
              </span>
            ) : (
              <>
                {showHubLabel && hubLabel && (
                  <Pill size="sm" variant="static" className="shrink-0">
                    {hubLabel}
                  </Pill>
                )}
                {isMobileCompactRecent && topicLabel && (
                  <TopicLabelChip
                    topicLabel={topicLabel}
                    tone={presentation.lifecycle.breadcrumbTone}
                    dreamAction={presentation.lifecycle.dreamAction}
                    isMobile={isMobile}
                  />
                )}
                {renderedTrailingActor}
                {trailingContentMeta}
                {showCount && (
                  <span className="text-[12px] text-fg-3 tabular-nums">
                    {m.access_count}×
                  </span>
                )}
                {showSource && (
                  <span className="text-[10px] text-fg-3">{m.source}</span>
                )}
                {!isMobileCompactRecent && topicLabel && (
                  <TopicLabelChip
                    topicLabel={topicLabel}
                    tone={presentation.lifecycle.breadcrumbTone}
                    dreamAction={presentation.lifecycle.dreamAction}
                    isMobile={isMobile}
                  />
                )}
                <span className="text-[13px] text-fg-3 tabular-nums">
                  {formatAge(m.created_at, t, interpolate)}
                </span>
                {showCopy && (
                  <button
                    onClick={copyRow}
                    className="text-fg-4 hover:text-fg-3 transition-colors cursor-pointer ml-0.5"
                  >
                    {rowCopied ? (
                      <Check className="h-3 w-3" />
                    ) : (
                      <Copy className="h-3 w-3" />
                    )}
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      )}
      {/* Line 2: summary — flat (no inline emphasis) so the row has a single
          scan anchor (the title) and the summary stays a quiet glance. Bold /
          italic / code are preserved in the detail view; here we strip them.
          See kitchen section 04 "Flat vs full summary" for the rule. */}
      {showSummary && (
        <p
          className={`text-[13px] text-fg-3 truncate mt-0.5 ${useStackedRecent ? "" : "pl-5"}`}
        >
          <MarkdownSnippet
            text={sanitizeSummary(m.summary)}
            maxLen={120}
            flat
          />
        </p>
      )}
    </div>
  );
}
