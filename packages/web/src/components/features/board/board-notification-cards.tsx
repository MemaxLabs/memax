"use client";

/**
 * 等你 band + 最近 strip — notifications merged onto the pulse board
 * (plan 25 P4, the "逆向整合").
 *
 * The board is hub-scoped with a membership guard; the six inbox
 * decision kinds are all AudienceUser. Those two facts don't line up
 * server-side, so this phase merges CLIENT-SIDE:
 *
 *   - hub-scoped reviews (review_contradiction / review_topic_merge /
 *     review_topic_restructure) render on THAT hub's board — strict
 *     `notification.hub_id === hubId`.
 *   - user-scoped rows the recipient may not have hub access to
 *     (hub_invite, hub_ownership_transfer, the onboarding checklist)
 *     render on the user's PERSONAL hub board, regardless of hub_id.
 *     Otherwise an invite to a hub you're not a member of would have
 *     nowhere to land now that /inbox is gone.
 *   - receipts (dream reports, membership changes, quota rows, system
 *     notices, gift links) land in the collapsed 最近 strip at the
 *     bottom of the board, on their own hub when it's visible and on
 *     the personal board otherwise. Nothing gets dropped.
 *
 * `decision_gate` is deliberately EXCLUDED: it is only a ping for a
 * board card that already renders as a slot on the same board, so
 * including it here would double-render the same decision.
 *
 * Bodies and action verbs are imported from inbox-control rather than
 * re-implemented — the payload type-guards and the server's resolve
 * allow-list are the parts most likely to drift under a copy.
 */

import { useMemo } from "react";
import type { Notification } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardKindLabel,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import {
  getInboxDetailComponent,
  inboxDecisionActions,
  inboxKindLabel,
  notificationToInboxItem,
  useInboxItemLocalization,
  type InboxActionButton,
  type InboxItem,
} from "@/components/features/inbox/inbox-control";
import { useAuth } from "@/lib/auth";
import { useNotifications } from "@/hooks/use-notifications";

/** Hub-scoped decisions — only shown on the hub they belong to. */
const HUB_REVIEW_KINDS: ReadonlySet<string> = new Set([
  "review_contradiction",
  "review_topic_merge",
  "review_topic_restructure",
]);

/**
 * User-scoped decisions. The recipient may have no membership in the
 * row's hub (that's the whole point of an invite), so these land on
 * the personal board.
 */
const USER_DECISION_KINDS: ReadonlySet<string> = new Set([
  "hub_invite",
  "hub_ownership_transfer",
]);

/** Kinds PinnedDispatch renders (onboarding checklist + founder note). */
const PINNED_KINDS: ReadonlySet<string> = new Set([
  "checklist",
  "digest",
  "system_notice",
]);

/**
 * Receipt kinds — no decision, just "this happened". Mirrors
 * RECEIPT_INBOX_KINDS in inbox-control (stripped-kind form there,
 * wire-kind form here; the two sets are identical because no receipt
 * kind carries a `review_` prefix).
 */
const RECEIPT_KINDS: ReadonlySet<string> = new Set([
  "hub_member_joined",
  "hub_invite_accepted",
  "hub_invite_declined",
  "hub_invite_declined_by_you",
  "hub_ownership_transferred",
  "hub_over_limit",
  "hub_frozen",
  "hub_restored",
  "system_notice",
  "gift_invite_link",
  "dream_run_completed",
]);

export interface BoardNotificationCardModel {
  id: string;
  /** Stripped kind ("contradiction", not "review_contradiction"). */
  kind: string;
  title: string;
  description: string;
  actions: InboxActionButton[];
  payload?: InboxItem["payload"];
  /** The adapted row the shared inbox body renderers consume. */
  item: InboxItem;
}

export interface BoardNotificationBuckets {
  /** 等你 — decisions, rendered as board cards above the slots. */
  waiting: BoardNotificationCardModel[];
  /**
   * Onboarding super-notifs (checklist / founder note). Kept separate
   * because they have their own full-bleed renderer (PinnedDispatch)
   * and are already mounted on /memories — only the full-page pulse
   * surface should render them, or they'd double up.
   */
  pinned: Notification[];
  /** 最近 — receipts, rendered as a collapsed strip at the bottom. */
  recent: BoardNotificationCardModel[];
}

const EMPTY_BUCKETS: BoardNotificationBuckets = {
  waiting: [],
  pinned: [],
  recent: [],
};

function isOnboardingPinned(notification: Notification): boolean {
  if (!PINNED_KINDS.has(notification.kind)) return false;
  if (notification.kind === "checklist") {
    return notification.source_kind === "onboarding";
  }
  if (notification.kind === "digest") return true;
  return notification.source_kind === "onboarding_welcome";
}

/**
 * useBoardNotificationCards reads the caller's pending notifications
 * (unscoped — the same query the badge uses, so the cache is shared)
 * and buckets them for one hub's board.
 */
