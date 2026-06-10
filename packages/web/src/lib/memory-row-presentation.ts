import type { Memory } from "@/hooks/use-memories";
import type { DreamActionRef, MemoryLifecycle } from "memax-sdk";
import {
  resolveMemoryAttribution,
  type ResolvedMemoryAttribution,
} from "@/lib/memory-attribution";

export type MemoryRowSurface =
  | "recent"
  | "inbox"
  | "topic"
  | "list"
  | "recall"
  | "card";

/**
 * Layout shape — `row` is the existing horizontal stripe used by
 * recent/inbox/topic/list/recall. `column` stacks header → preview →
 * footer for the new card preset (plan 20 §3.1).
 */
export type MemoryRowLayout = "row" | "column";

/**
 * Glass material flag — `true` applies the `.glass-card` recipe
 * (75% card fill + 12px blur + hairline border + inner ring) used by
 * the card preset. Other surfaces stay borderless on `--background`.
 */
export type MemoryRowGlassMaterial = boolean;

/**
 * Halo state for the memax-signature "freshness" wrapper behind a memory
 * row's leading indicator. Pure function of (now, memory.created_at) —
 * no server state involved. Decay tiers match the Phase 2 contract:
 *
 *   - age < 5min         → "breathing" (state-slow-breathe animation)
 *   - 5min ≤ age < 24h   → "static" (steady halo, no animation)
 *   - age ≥ 24h          → "off" (no halo)
 *
 * Clears purely by time — no acknowledge table, no mutation, no scroll
 * observer. Cross-device safe; same memory shows the same halo on any
 * device at any time.
 */
export type MemoryRowHalo = "off" | "static" | "breathing";

/**
 * Breadcrumb tone for the memory-row topic chip. "dream" means a dream
 * action is pending on this memory since the viewer last visited the
 * memory's current topic — server-resolved via memory.lifecycle.
 * pending_dream_action. Clears server-side on topic visit.
 */
export type MemoryRowBreadcrumbTone = "neutral" | "dream";

export interface MemoryRowLifecyclePresentation {
  halo: MemoryRowHalo;
  breadcrumbTone: MemoryRowBreadcrumbTone;
  /** Pass-through of the server-resolved pending action for popover
   *  content on the tinted breadcrumb chip. Null when tone is neutral. */
  dreamAction: DreamActionRef | null;
}

export type MemoryRowLeadingIdentity =
  | "processing"
  | "author"
  | "agent"
  | "content"
  | "none";

export type MemoryRowTrailingActor = "none" | "author" | "agent";

export type MemoryRowLifecycleState = "remembering" | "chunked" | "filed";

export interface MemoryRowPresentationContext {
  surface: MemoryRowSurface;
  isMobile: boolean;
  currentUserId?: string;
  isTeamHub: boolean;
  /**
   * Current hub id from the active-hub context. The card preset uses
   * this to decide whether to surface a cross-hub origin chip when
   * `memory.hub_id !== currentHubId` — useful when a card grid mixes
   * memories from multiple hubs (e.g., recall result lists). Other
   * surfaces previously gated the chip on `surface === "recall"`;
   * card grid extends that policy to the data-driven check.
   */
  currentHubId?: string;
  /** Current time — injected for deterministic halo computation in
   *  tests. Defaults to new Date() at call site if omitted. */
  now?: Date;
}

export interface MemoryRowPresentation {
  attribution: ResolvedMemoryAttribution;
  lifecycleState: MemoryRowLifecycleState;
  isProcessing: boolean;
  hasTeamAuthor: boolean;
  showSummary: boolean;
  showCopy: boolean;
  showHubLabel: boolean;
  showInlineContext: boolean;
  useStackedRecent: boolean;
  showTrailingContentMeta: boolean;
  leadingIdentity: MemoryRowLeadingIdentity;
  trailingActor: MemoryRowTrailingActor;
  /** Server-owned dream-delta + client-derived halo signals. See
   *  MemoryRowLifecyclePresentation docs for semantics. */
  lifecycle: MemoryRowLifecyclePresentation;
  /**
   * Layout shape — existing surfaces keep `row`; the card preset
   * emits `column`. `<MemoryRow>` branches its render on this.
   */
  layout: MemoryRowLayout;
  /**
   * `true` → root element gets the `.glass-card` recipe class. Card
   * preset only; other surfaces stay borderless.
   */
  glassMaterial: MemoryRowGlassMaterial;
  /**
   * Whether to render the topic chip in the row. Card preset always
   * shows it (cards are stand-alone discoverable units that need
   * their topic context); other surfaces inherit from prior behavior
   * (topic surface hides it because it's redundant on the topic page).
   */
  showTopic: boolean;
  /**
   * Whether to render the trailing actions cluster (copy / overflow
   * menu). Card preset hides it to keep the card surface clean —
   * actions surface on detail page. Other surfaces keep prior
   * `showCopy` semantics.
   */
  showActions: boolean;
  /**
   * Whether the card lifts on hover (-1px translate + soft shadow).
   * Card preset only; row surfaces have their own hover pattern.
   */
  haloOnHover: boolean;
}

