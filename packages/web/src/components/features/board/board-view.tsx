"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowRight, Plus } from "lucide-react";
import type { BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardDeckControls,
  BoardDeckShell,
  BoardSlotStrip,
  BoardVoiceStar,
  InfoPopover,
} from "@memaxlabs/ui";
import { pluralize, useInterpolate, useLocale } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { useActiveHub, useAuth } from "@/lib/auth";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildPulsePath } from "@/lib/route-helpers";
import { trackEvent } from "@/lib/posthog";
import {
  useCreateBoard,
  useCustomBoardsWithSlots,
  useDeleteBoard,
  useHubBoard,
  useHubBoards,
  useResolveBoardSlot,
  useSlotHistory,
} from "@/hooks/use-board";
import {
  useNotificationDismiss,
  useResolveNotification,
} from "@/hooks/use-notifications";
import type { NotificationResolveAction } from "memax-sdk";
import { useBoardCardActions } from "@/hooks/use-board-continue";
import {
  useSettings,
  useUpdateSettings,
  type Settings,
} from "@/hooks/use-settings";
import { queryClient } from "@/lib/query-client";
import { PinnedDispatch } from "@/components/features/onboarding/onboarding-pinned";
import { ActionMenu } from "@/components/features/action-menu";
import { buildBoardCardContext } from "./board-card-context";
import {
  BoardHighlightCard,
  BoardNotificationDeck,
  groupWaitingByKind,
  BoardRecentRow,
  useBoardNotificationCards,
} from "./board-notification-cards";
import { BoardShelf, groupSlotsByKind } from "./board-shelf";
import {
  BoardEmptyState,
  BoardGhostCard,
  CookingBoardCard,
  CustomBoardSlotCard,
} from "./board-custom-boards";
import {
  boardKindOptions,
  boardKindPurpose,
  boardKindStripSummary,
  boardKindTemporality,
  renderBoardSlotBody,
  slotContentTime,
} from "./board-kind-registry";
// Side-effect import: registers the Lane A + Lane B kind renderers
// before the first render so no card flashes through the fallback.
import "./board-kinds";

type BoardResolveAction = "ack" | "dismiss";

// useLayoutEffect warns during SSR; the shelf only exists client-side,
// but the module is imported into a server-rendered tree.
const useIsomorphicLayoutEffect =
  typeof window !== "undefined" ? useLayoutEffect : useEffect;

/**
 * BOARD_SHELF_RULES — the codified shelf + dismiss ruleset for the
 * embedded board (founder spec, 2026-09 revision). The shelf/card
 * handlers implement every behavior below; when changing one, change
 * both.
 *
 *   R1  (2026-09 revision) The EMBEDDED board (memories page) is
 *       ALWAYS the compact 2×2 tile shelf — it never expands in
 *       place. The memories page is a preview; the /pulse PAGE is the
 *       one full surface, where CARDS default EXPANDED, collapse
 *       recorded per content (slot_key + content_updated_at) so new
 *       content re-expands.
 *   R3  (2026-09 revision) Tile tap = NAVIGATE to /pulse focused on
 *       that card (`?focus=<slot_key>` → scroll + un-collapse +
 *       flash). Deck/highlight/cooking/ghost tiles navigate to the
 *       plain /pulse surface, where their content leads the stream.
 *   R4  Resolving a card moves it to the 已归档 section (2026-08
 *       archive revision superseded the inline-receipt swap).
 *
 *   R7  Every live tile/card has a quiet dismiss: tiles via the
 *       hover/long-press ×, expanded cards via the 不关心 verb. Both
 *       call resolve action="dismiss" (slots) or the notification
 *       dismiss (highlights), optimistically — the tile leaves the
 *       shelf immediately and lives on only as a receipt. Decision
 *       tiles (等你) are the exception: a decision needs an answer,
 *       and the server refuses plain dismiss on decision kinds.
 *
 * R2/R5 (auto-expand/collapse) died with the 2026-08 R1 revision, and
 * R6 (manual 展开/收起 persisted per session) died with this one: an
 * in-place expansion the tiles no longer trigger has no business
 * keeping a toggle alive — one content, one home.
 */
export const BOARD_SHELF_RULES = [
  "embedded-is-always-the-shelf-page-cards-expanded",
  "tile-tap-navigates-to-pulse-focused",
  "resolve-archives-with-undo",
  "tiles-dismiss-optimistically-except-decisions",
] as const;

/**
 * Where the board is mounted.
 *
 *   - "section" — embedded in the memories page under the hub header.
 *     Always the compact 2×2 tile shelf (BoardShelf) per
 *     BOARD_SHELF_RULES; tiles navigate to the /pulse surface. Stays
 *     zero-height when the hub has nothing, so card-less hubs keep
 *     the exact pre-board layout. No composer, no receipts strip:
 *     those belong to the full surface, one click away via 查看全部.
 *   - "page"    — the standalone /pulse route. The one surface: 等你
 *     decisions, highlights, the system board's cards, custom-board
 *     cards merged into the same stream (2026-08: no tabs), the ghost
 *     new-board card closing the stream, and the collapsed 最近
 *     receipts strip that absorbed the retired inbox.
 */
export type BoardSurface = "section" | "page";

/**
 * BoardView — the pulse board host (plan 25). The layout answer to
 * "cards eat the page": banding. The 等你 band (decisions merged from
 * notifications — plan 25 P4) comes first because it is the only band
 * that is actually blocked on the user. Then the system board's slots:
 * the FIRST live card renders expanded (the hero); every other slot
 * collapses to a one-line SlotStrip that expands on tap and can be
 * collapsed again. Making every live card open by default needs an
 * override keyed to CONTENT rather than to slot_key (slots are reused
 * across dream runs) plus a freeze on the resolve transition — see the
 * 2026-08 revert. Same-kind live slots render as ONE deck with a
 * ↻ cycle. Resolved receipts always render as strips. Finally the
 * 最近 strip — things that already happened.
 */
