"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useRouter, usePathname } from "next/navigation";
import {
  buildMemoriesPath,
  buildMemoryDetailPath,
  getHubSlugForPath,
} from "@/lib/route-helpers";
import {
  AlertCircle,
  ArrowUpRight,
  ChevronRight,
  Crown,
  Gift,
  GitBranch,
  GitMerge,
  HelpCircle,
  Inbox,
  Info,
  Mail,
  Shield,
  Sparkles,
  UserCheck,
  UserMinus,
  UserPlus,
  Waves,
  type LucideIcon,
} from "lucide-react";
import {
  ContentError,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SectionHeader,
  Skeleton,
  Surface,
} from "@memaxlabs/ui";
import {
  prefetchNotifications,
  useNotifications,
  useNotificationMarkSeen,
  useNotificationSummary,
  useResolveNotification,
  useNotificationDismiss,
} from "@/hooks/use-notifications";
import { useDreamSurfaceState } from "@/hooks/use-dream-surface-state";
import type {
  Notification,
  NotificationResolveAction,
  NotificationResolution,
  NotificationStatus,
  DreamRunCompletedPayload,
  ReviewContradictionPayload,
  ReviewTopicMergePayload,
  ReviewTopicRestructurePayload,
  HubInvitePayload,
  HubInviteAcceptedPayload,
  HubInviteDeclinedPayload,
  HubInviteDeclinedByYouPayload,
  HubMemberJoinedPayload,
  HubOwnershipTransferPayload,
  HubOwnershipTransferredPayload,
  SystemNoticePayload,
  GiftInviteLinkPayload,
} from "memax-sdk";

// InboxItem is the local view-model the inbox renderer consumes. It's
// derived from a notification row via notificationToInboxItem and
// carries exactly the fields the row / body / action-bar need. The
// decision discriminator `kind` is the notification kind with the
// `review_` prefix stripped ("contradiction", "topic_merge",
// "topic_restructure", …) because every switch in this file operates
// on the stripped form. Kinds without a promoted renderer fall back
// to the fallback body so the inbox cannot render blank.
export interface InboxItem {
  id: string;
  audience: Notification["audience"];
  hub_id?: string;
  hub?: {
    name: string;
    kind: "personal" | "team";
    accent?: string;
    initial: string;
  };
  kind: string;
  status: NotificationStatus;
  seen: boolean;
  resolution?: NotificationResolution;
  title: string;
  description: string;
  similarity: number;
  created_at: string;
  payload?:
    | ReviewContradictionPayload
    | ReviewTopicMergePayload
    | ReviewTopicRestructurePayload
    | DreamRunCompletedPayload
    | HubInvitePayload
    | HubInviteAcceptedPayload
    | HubInviteDeclinedPayload
    | HubInviteDeclinedByYouPayload
    | HubMemberJoinedPayload
    | HubOwnershipTransferPayload
    | HubOwnershipTransferredPayload
    | SystemNoticePayload
    | GiftInviteLinkPayload
    | Record<string, unknown>;
}
import { pluralize, useLocale, useInterpolate } from "@/i18n";
import { useInboxSurface } from "@/contexts/inbox-surface-context";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { useAuth } from "@/lib/auth";
import { formatAge } from "@/lib/format-age";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { MemaxStar } from "../memax-star";
import { PageShell, PageHeader } from "@/components/layout";
import { EASE, FAST, NORMAL } from "@memaxlabs/ui/tokens/motion";
import { HubBadge } from "../hub/hub-badge";
import { PinnedDispatch } from "@/components/features/onboarding/onboarding-pinned";

const INBOX_VIEW_ALL_THRESHOLD = 10;
const MIN_REASONABLE_RUN_TIMESTAMP_MS = Date.UTC(2020, 0, 1);

type InboxResolveAction = string;

// Decision kinds the notifications handler's allow-list resolves.
// Mirrors notificationResolveAllowList in
// packages/server/internal/handler/notifications.go.
const DECISION_INBOX_KINDS = new Set<string>([
  "contradiction",
  "topic_merge",
  "topic_restructure",
  "hub_invite",
  "hub_ownership_transfer",
  // Plan 25 P2 — the ping for a board decision gate. The notification
  // itself only accepts dismiss; the real choice happens on the board
  // card (kind has no review_ prefix, so it lands here unchanged).
  "decision_gate",
]);