type MemoryRowPresentationMemory = Pick<
  Memory,
  | "summary"
  | "state"
  | "content_type"
  | "author_name"
  | "source_agent"
  | "provenance"
  | "agent_display_name"
  | "agent_icon"
  | "owner_id"
> & {
  created_at?: string;
  /**
   * Optional. The card preset's cross-hub policy reads this to compare
   * against `context.currentHubId` and surface a hub label when they
   * differ. Row surfaces don't read it; the resolver gracefully
   * handles `undefined`.
   */
  hub_id?: string;
  lifecycle?: MemoryLifecycle;
};

export function resolveMemoryRowPresentation(
  memory: MemoryRowPresentationMemory,
  context: MemoryRowPresentationContext,
  options?: { hasTopicLabel?: boolean },
): MemoryRowPresentation {
  const { surface, isMobile, currentUserId, isTeamHub } = context;
  const attribution = resolveMemoryAttribution(memory as Memory, currentUserId);
  const lifecycleState = resolveMemoryRowLifecycleState(
    memory.state,
    options?.hasTopicLabel === true,
  );
  const isProcessing = lifecycleState === "remembering";
  const hasTeamAuthor = isTeamHub && !!memory.author_name;

  // Mobile compact: the recent surface on mobile drops summary + copy and
  // collapses to the single-line layout used by topic/inbox — title carries
  // the scan, a small trailing cluster carries actor + type + time. This
  // matches the user-requested "no description, title + actor + content
  // type if not doc" compact form.
  const isMobileCompactRecent = isMobile && surface === "recent";

  const showSummary =
    !isMobileCompactRecent &&
    !!memory.summary &&
    (surface === "recent" || surface === "list" || surface === "recall");
  const showCopy = surface === "recent" && !isMobileCompactRecent;
  const showHubLabel = isTeamHub && surface === "recall";
  const showInlineContext = surface === "recall" || surface === "list";

  const showPersonalRecentAttribution =
    attribution.isOwnMemory && !hasTeamAuthor && !attribution.hasAgent;
  const hasRecentAttribution =
    hasTeamAuthor || attribution.hasAgent || showPersonalRecentAttribution;
  const useStackedRecent =
    !isMobileCompactRecent && surface === "recent" && hasRecentAttribution;

  const trailingActor =
    !isProcessing &&
    (surface === "topic" || surface === "inbox" || isMobileCompactRecent)
      ? hasTeamAuthor
        ? "author"
        : attribution.hasAgent
          ? "agent"
          : "none"
      : "none";

  let leadingIdentity: MemoryRowLeadingIdentity = isProcessing
    ? "processing"
    : isMobileCompactRecent
      ? "none"
      : surface === "topic"
        ? "none"
        : trailingActor !== "none"
          ? "content"
          : hasTeamAuthor
            ? "author"
            : attribution.hasAgent
              ? "agent"
              : "content";

  if (
    surface === "recent" &&
    attribution.isOwnMemory &&
    !hasTeamAuthor &&
    !attribution.hasAgent
  ) {
    leadingIdentity = "none";
  }

  const showTrailingContentMeta =
    isRichContentType(memory.content_type) &&
    !useStackedRecent &&
    (surface === "topic" || isMobileCompactRecent || trailingActor === "none");

  const lifecycle = resolveMemoryRowLifecyclePresentation(
    memory,
    isProcessing,
    context.now,
  );

  // Card preset distinguishes itself from row surfaces:
  //   - column layout (vertical stack: header / preview / footer)
  //   - .glass-card material (75% fill + blur + hairline border)
  //   - always shows topic chip (cards are stand-alone discoverable
  //     units that need their topic context)
  //   - hides trailing actions cluster (clean card surface; actions
  //     surface on detail)
  //   - halo-on-hover (-1px lift + soft foreground-tinted shadow)
  //
  // Cross-hub chip on cards: if `currentHubId` is provided AND the
  // memory belongs to a different hub, surface the hub label so a
  // mixed-hub card grid (e.g., recall results) signals provenance.
  // Falls back to the existing `surface === "recall"` policy when
  // currentHubId isn't threaded through.
  const isCard = surface === "card";
  const layout: MemoryRowLayout = isCard ? "column" : "row";
  const glassMaterial: MemoryRowGlassMaterial = isCard;
  const showTopic = isCard ? true : surface !== "topic";
  const showActions = isCard ? false : showCopy;
  const haloOnHover = isCard;
  const memoryHubId = memory.hub_id;
  const cardCrossHub =
    isCard &&
    typeof context.currentHubId === "string" &&
    typeof memoryHubId === "string" &&
    memoryHubId !== context.currentHubId;
  const finalShowHubLabel = showHubLabel || cardCrossHub;

  return {
    attribution,
    lifecycleState,
    isProcessing,
    hasTeamAuthor,
    showSummary,
    showCopy,
    showHubLabel: finalShowHubLabel,
    showInlineContext,
    useStackedRecent,
    showTrailingContentMeta,
    leadingIdentity,
    trailingActor,
    lifecycle,
    layout,
    glassMaterial,
    showTopic,
    showActions,
    haloOnHover,
  };
}