export function BoardView({
  hubId,
  surface = "section",
}: {
  hubId: string;
  surface?: BoardSurface;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const isPage = surface === "page";
  const { hubs, user } = useAuth();
  // 查看全部 / cooking-tile taps go to the full surface for THIS board's
  // hub, never the bare `/pulse` forwarder — the embedded shelf can be
  // showing a hub the user reached by URL before active-hub state
  // caught up, and forwarding through `/pulse` would land on the other
  // hub's board. `null` (hub not in the list yet) falls back to the
  // forwarder, which resolves once auth hydrates.
  const pulseHref = useMemo(() => {
    const entry = hubs.find((h) => h.hub.id === hubId);
    return buildPulsePath(
      entry && user ? hubRouteSlug(entry.hub, user.id) : null,
    );
  }, [hubs, user, hubId]);
  // Personal-hub detection is by the board's OWN hub, not the active
  // hub context: user-scoped rows (invites, ownership transfers, the
  // onboarding checklist) land on the personal board, and mis-deriving
  // this would leak them onto a team board.
  const isPersonalHub = useMemo(() => {
    const entry = hubs.find((h) => h.hub.id === hubId);
    return entry ? entry.hub.hub_type !== "team" : false;
  }, [hubs, hubId]);

  const { data, isPending, isError } = useHubBoard(hubId);
  // Boards are fetched on BOTH surfaces now: the page needs them for
  // the stream, the embedded shelf for its 酝酿中 cooking tiles.
  const { data: boardsData } = useHubBoards(hubId);
  const resolve = useResolveBoardSlot(hubId);
  const cardActions = useBoardCardActions(hubId);
  const notifications = useBoardNotificationCards(hubId, isPersonalHub, isPage);
  const resolveNotification = useResolveNotification();
  const dismissNotification = useNotificationDismiss();
  // Personal-board aggregation controls: one-click hide a source hub
  // from the aggregated view (persisted cross-device in settings),
  // plus a restore affordance under the 最近 strip.
  const { data: settings } = useSettings();
  const updateSettings = useUpdateSettings();
  const hiddenPulseHubIds = useMemo(
    () => settings?.pulse_hidden_hub_ids ?? [],
    [settings?.pulse_hidden_hub_ids],
  );
  const onHideHub = useCallback(
    (hideId: string) => {
      // Read the freshest list from the query cache at call time —
      // the render-closure copy can be one hide behind under rapid
      // clicks, and a full-array PATCH built from it would drop the
      // earlier hide (codex PR review 2026-08-11).
      const cached = queryClient.getQueryData<Settings>(["settings"]);
      const current = cached?.pulse_hidden_hub_ids ?? hiddenPulseHubIds;
      if (current.includes(hideId)) return;
      updateSettings.mutate({
        pulse_hidden_hub_ids: [...current, hideId],
      });
    },
    [hiddenPulseHubIds, updateSettings],
  );
  const onRestoreHiddenHubs = useCallback(() => {
    updateSettings.mutate({ pulse_hidden_hub_ids: [] });
  }, [updateSettings]);
  const createBoard = useCreateBoard(hubId);
  const deleteBoard = useDeleteBoard(hubId);

  // Card-level expansion. Default is EXPANDED (2026-08-21) — the user's
  // collapse is recorded against the CONTENT (slot_key +
  // content_updated_at), so a card that gets new content overnight
  // re-expands: the collapse applied to the old version, not the slot
  // forever. slot_key alone is a REUSED slot identity (ON CONFLICT ...
  // DO UPDATE resets it in place), which is exactly why the earlier
  // expanded-by-default attempt was reverted — keyed on slot_key it
  // suppressed brand-new content. Session-scoped like the shelf state.
  const [collapsedCards, setCollapsedCards] = useState<ReadonlySet<string>>(
    new Set(),
  );
  // C5 — kind filter (page only). null = the full stream. Chips are
  // derived from the LIVE cards actually present, each carrying its
  // kind's freshest content_updated_at so the reader can see at a
  // glance which lenses moved recently.
  const [kindFilter, setKindFilter] = useState<string | null>(null);
  // Reviewer-confirmed dead-end: focus on kind A, triage every A card
  // (the exact flow a focus mode invites) → A leaves the live set →
  // its chip vanishes while the filter persists → blank board with no
  // way out. A filter whose kind no longer exists resets itself.
  //
  // (kindFilterResetRef avoids an effect-on-derived-value loop: the
  // reset runs post-render only when the stale state is observed.)
  useIsomorphicLayoutEffect(() => {
    try {
      const raw = globalThis.sessionStorage?.getItem(
        `memax_board_cards:${hubId}`,
      );
      setCollapsedCards(raw ? new Set(JSON.parse(raw) as string[]) : new Set());
    } catch {
      setCollapsedCards(new Set());
    }
  }, [hubId]);
  const [recentOpen, setRecentOpen] = useState(false);
  // Example chip → ghost-composer prefill (empty-state teaching
  // moment). The ghost re-keys its composer on this so a chip tap
  // always lands its copy.
  const [composerPrefill, setComposerPrefill] = useState<{
    title: string;
    instruction: string;
  } | null>(null);
  // Bumped on successful create so the ghost re-forms behind the new
  // cooking card instead of holding a stale composer open.
  const [ghostEpoch, setGhostEpoch] = useState(0);
  // Header + summon (2026-09): creation moved out of the card stream
  // into the page header; this mounts the composer at the stream top.
  const [composerOpen, setComposerOpen] = useState(false);

  const boards = useMemo(() => boardsData?.boards ?? [], [boardsData]);
  // 酝酿中 custom boards — inline cooking cards + shelf promise tiles.
  const cookingBoards = useMemo(
    () => boards.filter((b) => b.kind !== "system" && b.status === "cooking"),
    [boards],
  );
  // Unified stream (2026-08): every custom board's slots, aggregated
  // client-side. Live cards merge into the flow after the system
  // board's, each tagged with its board title. No tabs. Same-kind live
  // slots on one board deck together (groupSlotsByKind).
  const customBoards = useCustomBoardsWithSlots(hubId, boards);
  const customLiveDecks = customBoards.flatMap(({ board, slots: boardSlots }) =>
    groupSlotsByKind(boardSlots).map((group) => ({ board, group })),
  );
  const customLiveCount = customLiveDecks.reduce(
    (sum, { group }) => sum + group.length,
    0,
  );

  const slots = useMemo(() => data?.slots ?? [], [data]);
  const waiting = notifications.waiting;
  const highlights = notifications.highlights;
  const recent = notifications.recent;
  const pinned = isPage ? notifications.pinned : [];

  const liveSlots = useMemo(
    () => slots.filter((s) => s.state === "fresh" || s.state === "seen"),
    [slots],
  );
  const archivedSlots = useMemo(
    () =>
      slots.filter((s) => s.state === "resolved" || s.state === "dismissed"),
    [slots],
  );

  // C5 chip data: live kinds (system + custom) → freshest content
  // time. Lives BEFORE the early returns — the stale-filter reset
  // below is a hook (rules-of-hooks).
  const kindChips = (() => {
    const byKind = new Map<string, { latest: string; sample: BoardSlot }>();
    const absorb = (s2: BoardSlot) => {
      if (s2.state !== "fresh" && s2.state !== "seen") return;
      const ts = s2.content_updated_at ?? s2.updated_at;
      const existing = byKind.get(s2.kind);
      if (!existing || (ts && ts > existing.latest)) {
        byKind.set(s2.kind, { latest: ts ?? "", sample: s2 });
      }
    };
    slots.forEach(absorb);
    customLiveDecks.forEach(({ group }) => group.forEach(absorb));
    return [...byKind.entries()].sort((a, b) => (a[0] < b[0] ? -1 : 1));
  })();
  const kindFilterIsStale =
    kindFilter !== null && !kindChips.some(([kind]) => kind === kindFilter);
  useEffect(() => {
    if (kindFilterIsStale) setKindFilter(null);
  }, [kindFilterIsStale]);

  // One impression event per board load (not per re-render).
  const trackedFor = useRef<string | null>(null);
  const slotCount = data?.slots.length ?? 0;
  useEffect(() => {
    if (!data || slotCount === 0 || trackedFor.current === data.board.id) {
      return;
    }
    trackedFor.current = data.board.id;
    trackEvent("board_viewed", {
      hub_id: hubId,
      slot_count: slotCount,
      kinds: data.slots.map((s) => s.kind),
    });
  }, [data, hubId, slotCount]);

  const toggleCard = useCallback(
    (contentKey: string, willOpen: boolean) => {
      setCollapsedCards((prev) => {
        let next = new Set(prev);
        if (willOpen) next.delete(contentKey);
        else next.add(contentKey);
        // Cap the set — replaced content leaves stale keys behind, and
        // a long-lived tab across many dream runs would otherwise grow
        // the stored array without bound (codex review). Insertion
        // order makes the oldest collapses the ones dropped.
        if (next.size > 100) {
          next = new Set([...next].slice(-100));
        }
        try {
          globalThis.sessionStorage?.setItem(
            `memax_board_cards:${hubId}`,
            JSON.stringify([...next]),
          );
        } catch {
          // Private mode / quota — choice holds in-memory.
        }
        return next;
      });
      if (willOpen) {
        trackEvent("board_card_expand", {
          hub_id: hubId,
          slot_key: contentKey,
        });
      }
    },
    [hubId],
  );

  // ── R3 receiving side — `?focus=<slot_key>` (embedded tile tap).
  // Read from window.location in an effect rather than
  // useSearchParams(): the param is applied once client-side, and this
  // avoids the CSR/Suspense bailout useSearchParams imposes on every
  // page embedding this component. The URL is cleaned IMMEDIATELY on
  // read (refresh/back never replay the focus); once the focused card
  // shows up in the data, un-collapse it, scroll it to center, flash
  // it. If the slot never materializes, a give-up timer drops the
  // intent so a much-later refetch can't yank the scroll mid-reading.
  //
  // The scroll rAF / flash timer / give-up timer live in refs with an
  // unmount-only cleanup — NOT in this effect's own cleanup. The
  // consuming effect ends by setFocusKey(null), which re-runs itself,
  // and a per-run cleanup would cancel the rAF and flash reset it
  // scheduled milliseconds earlier (adversarial review: the scroll
  // usually lost that race and the flash class never reset).
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const [focusFlashKey, setFocusFlashKey] = useState<string | null>(null);
  const focusFrameRef = useRef<number | undefined>(undefined);
  const focusFlashTimerRef = useRef<number | undefined>(undefined);
  const focusGiveUpTimerRef = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!isPage) return;
    const param = new URLSearchParams(window.location.search).get("focus");
    if (!param) return;
    setFocusKey(param);
    window.history.replaceState(null, "", window.location.pathname);
    focusGiveUpTimerRef.current = window.setTimeout(
      () => setFocusKey(null),
      5000,
    );
  }, [isPage]);
  useEffect(() => {
    if (!isPage || !focusKey) return;
    const target =
      slots.find((s) => s.slot_key === focusKey) ??
      customBoards
        .flatMap((cb) => cb.slots)
        .find((s) => s.slot_key === focusKey);
    if (!target) return; // data not in yet — retry on the next load
    if (focusGiveUpTimerRef.current !== undefined) {
      window.clearTimeout(focusGiveUpTimerRef.current);
      focusGiveUpTimerRef.current = undefined;
    }
    toggleCard(`${focusKey}:${target.content_updated_at ?? ""}`, true);
    // Two frames: one for the un-collapse to commit, one for layout.
    focusFrameRef.current = window.requestAnimationFrame(() => {
      focusFrameRef.current = window.requestAnimationFrame(() => {
        document
          .querySelector(`[data-slot-focus="${CSS.escape(focusKey)}"]`)
          ?.scrollIntoView({ behavior: "smooth", block: "center" });
      });
    });
    setFocusFlashKey(focusKey);
    focusFlashTimerRef.current = window.setTimeout(
      () => setFocusFlashKey(null),
      2200,
    );
    setFocusKey(null);
  }, [isPage, focusKey, slots, customBoards, toggleCard]);
  useEffect(
    // Unmount-only: cancel whatever focus handle is still in flight.
    () => () => {
      if (focusFrameRef.current !== undefined) {
        window.cancelAnimationFrame(focusFrameRef.current);
      }
      if (focusFlashTimerRef.current !== undefined) {
        window.clearTimeout(focusFlashTimerRef.current);
      }
      if (focusGiveUpTimerRef.current !== undefined) {
        window.clearTimeout(focusGiveUpTimerRef.current);
      }
    },
    [],
  );

  const onResolveNotification = useCallback(
    (id: string, action: string) => {
      trackEvent("board_card_action", {
        hub_id: hubId,
        kind: "notification",
        slot_key: id,
        action,
      });
      resolveNotification.mutate({
        id,
        action: action as NotificationResolveAction,
      });
    },
    [hubId, resolveNotification],
  );

  // R7: tile × → the same dismiss the expanded card's 不关心 fires.
  const onDismissSlotFromShelf = useCallback(
    (slotKey: string, boardId: string) => {
      trackEvent("board_card_action", {
        hub_id: hubId,
        kind: "shelf",
        slot_key: slotKey,
        action: "dismiss",
      });
      resolve.mutate({ slotKey, action: "dismiss", boardId });
    },
    [hubId, resolve],
  );
  const onDismissNotificationFromShelf = useCallback(
    (id: string) => {
      trackEvent("board_card_action", {
        hub_id: hubId,
        kind: "shelf",
        slot_key: id,
        action: "dismiss",
      });
      dismissNotification.mutate(id);
    },
    [hubId, dismissNotification],
  );

  // Embedded surface stays zero-height until the hub actually has
  // something. The full page always renders — it needs its header,
  // ghost composer, and empty state.
  const embeddedHasContent =
    slots.length > 0 ||
    waiting.length > 0 ||
    highlights.length > 0 ||
    customLiveCount > 0;
  // The page surface must distinguish "nothing to show" from "we
  // couldn't load it" — telling a user their board is quiet during a
  // backend outage is a lie with no retry affordance.
  if (isPage && isError) {
    return (
      <div className="px-0.5 py-6 text-[13px] text-fg-3">
        {t.board.loadFailed}
      </div>
    );
  }
  if (!isPage) {
    if (isPending || isError || !data) return null;
    if (!embeddedHasContent) return null;
  }

  // Content identity for card expansion — see collapsedCards above.
  // Deck expansion follows the GROUP's anchor slot so cycling the deck
  // never collapses the card.
  const slotBySlotKey = new Map(slots.map((s) => [s.slot_key, s]));
  const cardContentKey = (slotKey: string): string => {
    const anchor = slotBySlotKey.get(slotKey);
    return `${slotKey}:${anchor?.content_updated_at ?? ""}`;
  };
  // Embedded surface → ALWAYS the compact tile shelf (R1, 2026-09).
  const collapsedShelf = !isPage;
  // Only claim the board is empty once the slots query has settled —
  // otherwise the empty pitch flashes on every cold load and is then
  // contradicted a beat later by a stack of cards.
  const pageIsEmpty =
    isPage &&
    !isPending &&
    slots.length === 0 &&
    waiting.length === 0 &&
    highlights.length === 0 &&
    customLiveCount === 0 &&
    cookingBoards.length === 0 &&
    pinned.length === 0 &&
    recent.length === 0;

  // System slots render in server order; live same-kind slots collapse
  // into one deck anchored at the group's first member. Terminal slots
  // (receipt strips) always render individually.
  const slotGroups = groupSlotsByKind(slots);
  const groupByAnchor = new Map(slotGroups.map((g) => [g[0].slot_key, g]));
  const groupedMemberKeys = new Set(
    slotGroups.flatMap((g) => g.slice(1).map((s) => s.slot_key)),
  );

  // `anchorKey` — a deck's expand/collapse state follows the GROUP
  // (its first member's key), so cycling never collapses the card.
  const renderSlotEntry = (
    slot: BoardSlot,
    entranceIndex: number,
    deckControls?: ReactNode,
    anchorKey: string = slot.slot_key,
  ) => (
    <div
      key={slot.slot_key}
      data-slot-focus={anchorKey}
      className={
        focusFlashKey === anchorKey
          ? "board-focus-flash rounded-[18px]"
          : undefined
      }
    >
      <BoardSlotEntry
        slot={slot}
        expanded={!collapsedCards.has(cardContentKey(anchorKey))}
        entranceIndex={entranceIndex}
        deckControls={deckControls}
        onToggle={(willOpen) => toggleCard(cardContentKey(anchorKey), willOpen)}
        onResolve={(action) => {
          trackEvent("board_card_action", {
            hub_id: hubId,
            kind: slot.kind,
            slot_key: slot.slot_key,
            action,
          });
          resolve.mutate({ slotKey: slot.slot_key, action });
        }}
        onContinue={() => void cardActions.continueInMemax(slot)}
        onCopy={() => void cardActions.copyForAgent(slot)}
        copied={cardActions.copiedSlotKey === slot.slot_key}
        continuing={cardActions.isContinuing}
        history={
          boardKindTemporality(slot.kind) === "stateful" ? (
            <SlotHistoryDisclosure hubId={hubId} slotKey={slot.slot_key} />
          ) : undefined
        }
      />
    </div>
  );

  let entranceCursor = (waiting.length > 0 ? 1 : 0) + highlights.length;

  return (
    <div className="mb-3 flex flex-col gap-2">
      {/* Section header — the exact SectionHeader "plain" idiom the
          sibling memories sections use (fresh memories via
          DataSectionCard variant="plain"): px-1 row, 14px semibold
          fg-2 label, flex-1 spacer, right-aligned 12px fg-3 trailing
          actions. Inlined (not <SectionHeader>) only because the ✦
          leading mark + adjacent InfoPopover don't fit its
          icon/label/trailing slots; every class matches. */}
      <div className="flex items-center gap-2 px-1">
        <span className="text-[14px] font-semibold text-fg-2">
          <BoardVoiceStar /> {t.board.title}
        </span>
        <InfoPopover
          ariaLabel={t.board.purposeAria}
          title={t.board.title}
          body={t.board.purpose}
        />
        <div className="flex-1" />
        {!isPage ? (
          /* 查看全部 — the ONE trailing action (2026-09: the in-place
             展开 toggle is gone; tiles navigate instead). Arrow is the
             lucide icon, not a literal glyph — same iconography as
             everywhere else in the design system. */
          <Link
            href={pulseHref}
            className="inline-flex items-center gap-1 text-[12px] text-fg-3 transition-colors hover:text-fg-2"
          >
            {t.board.shelfViewAll}
            <ArrowRight className="h-3 w-3" aria-hidden />
          </Link>
        ) : (
          /* 新建板 — creation is CHROME, not content (2026-09: the
             ghost card no longer trails every filtered view). Same
             quiet trailing-action family as the embedded 查看全部;
             tapping mounts the composer at the top of the stream.
             Hidden while the empty state teaches with its own ghost
             card — two creation surfaces at once is one too many. */
          !pageIsEmpty && (
            <button
              type="button"
              onClick={() => setComposerOpen(true)}
              aria-expanded={composerOpen}
              className="inline-flex cursor-pointer items-center gap-1 text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              <Plus className="h-3.5 w-3.5" aria-hidden />
              {t.board.newBoard}
            </button>
          )
        )}
      </div>

      {/* Header-summoned composer — top of the stream, right under
          the chrome that summoned it. Re-keyed per open so a fresh
          summon never resurrects an abandoned draft. */}
      {isPage && composerOpen && !pageIsEmpty ? (
        <BoardGhostCard
          key={`summon-${ghostEpoch}`}
          initialComposing
          onCloseSurface={() => setComposerOpen(false)}
          pending={createBoard.isPending}
          prefill={null}
          onPrefillConsumed={() => {}}
          onCreate={(input) => {
            createBoard.mutate(input, {
              onSuccess: () => {
                setComposerOpen(false);
                setGhostEpoch((n) => n + 1);
              },
            });
          }}
        />
      ) : null}

      {collapsedShelf ? (
        <BoardShelf
          waiting={waiting}
          highlights={highlights}
          slots={slots}
          customBoards={customBoards}
          cookingBoards={cookingBoards}
          onOpenDeck={() => router.push(pulseHref)}
          onOpenSlot={(slotKey) => {
            // R3 (2026-09): tiles navigate to the pulse surface with
            // this card focused — the receiving effect above scrolls,
            // un-collapses, and flashes it.
            router.push(`${pulseHref}?focus=${encodeURIComponent(slotKey)}`);
          }}
          onOpenBoards={() => router.push(pulseHref)}
          onDismissSlot={onDismissSlotFromShelf}
          onDismissNotification={onDismissNotificationFromShelf}
        />
      ) : null}

      {/* Onboarding super-notifs — the highest-priority thing a brand
          new user can act on, so they sit above even 等你. Page-only:
          /memories already mounts PinnedNotifications in its hero. */}
      {pinned.map((notification) => (
        <PinnedDispatch key={notification.id} notification={notification} />
      ))}

      {/* ── C5 kind chips — page only, and only when there's more
          than one lens live. Selecting a chip focuses the stream on
          that kind (notifications step back too — a filter is a focus
          mode); each chip carries its kind's freshest update age. ── */}
      {isPage && (kindChips.length > 1 || kindFilter !== null) ? (
        <div className="flex flex-wrap items-center gap-1.5 px-1">
          <button
            type="button"
            onClick={() => setKindFilter(null)}
            aria-pressed={kindFilter === null}
            className={`cursor-pointer rounded-full px-2.5 py-1 text-[12px] font-medium transition-colors ${
              kindFilter === null
                ? "bg-surface-3 text-foreground"
                : "text-fg-3 hover:bg-surface-1 hover:text-fg-2"
            }`}
          >
            {t.board.kindFilterAll}
          </button>
          {kindChips.map(([kind, info]) => (
            <button
              key={kind}
              type="button"
              onClick={() =>
                setKindFilter((prev) => (prev === kind ? null : kind))
              }
              aria-pressed={kindFilter === kind}
              className={`cursor-pointer rounded-full px-2.5 py-1 text-[12px] font-medium transition-colors ${
                kindFilter === kind
                  ? "bg-surface-3 text-foreground"
                  : "text-fg-3 hover:bg-surface-1 hover:text-fg-2"
              }`}
            >
              {boardKindStripSummary(info.sample, t).label}
              {info.latest ? (
                <span className="ml-1.5 text-[11px] font-normal text-fg-4">
                  {formatAge(info.latest, t, interpolate)}
                </span>
              ) : null}
            </button>
          ))}
        </div>
      ) : null}

      {/* ── 等你 — decisions merged from notifications (P4), rendered
          as a DECK: one card at a time, the pile counted behind it.
          Never a vertical list of N contradiction cards. ── */}
      {!collapsedShelf && kindFilter === null
        ? groupWaitingByKind(waiting).map((group) => (
            <BoardNotificationDeck
              key={group[0].kind}
              cards={group}
              countLabel={interpolate(t.board.deckMore, {
                n: group.length - 1,
              })}
              disabled={resolveNotification.isPending}
              onResolve={onResolveNotification}
            />
          ))
        : null}

      {/* ── Highlights — high-signal news (a member joined), each a
          standalone card between the decisions and the slots. ── */}
      {!collapsedShelf && kindFilter === null
        ? highlights.map((card, index) => (
            <BoardHighlightCard
              key={card.id}
              card={card}
              entranceIndex={(waiting.length > 0 ? 1 : 0) + index}
              disabled={dismissNotification.isPending}
              onDismiss={(id) => dismissNotification.mutate(id)}
              onHideHub={onHideHub}
            />
          ))
        : null}

      {/* ── System board slots — live same-kind groups deck up with a
          ↻ cycle; receipts stay individual strips. ── */}
      {!collapsedShelf &&
        slots.map((slot) => {
          const isLive = slot.state === "fresh" || slot.state === "seen";
          // Terminal cards leave the live flow entirely — they live in
          // the 已归档 section below (工单 8: dismiss = archive with
          // undo, not a grey strip forever holding its place in line).
          if (!isLive) return null;
          if (kindFilter !== null && slot.kind !== kindFilter) return null;
          if (groupedMemberKeys.has(slot.slot_key)) return null;
          const group = groupByAnchor.get(slot.slot_key);
          const entranceIndex = entranceCursor++;
          if (group && group.length > 1) {
            return (
              <BoardSlotDeck
                key={slot.slot_key}
                group={group}
                countLabel={interpolate(t.board.stackCount, {
                  n: group.length - 1,
                })}
                cycleAriaLabel={t.board.deckCycle}
              >
                {(current, controls) =>
                  renderSlotEntry(
                    current,
                    entranceIndex,
                    controls,
                    group[0].slot_key,
                  )
                }
              </BoardSlotDeck>
            );
          }
          return renderSlotEntry(slot, entranceIndex);
        })}

      {/* ── Custom-board cards — the unified stream (2026-08): live
          cards after the system board's, each tagged with its board
          title; same-kind slots on one board deck together; cooking
          boards as one compact card each. ── */}
      {!collapsedShelf &&
        customLiveDecks
          .filter(
            ({ group }) => kindFilter === null || group[0].kind === kindFilter,
          )
          .map(({ board, group }) => {
            const entranceIndex = entranceCursor++;
            if (group.length > 1) {
              return (
                <BoardSlotDeck
                  key={`${board.id}-${group[0].slot_key}`}
                  group={group}
                  countLabel={interpolate(t.board.stackCount, {
                    n: group.length - 1,
                  })}
                  cycleAriaLabel={t.board.deckCycle}
                >
                  {(current, controls) => (
                    <div
                      key={`${board.id}-${current.slot_key}`}
                      data-slot-focus={group[0].slot_key}
                      className={
                        focusFlashKey === group[0].slot_key
                          ? "board-focus-flash rounded-[18px]"
                          : undefined
                      }
                    >
                      <CustomBoardSlotCard
                        board={board}
                        slot={current}
                        entranceIndex={entranceIndex}
                        deletePending={deleteBoard.isPending}
                        onDelete={(boardId) => deleteBoard.mutate(boardId)}
                        deckControls={controls}
                      />
                    </div>
                  )}
                </BoardSlotDeck>
              );
            }
            return (
              <div
                key={`${board.id}-${group[0].slot_key}`}
                data-slot-focus={group[0].slot_key}
                className={
                  focusFlashKey === group[0].slot_key
                    ? "board-focus-flash rounded-[18px]"
                    : undefined
                }
              >
                <CustomBoardSlotCard
                  board={board}
                  slot={group[0]}
                  entranceIndex={entranceIndex}
                  deletePending={deleteBoard.isPending}
                  onDelete={(boardId) => deleteBoard.mutate(boardId)}
                />
              </div>
            );
          })}
      {!collapsedShelf &&
        cookingBoards.map((board) => (
          <CookingBoardCard
            key={board.id}
            board={board}
            entranceIndex={entranceCursor++}
            deletePending={deleteBoard.isPending}
            onDelete={(boardId) => deleteBoard.mutate(boardId)}
          />
        ))}

      {/* ── 已归档 — resolved/dismissed cards, out of the live flow but
          one tap from coming back (工单 8: dismiss is archive + undo,
          never data loss). Collapsed to a count until opened. ── */}
      {!collapsedShelf && kindFilter === null && archivedSlots.length > 0 ? (
        <BoardArchivedSection
          slots={archivedSlots}
          pending={resolve.isPending}
          onRestore={(slotKey) => {
            trackEvent("board_card_action", {
              hub_id: hubId,
              slot_key: slotKey,
              action: "reopen",
            });
            resolve.mutate({ slotKey, action: "reopen" });
          }}
        />
      ) : null}

      {pageIsEmpty ? (
        <BoardEmptyState
          onPickExample={(example) => {
            // Chips feed the ghost composer below — same in-place
            // morph, prefilled with the example's full instruction.
            setComposerPrefill(example);
          }}
        />
      ) : null}

      {/* ── The ghost card — EMPTY BOARD ONLY (2026-09: it used to
          close every card stream, which put a creation prompt at the
          end of each filtered view; creation now lives in the header
          + and this stays as the empty state's teaching centerpiece,
          prefilled by the example chips above). ── */}
      {isPage && pageIsEmpty ? (
        <BoardGhostCard
          key={ghostEpoch}
          pending={createBoard.isPending}
          prefill={composerPrefill}
          onPrefillConsumed={() => setComposerPrefill(null)}
          onCreate={(input) => {
            createBoard.mutate(input, {
              onSuccess: () => {
                // The new board's cooking card takes this spot; the
                // ghost re-forms behind it.
                setComposerPrefill(null);
                setGhostEpoch((n) => n + 1);
              },
            });
          }}
        />
      ) : null}

      {/* ── 最近 — the receipts the retired inbox used to hold. Always
          collapsed by default: nothing here needs a decision. ── */}
      {isPage && recent.length > 0 ? (
        <div className="mt-1 flex flex-col gap-1">
          <BoardSlotStrip
            label={t.board.recentTitle}
            detail={pluralize(
              t.board.recentDetailOne,
              t.board.recentDetail,
              recent.length,
            )}
            open={recentOpen}
            onToggle={() => setRecentOpen((open) => !open)}
            className="opacity-80"
          />
          {recentOpen ? (
            <div className="animate-fade-up divide-y divide-border/20 rounded-[14px] border border-border/40">
              {recent.map((card) => (
                <BoardRecentRow
                  key={card.id}
                  card={card}
                  disabled={dismissNotification.isPending}
                  onDismiss={(id) => dismissNotification.mutate(id)}
                  onHideHub={onHideHub}
                />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      {/* Restore muted hubs — outside the recent-strip conditional so
          it stays reachable even when hiding emptied the strip. */}
      {isPage && isPersonalHub && hiddenPulseHubIds.length > 0 ? (
        <button
          type="button"
          onClick={onRestoreHiddenHubs}
          disabled={updateSettings.isPending}
          className="mt-1 self-start px-1 text-[11.5px] text-fg-4 transition-colors hover:text-fg-2"
        >
          {pluralize(
            t.board.hiddenHubsRestoreOne,
            t.board.hiddenHubsRestore,
            hiddenPulseHubIds.length,
          )}
        </button>
      ) : null}
    </div>
  );
}

/**
 * BoardSection — mounts inside TopicGrid's content column, below the
 * hub header and pinned notifications (the parent provides the
 * max-width column and horizontal padding). Renders nothing — zero
 * height — until the hub has cards or a pending decision, so card-less
 * hubs keep the exact pre-board layout.
 */
export function BoardSection() {
  const { hubFilter } = useActiveHub();
  if (!hubFilter) return null;
  return <BoardView hubId={hubFilter} surface="section" />;
}

/**
 * BoardPage — the `/h/<slug>/pulse` route body. Same board, full
 * surface: the unified stream, the ghost new-board card, and the 最近
 * strip.
 *
 * `hubId` comes from the route (resolved from the URL slug). It stays
 * optional so a caller without a slug in hand — the bare `/pulse`
 * forwarder's ancestors, tests, `/dev/kitchen` — still gets the active
 * hub's board.
 */
export function BoardPage({ hubId }: { hubId?: string }) {
  const { hubFilter } = useActiveHub();
  const resolvedHubId = hubId ?? hubFilter;
  if (!resolvedHubId) return null;
  return <BoardView hubId={resolvedHubId} surface="page" />;
}

/**
 * BoardSlotDeck — same-kind live slots as one deck: ghost-stack edges
 * behind the current card, a depth pill + ↻ cycle in the card's
 * corner. Cycling is pure client state (no server call) — the pile is
 * browsed, not consumed. Exported for tests.
 */
export function BoardSlotDeck({
  group,
  countLabel,
  cycleAriaLabel,
  children,
}: {
  group: readonly BoardSlot[];
  countLabel: string;
  cycleAriaLabel: string;
  children: (slot: BoardSlot, deckControls?: ReactNode) => ReactNode;
}) {
  const [cursor, setCursor] = useState(0);
  if (group.length === 0) return null;
  const current = group[cursor % group.length];
  const controls =
    group.length > 1 ? (
      <BoardDeckControls
        countLabel={countLabel}
        onCycle={() => setCursor((i) => (i + 1) % group.length)}
        cycleAriaLabel={cycleAriaLabel}
      />
    ) : undefined;
  return (
    <BoardDeckShell depth={group.length - 1}>
      {children(current, controls)}
    </BoardDeckShell>
  );
}

function BoardSlotEntry({
  slot,
  expanded,
  entranceIndex,
  deckControls,
  onToggle,
  onResolve,
  onContinue,
  onCopy,
  copied,
  continuing,
  history,
}: {
  slot: BoardSlot;
  expanded: boolean;
  entranceIndex: number;
  /** Same-kind stack pill + ↻ cycle when this entry fronts a deck. */
  deckControls?: ReactNode;
  onToggle: (willOpen: boolean) => void;
  onResolve: (action: BoardResolveAction) => void;
  onContinue: () => void;
  onCopy: () => void;
  copied: boolean;
  continuing: boolean;
  /** 历史 disclosure — provided only for stateful kinds (工单 8). */
  history?: ReactNode;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  if (!expanded) {
    // Collapsed band: one line — kind name + per-kind summary. Resolved
    // receipts also live here so they stop costing vertical space.
    const terminal = slot.state === "resolved" || slot.state === "dismissed";
    return (
      <BoardSlotStrip
        label={boardKindStripSummary(slot, t).label}
        detail={
          terminal
            ? slot.resolution?.action === "dismiss"
              ? t.board.receiptDismissed
              : t.board.receiptAcked
            : boardKindStripSummary(slot, t).detail
        }
        open={false}
        onToggle={() => onToggle(true)}
        className={terminal ? "opacity-70" : undefined}
      />
    );
  }

  const options = boardKindOptions(slot.kind);
  const purpose = boardKindPurpose(slot.kind, t);
  // Receipts carry the RESOLVED time; the timestamp line separately
  // carries the generated-at time.
  const receiptLabel =
    slot.resolution?.action === "dismiss"
      ? t.board.receiptDismissed
      : t.board.receiptAcked;
  const receipt = slot.resolution
    ? `${receiptLabel} · ${formatAge(slot.resolution.resolved_at, t, interpolate)}`
    : receiptLabel;
  return (
    <BoardCard
      state={slot.state}
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      timestamp={formatAge(slotContentTime(slot), t, interpolate)}
      live={
        <BoardActionRow>
          {!options?.hideDefaultActions ? (
            <>
              <BoardAction emphasis="primary" onClick={() => onResolve("ack")}>
                {options?.actions?.ack?.(t) ?? t.board.actionAck}
              </BoardAction>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("dismiss")}
              >
                {options?.actions?.dismiss?.(t) ?? t.board.actionDismiss}
              </BoardAction>
            </>
          ) : null}
          {/* 续接 — the one secondary verb that earns a place on the
              row: acting on the card without retyping its context.
              Only offered when the card has citations. */}
          {(slot.cite_memory_ids?.length ?? 0) > 0 ? (
            <BoardAction
              emphasis="quiet"
              disabled={continuing}
              onClick={onContinue}
            >
              {t.board.continueInMemax}
            </BoardAction>
          ) : null}
          {/* The overflow holds the ONE occasional verb left —
              copy-for-agent. The 准/不准 feedback pair was cut
              (founder, 2026-09: options the product doesn't visibly
              act on yet are noise, not affordances). */}
          {buildBoardCardContext(slot) ? (
            <ActionMenu
              triggerAriaLabel={t.board.moreActions}
              items={[
                {
                  id: "copy-for-agent",
                  label: copied ? t.board.copied : t.board.copyForAgent,
                  onSelect: onCopy,
                },
              ]}
            />
          ) : null}
          <BoardAction
            emphasis="quiet"
            className="ml-auto"
            onClick={() => onToggle(false)}
          >
            {t.board.collapse}
          </BoardAction>
        </BoardActionRow>
      }
      receipt={receipt}
    >
      {purpose || deckControls ? (
        <div className="float-right ml-2 flex items-center gap-1.5">
          {deckControls}
          {purpose ? (
            <InfoPopover
              ariaLabel={t.board.purposeAria}
              title={t.board.title}
              body={purpose}
              side="left"
            />
          ) : null}
        </div>
      ) : null}
      {renderBoardSlotBody(slot)}
      {history}
    </BoardCard>
  );
}

/**
 * SlotHistoryDisclosure — the 历史 affordance on stateful cards. A
 * quiet toggle; open fetches the slot's archived versions (lazy — the
 * board GET stays one request) and lists them newest-first. Old
 * versions are read-only context, not cards: title + date only.
 */
function SlotHistoryDisclosure({
  hubId,
  slotKey,
}: {
  hubId: string | undefined;
  slotKey: string;
}) {
  const { t, locale } = useLocale();
  const [open, setOpen] = useState(false);
  const { data, isLoading } = useSlotHistory(hubId, slotKey, open);
  const versions = data?.versions ?? [];
  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="cursor-pointer text-[12px] font-medium text-fg-3 transition-colors hover:text-fg-2"
      >
        {t.board.history}
      </button>
      {open ? (
        isLoading ? (
          <p className="mt-1.5 text-[12.5px] text-fg-3">…</p>
        ) : versions.length === 0 ? (
          <p className="mt-1.5 text-[12.5px] text-fg-3">
            {t.board.historyEmpty}
          </p>
        ) : (
          <ul className="mt-1.5 flex flex-col gap-1">
            {versions.map((v) => (
              <li
                key={v.id}
                className="flex items-baseline gap-2 text-[12.5px]"
              >
                <span className="shrink-0 tabular-nums text-fg-3">
                  {new Date(v.content_produced_at).toLocaleDateString(
                    locale === "zh" ? "zh-CN" : "en-US",
                    { month: "short", day: "numeric" },
                  )}
                </span>
                <span className="min-w-0 truncate text-fg-2">{v.title}</span>
              </li>
            ))}
          </ul>
        )
      ) : null}
    </div>
  );
}

/**
 * BoardArchivedSection — the 已归档 read surface (工单 8). Terminal
 * cards leave the live flow and land here: a count header, collapsed
 * by default, each row restorable. Dismiss stops being a one-way
 * door without keeping grey strips in the middle of the board.
 */
export function BoardArchivedSection({
  slots,
  pending,
  onRestore,
}: {
  slots: BoardSlot[];
  pending: boolean;
  onRestore: (slotKey: string) => void;
}) {
  const { t } = useLocale();
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col gap-1.5">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex cursor-pointer items-center gap-1.5 px-1 text-[12px] font-medium text-fg-3 transition-colors hover:text-fg-2"
      >
        <span>{t.board.archivedSection}</span>
        <span className="tabular-nums">{slots.length}</span>
      </button>
      {open
        ? slots.map((slot) => (
            <div
              key={slot.slot_key}
              className="flex items-center gap-2 rounded-[14px] border border-border/40 px-4 py-2.5 opacity-80"
            >
              <span className="min-w-0 flex-1 truncate text-[12.5px] text-fg-2">
                {boardKindStripSummary(slot, t).label}
                {slot.title ? ` · ${slot.title}` : ""}
              </span>
              <span className="shrink-0 text-[12px] text-fg-3">
                {slot.resolution?.action === "dismiss"
                  ? t.board.receiptDismissed
                  : t.board.receiptAcked}
              </span>
              <button
                type="button"
                disabled={pending}
                onClick={() => onRestore(slot.slot_key)}
                className="shrink-0 cursor-pointer text-[12px] font-medium text-fg-3 transition-colors hover:text-fg-1 disabled:opacity-50"
              >
                {t.board.restore}
              </button>
            </div>
          ))
        : null}
    </div>
  );
}