// Receipt kinds that render without an action bar. The dismiss affordance
// for these rows goes through useNotificationDismiss, not /resolve.
const RECEIPT_INBOX_KINDS = new Set<string>([
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

// Review kinds that are scaffolded (renderer exists) but have no producer
// yet. The row renders an explicit "no action yet" note and no action
// buttons, because the server would reject every action we could offer.
const SCAFFOLD_REVIEW_KINDS = new Set<string>(["stale", "low_confidence"]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function hasString(value: unknown, key: string): boolean {
  return typeof (value as Record<string, unknown>)?.[key] === "string";
}

// hasTopicRef is the structural guard for an inline topic pointer.
// The only hard requirement is a string `id`; everything else
// (name, memory_count) is advisory and degrades to a friendly
// "(untitled topic)" placeholder in the renderer when absent. We
// deliberately do NOT require `name` here: a notification written
// against a freshly created (unnamed) topic, or one whose topic was
// renamed to empty, should still surface the merge/restructure row
// with ID-based chips instead of collapsing to a blank body.
function hasTopicRef(value: unknown): value is {
  id: string;
  name?: string;
  memory_count?: number;
} {
  return isRecord(value) && hasString(value, "id");
}

function hasTopicMergePayload(value: unknown): value is {
  target_topic: { id: string; name?: string; memory_count?: number };
  source_topics: { id: string; name?: string; memory_count?: number }[];
  reason?: string;
} {
  if (!isRecord(value) || !hasTopicRef(value.target_topic)) return false;
  if (!Array.isArray(value.source_topics)) return false;
  return value.source_topics.every(hasTopicRef);
}

function hasTopicRestructurePayload(value: unknown): value is {
  parent_topic: { id: string; name?: string; memory_count?: number };
  child_topic: { id: string; name?: string; memory_count?: number };
  reason?: string;
} {
  return (
    isRecord(value) &&
    hasTopicRef(value.parent_topic) &&
    hasTopicRef(value.child_topic)
  );
}

function hasHubRef(value: unknown): value is { name: string } {
  return isRecord(value) && hasString(value, "name");
}

function hasDisplayRef(value: unknown): value is { display?: string } {
  return isRecord(value);
}

function hasHubInvitePayload(value: unknown): value is HubInvitePayload {
  return isRecord(value) && hasHubRef(value.hub);
}

function hasHubMemberJoinedPayload(
  value: unknown,
): value is HubMemberJoinedPayload {
  return isRecord(value) && hasHubRef(value.hub) && hasDisplayRef(value.member);
}

function hasHubInviteAcceptedPayload(
  value: unknown,
): value is HubInviteAcceptedPayload {
  return isRecord(value) && hasHubRef(value.hub);
}

function hasHubInviteDeclinedPayload(
  value: unknown,
): value is HubInviteDeclinedPayload {
  return (
    isRecord(value) && hasHubRef(value.hub) && hasDisplayRef(value.invitee)
  );
}

function hasHubInviteDeclinedByYouPayload(
  value: unknown,
): value is HubInviteDeclinedByYouPayload {
  return isRecord(value) && hasHubRef(value.hub);
}

function hasHubOwnershipTransferPayload(
  value: unknown,
): value is HubOwnershipTransferPayload {
  return (
    isRecord(value) && hasHubRef(value.hub) && hasDisplayRef(value.initiator)
  );
}

function hasHubOwnershipTransferredPayload(
  value: unknown,
): value is HubOwnershipTransferredPayload {
  return (
    isRecord(value) && hasHubRef(value.hub) && hasDisplayRef(value.new_owner)
  );
}

function hasSystemNoticePayload(value: unknown): value is SystemNoticePayload {
  return isRecord(value);
}

function hasGiftInviteLinkPayload(
  value: unknown,
): value is GiftInviteLinkPayload {
  return isRecord(value) && hasDisplayRef(value.sender);
}

function hasContradictionPayload(
  value: unknown,
): value is ReviewContradictionPayload {
  return (
    isRecord(value) &&
    isRecord(value.memory_a) &&
    hasString(value.memory_a, "id") &&
    isRecord(value.memory_b) &&
    hasString(value.memory_b, "id")
  );
}

function hasDreamRunCompletedPayload(
  value: unknown,
): value is DreamRunCompletedPayload {
  return isRecord(value) && hasString(value, "run_id");
}

// reviewMemoryDisplayTitle mirrors reviewMemoryRefTitle on the server
// (see packages/server/internal/dreams/contradictions.go). Notifications
// written before the server-side fallback landed carry `title: ""`,
// and the inbox row was rendering raw UUIDs for those rows. Keep a
// friendly placeholder so the UI never surfaces a bare UUID even for
// legacy payloads. The server-side fix is canonical; this is the
// defensive backstop.
function reviewMemoryDisplayTitle(
  ref: { title?: string; id?: string } | undefined | null,
): string {
  if (!ref) return "(untitled memory)";
  const trimmed = ref.title?.trim() ?? "";
  if (trimmed) return trimmed;
  return "(untitled memory)";
}

interface InboxItemLocalization {
  topicUntitled: string;
  topicRemoved: string;
  dreamRunCompletedTitle: string;
  dreamRunCompletedCleanTitle: string;
  dreamRunCompletedPartialTitle: string;
  topicMergeOne: (vars: { source: string; target: string }) => string;
  topicMergeMany: (vars: { sources: string; target: string }) => string;
  topicMergeFallback: (vars: { target: string }) => string;
  topicRestructure: (vars: { child: string; parent: string }) => string;
  hubOwnershipTransferredSelfTitle: (vars: { hub: string }) => string;
  hubOwnershipTransferredOtherTitle: (vars: {
    owner: string;
    hub: string;
  }) => string;
}

function isAccountLevelInboxItem(item: InboxItem): boolean {
  return item.audience === "user";
}

function InboxHubChip({ hub }: { hub: NonNullable<InboxItem["hub"]> }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 rounded-full border border-border/40 bg-surface-1/70 px-1.5 py-0.5 text-[10px] text-fg-3">
      <HubBadge
        kind={hub.kind}
        label={hub.initial}
        accent={hub.accent}
        size="sm"
      />
      <span className="max-w-[120px] truncate">{hub.name}</span>
    </span>
  );
}

// pickTopicTitle is the row-header counterpart to
// resolveTopicChipDisplay. Collapsed row headlines need the caller to
// inject the localized placeholders because this helper runs before
// the React body components do.
function pickTopicTitle(
  ref: { id?: string; name?: string } | undefined | null,
  labels: Pick<InboxItemLocalization, "topicUntitled" | "topicRemoved">,
): string {
  if (!ref) return labels.topicUntitled;
  const trimmed = ref.name?.trim() ?? "";
  if (trimmed === SERVER_REMOVED_TOPIC_SENTINEL) return labels.topicRemoved;
  if (trimmed) return trimmed;
  return labels.topicUntitled;
}

function isDecisionInboxKind(item: InboxItem): boolean {
  return DECISION_INBOX_KINDS.has(item.kind);
}

function isReceiptInboxKind(item: InboxItem): boolean {
  return RECEIPT_INBOX_KINDS.has(item.kind);
}

interface InboxRowProps {
  item: InboxItem;
  isExpanded: boolean;
  isResolving: boolean;
  onToggle: () => void;
  onResolve: (id: string, action: InboxResolveAction) => void;
  onDismiss: (id: string) => void;
}

function InboxActions({
  item,
  disabled,
  onResolve,
  onDismiss,
}: {
  item: InboxItem;
  disabled: boolean;
  onResolve: (id: string, action: InboxResolveAction) => void;
  onDismiss: (id: string) => void;
}) {
  const { t } = useLocale();

  type ActionButton = {
    action: InboxResolveAction;
    label: string;
    tone: "dream" | "ghost";
  };

  // Receipt kinds render a single dismiss affordance that routes
  // through the notifications SDK dismiss method. Decision kinds
  // route through resolve via the InboxResolveAction path below.
  //
  // Flat layout: no inner card wrapper. The parent row owns the
  // visual container; this just contributes a button row at the
  // bottom with end-aligned ghost button.
  if (isReceiptInboxKind(item)) {
    return (
      <div className="flex flex-wrap items-center justify-end gap-2 px-3 pb-3">
        <button
          type="button"
          disabled={disabled}
          onClick={() => onDismiss(item.id)}
          className="min-h-8 cursor-pointer rounded-lg px-2.5 text-[12px] font-medium text-fg-4 transition-colors hover:text-fg-3"
        >
          {t.reviews.dismiss}
        </button>
      </div>
    );
  }

  // Scaffold kinds (review_stale / review_low_confidence) have no
  // action bar at all — the server has no producer yet and no
  // resolver.
  if (!isDecisionInboxKind(item)) {
    return null;
  }

  let buttons: ActionButton[];
  switch (item.kind) {
    case "contradiction":
      buttons = [
        { action: "keep_a", label: t.reviews.keepA, tone: "dream" },
        { action: "keep_b", label: t.reviews.keepB, tone: "dream" },
        { action: "keep_both", label: t.reviews.keepBoth, tone: "dream" },
        { action: "dismiss", label: t.reviews.dismiss, tone: "ghost" },
      ];
      break;
    case "topic_merge":
      buttons = [
        { action: "merge", label: t.reviews.merge, tone: "dream" },
        {
          action: "keep_separate",
          label: t.reviews.keepSeparate,
          tone: "dream",
        },
        { action: "dismiss", label: t.reviews.dismiss, tone: "ghost" },
      ];
      break;
    case "topic_restructure":
      buttons = [
        { action: "apply", label: t.reviews.apply, tone: "dream" },
        { action: "keep", label: t.reviews.keep, tone: "dream" },
        { action: "dismiss", label: t.reviews.dismiss, tone: "ghost" },
      ];
      break;
    case "hub_invite":
      buttons = [
        { action: "accept", label: t.reviews.accept, tone: "dream" },
        { action: "decline", label: t.reviews.decline, tone: "ghost" },
      ];
      break;
    case "hub_ownership_transfer":
      buttons = [
        { action: "accept", label: t.reviews.accept, tone: "dream" },
        { action: "decline", label: t.reviews.decline, tone: "ghost" },
      ];
      break;
    case "decision_gate":
      // The notification is only the ping — the real choice happens on
      // the board card (see DecisionGateInboxBody's board link), so the
      // only resolve action here is acknowledging the ping.
      buttons = [
        { action: "dismiss", label: t.inbox.gateDismiss, tone: "ghost" },
      ];
      break;
    default:
      // Defensive: DECISION_INBOX_KINDS and the switch should stay in
      // lockstep. If a new decision kind is added to the set but not
      // to the switch, fall through to null rather than shipping an
      // ambiguous button bar.
      return null;
  }

  return (
    // Flat action row — no nested card. Sits inside the flat
    // expanded body; the dream-tone fill on the primary buttons
    // still gives the action area a soft accent without a full
    // bordered container.
    <div className="flex flex-wrap items-center gap-2 px-3 pb-3">
      {buttons.map((button) => (
        <button
          key={button.action}
          type="button"
          disabled={disabled}
          onClick={() => onResolve(item.id, button.action)}
          className={`min-h-8 cursor-pointer rounded-lg px-2.5 text-[12px] font-medium transition-colors ${
            button.tone === "ghost" ? "ml-auto text-fg-4 hover:text-fg-3" : ""
          }`}
          style={
            button.tone === "dream"
              ? {
                  color: "var(--signature)",
                  background: "oklch(from var(--signature) l c h / 0.12)",
                }
              : undefined
          }
        >
          {button.label}
        </button>
      ))}
    </div>
  );
}

// TopicChipRef is the minimal shape TopicChip consumes. It's looser
// than the payload guard type (name and memory_count are optional)
// so the chip can render a friendly placeholder for legacy /
// stale / deleted topic references without the parent having to
// pre-filter them.
type TopicChipRef = {
  id?: string;
  name?: string;
  memory_count?: number;
};

// SERVER_REMOVED_TOPIC_SENTINEL is the exact string the Go handler's
// currentTopicRef stamps on a topic ref whose store row no longer
// exists (see packages/server/internal/handler/notifications.go).
// It's a locale-independent marker on purpose — the client swaps it
// for the localized `t.reviews.topicRemoved` label below. Changing
// this string without updating both sides in the same commit would
// silently regress the "(removed topic)" placeholder to English-only.
const SERVER_REMOVED_TOPIC_SENTINEL = "(removed topic)";

interface ResolvedTopicChip {
  displayName: string;
  isRemoved: boolean;
  isUntitled: boolean;
}

// resolveTopicChipDisplay centralises the precedence rule:
//   server `(removed topic)` sentinel → localized removed label
//   trimmed name                      → as-is
//   missing / blank                   → localized untitled label
// Keeping this in one place means every topic-rendering body picks
// the same placeholder, and the en / zh labels swap cleanly with
// the user's locale.
function resolveTopicChipDisplay(
  ref: TopicChipRef | undefined | null,
  labels: { removed: string; untitled: string },
): ResolvedTopicChip {
  const raw = ref?.name?.trim() ?? "";
  if (raw === SERVER_REMOVED_TOPIC_SENTINEL) {
    return { displayName: labels.removed, isRemoved: true, isUntitled: false };
  }
  if (raw) {
    return { displayName: raw, isRemoved: false, isUntitled: false };
  }
  return { displayName: labels.untitled, isRemoved: false, isUntitled: true };
}

function TopicChip({
  name,
  memoryCount,
  countLabel,
  muted,
}: {
  name: string;
  memoryCount?: number;
  countLabel: string;
  muted?: boolean;
}) {
  return (
    <div
      className={`flex min-w-0 max-w-full flex-col gap-0.5 rounded-lg border border-border/40 px-3 py-2 ${
        muted ? "bg-surface-1/30" : "bg-surface-1/60"
      }`}
    >
      <div
        className={`truncate text-[13px] font-medium ${
          muted ? "text-fg-3 italic" : "text-fg-1"
        }`}
      >
        {name}
      </div>
      {typeof memoryCount === "number" && memoryCount >= 0 && (
        <div className="text-[11px] text-fg-3 tabular-nums">{countLabel}</div>
      )}
    </div>
  );
}

// TopicMergeRowChips — the reusable source-list → target chip block
// used by both TopicMergeBody and TopicRestructureBody (and any
// future promoted renderer that wants the same "N sources → 1
// target" visual). Centralising it here means the arrow spacing,
// placeholder policy, and memory-count label only live in one place,
// and both the merge and restructure rows stay visually identical
// from the user's perspective.
function TopicMergeRowChips({
  sourceTopics,
  targetTopic,
}: {
  sourceTopics: TopicChipRef[];
  targetTopic: TopicChipRef;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const labels = {
    removed: t.reviews.topicRemoved,
    untitled: t.reviews.topicUntitled,
  };
  const renderChip = (ref: TopicChipRef, key: string) => {
    const resolved = resolveTopicChipDisplay(ref, labels);
    const count = ref.memory_count ?? 0;
    return (
      <TopicChip
        key={key}
        name={resolved.displayName}
        memoryCount={resolved.isRemoved ? undefined : count}
        countLabel={pluralize(
          t.reviews.topicMemoryCountOne,
          t.reviews.topicMemoryCount,
          count,
        )}
        muted={resolved.isRemoved || resolved.isUntitled}
      />
    );
  };
  return (
    <div className="flex flex-wrap items-center gap-2">
      {sourceTopics.map((src, idx) => renderChip(src, src.id ?? `src-${idx}`))}
      <span className="text-[11px] text-fg-4" aria-hidden>
        →
      </span>
      {renderChip(targetTopic, targetTopic.id ?? "target")}
    </div>
  );
}

function TopicMergeBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasTopicMergePayload(item.payload) ? item.payload : null;
  const description = item.description?.trim() || t.reviews.topicMergeBody;

  // Defensive: if the guard rejects the payload entirely (very old
  // rows, malformed JSON) still render the description so the row
  // doesn't look empty. The chip block only renders when we have at
  // least a target_topic; source_topics can be empty and the arrow
  // will read as "merge into" visually.
  return (
    <div className="space-y-3 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">{description}</p>
      {payload && (
        <TopicMergeRowChips
          sourceTopics={payload.source_topics}
          targetTopic={payload.target_topic}
        />
      )}
    </div>
  );
}

function TopicRestructureBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasTopicRestructurePayload(item.payload)
    ? item.payload
    : null;
  const description =
    item.description?.trim() || t.reviews.topicRestructureBody;

  return (
    <div className="space-y-3 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">{description}</p>
      {payload && (
        <TopicMergeRowChips
          sourceTopics={[payload.child_topic]}
          targetTopic={payload.parent_topic}
        />
      )}
    </div>
  );
}

function HubInviteBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubInvitePayload(item.payload) ? item.payload : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const inviterName = payload.inviter?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">{inviterName}</span>{" "}
        {t.inbox.hubInviteBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {payload.role && (
          <>
            {" "}
            <span className="text-fg-3">
              ({t.inbox.hubInviteRoleLabel}: {payload.role})
            </span>
          </>
        )}
      </p>
    </div>
  );
}

function HubMemberJoinedBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubMemberJoinedPayload(item.payload) ? item.payload : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const memberName = payload.member?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">{memberName}</span>{" "}
        {t.inbox.hubMemberJoinedBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {payload.role && (
          <>
            {" "}
            <span className="text-fg-3">
              ({t.inbox.hubInviteRoleLabel}: {payload.role})
            </span>
          </>
        )}
      </p>
    </div>
  );
}

function HubInviteAcceptedBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubInviteAcceptedPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {t.inbox.hubInviteAcceptedBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {payload.role && (
          <>
            {" "}
            <span className="text-fg-3">
              ({t.inbox.hubInviteRoleLabel}: {payload.role})
            </span>
          </>
        )}
      </p>
    </div>
  );
}

function HubInviteDeclinedBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubInviteDeclinedPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const inviteeName = payload.invitee?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">{inviteeName}</span>{" "}
        {t.inbox.hubInviteDeclinedBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {"."}
      </p>
    </div>
  );
}

function HubInviteDeclinedByYouBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubInviteDeclinedByYouPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {t.inbox.hubInviteDeclinedByYouBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {"."}
      </p>
    </div>
  );
}

function HubOwnershipTransferBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasHubOwnershipTransferPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const initiatorName =
    payload.initiator?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">{initiatorName}</span>{" "}
        {t.inbox.hubOwnershipTransferBody}{" "}
        <span className="font-medium text-fg-1">{payload.hub.name}</span>
        {". "}
        {t.inbox.hubOwnershipTransferHint}
      </p>
    </div>
  );
}

function HubOwnershipTransferredBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const { user } = useAuth();
  const payload = hasHubOwnershipTransferredPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const isSelfReceipt = payload.new_owner?.id === user?.id;
  const newOwnerName = payload.new_owner?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {isSelfReceipt ? (
          <>
            {t.inbox.hubOwnershipTransferredSelfBody}{" "}
            <span className="font-medium text-fg-1">{payload.hub.name}</span>
            {"."}
          </>
        ) : (
          <>
            <span className="font-medium text-fg-1">{newOwnerName}</span>{" "}
            {t.inbox.hubOwnershipTransferredBody}{" "}
            <span className="font-medium text-fg-1">{payload.hub.name}</span>
            {"."}
          </>
        )}
      </p>
    </div>
  );
}

function SystemNoticeBody({ item }: { item: InboxItem }) {
  const payload = hasSystemNoticePayload(item.payload) ? item.payload : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {payload.body && (
        <p className="text-[12px] leading-relaxed text-fg-2">{payload.body}</p>
      )}
      {payload.link && (
        <a
          href={payload.link}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-[12px] font-medium"
          style={{ color: "var(--signature)" }}
        >
          {payload.link_text || payload.link}
          <ArrowUpRight className="h-3 w-3" />
        </a>
      )}
    </div>
  );
}

function DreamRunCompletedBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const payload = hasDreamRunCompletedPayload(item.payload)
    ? item.payload
    : null;
  if (!payload) return <FallbackInboxBody item={item} />;

  const lines = [
    payload.counts.merged > 0
      ? interpolate(t.dreams.notificationMerged, {
          n: String(payload.counts.merged),
        })
      : null,
    payload.counts.contradictions > 0
      ? interpolate(t.dreams.notificationContradictions, {
          n: String(payload.counts.contradictions),
        })
      : null,
    payload.counts.archived > 0
      ? interpolate(t.dreams.notificationArchived, {
          n: String(payload.counts.archived),
        })
      : null,
    payload.counts.organized > 0
      ? interpolate(t.dreams.notificationOrganized, {
          n: String(payload.counts.organized),
        })
      : null,
    payload.counts.restructures > 0
      ? interpolate(t.dreams.restructured, {
          n: String(payload.counts.restructures),
        })
      : null,
  ].filter((line): line is string => !!line);
  const cleanRun = lines.length === 0;

  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {interpolate(t.dreams.scanned, {
          n: String(payload.memories_scanned ?? 0),
        })}
      </p>
      {cleanRun && (
        <p className="text-[12px] leading-relaxed text-fg-2">
          {t.dreams.notificationCleanBody}
        </p>
      )}
      {lines.length > 0 && (
        <div className="space-y-1">
          {lines.map((line) => (
            <p key={line} className="text-[12px] leading-relaxed text-fg-2">
              {line}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

function GiftInviteLinkBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const payload = hasGiftInviteLinkPayload(item.payload) ? item.payload : null;
  if (!payload) return <FallbackInboxBody item={item} />;
  const senderName = payload.sender?.display || t.inbox.hubInviteAnonymous;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">{senderName}</span>{" "}
        {t.inbox.giftInviteBody}
        {payload.hub?.name && (
          <>
            {" "}
            <span className="font-medium text-fg-1">{payload.hub.name}</span>
          </>
        )}
        .
      </p>
      {payload.url && (
        <a
          href={payload.url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-[12px] font-medium"
          style={{ color: "var(--signature)" }}
        >
          {t.inbox.giftInviteOpenLink}
          <ArrowUpRight className="h-3 w-3" />
        </a>
      )}
    </div>
  );
}

// DecisionGateInboxBody — the ping for a board decision gate (plan 25
// P2). The collapsed row already shows the gate's question as the
// title; the body adds the context paragraph plus a link to the hub's
// board, where the actual choice happens. Navigation is a link-styled
// button (same visual as the gift-invite / system-notice links) rather
// than hijacking the row's primary click, which every other kind uses
// for expand/collapse.
function DecisionGateInboxBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const router = useRouter();
  const payload = isRecord(item.payload) ? item.payload : undefined;
  const hubSlug = typeof payload?.hub_slug === "string" ? payload.hub_slug : "";
  if (!item.description && !hubSlug) return null;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {item.description && (
        <p className="text-[12px] leading-relaxed text-fg-2">
          {item.description}
        </p>
      )}
      {hubSlug && (
        <button
          type="button"
          onClick={() => {
            // Same rationale as ContradictionBody: let the pathname
            // change drive the popover close instead of calling
            // closeInbox() here.
            router.push(buildMemoriesPath(hubSlug));
          }}
          className="inline-flex cursor-pointer items-center gap-1 text-[12px] font-medium"
          style={{ color: "var(--signature)" }}
        >
          {t.inbox.gateGoToBoard}
          <ArrowUpRight className="h-3 w-3" />
        </button>
      )}
    </div>
  );
}

function ReviewScaffoldBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const note =
    item.kind === "stale"
      ? t.inbox.reviewStaleNote
      : t.inbox.reviewLowConfidenceNote;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {item.description && (
        <p className="text-[12px] leading-relaxed text-fg-2">
          {item.description}
        </p>
      )}
      <p className="text-[11px] italic text-fg-4">{note}</p>
    </div>
  );
}

