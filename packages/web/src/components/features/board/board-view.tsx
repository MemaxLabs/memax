"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { BoardFeedbackVerdict, BoardSlot } from "memax-sdk";
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
} from "@/hooks/use-board";
import {
  useNotificationDismiss,
  useResolveNotification,
} from "@/hooks/use-notifications";
import type { NotificationResolveAction } from "memax-sdk";
import { useBoardCardActions } from "@/hooks/use-board-continue";
import { PinnedDispatch } from "@/components/features/onboarding/onboarding-pinned";
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
  renderBoardSlotBody,
  slotContentTime,
} from "./board-kind-registry";
// Side-effect import: registers the Lane A + Lane B kind renderers
// before the first render so no card flashes through the fallback.
import "./board-kinds";

type BoardResolveAction = "ack" | "dismiss" | "feedback";

/** Per-hub sessionStorage key for the embedded shelf's expand state. */
function shelfStorageKey(hubId: string) {
  return `memax-board-shelf-expanded-${hubId}`;
}

/**
 * BOARD_SHELF_RULES — the codified collapse/expand + dismiss ruleset
 * for the embedded board (founder spec, 2026-08). Every behavior below
 * is implemented by useShelfExpansion + the shelf/card handlers; when
 * changing one, change both.
 *
 *   R1  The shelf is COLLAPSED by default on every fresh visit.
 *   R2  Auto-expand ONLY when something needs the user: a pending 等你
 *       decision or a fresh highlight. Ambient intelligence (slots)
 *       never auto-expands the shelf.
 *   R3  Tile tap = expand the shelf in place AND open that card.
 *   R4  Resolving a card while expanded swaps it to a receipt INLINE —
 *       no reflow jump, nothing disappears.
 *   R5  When the LAST live card resolves, the shelf auto-collapses
 *       back after a beat (~800ms, spring) — the board exhales.
 *   R6  Manual 展开/收起 always overrides R2/R5 and persists per hub
 *       for the SESSION (sessionStorage; a fresh visit re-applies R1).
 *   R7  Every live tile/card has a quiet dismiss: tiles via the
 *       hover/long-press ×, expanded cards via the 不关心 verb. Both
 *       call resolve action="dismiss" (slots) or the notification
 *       dismiss (highlights), optimistically — the tile leaves the
 *       shelf immediately and lives on only as a receipt. Decision
 *       tiles (等你) are the exception: a decision needs an answer,
 *       and the server refuses plain dismiss on decision kinds.
 */
export const BOARD_SHELF_RULES = [
  "collapsed-by-default",
  "auto-expand-only-on-decision-or-highlight",
  "tile-tap-expands-and-opens",
  "resolve-swaps-to-receipt-inline",
  "auto-collapse-after-last-resolve",
  "manual-toggle-overrides-and-persists-per-session",
  "tiles-dismiss-optimistically-except-decisions",
] as const;

/**
 * useShelfExpansion — the state machine behind BOARD_SHELF_RULES R1,
 * R2, R5 and R6. Exported for tests.
 *
 * `manual` means the CURRENT state was chosen by the user (toggle or
 * tile tap) this session — automatic transitions only ever apply on
 * top of the default state, never over a user choice.
 */
export function useShelfExpansion({
  hubId,
  enabled,
  needsAttention,
  liveCount,
}: {
  hubId: string;
  /** False on the /pulse page — the full surface never collapses. */
  enabled: boolean;
  /** R2: a pending 等你 decision or a fresh highlight exists. */
  needsAttention: boolean;
  /** R5: live (unresolved) cards across all bands. */
  liveCount: number;
}) {
  const [expanded, setExpandedState] = useState(false);
  const [manual, setManual] = useState(false);
  // The auto rules (R2/R5) must not race the sessionStorage read —
  // they only engage once the stored choice (or its absence) is known,
  // or a stored 收起 would be overridden by auto-expand on remount.
  const [hydrated, setHydrated] = useState(false);

  // R1 + R6: default collapsed; a stored per-session choice wins.
  // sessionStorage is read in an effect (not the initializer) so the
  // SSR and first client render agree.
  useEffect(() => {
    if (!enabled) return;
    try {
      const stored = globalThis.sessionStorage?.getItem(shelfStorageKey(hubId));
      if (stored === "1" || stored === "0") {
        setExpandedState(stored === "1");
        setManual(true);
      } else {
        setExpandedState(false);
        setManual(false);
      }
    } catch {
      setExpandedState(false);
      setManual(false);
    }
    setHydrated(true);
  }, [hubId, enabled]);

  /** Manual toggle / tile tap — R3, R6. */
  const setExpanded = useCallback(
    (next: boolean) => {
      setExpandedState(next);
      setManual(true);
      try {
        globalThis.sessionStorage?.setItem(
          shelfStorageKey(hubId),
          next ? "1" : "0",
        );
      } catch {
        // Private mode / quota — choice holds in-memory.
      }
    },
    [hubId],
  );

  // R2: auto-expand only for things blocked on the user.
  useEffect(() => {
    if (!enabled || !hydrated || manual || !needsAttention) return;
    setExpandedState(true);
  }, [enabled, hydrated, manual, needsAttention]);

  // R5: last live card resolved → collapse back after a beat.
  const prevLiveCount = useRef(liveCount);
  useEffect(() => {
    const prev = prevLiveCount.current;
    prevLiveCount.current = liveCount;
    if (!enabled || !hydrated || manual) return;
    if (prev > 0 && liveCount === 0) {
      const timer = window.setTimeout(() => setExpandedState(false), 800);
      return () => window.clearTimeout(timer);
    }
  }, [enabled, hydrated, manual, liveCount]);

  return { expanded, setExpanded };
}

