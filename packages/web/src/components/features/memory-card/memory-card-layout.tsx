"use client";

/**
 * MemoryCardLayout — canonical card visual for a single memory.
 *
 * Used wherever memories render as cards (vs the row layout in
 * `<MemoryRow>`):
 *   - fresh-memories grid in `<RecentSection>`
 *   - topic detail card grid (when surface="card" preset is active)
 *   - related-memories card grid (planned)
 *   - search results card grid (planned)
 *
 * Visual contract (plan 26 phase 2):
 *   - Material: `glass-card` recipe + `rounded-card` (20px) + `p-4`.
 *   - Min height: 168px (mobile 1-col stays comfortable). Matches
 *     `<ComposePlusCell>` so the new-memory CTA aligns visually with
 *     real cards in the same grid.
 *   - Hover: NO LIFT (kitchen 2026 standard rejected -translate-y).
 *     Notion-style — surface tint shifts from glass-card baseline to
 *     `bg-surface-1`, border opacity ticks up, and a subtle right-arrow
 *     cursor indicator fades in from a `translateX(-4px)` resting state
 *     to communicate "click to enter." No `--signature` border accent —
 *     signature is reserved for AI moments per design system §0.
 *   - Metadata band carries title, summary (3-line clamp via
 *     MarkdownSnippet `flat` — same component memory-row uses; `flat`
 *     STRIPS inline markdown markers like `**`/`_`/`` ` `` so the card
 *     reads as plain prose instead of leaking syntax characters), file-
 *     type icon, age, attribution (agent / human author), cross-hub
 *     label, and topic chip. Tags are deliberately omitted on cards
 *     (visual noise on a grid where title + summary already carry the
 *     signal); they remain on memory-detail and the row view.
 *
 * Future hover actions (forget, copy-link, ...) will compose into the
 * top-right slot via `group-hover` opacity. Held back from v1 because
 * the row layout doesn't have an action surface either; both should
 * gain one in the same pass when the action set is designed.
 */

import { type MouseEvent as ReactMouseEvent } from "react";
import {
  ChevronRight,
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
  Quote as QuoteIcon,
} from "lucide-react";
import { MarkdownSnippet } from "@memaxlabs/ui";
import { useAuth } from "@/lib/auth";
import { formatAge } from "@/lib/format-age";
import { sanitizeSummary, sanitizeTitle } from "@/lib/sanitize-metadata";
import {
  resolveMemoryRowHubType,
  resolveMemoryRowPresentation,
} from "@/lib/memory-row-presentation";
import { useLocale, useInterpolate } from "@/i18n";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { isTextSelectionActiveIn } from "@/lib/text-selection";
import type { Memory } from "@/hooks/use-memories";
import type { TopicLabelData } from "@/lib/topic-label";
import { TopicChip } from "@/components/features/topic/topic-chip";
import { DreamActionPopoverBody } from "@/components/features/dream-action-popover-body";
import { AgentInlineIdentity } from "@/components/features/agent-inline-identity";
import { linkedAgentAttributionCopy } from "@/lib/memory-attribution-copy";

interface MemoryCardLayoutProps {
  memory: Memory;
  topicLabel?: TopicLabelData;
  /**
   * Active-hub id from `useActiveHub`. When the memory's hub_id
   * differs, the card surfaces a cross-hub label (e.g., "from
   * Engineering hub") so a mixed-hub grid (recall results, all-hubs
   * stream) signals provenance.
   */
  currentHubId?: string;
  onClick?: () => void;
  isNew?: boolean;
  /**
   * Drag-to-topic wiring (plan 26 follow-up). When provided, the card
   * becomes a dnd-kit draggable source — same pattern memory-row uses
   * via `<DraggableMemoryRow>`. Wire from the parent via
   * `<DraggableMemoryCard>` rather than calling `useDraggableMemory`
   * here; that keeps this layout component pure presentation.
   *
   * dnd-kit's PointerSensor uses `activationConstraint: { distance: 8 }`
   * (topic-dnd-provider), so listeners on the whole card don't conflict
   * with the click-to-navigate handler — a click without 8px of motion
   * stays a click.
   */
  dragRef?: (node: HTMLElement | null) => void;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  dragAttributes?: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  dragListeners?: any;
  isDragging?: boolean;
}