export function useBoardNotificationCards(
  hubId: string | undefined,
  isPersonalHub: boolean,
): BoardNotificationBuckets {
  const { data } = useNotifications({ status: "pending" });
  const { user } = useAuth();
  const labels = useInboxItemLocalization();
  const { t } = useLocale();
  const userId = user?.id;

  return useMemo<BoardNotificationBuckets>(() => {
    const rows = data?.notifications ?? [];
    if (!hubId || rows.length === 0) return EMPTY_BUCKETS;

    const waiting: BoardNotificationCardModel[] = [];
    const pinned: Notification[] = [];
    const recent: BoardNotificationCardModel[] = [];

    const toModel = (
      notification: Notification,
    ): BoardNotificationCardModel => {
      const item = notificationToInboxItem(notification, labels, userId);
      return {
        id: item.id,
        kind: item.kind,
        title: item.title,
        description: item.description,
        actions: inboxDecisionActions(item.kind, t) ?? [],
        payload: item.payload,
        item,
      };
    };

    for (const notification of rows) {
      if (isOnboardingPinned(notification)) {
        // Onboarding rows are addressed to the person, not the hub —
        // the personal board is their home.
        if (isPersonalHub) pinned.push(notification);
        continue;
      }
      if (HUB_REVIEW_KINDS.has(notification.kind)) {
        if (notification.hub_id === hubId) waiting.push(toModel(notification));
        continue;
      }
      if (USER_DECISION_KINDS.has(notification.kind)) {
        if (isPersonalHub) waiting.push(toModel(notification));
        continue;
      }
      if (RECEIPT_KINDS.has(notification.kind)) {
        // On its own hub when the viewer is looking at it; on the
        // personal board otherwise, so nothing is unreachable.
        const belongsHere = notification.hub_id
          ? notification.hub_id === hubId || isPersonalHub
          : isPersonalHub;
        if (belongsHere) recent.push(toModel(notification));
        continue;
      }
      // Everything else (decision_gate pings, scaffold review kinds
      // with no producer) is intentionally dropped — decision_gate
      // already renders as a board slot, scaffolds have no action.
    }

    const byNewest = (
      a: BoardNotificationCardModel,
      b: BoardNotificationCardModel,
    ) => Date.parse(b.item.created_at) - Date.parse(a.item.created_at) || 0;
    waiting.sort(byNewest);
    recent.sort(byNewest);
    pinned.sort((a, b) => {
      if (a.priority !== b.priority) return b.priority - a.priority;
      return Date.parse(b.created_at) - Date.parse(a.created_at);
    });

    return { waiting, pinned, recent };
  }, [data, hubId, isPersonalHub, labels, t, userId]);
}

/**
 * BoardNotificationCard — a pending decision rendered in the board's
 * card language. Same 等你 eyebrow the decision-gate slot uses, same
 * BoardCard shell, but the body and the action verbs come from the
 * inbox renderers so the wire contract stays single-sourced.
 */
export function BoardNotificationCard({
  card,
  entranceIndex,
  disabled,
  onResolve,
}: {
  card: BoardNotificationCardModel;
  entranceIndex: number;
  disabled: boolean;
  onResolve: (id: string, action: string) => void;
}) {
  const { t } = useLocale();
  const Body = getInboxDetailComponent(card.kind);
  return (
    <BoardCard
      state="fresh"
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      live={
        card.actions.length > 0 ? (
          <BoardActionRow className="flex-wrap">
            {card.actions.map((action) => (
              <BoardAction
                key={action.action}
                emphasis={action.tone === "ghost" ? "quiet" : "primary"}
                disabled={disabled}
                className={action.tone === "ghost" ? "ml-auto" : undefined}
                onClick={() => onResolve(card.id, action.action)}
              >
                {action.label}
              </BoardAction>
            ))}
          </BoardActionRow>
        ) : null
      }
    >
      <BoardKindLabel star>{t.board.kindWaiting}</BoardKindLabel>
      {card.title ? (
        <p className="m-0 text-[14px] leading-snug text-fg-1">{card.title}</p>
      ) : null}
      {Body ? (
        // The inbox bodies carry their own px-3 row padding, which is
        // one notch wider than BoardCard's px-4 gutter — pull it back
        // so quotes and topic chips align with the card's own text.
        <div className="-mx-3 -mb-3">
          <Body item={card.item} />
        </div>
      ) : null}
    </BoardCard>
  );
}

/**
 * BoardRecentRow — one receipt inside the expanded 最近 strip. Reuses
 * the inbox's kind label so "NEW MEMBER" / "GIFT" / "NOTICE" read the
 * same wherever they surface.
 */
export function BoardRecentRow({
  card,
  disabled,
  onDismiss,
}: {
  card: BoardNotificationCardModel;
  disabled: boolean;
  onDismiss: (id: string) => void;
}) {
  const { t } = useLocale();
  return (
    <div className="flex items-start gap-3 px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="text-[10.5px] uppercase tracking-[0.12em] text-fg-4">
          {inboxKindLabel(card.item, t)}
        </div>
        <div className="mt-0.5 truncate text-[12.5px] text-fg-2">
          {card.title}
        </div>
      </div>
      <BoardAction
        emphasis="quiet"
        disabled={disabled}
        onClick={() => onDismiss(card.id)}
      >
        {t.reviews.dismiss}
      </BoardAction>
    </div>
  );
}