/**
 * resolveMemoryRowLifecyclePresentation — collapses server-owned lifecycle
 * fields (from memory.lifecycle) and client-derived halo state (from
 * memory.created_at) into the single row-facing presentation object.
 *
 * Halo is suppressed during processing so the row doesn't double-signal
 * (processing star + halo would fight for the same slot).
 */
export function resolveMemoryRowLifecyclePresentation(
  memory: Pick<MemoryRowPresentationMemory, "created_at" | "lifecycle">,
  isProcessing: boolean,
  now?: Date,
): MemoryRowLifecyclePresentation {
  const pending = memory.lifecycle?.pending_dream_action ?? null;
  const breadcrumbTone: MemoryRowBreadcrumbTone = pending ? "dream" : "neutral";
  const halo: MemoryRowHalo = isProcessing
    ? "off"
    : resolveHaloState(memory.created_at, now ?? new Date());
  return { halo, breadcrumbTone, dreamAction: pending };
}

/**
 * resolveHaloState — pure time-based decay for the freshness halo.
 * Exported for tests; call sites go through
 * resolveMemoryRowLifecyclePresentation.
 */
export function resolveHaloState(
  createdAtISO: string | undefined,
  now: Date,
): MemoryRowHalo {
  if (!createdAtISO) return "off";
  const createdAt = new Date(createdAtISO).getTime();
  if (Number.isNaN(createdAt)) return "off";
  const ageMs = now.getTime() - createdAt;
  if (ageMs < 0) return "off"; // future-dated; safer to not render
  const FIVE_MIN = 5 * 60 * 1000;
  const TWENTY_FOUR_H = 24 * 60 * 60 * 1000;
  if (ageMs < FIVE_MIN) return "breathing";
  if (ageMs < TWENTY_FOUR_H) return "static";
  return "off";
}

export function resolveMemoryRowLifecycleState(
  state: string,
  hasTopicLabel: boolean,
): MemoryRowLifecycleState {
  if (state === "processing" || state === "remembering") {
    return "remembering";
  }
  return hasTopicLabel ? "filed" : "chunked";
}

export function resolveMemoryRowHubType(
  memory: Pick<Memory, "author_name">,
  explicitHubType?: string | null,
) {
  if (explicitHubType === "team") return "team";
  if (explicitHubType === "personal") return "personal";
  return memory.author_name ? "team" : "personal";
}

function isRichContentType(contentType: string) {
  return (
    contentType === "pdf" || contentType === "image" || contentType === "link"
  );
}