function CardFileTypeIcon({ contentType }: { contentType: string }) {
  if (contentType === "image") {
    return <ImageIcon className="h-3 w-3 text-fg-3" strokeWidth={2} />;
  }
  if (contentType === "link") {
    return <LinkIcon className="h-3 w-3 text-fg-3" strokeWidth={2} />;
  }
  if (contentType === "quote") {
    return <QuoteIcon className="h-3 w-3 text-fg-3" strokeWidth={2} />;
  }
  return <FileText className="h-3 w-3 text-fg-3" strokeWidth={2} />;
}

export function MemoryCardLayout({
  memory: m,
  topicLabel,
  currentHubId,
  onClick,
  isNew = false,
  dragRef,
  dragAttributes,
  dragListeners,
  isDragging = false,
}: MemoryCardLayoutProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const isMobile = useIsMobile();
  const { hubs, user } = useAuth();

  const hubInfo = hubs.find((h) => h.hub.id === m.hub_id);
  const hubLabel = m.hub_name || hubInfo?.hub.name;
  const isTeamHub =
    resolveMemoryRowHubType(m, hubInfo?.hub.hub_type) === "team";

  const presentation = resolveMemoryRowPresentation(m, {
    surface: "card",
    isMobile,
    currentUserId: user?.id,
    isTeamHub,
    currentHubId,
  });

  const cleanTitle =
    sanitizeTitle(m.title) || (m.title ? t.toast.untitled : "");
  const cleanSummary = sanitizeSummary(m.summary) || "";
  // Preview fallback: when memax hasn't produced a summary yet (short
  // notes, freshly-saved memories pre-processing, older imports), show
  // the raw body so the card isn't a half-blank tile. Skip when content
  // is just the title repeated (short notes where title === body),
  // matching memory-detail-view's `hasSummary` logic. The 240-char cap
  // is roughly the visual capacity of a 3-line clamp at 12px; the
  // line-clamp handles overflow but capping early avoids paying parse
  // cost on a 50KB note's preview.
  const previewText = cleanSummary
    ? cleanSummary
    : m.content && m.content !== m.title
      ? m.content.slice(0, 240)
      : "";
  const linkedCopy = linkedAgentAttributionCopy(presentation.attribution, t);

  // Team-hub combined attribution: when a memory has BOTH a team
  // author AND an agent, render `[author avatar] [author name] [verb]
  // via [agent identity]` on desktop and the compact `[author avatar]
  // via [agent icon]` on mobile. Earlier this branch was missing — the
  // card showed only the agent and dropped the author silently.
  const hasBothAuthorAndAgent =
    presentation.hasTeamAuthor &&
    !!m.author_name &&
    presentation.attribution.hasAgent;
  // Verb selection mirrors row-level attribution semantics:
  //   isHumanRequestedAgent = true  → user explicitly invoked the agent → "saved"
  //   isHumanRequestedAgent = false → agent autonomously captured       → "captured"
  const teamCombinedVerb = presentation.attribution.isHumanRequestedAgent
    ? t.attribution.saved
    : t.attribution.captured;
  // Reusable author-avatar renderer (image with initial-letter
  // fallback). Same shape both the existing author-only branch and
  // the new combined branch consume.
  const authorAvatar =
    presentation.hasTeamAuthor && m.author_name ? (
      m.author_avatar_url ? (
        <img
          src={m.author_avatar_url}
          alt=""
          className="h-4 w-4 shrink-0 rounded-full object-cover"
          title={m.author_name}
        />
      ) : (
        <span
          aria-hidden
          className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[9px] font-medium text-foreground/55"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.08)",
          }}
          title={m.author_name}
        >
          {m.author_name[0]?.toUpperCase()}
        </span>
      )
    ) : null;

  const handleClick = (event: ReactMouseEvent<HTMLElement>) => {
    // Honor in-row text selection so users copy-pasting from the
    // card don't get yanked away to detail.
    if (isTextSelectionActiveIn(event.currentTarget)) return;
    onClick?.();
  };

  return (
    <article
      ref={dragRef}
      onClick={handleClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          onClick?.();
          return;
        }
        // Space activates the card but prevents the default scroll
        // behavior that would fire when the card is focused.
        if (e.key === " ") {
          e.preventDefault();
          onClick?.();
        }
      }}
      {...(dragAttributes ?? {})}
      {...(dragListeners ?? {})}
      className={`group relative flex h-full min-h-[168px] flex-col rounded-card glass-card p-4 text-left transition-colors duration-200 cursor-pointer ${
        isNew ? "state-memory-arrive" : ""
      } ${isDragging ? "opacity-40" : ""}`}
    >
      {/* Header: file-type icon + title + age */}
      <div className="mb-3 flex items-start gap-2">
        <span
          aria-hidden
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-surface-2"
        >
          <CardFileTypeIcon contentType={m.content_type} />
        </span>
        <h4
          className="flex-1 text-[13px] font-semibold leading-snug text-foreground"
          style={{ letterSpacing: "-0.005em" }}
        >
          {cleanTitle}
          {m.source_kind === "onboarding-seed" && (
            // Plan 23 tutorial curriculum — four seed cards (e001..e004)
            // land at signup alongside the onboarding checklist. Without
            // a visual signal they read as "did I sync this from
            // somewhere?" — they're memax-authored sample content.
            // Inline chip beside the title keeps the seed origin
            // discoverable without taking row real estate.
            <span
              className="ml-2 inline-flex items-center px-1.5 py-px rounded text-[10px] font-medium align-middle"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.06)",
                color: "var(--fg-3)",
                letterSpacing: "0.02em",
              }}
              title={t.onboarding.seedTag.tooltip}
            >
              {t.onboarding.seedTag.label}
            </span>
          )}
        </h4>
        <span
          className="shrink-0 text-[11px] text-fg-4 tabular-nums"
          aria-hidden
        >
          {/* `created_at` for parity with the row preset (memory-row.tsx
              uses `m.created_at`). Cards on a mixed grid showing the
              same memory shouldn't disagree about its age. */}
          {formatAge(m.created_at, t, interpolate)}
        </span>
      </div>

      {/* Preview — clamped 3-line summary, rendered through MarkdownSnippet
          `flat`. Flat mode STRIPS inline markdown markers (`**`, `_`,
          `` ` ``, link syntax, etc.) so the card reads as plain prose
          instead of showing raw markdown characters like `**Root cause**`.
          The title is the scan anchor; the summary is a glance — quiet
          on purpose. Same component memory-row uses for the row preset,
          so card + row stay in sync on what's a "preview."
          When `m.summary` is empty (short notes, freshly-saved memories
          pre-processing), `previewText` falls back to `m.content` so
          the card surfaces something rather than reading as half-blank. */}
      {previewText ? (
        <div className="mb-3 line-clamp-3 flex-1 text-[12px] leading-relaxed text-fg-3">
          <MarkdownSnippet text={previewText} flat />
        </div>
      ) : (
        // Empty preview slot keeps card height consistent in the grid
        // when neither summary nor content is available (rare: empty
        // memory, or content === title for one-word notes). flex-1
        // grows so the footer hugs the bottom regardless.
        <div className="mb-3 flex-1" aria-hidden />
      )}

      {/* Topic row — own line above the attribution footer (plan 26
          follow-up). Earlier the topic chip lived as the rightmost
          element of the footer's single-line metadata strip, but on
          cards with long agent attribution ("captured by · Memax
          Architecture") the topic got cropped. Topic now sits on
          its own line so it always renders fully; footer below is
          attribution + hub only.
          Same `pr-6` so the chip can't extend under the bottom-right
          chevron. Mobile dream-action popover skipped (tap navigates
          to detail; detail page's dream-history is the mobile
          equivalent). */}
      {topicLabel && presentation.showTopic ? (
        <div className="mb-1 flex pr-6">
          <TopicChip
            segments={topicLabel.path}
            tone={presentation.lifecycle.breadcrumbTone}
            intent={topicLabel.intent}
            onClick={topicLabel.onClick}
            popover={
              !isMobile && presentation.lifecycle.dreamAction ? (
                <DreamActionPopoverBody
                  action={presentation.lifecycle.dreamAction}
                />
              ) : undefined
            }
          />
        </div>
      ) : null}

      {/* Footer — attribution + cross-hub label only. Topic moved to
          its own row above (plan 26 follow-up). Truncation policy at
          narrow widths (mobile 1-col, ~320px):
          - Attribution gets the flexible truncation lane (`flex-1
            min-w-0`). Long agent names get `text-overflow: ellipsis`.
          - Hub label is secondary; bounded by max-width so it doesn't
            outgrow the attribution lane.
          - `pr-6` reserves space for the bottom-right chevron indicator. */}
      <div className="flex min-w-0 items-center gap-2 pr-6 text-[11px] text-fg-4">
        {hasBothAuthorAndAgent ? (
          isMobile ? (
            // Mobile compact: [author avatar] via [agent icon]. No
            // names — saves horizontal space; full author + agent
            // names are accessible via title/aria attributes.
            <span
              className="inline-flex flex-1 items-center gap-1.5 text-fg-3"
              title={`${m.author_name} ${teamCombinedVerb} ${t.attribution.via} ${presentation.attribution.agentDisplayName}`}
            >
              {authorAvatar}
              <span className="shrink-0">{t.attribution.via}</span>
              <AgentInlineIdentity
                attribution={presentation.attribution}
                iconOnly
              />
            </span>
          ) : (
            // Desktop full: [author avatar] [author name] [verb] via
            // [agent icon] [agent name]. `truncate` on the outer
            // collapses the row at the end if both names are too long.
            <span className="inline-flex min-w-0 flex-1 items-center gap-1.5 truncate text-fg-3">
              {authorAvatar}
              <span className="min-w-0 truncate">{m.author_name}</span>
              <span className="shrink-0">{teamCombinedVerb}</span>
              <span className="shrink-0">{t.attribution.via}</span>
              <AgentInlineIdentity
                attribution={presentation.attribution}
                className="min-w-0"
              />
            </span>
          )
        ) : presentation.attribution.hasAgent ? (
          <span className="inline-flex min-w-0 flex-1 items-center gap-1 truncate">
            <span className="shrink-0">{linkedCopy.prefix}</span>
            <AgentInlineIdentity attribution={presentation.attribution} />
            {linkedCopy.suffix ? (
              <span className="shrink-0">{linkedCopy.suffix}</span>
            ) : null}
          </span>
        ) : presentation.hasTeamAuthor && m.author_name ? (
          // Author-only branch (team hub, no agent involved).
          <span className="inline-flex min-w-0 flex-1 items-center gap-1.5 truncate text-fg-3">
            {authorAvatar}
            <span className="min-w-0 truncate">{m.author_name}</span>
          </span>
        ) : null}

        {presentation.showHubLabel && hubLabel ? (
          <>
            <span aria-hidden className="shrink-0 text-fg-4">
              ·
            </span>
            <span className="max-w-[40%] shrink truncate text-fg-3">
              {hubLabel}
            </span>
          </>
        ) : null}
      </div>

      {/* Industry-2026 hover affordance — subtle right-arrow cursor
          indicator, fades in from `translateX(-4px)` resting state.
          Communicates "click to enter" without an explicit action
          button. Sits in the bottom-right corner so it doesn't fight
          with title or footer for attention. NO `--signature` accent
          (signature reserved for AI moments per design system §0). */}
      <ChevronRight
        aria-hidden
        className="pointer-events-none absolute bottom-3 right-3 h-3.5 w-3.5 text-fg-4 opacity-0 -translate-x-1 transition-all duration-200 group-hover:opacity-100 group-hover:translate-x-0"
        strokeWidth={2}
      />
    </article>
  );
}
