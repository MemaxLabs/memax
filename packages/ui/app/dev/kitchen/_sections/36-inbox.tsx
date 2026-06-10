"use client";

import { useState, type ReactNode } from "react";
import {
  AlertCircle,
  ArrowUpRight,
  ChevronRight,
  Inbox,
  X,
} from "lucide-react";
import { DemoCard, Section } from "../_shared";
import { Surface } from "@memaxlabs/ui";
import { FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import { DREAM_BANNER } from "@memaxlabs/ui/tokens/dream";

type BarKind =
  | "dreaming"
  | "dream_complete"
  | "update"
  | "info"
  | "success"
  | "error";

type InboxKind =
  | "contradiction"
  | "topic_merge"
  | "topic_restructure"
  | "hub_invite"
  | "hub_member_joined"
  | "system_notice"
  | "gift_invite_link"
  | "dream_run_completed"
  | "stale"
  | "low_confidence";

type InboxTone = "decision" | "receipt" | "scaffold";

interface TopicRef {
  id: string;
  name: string;
  memory_count?: number;
}

interface InboxItem {
  id: string;
  kind: InboxKind;
  tone: InboxTone;
  age: string;
  title: string;
  description?: string;
  similarity?: number;
  payload?: Record<string, unknown>;
}

interface BarItem {
  kind: BarKind;
  message: string;
  detail?: string;
  actionLabel?: string;
  dismiss?: boolean;
}

const BAR_ITEMS: BarItem[] = [
  {
    kind: "dreaming",
    message: "Dreaming...",
    detail: "12 memories in progress",
  },
  {
    kind: "dream_complete",
    message: "Dream organized 7 memories",
    detail: "last night · 18 scanned",
    actionLabel: "View",
    dismiss: true,
  },
  {
    kind: "update",
    message: "Hub switched",
    detail: "memax-test",
    dismiss: true,
  },
  {
    kind: "info",
    message: "Copied invite link",
    dismiss: true,
  },
  {
    kind: "success",
    message: "Memory saved",
    dismiss: true,
  },
  {
    kind: "error",
    message: "Push failed",
    detail: "Try again",
    dismiss: true,
  },
];

const INBOX_ITEMS: InboxItem[] = [
  {
    id: "contradiction-1",
    kind: "contradiction",
    tone: "decision",
    age: "3h",
    title:
      'Conflicting memories: "PKCE for native apps: use S256" vs "PKCE native apps should use plain"',
    description:
      "Both memories discuss PKCE but give conflicting guidance. Keep one, keep both, or dismiss.",
    similarity: 87,
    payload: {
      reason:
        "Both memories discuss PKCE but give conflicting guidance. Keep one, keep both, or dismiss.",
      memory_a: { id: "mem-a", title: "PKCE for native apps: use S256" },
      memory_b: {
        id: "mem-b",
        title: "PKCE native apps should use plain",
      },
    },
  },
  {
    id: "topic-merge-1",
    kind: "topic_merge",
    tone: "decision",
    age: "6h",
    title: 'Merge 3 topics into "OAuth"?',
    description:
      "These topics overlap heavily and should likely be consolidated into a single topic.",
    payload: {
      target_topic: { id: "oauth", name: "OAuth", memory_count: 12 },
      source_topics: [
        { id: "pkce", name: "PKCE", memory_count: 4 },
        { id: "oauth-native", name: "OAuth Native", memory_count: 3 },
      ],
    },
  },
  {
    id: "topic-restructure-1",
    kind: "topic_restructure",
    tone: "decision",
    age: "9h",
    title: 'Move "Connection pool" under "Postgres"?',
    description:
      "The child topic fits better under Postgres than its current parent.",
    payload: {
      child_topic: { id: "pool", name: "Connection pool", memory_count: 6 },
      parent_topic: { id: "pg", name: "Postgres", memory_count: 14 },
    },
  },
  {
    id: "hub-invite-1",
    kind: "hub_invite",
    tone: "decision",
    age: "1d",
    title: "Annie invited you to memax-test",
    payload: {
      inviter: { display: "Annie" },
      hub: { name: "memax-test" },
      role: "contributor",
    },
  },
  {
    id: "member-joined-1",
    kind: "hub_member_joined",
    tone: "receipt",
    age: "2h",
    title: "Lina joined memax-test",
    payload: {
      member: { display: "Lina" },
      hub: { name: "memax-test" },
      role: "viewer",
    },
  },
  {
    id: "system-notice-1",
    kind: "system_notice",
    tone: "receipt",
    age: "1d",
    title: "Scheduled maintenance",
    payload: {
      body: "The API will restart during the maintenance window tonight.",
      link: "https://status.memax.app",
      link_text: "View status",
    },
  },
  {
    id: "gift-invite-1",
    kind: "gift_invite_link",
    tone: "receipt",
    age: "2d",
    title: "Chris sent you a Memax invite",
    payload: {
      sender: { display: "Chris" },
      hub: { name: "Growth" },
      url: "https://memax.app/invite/example",
    },
  },
  {
    id: "dream-complete-1",
    kind: "dream_run_completed",
    tone: "receipt",
    age: "14h",
    title: "Dream organized 7 memories",
    description: "This is the durable inbox receipt after the bar push.",
  },
  {
    id: "stale-1",
    kind: "stale",
    tone: "scaffold",
    age: "2d",
    title: "",
    description:
      "Stale review kind exists in the current system, but only as a scaffolded/fallback row.",
  },
  {
    id: "low-confidence-1",
    kind: "low_confidence",
    tone: "scaffold",
    age: "3d",
    title: "",
    description:
      "Low-confidence review kind exists in the current system, but only as a scaffolded/fallback row.",
  },
];

function barStyle(kind: BarKind) {
  switch (kind) {
    case "dreaming":
      return {
        accent: DREAM_BANNER.dreaming.accent,
        bg: DREAM_BANNER.dreaming.bg,
        icon: "✦",
        animated: true,
      };
    case "dream_complete":
      return {
        accent: DREAM_BANNER.dream.accent,
        bg: DREAM_BANNER.dream.bg,
        icon: "✦",
        animated: false,
      };
    case "update":
      return {
        accent: "var(--signature)",
        bg: "linear-gradient(135deg, oklch(from var(--signature) l c h / 0.08), oklch(from var(--signature) l c h / 0.04))",
        icon: "✦",
        animated: false,
      };
    case "success":
      return {
        accent: "var(--fg-1)",
        bg: "transparent",
        icon: "✓",
        animated: false,
      };
    case "error":
      return {
        accent: "var(--destructive)",
        bg: "oklch(from var(--destructive) l c h / 0.06)",
        icon: "✕",
        animated: false,
      };
    case "info":
    default:
      return {
        accent: "var(--fg-1)",
        bg: "transparent",
        icon: "✦",
        animated: false,
      };
  }
}

function BarNotificationPreview({ item }: { item: BarItem }) {
  const style = barStyle(item.kind);
  const dreamFamily =
    item.kind === "dreaming" || item.kind === "dream_complete";

  return (
    <div
      className={`notif-glass${dreamFamily ? " is-dream" : ""} relative w-full overflow-hidden`}
      style={{
        height: "var(--notif-outer)",
        borderRadius: "var(--app-radius-surface)",
      }}
    >
      <div
        className={`pointer-events-none absolute inset-0 ${style.animated ? "animate-dream-banner" : ""}`}
        style={{ background: style.bg }}
      />
      <div className="relative z-10 flex h-full items-center gap-2 px-4 sm:px-5">
        <span
          className={`shrink-0 text-[13px] ${item.kind === "dreaming" || item.kind === "info" ? "state-slow-breathe" : ""}`}
          style={{ color: style.accent }}
        >
          {style.icon}
        </span>
        <span className="flex-1 truncate text-[13px] text-fg-2">
          <span className="font-medium" style={{ color: style.accent }}>
            {item.message}
          </span>
          {item.detail && <span className="text-fg-3"> · {item.detail}</span>}
        </span>
        {item.actionLabel && (
          <button
            type="button"
            className="relative shrink-0 cursor-pointer px-1 text-[13px] font-medium text-fg-2 transition-colors hover:text-fg-1"
          >
            {item.actionLabel}
          </button>
        )}
        {item.dismiss && (
          <button
            type="button"
            aria-label="Dismiss"
            className="relative flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg-1"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

function TopicChip({ topic }: { topic: TopicRef }) {
  return (
    <div
      className="flex min-w-0 flex-col gap-0.5 rounded-lg border border-border/40 bg-surface-1/60 px-3 py-2"
      style={{ minWidth: 140 }}
    >
      <div className="truncate text-[13px] font-medium text-fg-1">
        {topic.name}
      </div>
      {typeof topic.memory_count === "number" && (
        <div className="text-[11px] text-fg-3 tabular-nums">
          {topic.memory_count} memories
        </div>
      )}
    </div>
  );
}

function ContradictionBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        reason?: string;
        memory_a: { id: string; title: string };
        memory_b: { id: string; title: string };
      }
    | undefined;
  if (!payload) return <FallbackBody item={item} />;
  const pair = [payload.memory_a, payload.memory_b];
  return (
    <div className="space-y-0 px-3 pb-3 pt-3">
      {payload.reason && (
        <p className="pb-3 text-[12px] leading-relaxed text-fg-2">
          {payload.reason}
        </p>
      )}
      {pair.map((memory, index) => (
        <button
          key={memory.id}
          type="button"
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-left transition-colors hover:bg-surface-1 ${
            index > 0 ? "mt-1" : ""
          }`}
        >
          <div className="flex-1 truncate text-[13px] text-fg-1">
            {memory.title}
          </div>
          <ArrowUpRight className="h-3 w-3 shrink-0 text-fg-4" />
        </button>
      ))}
    </div>
  );
}

function TopicMergeBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        target_topic: TopicRef;
        source_topics: TopicRef[];
      }
    | undefined;
  return (
    <div className="space-y-3 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {item.description}
      </p>
      {payload && (
        <div className="flex flex-wrap items-center gap-2">
          {payload.source_topics.map((topic) => (
            <TopicChip key={topic.id} topic={topic} />
          ))}
          <span className="text-[11px] text-fg-4">→</span>
          <TopicChip topic={payload.target_topic} />
        </div>
      )}
    </div>
  );
}

function TopicRestructureBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        child_topic: TopicRef;
        parent_topic: TopicRef;
      }
    | undefined;
  return (
    <div className="space-y-3 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        {item.description}
      </p>
      {payload && (
        <div className="flex flex-wrap items-center gap-2">
          <TopicChip topic={payload.child_topic} />
          <span className="text-[11px] text-fg-4">→</span>
          <TopicChip topic={payload.parent_topic} />
        </div>
      )}
    </div>
  );
}

function HubInviteBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        inviter?: { display?: string };
        hub?: { name?: string };
        role?: string;
      }
    | undefined;
  if (!payload) return <FallbackBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">
          {payload.inviter?.display ?? "Someone"}
        </span>{" "}
        invited you to{" "}
        <span className="font-medium text-fg-1">
          {payload.hub?.name ?? "a hub"}
        </span>
        {payload.role && (
          <span className="text-fg-3"> (role: {payload.role})</span>
        )}
      </p>
    </div>
  );
}

function HubMemberJoinedBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        member?: { display?: string };
        hub?: { name?: string };
        role?: string;
      }
    | undefined;
  if (!payload) return <FallbackBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">
          {payload.member?.display ?? "Someone"}
        </span>{" "}
        joined{" "}
        <span className="font-medium text-fg-1">
          {payload.hub?.name ?? "your hub"}
        </span>
        {payload.role && (
          <span className="text-fg-3"> (role: {payload.role})</span>
        )}
      </p>
    </div>
  );
}

function SystemNoticeBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        body?: string;
        link?: string;
        link_text?: string;
      }
    | undefined;
  if (!payload) return <FallbackBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {payload.body && (
        <p className="text-[12px] leading-relaxed text-fg-2">{payload.body}</p>
      )}
      {payload.link && (
        <a
          href={payload.link}
          className="inline-flex items-center gap-1 text-[12px] font-medium"
          style={{ color: "var(--signature)" }}
        >
          {payload.link_text ?? payload.link}
          <ArrowUpRight className="h-3 w-3" />
        </a>
      )}
    </div>
  );
}

function GiftInviteLinkBody({ item }: { item: InboxItem }) {
  const payload = item.payload as
    | {
        sender?: { display?: string };
        hub?: { name?: string };
        url?: string;
      }
    | undefined;
  if (!payload) return <FallbackBody item={item} />;
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      <p className="text-[12px] leading-relaxed text-fg-2">
        <span className="font-medium text-fg-1">
          {payload.sender?.display ?? "Someone"}
        </span>{" "}
        sent you a Memax invite
        {payload.hub?.name && (
          <>
            {" "}
            for{" "}
            <span className="font-medium text-fg-1">{payload.hub.name}</span>
          </>
        )}
        .
      </p>
      {payload.url && (
        <a
          href={payload.url}
          className="inline-flex items-center gap-1 text-[12px] font-medium"
          style={{ color: "var(--signature)" }}
        >
          Open invite link
          <ArrowUpRight className="h-3 w-3" />
        </a>
      )}
    </div>
  );
}

function ReceiptBody({ item }: { item: InboxItem }) {
  return (
    <div className="space-y-2 px-3 pb-3 pt-3">
      {item.description && (
        <p className="text-[12px] leading-relaxed text-fg-2">
          {item.description}
        </p>
      )}
    </div>
  );
}

function FallbackBody({ item }: { item: InboxItem }) {
  const scaffold = item.kind === "stale" || item.kind === "low_confidence";
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
          {item.kind === "stale"
            ? "Shipped today as a scaffolded stale-review row only."
            : "Shipped today as a scaffolded low-confidence row only."}
        </p>
      )}
    </div>
  );
}

function actionButtons(item: InboxItem) {
  switch (item.kind) {
    case "contradiction":
      return [
        { label: "Keep A", tone: "dream" },
        { label: "Keep B", tone: "dream" },
        { label: "Keep Both", tone: "dream" },
        { label: "Dismiss", tone: "ghost" },
      ];
    case "topic_merge":
      return [
        { label: "Merge", tone: "dream" },
        { label: "Keep Separate", tone: "dream" },
        { label: "Dismiss", tone: "ghost" },
      ];
    case "topic_restructure":
      return [
        { label: "Apply", tone: "dream" },
        { label: "Keep", tone: "dream" },
        { label: "Dismiss", tone: "ghost" },
      ];
    case "hub_invite":
      return [
        { label: "Accept", tone: "dream" },
        { label: "Decline", tone: "ghost" },
      ];
    default:
      return [];
  }
}

function InboxActions({ item }: { item: InboxItem }) {
  if (item.tone === "receipt") {
    return (
      <div
        className="mx-3 mb-3 rounded-xl px-3 py-3"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.03)",
          border: "1px solid oklch(from var(--border) l c h / 0.45)",
        }}
      >
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className="ml-auto min-h-8 cursor-pointer rounded-lg px-2.5 text-[12px] font-medium text-fg-4 transition-colors hover:text-fg-3"
          >
            Dismiss
          </button>
        </div>
      </div>
    );
  }

  if (item.tone === "scaffold") return null;

  return (
    <div
      className="mx-3 mb-3 rounded-xl px-3 py-3"
      style={{
        background: "oklch(from var(--foreground) l c h / 0.03)",
        border: "1px solid oklch(from var(--border) l c h / 0.45)",
      }}
    >
      <div className="flex flex-wrap items-center gap-2">
        {actionButtons(item).map((button) => (
          <button
            key={button.label}
            type="button"
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
    </div>
  );
}

function inboxLabel(item: InboxItem) {
  switch (item.kind) {
    case "contradiction":
      return "Contradiction";
    case "topic_merge":
      return "Topic Merge";
    case "topic_restructure":
      return "Topic Restructure";
    case "hub_invite":
      return "Hub Invite";
    case "hub_member_joined":
      return "Hub Member Joined";
    case "system_notice":
      return "System Notice";
    case "gift_invite_link":
      return "Gift Invite";
    case "dream_run_completed":
      return "Dream Completed";
    case "stale":
      return "Stale";
    case "low_confidence":
      return "Low Confidence";
  }
}

function bodyForItem(item: InboxItem): ReactNode {
  switch (item.kind) {
    case "contradiction":
      return <ContradictionBody item={item} />;
    case "topic_merge":
      return <TopicMergeBody item={item} />;
    case "topic_restructure":
      return <TopicRestructureBody item={item} />;
    case "hub_invite":
      return <HubInviteBody item={item} />;
    case "hub_member_joined":
      return <HubMemberJoinedBody item={item} />;
    case "system_notice":
      return <SystemNoticeBody item={item} />;
    case "gift_invite_link":
      return <GiftInviteLinkBody item={item} />;
    case "dream_run_completed":
      return <ReceiptBody item={item} />;
    case "stale":
    case "low_confidence":
      return <FallbackBody item={item} />;
  }
}

function InboxRowPreview({
  item,
  defaultExpanded = true,
}: {
  item: InboxItem;
  defaultExpanded?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  return (
    <div className="border-t border-border/20 first:border-t-0">
      <div className="flex items-start">
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="flex h-11 shrink-0 cursor-pointer items-center pl-4 pr-2 text-fg-3 transition-colors hover:text-fg-1"
        >
          <ChevronRight
            className="h-3 w-3 shrink-0"
            style={{
              transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
              transformOrigin: "center",
              transition: `transform ${FAST}s ${EASE}`,
            }}
          />
        </button>
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="flex-1 cursor-pointer py-3 pr-4 text-left transition-colors hover:bg-surface-1"
          aria-label={`${inboxLabel(item)}: ${item.title || "Unsupported kind"}`}
        >
          <div className="flex min-w-0 items-center gap-2">
            <AlertCircle
              className="h-3.5 w-3.5 shrink-0"
              style={{
                color:
                  item.kind === "stale" ? "var(--fg-3)" : "var(--signature)",
              }}
            />
            <span
              className="shrink-0 text-[11px] font-medium uppercase tracking-wide"
              style={{
                color:
                  item.kind === "stale" ? "var(--fg-3)" : "var(--signature)",
              }}
            >
              {inboxLabel(item)}
            </span>
            <div className="flex-1" />
            <span className="shrink-0 text-[11px] text-fg-4 tabular-nums">
              {item.age}
            </span>
          </div>
          <div className="mt-0.5 flex items-center gap-2">
            <div className="min-w-0 flex-1 truncate text-[13px] text-fg-1">
              {item.title || "Kind unsupported yet"}
            </div>
            {typeof item.similarity === "number" && (
              <span className="shrink-0 text-[11px] text-fg-4 tabular-nums">
                {item.similarity}%
              </span>
            )}
          </div>
        </button>
      </div>
      {expanded && (
        <div className="overflow-hidden">
          <div className="px-4 pb-4">
            <div className="overflow-hidden rounded-xl border border-border/40 bg-background">
              {bodyForItem(item)}
              <InboxActions item={item} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function InboxPanelPreview({
  title,
  items,
  note,
}: {
  title: string;
  items: InboxItem[];
  note?: string;
}) {
  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <p className="text-[13px] font-medium text-fg-1">{title}</p>
        {note && <p className="text-[12px] text-fg-3">{note}</p>}
      </div>
      <Surface
        rounded="2xl"
        className="overflow-hidden"
        style={{
          background: "oklch(from var(--card) l c h / 0.95)",
          border: "1px solid oklch(from var(--border) l c h / 0.5)",
          backdropFilter: "blur(24px) saturate(140%)",
          WebkitBackdropFilter: "blur(24px) saturate(140%)",
        }}
      >
        <div className="flex items-center justify-between border-b border-border/30 px-4 py-3">
          <div className="flex items-center gap-2">
            <Inbox className="h-4 w-4 text-fg-2" />
            <span className="text-[13px] font-medium text-fg-1">Inbox</span>
          </div>
          <span className="text-[12px] text-fg-3 tabular-nums">
            {items.length}
          </span>
        </div>
        <div className="divide-y divide-border/20">
          {items.map((item) => (
            <InboxRowPreview key={item.id} item={item} />
          ))}
        </div>
      </Surface>
    </div>
  );
}

export function InboxSection() {
  const decisionItems = INBOX_ITEMS.filter((item) => item.tone === "decision");
  const receiptItems = INBOX_ITEMS.filter((item) => item.tone === "receipt");
  const scaffoldItems = INBOX_ITEMS.filter((item) => item.tone === "scaffold");

  return (
    <Section
      title="36. Inbox"
      description="Current shipped notification surfaces in prod. This section mirrors today’s bar and inbox states so you can inspect the real kind coverage without the older north-star mock."
    >
      <DemoCard label="36-now. What ships today">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            The current system separates three things cleanly: live{" "}
            <span className="font-medium text-fg-1">dream running</span> in the
            bar, durable{" "}
            <span className="font-medium text-fg-1">notification receipts</span>
            , and durable{" "}
            <span className="font-medium text-fg-1">decision rows</span> in
            inbox.
          </p>
          <p>
            The visuals below are intentionally the shipped ones: current bar
            card, current inbox row shell, current per-kind bodies, current
            scaffold/fallback treatment.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="36-bar. Current bar notification states">
        <div className="grid gap-4 md:grid-cols-2">
          {BAR_ITEMS.map((item) => (
            <div key={item.kind} className="space-y-2">
              <p className="text-[12px] font-medium uppercase tracking-wide text-fg-3">
                {item.kind}
              </p>
              <BarNotificationPreview item={item} />
            </div>
          ))}
        </div>
      </DemoCard>

      <DemoCard label="36-inbox-decision. Current decision kinds">
        <InboxPanelPreview
          title="Decision rows"
          note="These are the actionable inbox kinds shipping today."
          items={decisionItems}
        />
      </DemoCard>

      <DemoCard label="36-inbox-receipts. Current receipt kinds">
        <InboxPanelPreview
          title="Receipt rows"
          note="These render with dismiss-only affordances in the inbox. dream_run_completed also appears as a bar push first."
          items={receiptItems}
        />
      </DemoCard>

      <DemoCard label="36-inbox-scaffold. Current scaffolded kinds">
        <InboxPanelPreview
          title="Scaffold / fallback rows"
          note="These kinds are typed and rendered, but still shipped as scaffold/fallback states rather than fully produced polished flows."
          items={scaffoldItems}
        />
      </DemoCard>
    </Section>
  );
}