/**
 * notificationToInboxItem adapts a Phase 3b notification row into
 * the local InboxItem view-model the inbox renderer consumes.
 * Everything the renderer reads is filled in from the notification
 * row + decoded payload — no cross-hub state is touched. Kinds the
 * plan has not promoted to the decision bucket still round-trip so
 * the renderer's fallback body path keeps working.
 */
export function notificationToInboxItem(
  notification: Notification,
  labels: InboxItemLocalization,
  currentUserId?: string,
): InboxItem {
  const rawKind = notification.kind;
  const kind = rawKind.startsWith("review_")
    ? rawKind.slice("review_".length)
    : rawKind;
  let similarity = 0;
  let title = "";
  let description = "";
  if (kind === "contradiction") {
    const payload = notification.payload as
      | ReviewContradictionPayload
      | undefined;
    similarity = payload?.similarity ?? 0;
    description = payload?.reason ?? "";
    if (payload?.memory_a && payload.memory_b) {
      const aTitle = reviewMemoryDisplayTitle(payload.memory_a);
      const bTitle = reviewMemoryDisplayTitle(payload.memory_b);
      title = `Tangled memories: "${aTitle}" vs "${bTitle}"`;
    }
  } else if (kind === "topic_merge") {
    const payload = notification.payload as
      | {
          target_topic?: { id?: string; name?: string };
          source_topics?: Array<{ id?: string; name?: string }>;
          reason?: string;
        }
      | undefined;
    const targetName = pickTopicTitle(payload?.target_topic, labels);
    const sources = payload?.source_topics ?? [];
    const sourceNames = sources.map((source) => pickTopicTitle(source, labels));
    // Build a human sentence that names the actual topics being
    // folded, so the collapsed row is self-explanatory even if the
    // user never expands the body. One source: `Fold "A" into "B"?`.
    // Multiple: `Fold "A", "B" into "C"?`. Empty: fall back to the
    // count-based phrasing to preserve the legacy shape for rows
    // whose payload lost its source_topics entirely.
    if (sourceNames.length > 0) {
      const quoted = sourceNames.map((n) => `"${n}"`).join(", ");
      if (sourceNames.length === 1) {
        title = labels.topicMergeOne({
          source: `"${sourceNames[0]}"`,
          target: `"${targetName}"`,
        });
      } else {
        title = labels.topicMergeMany({
          sources: quoted,
          target: `"${targetName}"`,
        });
      }
    } else {
      title = labels.topicMergeFallback({ target: `"${targetName}"` });
    }
    description = payload?.reason ?? "";
  } else if (kind === "topic_restructure") {
    const payload = notification.payload as
      | {
          child_topic?: { id?: string; name?: string };
          parent_topic?: { id?: string; name?: string };
          reason?: string;
        }
      | undefined;
    const childName = pickTopicTitle(payload?.child_topic, labels);
    const parentName = pickTopicTitle(payload?.parent_topic, labels);
    title = labels.topicRestructure({
      child: `"${childName}"`,
      parent: `"${parentName}"`,
    });
    description = payload?.reason ?? "";
  } else if (kind === "hub_invite") {
    const payload = notification.payload as HubInvitePayload | undefined;
    const hubName = payload?.hub?.name ?? "a hub";
    const inviter = payload?.inviter?.display ?? "Someone";
    title = `${inviter} invited you to ${hubName}`;
  } else if (kind === "hub_member_joined") {
    const payload = notification.payload as HubMemberJoinedPayload | undefined;
    const memberName = payload?.member?.display ?? "Someone";
    const hubName = payload?.hub?.name ?? "your hub";
    title = `${memberName} joined ${hubName}`;
  } else if (kind === "hub_invite_accepted") {
    const payload = notification.payload as
      | HubInviteAcceptedPayload
      | undefined;
    const hubName = payload?.hub?.name ?? "the hub";
    title = `You joined ${hubName}`;
  } else if (kind === "hub_invite_declined") {
    const payload = notification.payload as
      | HubInviteDeclinedPayload
      | undefined;
    const inviteeName = payload?.invitee?.display ?? "Someone";
    const hubName = payload?.hub?.name ?? "your hub";
    title = `${inviteeName} declined your invite to ${hubName}`;
  } else if (kind === "hub_invite_declined_by_you") {
    const payload = notification.payload as
      | HubInviteDeclinedByYouPayload
      | undefined;
    const hubName = payload?.hub?.name ?? "the invite";
    title = `You declined the invite to ${hubName}`;
  } else if (kind === "hub_ownership_transfer") {
    const payload = notification.payload as
      | HubOwnershipTransferPayload
      | undefined;
    const initiator = payload?.initiator?.display ?? "Someone";
    const hubName = payload?.hub?.name ?? "a hub";
    title = `${initiator} wants to transfer ownership of ${hubName} to you`;
  } else if (kind === "hub_ownership_transferred") {
    const payload = notification.payload as
      | HubOwnershipTransferredPayload
      | undefined;
    const newOwner = payload?.new_owner?.display ?? "Someone";
    const hubName = payload?.hub?.name ?? "your hub";
    title =
      payload?.new_owner?.id === currentUserId
        ? labels.hubOwnershipTransferredSelfTitle({ hub: hubName })
        : labels.hubOwnershipTransferredOtherTitle({
            owner: newOwner,
            hub: hubName,
          });
  } else if (kind === "system_notice") {
    const payload = notification.payload as SystemNoticePayload | undefined;
    title = payload?.title ?? "System notice";
    description = payload?.body ?? "";
  } else if (kind === "dream_run_completed") {
    const payload = notification.payload as
      | DreamRunCompletedPayload
      | undefined;
    const counts = payload?.counts;
    const hasOutcome =
      (counts?.merged ?? 0) +
        (counts?.contradictions ?? 0) +
        (counts?.archived ?? 0) +
        (counts?.organized ?? 0) +
        (counts?.restructures ?? 0) >
      0;
    // partial_failed wins over the count-based branch: a run that
    // finished with some phase errors should surface that honestly
    // even if other phases still produced work. Empty status on
    // historical rows means "completed" by contract.
    if (payload?.status === "partial_failed") {
      title = labels.dreamRunCompletedPartialTitle;
    } else {
      title = hasOutcome
        ? labels.dreamRunCompletedTitle
        : labels.dreamRunCompletedCleanTitle;
    }
  } else if (kind === "gift_invite_link") {
    const payload = notification.payload as GiftInviteLinkPayload | undefined;
    const sender = payload?.sender?.display ?? "Someone";
    title = `${sender} sent you a memax invite`;
  } else if (
    kind === "hub_over_limit" ||
    kind === "hub_frozen" ||
    kind === "hub_restored"
  ) {
    const payload = notification.payload as
      | {
          hub?: { name?: string };
          memory_count?: number;
          memory_limit?: number;
        }
      | undefined;
    const hubName = payload?.hub?.name ?? "a hub";
    if (kind === "hub_over_limit") {
      title = `${hubName} is over capacity`;
      description =
        payload?.memory_limit && payload?.memory_limit > 0
          ? `${payload.memory_count ?? 0}/${payload.memory_limit} memories — the hub will freeze in 7 days unless the count is back under the limit.`
          : "The hub is over its plan limit.";
    } else if (kind === "hub_frozen") {
      title = `${hubName} is frozen`;
      description =
        "Pushes are rejected and memories are hidden from recall/ask. Upgrade the plan or delete memories to restore.";
    } else {
      title = `${hubName} is back under capacity`;
      description = "The hub is no longer frozen.";
    }
  } else if (kind === "decision_gate") {
    // Payload written by the Go createDecisionGate ping: title is the
    // gate's question, description its context (both plain text).
    const payload = notification.payload as
      | { title?: string; description?: string }
      | undefined;
    title = payload?.title ?? "";
    description = payload?.description ?? "";
  } else if (kind === "stale" || kind === "low_confidence") {
    // Scaffold kinds — no producer yet. Title stays blank so the row
    // renders the fallback "kind unsupported yet" note.
    title = "";
    description = "";
  }
  return {
    id: notification.id,
    audience: notification.audience,
    hub_id: notification.hub_id,
    kind,
    status: notification.status,
    seen: !!notification.seen_at,
    resolution: notification.resolution,
    title,
    description,
    similarity,
    created_at: notification.created_at,
    payload: notification.payload as InboxItem["payload"],
  };
}