/**
 * Where the board is mounted.
 *
 *   - "section" — embedded in the memories page under the hub header.
 *     Collapsed by default into a ONE-ROW horizontal tile shelf
 *     (BoardShelf); a header toggle expands it in place to the full
 *     vertical card layout, per BOARD_SHELF_RULES. Stays zero-height
 *     when the hub has nothing, so card-less hubs keep the exact
 *     pre-board layout. No composer, no receipts strip: those belong
 *     to the full surface, one click away via 查看全部.
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
 * only the FIRST live card renders expanded (the hero); every other
 * slot collapses to a one-line SlotStrip that expands on tap and can
 * be collapsed again. Same-kind live slots render as ONE deck with a
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
  const createBoard = useCreateBoard(hubId);
  const deleteBoard = useDeleteBoard(hubId);

  const [openSlots, setOpenSlots] = useState<ReadonlySet<string>>(new Set());
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

  // Embedded shelf expansion — BOARD_SHELF_RULES R1/R2/R5/R6.
  const { expanded: shelfExpanded, setExpanded: setShelfExpanded } =
    useShelfExpansion({
      hubId,
      enabled: !isPage,
      needsAttention: waiting.length > 0 || highlights.length > 0,
      liveCount:
        waiting.length + highlights.length + liveSlots.length + customLiveCount,
    });

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

  const toggleSlot = useCallback(
    (slotKey: string, willOpen: boolean) => {
      setOpenSlots((prev) => {
        const next = new Set(prev);
        if (willOpen) {
          next.add(slotKey);
        } else {
          next.delete(slotKey);
        }
        return next;
      });
      if (willOpen) {
        trackEvent("board_card_expand", { hub_id: hubId, slot_key: slotKey });
      }
    },
    [hubId],
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

  const heroKey = liveSlots[0]?.slot_key;
  // Embedded surface, not yet expanded → the compact tile shelf.
  const collapsedShelf = !isPage && !shelfExpanded;
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
    <BoardSlotEntry
      key={slot.slot_key}
      slot={slot}
      expanded={anchorKey === heroKey || openSlots.has(anchorKey)}
      entranceIndex={entranceIndex}
      deckControls={deckControls}
      onToggle={(willOpen) => toggleSlot(anchorKey, willOpen)}
      onResolve={(action, verdict) => {
        trackEvent("board_card_action", {
          hub_id: hubId,
          kind: slot.kind,
          slot_key: slot.slot_key,
          action,
          verdict,
        });
        resolve.mutate({ slotKey: slot.slot_key, action, verdict });
      }}
      onContinue={() => void cardActions.continueInMemax(slot)}
      onCopy={() => void cardActions.copyForAgent(slot)}
      copied={cardActions.copiedSlotKey === slot.slot_key}
      continuing={cardActions.isContinuing}
    />
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
          <>
            {/* 展开/收起 — the shelf expands IN PLACE; the full pulse
                surface stays one click away via 查看全部. Trailing
                actions match the fresh-memories header's verbs
                (Select / filter): 12px fg-3 → fg-2 text buttons. */}
            <button
              type="button"
              onClick={() => setShelfExpanded(!shelfExpanded)}
              className="cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              {shelfExpanded ? t.board.shelfCollapse : t.board.shelfExpand}
            </button>
            <Link
              href={pulseHref}
              className="text-[12px] text-fg-3 transition-colors hover:text-fg-2"
            >
              {t.board.shelfViewAll}
            </Link>
          </>
        ) : null}
      </div>

      {collapsedShelf ? (
        <BoardShelf
          waiting={waiting}
          highlights={highlights}
          slots={slots}
          customBoards={customBoards}
          cookingBoards={cookingBoards}
          onOpenDeck={() => setShelfExpanded(true)}
          onOpenSlot={(slotKey) => {
            // R3: remember the tapped card so it renders expanded once
            // the full layout unfolds.
            setOpenSlots((prev) => new Set(prev).add(slotKey));
            setShelfExpanded(true);
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

      {/* ── 等你 — decisions merged from notifications (P4), rendered
          as a DECK: one card at a time, the pile counted behind it.
          Never a vertical list of N contradiction cards. ── */}
      {!collapsedShelf
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
      {!collapsedShelf
        ? highlights.map((card, index) => (
            <BoardHighlightCard
              key={card.id}
              card={card}
              entranceIndex={(waiting.length > 0 ? 1 : 0) + index}
              disabled={dismissNotification.isPending}
              onDismiss={(id) => dismissNotification.mutate(id)}
            />
          ))
        : null}

      {/* ── System board slots — live same-kind groups deck up with a
          ↻ cycle; receipts stay individual strips. ── */}
      {!collapsedShelf &&
        slots.map((slot) => {
          const isLive = slot.state === "fresh" || slot.state === "seen";
          if (isLive && groupedMemberKeys.has(slot.slot_key)) return null;
          const group = isLive ? groupByAnchor.get(slot.slot_key) : undefined;
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
        customLiveDecks.map(({ board, group }) => {
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
                  <CustomBoardSlotCard
                    key={`${board.id}-${current.slot_key}`}
                    board={board}
                    slot={current}
                    entranceIndex={entranceIndex}
                    deletePending={deleteBoard.isPending}
                    onDelete={(boardId) => deleteBoard.mutate(boardId)}
                    deckControls={controls}
                  />
                )}
              </BoardSlotDeck>
            );
          }
          return (
            <CustomBoardSlotCard
              key={`${board.id}-${group[0].slot_key}`}
              board={board}
              slot={group[0]}
              entranceIndex={entranceIndex}
              deletePending={deleteBoard.isPending}
              onDelete={(boardId) => deleteBoard.mutate(boardId)}
            />
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

      {pageIsEmpty ? (
        <BoardEmptyState
          onPickExample={(example) => {
            // Chips feed the ghost composer below — same in-place
            // morph, prefilled with the example's full instruction.
            setComposerPrefill(example);
          }}
        />
      ) : null}

      {/* ── The ghost card — the latent new-board affordance, always
          closing the card stream on the full surface. ── */}
      {isPage ? (
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
                />
              ))}
            </div>
          ) : null}
        </div>
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
}: {
  slot: BoardSlot;
  expanded: boolean;
  entranceIndex: number;
  /** Same-kind stack pill + ↻ cycle when this entry fronts a deck. */
  deckControls?: ReactNode;
  onToggle: (willOpen: boolean) => void;
  onResolve: (
    action: BoardResolveAction,
    verdict?: BoardFeedbackVerdict,
  ) => void;
  onContinue: () => void;
  onCopy: () => void;
  copied: boolean;
  continuing: boolean;
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
          {options?.feedback ? (
            // 准/不准 — only on synthesized kinds, where the card makes
            // a claim that can be right or wrong. The verdict feeds the
            // next synthesis run.
            <>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("feedback", "accurate")}
              >
                {t.board.feedbackAccurate}
              </BoardAction>
              <BoardAction
                emphasis="quiet"
                onClick={() => onResolve("feedback", "inaccurate")}
              >
                {t.board.feedbackInaccurate}
              </BoardAction>
            </>
          ) : null}
          {/* 续接 — a card is where memax noticed something; these
              two verbs are how the user acts on it without retyping
              the context. Only offered when the card has citations
              (continue) or quotable substance (copy). */}
          {(slot.cite_memory_ids?.length ?? 0) > 0 ? (
            <BoardAction
              emphasis="quiet"
              disabled={continuing}
              onClick={onContinue}
            >
              {t.board.continueInMemax}
            </BoardAction>
          ) : null}
          {buildBoardCardContext(slot) ? (
            <BoardAction emphasis="quiet" onClick={onCopy}>
              {copied ? t.board.copied : t.board.copyForAgent}
            </BoardAction>
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
    </BoardCard>
  );
}
