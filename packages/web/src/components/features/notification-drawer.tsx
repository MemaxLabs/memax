"use client";

/**
 * NotificationDrawer — the app's ONE notification home (founder spec
 * 2026-09-14, design doc: artifact 5a85ee8c "脉搏通知审查").
 *
 * The pulse stream keeps only CONTENT (等你 decisions + dream cards);
 * everything "the system says to you" lives here, in three sections:
 *
 *   置顶  broadcasts — kind=system_notice whose source is NOT the
 *         onboarding welcome (that one stays a pinned board card).
 *         Explicit dismiss; opening the drawer does not clear them.
 *   动态  news — hub_member_joined, hub-tagged.
 *   回执  receipts — the RECEIPT_KINDS timeline the retired 最近
 *         strip used to hold.
 *
 * Read semantics (Linear-inbox grammar): OPENING the drawer marks
 * news + receipts seen (bulk, non-decision kinds only — the server
 * refuses decision kinds in bulk ops by design). Broadcasts stay
 * unseen until individually dismissed, which keeps the bell lit
 * while an unhandled announcement exists. Row × = dismiss (removes
 * the row for good, server-side).
 *
 * Hosts:
 *   desktop — <NotificationBell/> in the LeftRail footer (next to
 *             the avatar, founder placement), anchored Popover panel
 *             in the hub-switcher grammar;
 *   mobile  — <MobileNotificationSheet/> hosted by ShellLayoutMobile
 *             (hoisted above the nav drawer — the onboarding-modal
 *             focus-trap lesson), opened from a drawer nav row.
 *
 * Badge split (D3): the bell shows summary.updates_unseen; the rail
 * pulse tab dot narrows to needs_action_pending only. Two channels,
 * two clears.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Bell, X } from "lucide-react";
import type { Notification } from "memax-sdk";
import {
  BottomSheet,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { useAuth } from "@/lib/auth";
import { getHubDisplayName } from "@/lib/hub-display";
import {
  useBulkNotificationSeen,
  useNotificationDismiss,
  useNotifications,
} from "@/hooks/use-notifications";
import {
  HIGHLIGHT_KINDS,
  RECEIPT_KINDS,
} from "@/components/features/board/board-notification-cards";
import {
  inboxKindLabel,
  notificationToInboxItem,
  useInboxItemLocalization,
} from "@/components/features/inbox/inbox-control";

/** Broadcast = system notice that is NOT the onboarding welcome. */
function isBroadcast(n: Notification): boolean {
  return n.kind === "system_notice" && n.source_kind !== "onboarding_welcome";
}

export interface DrawerBuckets {
  broadcasts: Notification[];
  news: Notification[];
  receipts: Notification[];
  /** Unseen across all three buckets — mirrors the bell's intent. */
  unseen: number;
}

/**
 * Pure classifier — exported for tests. Kind tables are imported from
 * board-notification-cards so the drawer and the board can never
 * disagree about what a receipt is (single kind source).
 */
export function classifyDrawerRows(
  rows: readonly Notification[],
): DrawerBuckets {
  const broadcasts: Notification[] = [];
  const news: Notification[] = [];
  const receipts: Notification[] = [];
  for (const n of rows) {
    if (n.status !== "pending") continue;
    if (isBroadcast(n)) {
      broadcasts.push(n);
      continue;
    }
    if (n.kind === "system_notice") continue; // onboarding welcome → pinned board card
    if (HIGHLIGHT_KINDS.has(n.kind)) {
      news.push(n);
      continue;
    }
    if (RECEIPT_KINDS.has(n.kind)) {
      receipts.push(n);
    }
  }
  const byNewest = (a: Notification, b: Notification) =>
    Date.parse(b.created_at) - Date.parse(a.created_at) || 0;
  broadcasts.sort(byNewest);
  news.sort(byNewest);
  receipts.sort(byNewest);
  const unseen = [...broadcasts, ...news, ...receipts].filter(
    (n) => !n.seen_at,
  ).length;
  return { broadcasts, news, receipts, unseen };
}

/**
 * The kinds "open = read" applies to. Broadcasts are deliberately
 * absent (explicit dismiss keeps the bell honest about unhandled
 * announcements); decision kinds would be refused by the server.
 */