/**
 * ContradictionBody renders a contradiction notification's memory
 * pair directly from the notification payload's memory refs. The
 * ReviewContradictionPayload carries only id + title — enough for a
 * title-and-click row, not enough for a content preview or
 * drag-to-topic. A follow-up can restore the full DraggableMemoryRow
 * once there is either a richer payload shape or a memories
 * batch-get endpoint.
 */
function ContradictionBody({ item }: { item: InboxItem }) {
  const router = useRouter();
  const pathname = usePathname();
  const currentHubSlug = getHubSlugForPath(pathname);
  const isMobile = useIsMobile();
  const payload = hasContradictionPayload(item.payload) ? item.payload : null;
  if (!payload) {
    return <FallbackInboxBody item={item} />;
  }
  const pair: Array<{ id: string; title: string }> = [
    {
      id: payload.memory_a.id,
      title: reviewMemoryDisplayTitle(payload.memory_a),
    },
    {
      id: payload.memory_b.id,
      title: reviewMemoryDisplayTitle(payload.memory_b),
    },
  ];
  return (
    <div className="space-y-0 px-3 pb-3 pt-3">
      {payload.reason && (
        <p className="pb-3 text-[12px] leading-relaxed text-fg-2">
          {payload.reason}
        </p>
      )}
      {pair.map((mem, index) => (
        <button
          key={mem.id}
          type="button"
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-left transition-colors ${
            isMobile ? "" : "hover:bg-surface-1"
          } ${index > 0 ? "mt-1" : ""}`}
          onClick={() => {
            // Do NOT call closeInbox() here. The popover has an
            // auto-close effect wired on pathname change
            // (inbox-surface-context.tsx) — letting the route drive
            // the close keeps the history stack clean so the memory
            // detail page's `router.back()` lands back on the hub
            // that was behind the popover. Calling closeInbox before
            // router.push was racing with base-ui's unmount + focus
            // restore and leaving the back button stranded.
            router.push(buildMemoryDetailPath(currentHubSlug, mem.id));
          }}
        >
          <div className="flex-1 truncate text-[13px] text-fg-1">
            {mem.title}
          </div>
          <ArrowUpRight className="h-3 w-3 shrink-0 text-fg-4" />
        </button>
      ))}
    </div>
  );
}

function FallbackInboxBody({ item }: { item: InboxItem }) {
  const { t } = useLocale();
  const scaffold = SCAFFOLD_REVIEW_KINDS.has(item.kind);
  if (!item.description && !scaffold) return null;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {item.description && (
        <p className="text-[12px] leading-relaxed text-fg-2">
          {item.description}
        </p>
      )}
      {scaffold && (
        <p className="text-[11px] italic text-fg-4">
          {t.reviews.kindUnsupportedYet}
        </p>
      )}
    </div>
  );
}

function inboxKindLabel(
  item: InboxItem,
  t: ReturnType<typeof useLocale>["t"],
): string {
  switch (item.kind) {
    case "contradiction":
      return t.reviews.kindContradiction;
    case "stale":
      return t.reviews.kindStale;
    case "low_confidence":
      return t.reviews.kindLowConfidence;
    case "topic_merge":
      return t.reviews.kindTopicMerge;
    case "topic_restructure":
      return t.reviews.kindTopicRestructure;
    case "hub_invite":
      return t.inbox.kindHubInvite;
    case "hub_invite_accepted":
      return t.inbox.kindHubInviteAccepted;
    case "hub_invite_declined":
      return t.inbox.kindHubInviteDeclined;
    case "hub_invite_declined_by_you":
      return t.inbox.kindHubInviteDeclinedByYou;
    case "hub_member_joined":
      return t.inbox.kindHubMemberJoined;
    case "hub_ownership_transfer":
      return t.inbox.kindHubOwnershipTransfer;
    case "hub_ownership_transferred":
      return t.inbox.kindHubOwnershipTransferred;
    case "dream_run_completed":
      return t.dreams.report;
    case "system_notice":
      return t.inbox.kindSystemNotice;
    case "gift_invite_link":
      return t.inbox.kindGiftInvite;
    case "hub_over_limit":
      return t.inbox.kindHubOverLimit;
    case "hub_frozen":
      return t.inbox.kindHubFrozen;
    case "hub_restored":
      return t.inbox.kindHubRestored;
    case "decision_gate":
      return t.inbox.kindDecisionGate;
    default:
      return t.reviews.title;
  }
}

// InboxKindVisual carries the icon + accent color the compact row
// header uses for a given notification kind. Keeping icon + color in a
// single resolver (instead of two parallel switches) means every new
// kind picks up a semantic icon AND a matching tone in one place.
interface InboxKindVisual {
  icon?: LucideIcon;
  glyph?: string;
  color: string;
}

// Tone buckets — kept as named constants so per-kind entries stay
// declarative and visually coherent. `signature` is the default
// memax accent; `muted` is the fg-3 archival tone used for stale /
// scaffold rows; `warn` is the neutral warn tone used for decline /
// removed-by-you outcomes so positive and negative receipts read
// differently at a glance.
const INBOX_TONE_SIGNATURE = "var(--signature)";
const INBOX_TONE_MUTED = "var(--fg-3)";
const INBOX_TONE_WARN = "var(--fg-2)";

const INBOX_KIND_VISUALS: Record<string, InboxKindVisual> = {
  // Review kinds — decisions that reshape the memory graph.
  // `Waves` for tangled memories evokes two ripples merging into
  // one — closer to memax's "dreaming / consolidating" vocabulary
  // than the clinical AlertTriangle we used before.
  contradiction: { icon: Waves, color: INBOX_TONE_SIGNATURE },
  topic_merge: { icon: GitMerge, color: INBOX_TONE_SIGNATURE },
  topic_restructure: { icon: GitBranch, color: INBOX_TONE_SIGNATURE },
  stale: { icon: AlertCircle, color: INBOX_TONE_MUTED },
  low_confidence: { icon: HelpCircle, color: INBOX_TONE_MUTED },
  // Board decision gate ping — an agent is waiting on the user's call.
  // HelpCircle reads as "a question for you"; signature tone because
  // it's a needs-action row, not an archival one.
  decision_gate: { icon: HelpCircle, color: INBOX_TONE_SIGNATURE },

  // Hub social kinds — invites and membership changes.
  hub_invite: { icon: Mail, color: INBOX_TONE_SIGNATURE },
  hub_invite_accepted: { icon: UserCheck, color: INBOX_TONE_SIGNATURE },
  hub_invite_declined: { icon: UserMinus, color: INBOX_TONE_WARN },
  hub_invite_declined_by_you: { icon: UserMinus, color: INBOX_TONE_WARN },
  hub_member_joined: { icon: UserPlus, color: INBOX_TONE_SIGNATURE },
  hub_ownership_transfer: { icon: Shield, color: INBOX_TONE_SIGNATURE },
  hub_ownership_transferred: { icon: Crown, color: INBOX_TONE_SIGNATURE },

  // Receipt / system kinds.
  dream_run_completed: { glyph: "✦", color: INBOX_TONE_SIGNATURE },
  gift_invite_link: { icon: Gift, color: INBOX_TONE_SIGNATURE },
  system_notice: { icon: Info, color: INBOX_TONE_MUTED },
};

// inboxKindVisual resolves the icon + color for a notification kind,
// falling back to a neutral info icon for any kind that lands before
// its entry is added here (defensive — should never fire in prod
// because every promoted kind above has an entry).
function inboxKindVisual(item: InboxItem): InboxKindVisual {
  return (
    INBOX_KIND_VISUALS[item.kind] ?? {
      icon: Info,
      color: INBOX_TONE_MUTED,
    }
  );
}

// ---------------------------------------------------------------------------
// KIND_DETAIL_COMPONENTS — the scalable layout contract (doc 17 §5.2)
// ---------------------------------------------------------------------------
//
// Each entry is a React component that renders the "detail slot" —
// the content between the collapsed title and the action buttons —
// or `null` when the kind has no detail beyond what the title says.
//
// Adding a new kind = add ONE entry here. No edits to InboxRow, no
// edits to InboxActions, no per-kind wrapper divs. The shell owns
// ALL chrome (chevron, icon, label, title, age, expand animation,
// exit animation, action row padding). The detail slot is the ONLY
// per-kind surface.
//
// Layout rule: if the entry is null, the shell skips the detail block
// entirely and renders the action row directly below the title.
// Receipt kinds that just echo the title (hub_member_joined,
// hub_ownership_transferred, etc.) are null on purpose — the user
// already knows everything from the collapsed row.
const KIND_DETAIL_COMPONENTS: Record<
  string,
  React.FC<{ item: InboxItem }> | null
> = {
  // Decision kinds — the detail adds context the user needs before
  // tapping an action button.
  contradiction: ContradictionBody,
  topic_merge: TopicMergeBody,
  topic_restructure: TopicRestructureBody,
  hub_invite: HubInviteBody,
  hub_ownership_transfer: HubOwnershipTransferBody,
  decision_gate: DecisionGateInboxBody,

  // Receipt kinds that carry payload.role — the title only says
  // "X joined Y" / "You joined Y", so the detail slot is where
  // the role (owner / admin / contributor) surfaces. Dropping
  // these to null would silently hide that info.
  hub_member_joined: HubMemberJoinedBody,
  hub_invite_accepted: HubInviteAcceptedBody,

  // Receipt kinds — title says it all. No detail, just dismiss.
  hub_invite_declined: null,
  hub_invite_declined_by_you: null,
  hub_ownership_transferred: null,
  dream_run_completed: DreamRunCompletedBody,

  // Receipt kinds WITH useful detail (link, extra body text).
  gift_invite_link: GiftInviteLinkBody,
  system_notice: SystemNoticeBody,

  // Scaffold kinds — no producer yet; rendered with a hint note.
  stale: ReviewScaffoldBody,
  low_confidence: ReviewScaffoldBody,
};

export function InboxRow({
  item,
  isExpanded,
  isResolving,
  onToggle,
  onResolve,
  onDismiss,
}: InboxRowProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const isMobile = useIsMobile();
  const isContradiction = item.kind === "contradiction";
  const isSeenReceipt = item.seen && isReceiptInboxKind(item);
  const age = formatAge(item.created_at, t, interpolate);
  const similarity = Math.round((item.similarity ?? 0) * 100);

  const label = inboxKindLabel(item, t);
  const visual = inboxKindVisual(item);
  const iconColor = visual.color;

  const bodyRef = useRef<HTMLDivElement | null>(null);
  const hasAutoFocused = useRef(false);

  // On expand: once the grid-rows height animation settles, scroll
  // the body into view if it was clipped below the fold, then move
  // focus to the first action button inside it. That handoff is what
  // makes Space / Enter fire the primary action naturally (native
  // button click) instead of bubbling up to the list-level keyboard
  // handler and collapsing the row. On collapse we reset the guard
  // so the next expand re-triggers the handoff.
  useEffect(() => {
    if (!isExpanded) {
      hasAutoFocused.current = false;
      return;
    }
    if (hasAutoFocused.current) return;
    hasAutoFocused.current = true;

    const el = bodyRef.current;
    if (!el) return;

    const timerId = window.setTimeout(
      () => {
        el.scrollIntoView({ behavior: "smooth", block: "nearest" });
        const firstButton = el.querySelector<HTMLButtonElement>(
          "button:not([disabled])",
        );
        firstButton?.focus({ preventScroll: true });
      },
      Math.round(NORMAL * 1000),
    );

    return () => window.clearTimeout(timerId);
  }, [isExpanded]);

  // Registry-driven detail slot. The shell owns all chrome; each
  // kind only contributes the detail content between the title and
  // the action buttons (or null when the title says everything).
  // Adding a new kind = one entry in KIND_DETAIL_COMPONENTS above.
  const DetailComponent =
    KIND_DETAIL_COMPONENTS[item.kind] !== undefined
      ? KIND_DETAIL_COMPONENTS[item.kind]
      : FallbackInboxBody;

  return (
    /*
     * Exit animation: when the row is resolving (mutation in flight
     * after a user click), the outer wrapper collapses via the same
     * grid-template-rows `1fr → 0fr` trick used for the expand body
     * AND fades its opacity to 0. `InboxList.resolve` / `.dismiss`
     * delay the actual mutation call by NORMAL*1000 ms so this
     * animation finishes playing before React Query's invalidation
     * unmounts the row. The `border-t` sits on the animating wrapper
     * so it fades out with the rest of the row instead of leaving a
     * 1px ghost line during the transition.
     */
    <div
      className="grid w-full overflow-hidden border-t border-border/20 first:border-t-0"
      style={{
        // Explicit `minmax(0, 1fr)` column: without it the implicit
        // `auto` column sizes to content min-content, which means a
        // long row title can push this grid wider than its parent
        // and bypass the scroll container's `overflow-x-hidden`. With
        // `minmax(0, 1fr)` the column is pinned to the parent's width
        // and children inherit a true shrinkable inline-axis so the
        // `truncate` on the title actually engages.
        gridTemplateColumns: "minmax(0, 1fr)",
        gridTemplateRows: isResolving ? "0fr" : "1fr",
        opacity: isResolving ? 0 : 1,
        transition: `grid-template-rows ${NORMAL}s cubic-bezier(${EASE.join(", ")}), opacity ${NORMAL}s cubic-bezier(${EASE.join(", ")})`,
        pointerEvents: isResolving ? "none" : undefined,
      }}
      aria-hidden={isResolving || undefined}
    >
      <div className="min-h-0 min-w-0">
        <div className="flex items-start">
          <button
            type="button"
            onClick={onToggle}
            disabled={isResolving}
            aria-expanded={isExpanded}
            aria-label={
              isExpanded ? t.inbox.collapseReview : t.inbox.expandReview
            }
            className={`flex h-11 shrink-0 cursor-pointer items-center pl-4 pr-2 text-fg-3 transition-colors ${
              isMobile ? "" : "hover:text-fg-1"
            }`}
          >
            <ChevronRight
              className="h-3 w-3 shrink-0"
              style={{
                transform: isExpanded ? "rotate(90deg)" : "rotate(0deg)",
                transformOrigin: "center",
                transition: `transform ${FAST}s cubic-bezier(${EASE.join(", ")})`,
              }}
            />
          </button>
          <button
            type="button"
            onClick={onToggle}
            disabled={isResolving}
            className={`min-w-0 flex-1 cursor-pointer py-3 pr-4 text-left transition-colors ${
              isMobile ? "" : "hover:bg-surface-1"
            }`}
            aria-label={`${label}: ${item.title}`}
          >
            <div className="flex min-w-0 items-center gap-2">
              {visual.icon ? (
                <visual.icon
                  className="h-3.5 w-3.5 shrink-0"
                  style={{
                    color: iconColor,
                    opacity: isSeenReceipt ? 0.58 : 1,
                  }}
                  aria-hidden
                />
              ) : (
                <span
                  className="shrink-0 text-[13px] leading-none"
                  style={{
                    color: iconColor,
                    opacity: isSeenReceipt ? 0.58 : 1,
                  }}
                  aria-hidden
                >
                  {visual.glyph ?? "✦"}
                </span>
              )}
              <span
                className="shrink-0 text-[11px] font-medium uppercase tracking-wide"
                style={{
                  color: iconColor,
                  opacity: isSeenReceipt ? 0.72 : 1,
                }}
              >
                {label}
              </span>
              {!isAccountLevelInboxItem(item) && item.hub && (
                <InboxHubChip hub={item.hub} />
              )}
              <div className="flex-1" />
              <span className="shrink-0 text-[11px] text-fg-4 tabular-nums">
                {age}
              </span>
            </div>
            <div className="mt-0.5 flex min-w-0 items-center gap-2">
              <div
                className={`min-w-0 flex-1 truncate text-[13px] ${
                  isSeenReceipt ? "text-fg-2" : "text-fg-1"
                }`}
              >
                {item.title}
              </div>
              {isContradiction && (
                <span className="shrink-0 text-[11px] text-fg-4 tabular-nums">
                  {interpolate(t.dreams.similarity, { n: String(similarity) })}
                </span>
              )}
            </div>
          </button>
        </div>

        {/*
         * Expand animation: CSS-only grid-template-rows `0fr → 1fr`
         * trick — the only way to animate `height: auto` without JS
         * measurement. The body stays mounted in both states so the
         * transition can run both directions. `inert` on the collapsed
         * state disables pointer, keyboard, and screen-reader access to
         * the hidden subtree so Tab cannot reach action buttons of a
         * collapsed row.
         */}
        <div
          className="grid w-full"
          inert={!isExpanded}
          style={{
            // Same `minmax(0, 1fr)` column fix as the outer exit-
            // animation wrapper — the expanded body can contain long
            // UUID memory refs and wide topic chips, and without an
            // explicit shrinkable column they would push this grid
            // wider than the row.
            gridTemplateColumns: "minmax(0, 1fr)",
            gridTemplateRows: isExpanded ? "1fr" : "0fr",
            transition: `grid-template-rows ${NORMAL}s cubic-bezier(${EASE.join(", ")})`,
          }}
        >
          <div className="min-w-0 overflow-hidden" ref={bodyRef}>
            {/* Registry-driven detail slot. null = no detail, just
             * action buttons. Component = per-kind content that adds
             * info the collapsed title doesn't already carry. */}
            {DetailComponent && <DetailComponent item={item} />}
            <InboxActions
              item={item}
              disabled={isResolving}
              onResolve={onResolve}
              onDismiss={onDismiss}
            />
          </div>
        </div>
      </div>
    </div>
  );
}

function InboxEmptyState() {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const dreamSurface = useDreamSurfaceState({ receiptScope: "hub" });
  const run = dreamSurface.report?.run;
  const isDreaming = dreamSurface.runStatus === "running";
  const lastRunAt = !isDreaming
    ? pickRenderableRunTimestamp(run?.finished_at, run?.started_at)
    : null;
  const lastRunLine =
    !isDreaming && run && lastRunAt
      ? interpolate(t.inbox.lastRun, {
          time: formatAge(lastRunAt, t, interpolate),
          n: String(run.memories_scanned ?? 0),
        })
      : null;

  return (
    <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
      <MemaxStar
        className="text-[20px] state-slow-breathe"
        style={{ color: "var(--signature)" }}
      />
      <div className="space-y-1">
        <p className="text-[14px] font-medium text-fg-2">
          {isDreaming ? t.topics.dreamingNow : t.inbox.emptyTitle}
        </p>
        <p className="text-[12px] text-fg-3">
          {isDreaming ? t.topics.dreamingNowHint : t.inbox.emptySubtitle}
        </p>
      </div>
      {lastRunLine && <p className="text-[11px] text-fg-4">{lastRunLine}</p>}
    </div>
  );
}

function InboxLoadingState({
  surface,
  rows = 5,
}: {
  surface: "panel" | "page";
  rows?: number;
}) {
  return (
    <div className="animate-content-ready">
      <div className="space-y-0">
        {Array.from({ length: rows }, (_, index) => (
          <div
            key={index}
            className="border-t border-border/20 px-4 py-3 first:border-t-0"
          >
            <div className="flex items-start gap-2">
              <Skeleton className="mt-1 h-3 w-3 rounded-full bg-foreground/[0.07]" />
              <div className="min-w-0 flex-1 space-y-2">
                <div className="flex items-center gap-2">
                  <Skeleton className="h-3 w-20 bg-foreground/[0.07]" />
                  <Skeleton className="ml-auto h-3 w-8 bg-foreground/[0.07]" />
                </div>
                <Skeleton
                  className={`h-4 bg-foreground/[0.07] ${
                    index % 3 === 0
                      ? "w-[86%]"
                      : index % 3 === 1
                        ? "w-[72%]"
                        : "w-[91%]"
                  }`}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function pickRenderableRunTimestamp(
  ...candidates: Array<string | null | undefined>
): string | null {
  for (const candidate of candidates) {
    if (!candidate) continue;
    const parsed = Date.parse(candidate);
    if (!Number.isFinite(parsed)) continue;
    if (parsed < MIN_REASONABLE_RUN_TIMESTAMP_MS) continue;
    if (parsed > Date.now() + 5 * 60 * 1000) continue;
    return candidate;
  }
  return null;
}

function withInboxHubMetadata(
  item: InboxItem,
  hubLookup: Map<string, ReturnType<typeof useAuth>["hubs"][number]["hub"]>,
  t: ReturnType<typeof useLocale>["t"],
  viewer?: { name?: string; display_name?: string },
): InboxItem {
  if (!item.hub_id || isAccountLevelInboxItem(item)) return item;
  const hub = hubLookup.get(item.hub_id);
  if (!hub) return item;
  return {
    ...item,
    hub: {
      name: getHubDisplayName(
        {
          name: hub.name,
          icon: hub.icon,
          hub_type: hub.hub_type,
        },
        t,
        { name: viewer?.name, displayName: viewer?.display_name },
      ),
      kind: hub.hub_type === "team" ? "team" : "personal",
      accent: hub.accent,
      initial: getHubDisplayInitial(
        {
          name: hub.name,
          icon: hub.icon,
          hub_type: hub.hub_type,
        },
        t,
        { name: viewer?.name, displayName: viewer?.display_name },
      ),
    },
  };
}

function InboxList({
  rows: initialRows,
  initialExpandedId,
}: {
  rows: InboxItem[];
  /**
   * If set AND found in `rows`, expand this notification on mount and
   * scroll it into view. Drives the `/inbox/[id]` deep-link route. Only
   * acts on first mount so a parent re-render with the same id doesn't
   * keep re-opening it after the user collapsed it.
   */
  initialExpandedId?: string;
}) {
  const resolveNotification = useResolveNotification();
  const dismissNotification = useNotificationDismiss();
  const markSeenNotification = useNotificationMarkSeen();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [resolvingId, setResolvingId] = useState<string | null>(null);
  const seenDuringSessionRef = useRef<Set<string>>(new Set());
  const rows = initialRows;

  useEffect(() => {
    setExpandedId((current) =>
      current && rows.some((row) => row.id === current) ? current : null,
    );
    setResolvingId(null);
  }, [rows]);

  // Deep-link expansion: /inbox/[id] passes initialExpandedId. Once
  // the matching row appears in `rows` (post-fetch), expand it and
  // scroll into view. Run-once via the ref guard so the user can
  // collapse manually without it snapping back open.
  const deepLinkAppliedRef = useRef(false);
  useEffect(() => {
    if (deepLinkAppliedRef.current) return;
    if (!initialExpandedId) return;
    if (!rows.some((row) => row.id === initialExpandedId)) return;
    deepLinkAppliedRef.current = true;
    setExpandedId(initialExpandedId);
    // Wait one frame so the row's DOM is mounted before scroll.
    requestAnimationFrame(() => {
      const el = document.getElementById(`inbox-row-${initialExpandedId}`);
      el?.scrollIntoView({ behavior: "auto", block: "start" });
    });
  }, [initialExpandedId, rows]);

  const markSeenIfNeeded = useCallback(
    (item: InboxItem) => {
      if (item.seen || seenDuringSessionRef.current.has(item.id)) return;
      seenDuringSessionRef.current.add(item.id);
      markSeenNotification.mutate(item.id, {
        onError: () => {
          seenDuringSessionRef.current.delete(item.id);
        },
      });
    },
    [markSeenNotification],
  );

  const setExpanded = useCallback(
    (item: InboxItem) => {
      if (resolvingId) return;
      setExpandedId((current) => {
        const nextExpanded = current === item.id ? null : item.id;
        if (nextExpanded) {
          markSeenIfNeeded(item);
        }
        return nextExpanded;
      });
    },
    [markSeenIfNeeded, resolvingId],
  );

  const resolve = useCallback(
    (id: string, action: InboxResolveAction) => {
      if (resolvingId) return;
      setResolvingId(id);
      if (expandedId === id) {
        setExpandedId(null);
      }
      resolveNotification.mutate(
        { id, action: action as NotificationResolveAction },
        {
          onError: () => {
            setResolvingId(null);
          },
          onSettled: () => {
            setResolvingId(null);
          },
        },
      );
    },
    [expandedId, resolveNotification, resolvingId],
  );

  const dismiss = useCallback(
    (id: string) => {
      if (resolvingId) return;
      setResolvingId(id);
      if (expandedId === id) {
        setExpandedId(null);
      }
      dismissNotification.mutate(id, {
        onError: () => {
          setResolvingId(null);
        },
        onSettled: () => {
          setResolvingId(null);
        },
      });
    },
    [dismissNotification, expandedId, resolvingId],
  );

  if (rows.length === 0) return <InboxEmptyState />;

  return (
    <div>
      {rows.map((row) => (
        // The wrapping div carries the id so deep links
        // (`/inbox/<id>`) can scrollIntoView the target row even
        // before InboxRow itself fully mounts.
        <div key={row.id} id={`inbox-row-${row.id}`}>
          <InboxRow
            item={row}
            isExpanded={expandedId === row.id}
            isResolving={resolvingId === row.id}
            onToggle={() => {
              setExpanded(row);
            }}
            onResolve={resolve}
            onDismiss={dismiss}
          />
        </div>
      ))}
    </div>
  );
}

function InboxPanelContent({
  surface,
  initialExpandedId,
}: {
  surface: "panel" | "page";
  initialExpandedId?: string;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const isMobile = useIsMobile();
  const { hubs, user } = useAuth();
  // The inbox badge is driven by the unscoped notification summary,
  // so the compact panel must read the same global pending queue.
  // Scoping the panel to activeHubId hid addressed user-audience
  // rows like hub_invite whenever the recipient was not already a
  // member of that hub, which is exactly when they most need to see
  // the invite.
  const query = { status: "pending" as const };
  const {
    data: notificationListData,
    isError,
    isLoading,
    refetch,
  } = useNotifications(query);
  const { closeInbox } = useInboxSurface();
  const inboxItemLocalization = useMemo<InboxItemLocalization>(
    () => ({
      topicUntitled: t.reviews.topicUntitled,
      topicRemoved: t.reviews.topicRemoved,
      dreamRunCompletedTitle: t.dreams.notificationTitle,
      dreamRunCompletedCleanTitle: t.dreams.notificationCleanTitle,
      dreamRunCompletedPartialTitle: t.dreams.notificationPartialTitle,
      topicMergeOne: ({ source, target }) =>
        interpolate(t.inbox.topicMergeOneTitle, { source, target }),
      topicMergeMany: ({ sources, target }) =>
        interpolate(t.inbox.topicMergeManyTitle, { sources, target }),
      topicMergeFallback: ({ target }) =>
        interpolate(t.inbox.topicMergeFallbackTitle, { target }),
      topicRestructure: ({ child, parent }) =>
        interpolate(t.inbox.topicRestructureTitle, { child, parent }),
      hubOwnershipTransferredSelfTitle: ({ hub }) =>
        interpolate(t.inbox.hubOwnershipTransferredSelfTitle, { hub }),
      hubOwnershipTransferredOtherTitle: ({ owner, hub }) =>
        interpolate(t.inbox.hubOwnershipTransferredOtherTitle, { owner, hub }),
    }),
    [interpolate, t],
  );
  // Pending onboarding rows render as a hero section above the row
  // list (same visuals as /memories' pinned hero) — the inbox's
  // kind-switch doesn't have checklist/digest cases, so without this
  // mount the row list silently skips them and the user sees an empty
  // inbox while the unread dot insists there's something pending.
  // Plan-18 onboarding-welcome system_notice + onboarding checklist
  // are the two kinds that fall in this gap today.
  const onboardingHeroRows = useMemo(() => {
    const rows = notificationListData?.notifications ?? [];
    return rows
      .filter((n) => {
        const kind = n.kind;
        const sourceKind = (n as { source_kind?: string }).source_kind ?? "";
        if (kind === "checklist" && sourceKind === "onboarding") return true;
        if (kind === "system_notice" && sourceKind === "onboarding_welcome")
          return true;
        return false;
      })
      .sort((a, b) => {
        if (a.priority !== b.priority) return b.priority - a.priority;
        return (
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        );
      });
  }, [notificationListData]);
  const onboardingHeroIds = useMemo(
    () => new Set(onboardingHeroRows.map((n) => n.id)),
    [onboardingHeroRows],
  );
  const pendingItems = useMemo(() => {
    const hubLookup = new Map(hubs.map((entry) => [entry.hub.id, entry.hub]));
    const rows = notificationListData?.notifications ?? [];
    // Drop rows the hero already renders so they don't dual-display.
    return rows
      .filter((row) => !onboardingHeroIds.has(row.id))
      .map((row) =>
        withInboxHubMetadata(
          notificationToInboxItem(row, inboxItemLocalization, user?.id),
          hubLookup,
          t,
          user ?? undefined,
        ),
      );
  }, [
    hubs,
    inboxItemLocalization,
    notificationListData,
    onboardingHeroIds,
    t,
    user,
  ]);
  const showQueryError = isError && !notificationListData;
  const showInitialLoading = isLoading && !notificationListData;
  const showViewAll =
    surface === "panel" && pendingItems.length > INBOX_VIEW_ALL_THRESHOLD;
  const showHero = onboardingHeroRows.length > 0;
  // The "your inbox is empty" state should only show when there are
  // genuinely no pending rows of any kind — hero rows count too.
  // Otherwise a new user with a pending onboarding checklist would
  // see the empty state above the hero, which is contradictory.
  const showEmpty = pendingItems.length === 0 && !showHero;

  return (
    // When surface="panel" the outer PopoverContent owns the glass
    // material (.glass-dropdown), so the inner container must be a bare
    // flex wrapper — otherwise the two glass recipes stack and the
    // panel reads muddy. On the standalone /inbox route (surface="page")
    // there is no outer chrome, so the inner keeps its own glass card.
    <Surface
      variant={surface === "panel" ? "borderless" : undefined}
      rounded="2xl"
      className={`flex flex-col overflow-hidden ${surface === "panel" ? "w-[min(440px,calc(100vw-32px))]" : "w-full"}`}
      style={
        surface === "panel"
          ? { maxHeight: "70vh" }
          : {
              background: "oklch(from var(--card) l c h / 0.95)",
              border: "1px solid oklch(from var(--border) l c h / 0.5)",
              backdropFilter: "blur(24px) saturate(140%)",
              WebkitBackdropFilter: "blur(24px) saturate(140%)",
            }
      }
    >
      {/* Panel surface only: the popover has no other title. On the
          /inbox page the PageHeader above already says "Inbox" (and
          mobile adds the top-bar tab title) — a third label inside the
          card was pure repetition (audit E6). The pending count moves
          into the PageHeader subtitle territory via the rows below. */}
      {surface === "panel" && (
        <SectionHeader
          icon={Inbox}
          label={t.inbox.title}
          trailing={
            pendingItems.length > 0 ? (
              <span className="text-[12px] text-fg-3 tabular-nums">
                {pendingItems.length}
              </span>
            ) : undefined
          }
        />
      )}

      {showQueryError ? (
        <div className="p-3">
          <ContentError
            message={t.inbox.loadFailed}
            detail={t.inbox.loadFailedHint}
            retryLabel={t.states.error.retry}
            onRetry={() => {
              void refetch();
            }}
          />
        </div>
      ) : showInitialLoading ? (
        <InboxLoadingState
          surface={surface}
          rows={surface === "panel" ? 4 : 6}
        />
      ) : showEmpty ? (
        <div className="animate-content-ready">
          <InboxEmptyState />
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain animate-content-ready">
          {showHero && (
            // Onboarding hero: pending checklist + founder welcome
            // render as compact pinned cards above the row list. Same
            // visuals as /memories' pinned hero (PinnedDispatch is
            // the shared dispatch table). The panel surface gets
            // tighter padding to fit within the dropdown's
            // glass-dropdown geometry.
            <div
              className={
                surface === "panel"
                  ? "flex flex-col gap-2 px-3 pt-3 pb-1"
                  : "flex flex-col gap-3 px-4 pt-4 pb-2"
              }
            >
              {onboardingHeroRows.map((n) => (
                <PinnedDispatch key={n.id} notification={n} />
              ))}
            </div>
          )}
          {showViewAll && (
            <button
              type="button"
              className={`sticky top-0 z-10 flex w-full cursor-pointer items-center justify-between gap-2 border-b border-border/20 bg-card px-4 py-2 text-[11px] text-fg-3 transition-colors ${
                isMobile ? "" : "hover:bg-surface-1 hover:text-fg-1"
              }`}
              onClick={() => {
                closeInbox();
                router.push("/inbox");
              }}
            >
              <span>
                {interpolate(t.inbox.viewAll, {
                  n: String(pendingItems.length),
                })}
              </span>
              <ArrowUpRight className="h-3 w-3" />
            </button>
          )}
          {pendingItems.length > 0 && (
            <InboxList
              rows={pendingItems}
              initialExpandedId={initialExpandedId}
            />
          )}
        </div>
      )}
    </Surface>
  );
}

export function InboxControl() {
  const { t } = useLocale();
  const isMobile = useIsMobile();
  // Badge-dot reads from the notification summary. Needs-action rows
  // get the strong signature dot; unseen update/receipt rows get a
  // subtler neutral dot so the top-level chrome communicates urgency,
  // not detailed taxonomy.
  const { data: notificationSummary } = useNotificationSummary();
  const hasNeedsAction = (notificationSummary?.needs_action_pending ?? 0) > 0;
  const hasUpdatesUnseen = (notificationSummary?.updates_unseen ?? 0) > 0;
  const badgeTone = hasNeedsAction
    ? "needs-action"
    : hasUpdatesUnseen
      ? "updates"
      : null;
  const { open, setOpen, openInbox } = useInboxSurface();
  const pendingQuery = useMemo(() => ({ status: "pending" as const }), []);

  useEffect(() => {
    void prefetchNotifications(pendingQuery);
  }, [pendingQuery]);

  const warmInbox = useCallback(() => {
    void prefetchNotifications(pendingQuery);
  }, [pendingQuery]);

  const triggerContent = (
    <>
      <Inbox className="h-4 w-4 text-fg-2" strokeWidth={1.8} />
      {badgeTone && (
        <span
          className="absolute right-[7px] top-[7px] h-[7px] w-[7px] rounded-full"
          style={{
            background:
              badgeTone === "needs-action"
                ? "var(--signature)"
                : "oklch(from var(--foreground) l c h / 0.42)",
            boxShadow: "0 0 0 1.5px var(--card)",
          }}
        />
      )}
    </>
  );

  if (isMobile) {
    return (
      <button
        type="button"
        aria-label={t.inbox.open}
        onClick={openInbox}
        onMouseEnter={warmInbox}
        onFocus={warmInbox}
        className="relative flex h-8 w-8 items-center justify-center rounded-chrome transition-colors hover:bg-surface-2 cursor-pointer"
      >
        {triggerContent}
      </button>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={t.inbox.open}
        onMouseEnter={warmInbox}
        onFocus={warmInbox}
        className="relative flex h-8 w-8 items-center justify-center rounded-chrome transition-colors hover:bg-surface-2 cursor-pointer"
      >
        {triggerContent}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        side="bottom"
        sideOffset={8}
        className="overflow-hidden p-0"
      >
        <InboxPanelContent surface="panel" />
      </PopoverContent>
    </Popover>
  );
}

export function InboxView({
  initialExpandedId,
}: { initialExpandedId?: string } = {}) {
  const { t } = useLocale();
  return (
    <PageShell>
      <PageHeader title={t.inbox.title} subtitle={t.inbox.pageSubtitle} />
      <InboxPanelContent surface="page" initialExpandedId={initialExpandedId} />
    </PageShell>
  );
}
