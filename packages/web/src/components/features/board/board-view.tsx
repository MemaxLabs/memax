"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { BoardFeedbackVerdict, BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardSlotStrip,
  BoardVoiceStar,
  InfoPopover,
} from "@memaxlabs/ui";
import { pluralize, useLocale } from "@/i18n";
import { useActiveHub, useAuth } from "@/lib/auth";
import { trackEvent } from "@/lib/posthog";
import {
  useCreateBoard,
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
  BoardNotificationCard,
  BoardRecentRow,
  useBoardNotificationCards,
} from "./board-notification-cards";
import {
  BoardComposer,
  BoardCookingReceipt,
  BoardTabs,
  CustomBoardView,
  NewBoardButton,
} from "./board-custom-boards";
import {
  boardKindOptions,
  boardKindPurpose,
  boardKindStripSummary,
  renderBoardSlotBody,
} from "./board-kind-registry";
// Side-effect import: registers the Lane A + Lane B kind renderers
// before the first render so no card flashes through the fallback.
import "./board-kinds";

type BoardResolveAction = "ack" | "dismiss" | "feedback";

/**
 * Where the board is mounted.
 *
 *   - "section" — embedded in the memories page under the hub header.
 *     Stays zero-height when the hub has nothing, so card-less hubs
 *     keep the exact pre-board layout. No board tabs, no composer, no
 *     receipts strip: those belong to the full surface.
 *   - "page"    — the standalone /pulse route. The one surface: 等你
 *     decisions, the system board's cards, custom boards, and the
 *     collapsed 最近 receipts strip that absorbed the retired inbox.
 */
export type BoardSurface = "section" | "page";

/**
 * BoardView — the pulse board host (plan 25). The layout answer to
 * "cards eat the page": banding. The 等你 band (decisions merged from
 * notifications — plan 25 P4) comes first because it is the only band
 * that is actually blocked on the user. Then the system board's slots:
 * only the FIRST live card renders expanded (the hero); every other
 * slot collapses to a one-line SlotStrip that expands on tap and can
 * be collapsed again. Resolved receipts always render as strips.
 * Finally the 最近 strip — things that already happened.
 */