function bulkSeenKinds(buckets: DrawerBuckets): string[] {
  const kinds = new Set<string>();
  for (const n of buckets.news) if (!n.seen_at) kinds.add(n.kind);
  for (const n of buckets.receipts) if (!n.seen_at) kinds.add(n.kind);
  return [...kinds];
}

/**
 * Exported: the bell/nav-row badge counts THIS hook's `unseen`, not
 * the server summary's updates_unseen. The summary bucket is wider
 * than the drawer (onboarding system_notice, digest, producerless
 * scaffold kinds all count there but never render here), so a badge
 * driven by it could show a number the drawer cannot clear — the
 * founder hit exactly that: 全部已读 "did nothing" against a count
 * fed by rows the drawer deliberately doesn't own. One classifier,
 * one number: what the badge counts is what the panel can clear.
 */
export function useDrawerData(): DrawerBuckets {
  const { data } = useNotifications();
  const rows = useMemo(() => data?.notifications ?? [], [data]);
  return useMemo(() => classifyDrawerRows(rows), [rows]);
}

function DrawerRow({
  n,
  emphasized,
  onDismiss,
  dismissPending,
}: {
  n: Notification;
  /** Unseen rows lead with the signature dot. */
  emphasized: boolean;
  onDismiss: (id: string) => void;
  dismissPending: boolean;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { hubs, user } = useAuth();
  // Same adapter + label bundle the inbox panel and the 等你 band use,
  // so the drawer's wording can never drift from theirs.
  const labels = useInboxItemLocalization();
  const item = useMemo(
    () => notificationToInboxItem(n, labels, user?.id),
    [n, labels, user?.id],
  );
  // Hub tag only when the row belongs to a hub that is not the
  // viewer's personal hub — "which room did this happen in".
  const hubName = useMemo(() => {
    if (!n.hub_id) return null;
    const entry = hubs.find((h) => h.hub.id === n.hub_id);
    if (!entry || entry.hub.hub_type !== "team") return null;
    return getHubDisplayName(
      entry.hub,
      t,
      user ? { name: user.name, displayName: user.display_name } : undefined,
    );
  }, [n.hub_id, hubs, t, user]);
  return (
    <div className="group flex items-start gap-2.5 border-t border-border/20 px-3 py-2.5 first:border-t-0">
      <span
        aria-hidden
        className="mt-[7px] h-[6px] w-[6px] shrink-0 rounded-full"
        style={{
          background: emphasized ? "var(--signature)" : "var(--border)",
        }}
      />
      <div className="min-w-0 flex-1">
        <p className="m-0 truncate text-[13px] leading-snug text-fg-1">
          {item.title}
        </p>
        <p className="m-0 mt-0.5 flex items-center gap-1.5 text-[11px] text-fg-4">
          <span>{inboxKindLabel(item, t)}</span>
          {hubName ? (
            <span className="max-w-[120px] truncate rounded-full bg-surface-2 px-1.5 py-px text-fg-3">
              {hubName}
            </span>
          ) : null}
          <span className="tabular-nums">
            {formatAge(n.created_at, t, interpolate)}
          </span>
        </p>
      </div>
      <button
        type="button"
        aria-label={t.reviews.dismiss}
        disabled={dismissPending}
        onClick={() => onDismiss(n.id)}
        className="mt-0.5 shrink-0 cursor-pointer rounded-md p-1 text-fg-4 opacity-0 transition-opacity hover:text-fg-2 focus-visible:opacity-100 group-hover:opacity-100"
      >
        <X className="h-3 w-3" aria-hidden />
      </button>
    </div>
  );
}

/**
 * The drawer body — shared verbatim between the desktop anchored
 * panel and the mobile sheet, so the two hosts can never drift.
 */
export function NotificationDrawerPanel() {
  const { t } = useLocale();
  const buckets = useDrawerData();
  const dismiss = useNotificationDismiss();
  const bulkSeen = useBulkNotificationSeen();

  // Open = read (news + receipts). One shot per mount; re-arms only
  // when a new unseen row appears while the panel stays open. The
  // markedRef key dedups against the refetch that the mutation's own
  // invalidation triggers. `mutate` is referentially stable (React
  // Query guarantee) — the standard stable-setter dep pattern.
  const markedRef = useRef<string>("");
  const bulkSeenMutate = bulkSeen.mutate;
  useEffect(() => {
    const kinds = bulkSeenKinds(buckets);
    if (kinds.length === 0) return;
    const key = kinds.sort().join(",");
    if (markedRef.current === key) return;
    markedRef.current = key;
    bulkSeenMutate({ kinds });
  }, [buckets, bulkSeenMutate]);

  const empty =
    buckets.broadcasts.length === 0 &&
    buckets.news.length === 0 &&
    buckets.receipts.length === 0;

  const section = (label: string, rows: Notification[], emphasize: boolean) =>
    rows.length > 0 ? (
      <div>
        <p className="m-0 px-3 pb-1 pt-3 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-4">
          {label}
        </p>
        {rows.map((n) => (
          <DrawerRow
            key={n.id}
            n={n}
            emphasized={emphasize && !n.seen_at}
            onDismiss={(id) => dismiss.mutate(id)}
            dismissPending={dismiss.isPending}
          />
        ))}
      </div>
    ) : null;

  return (
    <div className="flex max-h-[440px] w-full flex-col">
      <div className="flex items-center justify-between border-b border-border/30 px-3 py-2.5">
        <span className="text-[13px] font-semibold text-fg-1">
          {t.notificationDrawer.title}
        </span>
        {buckets.unseen > 0 ? (
          <button
            type="button"
            onClick={() =>
              // Broadcasts are deliberately EXCLUDED (codex review):
              // marking system_notice seen would darken the bell while
              // the announcement is still pending — broadcasts clear
              // only by their per-row dismiss, and 全部已读 must not
              // contradict that contract.
              bulkSeen.mutate({
                kinds: [
                  ...new Set(
                    [...buckets.news, ...buckets.receipts].map((n) => n.kind),
                  ),
                ],
              })
            }
            className="cursor-pointer text-[12px] text-fg-3 transition-colors hover:text-fg-2"
          >
            {t.notificationDrawer.markAllRead}
          </button>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto pb-1">
        {empty ? (
          <p className="m-0 px-3 py-8 text-center text-[12.5px] text-fg-4">
            {t.notificationDrawer.empty}
          </p>
        ) : (
          <>
            {section(
              t.notificationDrawer.sectionPinned,
              buckets.broadcasts,
              true,
            )}
            {section(t.notificationDrawer.sectionNews, buckets.news, true)}
            {section(
              t.notificationDrawer.sectionReceipts,
              buckets.receipts,
              false,
            )}
          </>
        )}
      </div>
    </div>
  );
}

/**
 * NotificationBell — the desktop entry, living in the LeftRail footer
 * beside the avatar. Badge = summary.updates_unseen (the server's
 * canonical "things you haven't seen" count; decisions light the
 * pulse tab instead).
 */
export function NotificationBell() {
  const { t } = useLocale();
  const [open, setOpen] = useState(false);
  const { unseen } = useDrawerData();
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={t.notificationDrawer.openAria}
        title={t.notificationDrawer.title}
        className="relative flex h-9 w-9 shrink-0 cursor-pointer items-center justify-center rounded-lg text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1"
      >
        <Bell className="h-[18px] w-[18px]" strokeWidth={1.8} />
        {unseen > 0 ? (
          <span
            className="absolute right-1 top-1 flex h-[15px] min-w-[15px] items-center justify-center rounded-full px-[3px] text-[9.5px] font-semibold leading-none text-white"
            style={{ background: "var(--signature)" }}
          >
            {unseen > 9 ? "9+" : unseen}
          </span>
        ) : null}
      </PopoverTrigger>
      <PopoverContent
        side="right"
        align="end"
        sideOffset={8}
        className="w-[340px] p-0"
      >
        <NotificationDrawerPanel />
      </PopoverContent>
    </Popover>
  );
}

/** Mobile host — same panel body inside the standard bottom sheet. */
export function MobileNotificationSheet({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useLocale();
  return (
    <BottomSheet
      open={open}
      onClose={onClose}
      ariaLabel={t.notificationDrawer.title}
      title={t.notificationDrawer.title}
    >
      <NotificationDrawerPanel />
    </BottomSheet>
  );
}