export function BoardView({
  hubId,
  surface = "section",
}: {
  hubId: string;
  surface?: BoardSurface;
}) {
  const { t } = useLocale();
  const isPage = surface === "page";
  const { hubs } = useAuth();
  // Personal-hub detection is by the board's OWN hub, not the active
  // hub context: user-scoped rows (invites, ownership transfers, the
  // onboarding checklist) land on the personal board, and mis-deriving
  // this would leak them onto a team board.
  const isPersonalHub = useMemo(() => {
    const entry = hubs.find((h) => h.hub.id === hubId);
    return entry ? entry.hub.hub_type !== "team" : false;
  }, [hubs, hubId]);

  const { data, isPending, isError } = useHubBoard(hubId);
  const { data: boardsData } = useHubBoards(isPage ? hubId : undefined);
  const resolve = useResolveBoardSlot(hubId);
  const cardActions = useBoardCardActions(hubId);
  const notifications = useBoardNotificationCards(hubId, isPersonalHub);
  const resolveNotification = useResolveNotification();
  const dismissNotification = useNotificationDismiss();
  const createBoard = useCreateBoard(hubId);
  const deleteBoard = useDeleteBoard(hubId);

  const [openSlots, setOpenSlots] = useState<ReadonlySet<string>>(new Set());
  const [recentOpen, setRecentOpen] = useState(false);
  const [composerOpen, setComposerOpen] = useState(false);
  const [cookingTitle, setCookingTitle] = useState<string | null>(null);
  const [selectedBoardId, setSelectedBoardId] = useState<string | null>(null);

  const boards = useMemo(() => boardsData?.boards ?? [], [boardsData]);
  const systemBoardId = data?.board.id ?? null;
  // Selection falls back to the system board whenever the selected id
  // no longer exists (deleted board, hub switch).
  const activeBoardId =
    selectedBoardId && boards.some((b) => b.id === selectedBoardId)
      ? selectedBoardId
      : systemBoardId;
  const activeCustomBoard =
    activeBoardId && activeBoardId !== systemBoardId
      ? (boards.find((b) => b.id === activeBoardId) ?? null)
      : null;

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

  const slots = data?.slots ?? [];
  const waiting = notifications.waiting;
  const recent = notifications.recent;
  const pinned = isPage ? notifications.pinned : [];

  // Embedded surface stays zero-height until the hub actually has
  // something. The full page always renders — it needs its header,
  // composer, and empty state.
  const embeddedHasContent = slots.length > 0 || waiting.length > 0;
  if (!isPage) {
    if (isPending || isError || !data) return null;
    if (!embeddedHasContent) return null;
  }

  const liveSlots = slots.filter(
    (s) => s.state === "fresh" || s.state === "seen",
  );
  const heroKey = liveSlots[0]?.slot_key;
  const showSlots = !activeCustomBoard;
  // Only claim the board is empty once the slots query has settled —
  // otherwise "the board is quiet" flashes on every cold load and is
  // then contradicted a beat later by a stack of cards.
  const pageIsEmpty =
    isPage &&
    !isPending &&
    !activeCustomBoard &&
    slots.length === 0 &&
    waiting.length === 0 &&
    pinned.length === 0 &&
    recent.length === 0 &&
    !composerOpen;

  return (
    <div className="mb-3 flex flex-col gap-2">
      <div className="flex items-center gap-1 px-0.5">
        <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-3">
          <BoardVoiceStar /> {t.board.title}
        </span>
        <InfoPopover
          ariaLabel={t.board.purposeAria}
          title={t.board.title}
          body={t.board.purpose}
        />
        {isPage && !composerOpen ? (
          <NewBoardButton onClick={() => setComposerOpen(true)} />
        ) : null}
      </div>

      {isPage ? (
        <BoardTabs
          boards={boards}
          activeBoardId={activeBoardId}
          onSelect={setSelectedBoardId}
        />
      ) : null}

      {composerOpen ? (
        <BoardComposer
          pending={createBoard.isPending}
          onCancel={() => setComposerOpen(false)}
          onCreate={(input) => {
            createBoard.mutate(input, {
              onSuccess: (result) => {
                setComposerOpen(false);
                setCookingTitle(result.board.title || input.title);
                setSelectedBoardId(result.board.id);
              },
            });
          }}
        />
      ) : null}

      {cookingTitle ? <BoardCookingReceipt title={cookingTitle} /> : null}

      {/* Onboarding super-notifs — the highest-priority thing a brand
          new user can act on, so they sit above even 等你. Page-only:
          /memories already mounts PinnedNotifications in its hero. */}
      {pinned.map((notification) => (
        <PinnedDispatch key={notification.id} notification={notification} />
      ))}

      {activeCustomBoard ? (
        <CustomBoardView
          board={activeCustomBoard}
          hubId={hubId}
          deletePending={deleteBoard.isPending}
          onDelete={(boardId) => {
            deleteBoard.mutate(boardId, {
              onSuccess: () => {
                setSelectedBoardId(null);
                setCookingTitle(null);
              },
            });
          }}
        />
      ) : null}

      {/* ── 等你 band — decisions merged from notifications (P4). ── */}
      {showSlots &&
        waiting.map((card, index) => (
          <BoardNotificationCard
            key={card.id}
            card={card}
            entranceIndex={index}
            disabled={resolveNotification.isPending}
            onResolve={onResolveNotification}
          />
        ))}

      {/* ── System board slots. ── */}
      {showSlots &&
        slots.map((slot, index) => {
          const expanded =
            slot.slot_key === heroKey || openSlots.has(slot.slot_key);
          return (
            <BoardSlotEntry
              key={slot.slot_key}
              slot={slot}
              expanded={expanded}
              entranceIndex={waiting.length + index}
              onToggle={(willOpen) => toggleSlot(slot.slot_key, willOpen)}
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
        })}

      {pageIsEmpty ? (
        <div className="glass-card rounded-[18px] px-4 py-6 text-center">
          <p className="m-0 text-[13.5px] text-fg-2">
            {t.board.pageEmptyTitle}
          </p>
          <p className="m-0 mt-1 text-[12.5px] leading-relaxed text-fg-3">
            {t.board.pageEmptyBody}
          </p>
        </div>
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
 * BoardPage — the /pulse route body. Same board, full surface: board
 * tabs, the custom-board composer, and the 最近 receipts strip.
 */
export function BoardPage() {
  const { hubFilter } = useActiveHub();
  if (!hubFilter) return null;
  return <BoardView hubId={hubFilter} surface="page" />;
}

function BoardSlotEntry({
  slot,
  expanded,
  entranceIndex,
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
  return (
    <BoardCard
      state={slot.state}
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
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
      receipt={
        slot.resolution?.action === "dismiss"
          ? t.board.receiptDismissed
          : t.board.receiptAcked
      }
    >
      {purpose ? (
        <div className="float-right ml-2">
          <InfoPopover
            ariaLabel={t.board.purposeAria}
            title={t.board.title}
            body={purpose}
            side="left"
          />
        </div>
      ) : null}
      {renderBoardSlotBody(slot)}
    </BoardCard>
  );
}
