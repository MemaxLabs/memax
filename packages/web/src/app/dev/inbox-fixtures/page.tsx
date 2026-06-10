"use client";

import { useState } from "react";
import type { Notification } from "memax-sdk";
import { useInterpolate, useLocale } from "@/i18n";
import {
  InboxRow,
  notificationToInboxItem,
} from "@/components/features/inbox/inbox-control";
import { InboxSurfaceProvider } from "@/contexts/inbox-surface-context";
import { IsMobileProvider } from "@/hooks/use-is-mobile";
import { DOCS_URL } from "@/lib/urls";
import { Surface } from "@memaxlabs/ui";

// Dev fixture for the inbox notification framework (plan 17 P5-6).
// Mounts one row per NotificationKind the renderer supports so
// designers can preview every kind without a populated database.
// Every handler is a no-op — toggling expand works, resolve/dismiss
// calls are swallowed.

const NOW = new Date().toISOString();

function fixtureNotification(
  id: string,
  kind: Notification["kind"],
  payload: Record<string, unknown>,
  opts: Partial<Notification> = {},
): Notification {
  return {
    id,
    audience: "hub_member",
    hub_id: "hub-dev",
    kind,
    status: "pending",
    priority: 0,
    source_kind: "fixture",
    source_id: id,
    payload,
    created_at: NOW,
    ...opts,
  };
}

const FIXTURES: Notification[] = [
  fixtureNotification("fixture-contradiction", "review_contradiction", {
    memory_a: { id: "mem-a", title: "Redis cache TTL is 3600s" },
    memory_b: { id: "mem-b", title: "Redis cache TTL is 1800s" },
    similarity: 0.87,
    reason: "These two notes disagree about the default Redis cache TTL.",
  }),
  fixtureNotification("fixture-topic-merge", "review_topic_merge", {
    target_topic: { id: "topic-a", name: "React", memory_count: 42 },
    source_topics: [
      { id: "topic-b", name: "React 18", memory_count: 12 },
      { id: "topic-c", name: "React Server Components", memory_count: 8 },
    ],
    reason: "All three topics cover the same React ecosystem content.",
  }),
  fixtureNotification("fixture-topic-restructure", "review_topic_restructure", {
    parent_topic: { id: "topic-p", name: "Backend", memory_count: 31 },
    child_topic: { id: "topic-c", name: "PostgreSQL tuning", memory_count: 9 },
    reason: "PostgreSQL notes fit under the Backend tree.",
  }),
  fixtureNotification(
    "fixture-hub-invite",
    "hub_invite",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      inviter: { id: "user-derek", display: "Derek", avatar_url: "" },
      role: "member",
      expires_at: new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString(),
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-member-joined",
    "hub_member_joined",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      member: { id: "user-new", display: "Alex Chen", avatar_url: "" },
      role: "member",
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-invite-accepted",
    "hub_invite_accepted",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      role: "contributor",
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-invite-declined",
    "hub_invite_declined",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      invitee: { id: "user-alex", display: "Alex Chen", avatar_url: "" },
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-invite-declined-by-you",
    "hub_invite_declined_by_you",
    {
      hub: {
        id: "hub-other",
        name: "Design Collective",
        icon: "🎨",
        accent: "violet",
      },
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-system-notice",
    "system_notice",
    {
      title: "Memax is getting a new dream engine",
      body: "We're rolling out a faster dream cycle this week. No action needed.",
      link: `${DOCS_URL}/changelog`,
      link_text: "Read the changelog",
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-dream-run-completed",
    "dream_run_completed",
    {
      run_id: "dream-run-1",
      mode: "maintenance",
      counts: {
        merged: 3,
        contradictions: 1,
        archived: 2,
        organized: 4,
        restructures: 1,
      },
      memories_scanned: 38,
      finished_at: NOW,
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-dream-run-clean",
    "dream_run_completed",
    {
      run_id: "dream-run-2",
      mode: "maintenance",
      counts: {
        merged: 0,
        contradictions: 0,
        archived: 0,
        organized: 0,
        restructures: 0,
      },
      memories_scanned: 12,
      finished_at: NOW,
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-gift-invite-link",
    "gift_invite_link",
    {
      sender: { id: "user-friend", display: "Priya", avatar_url: "" },
      hub: { id: "hub-team", name: "Platform Team" },
      token: "gift-token-abc",
      url: "https://memax.app/gift/abc",
      expires_at: new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString(),
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-ownership-transfer",
    "hub_ownership_transfer",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      initiator: { id: "user-derek", display: "Derek", avatar_url: "" },
      role: "owner",
      expires_at: new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString(),
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-hub-ownership-transferred",
    "hub_ownership_transferred",
    {
      hub: {
        id: "hub-team",
        name: "Platform Team",
        icon: "🛠",
        accent: "blue",
      },
      new_owner: { id: "user-new", display: "Alex Chen", avatar_url: "" },
      old_owner: { id: "user-me", display: "You", avatar_url: "" },
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  fixtureNotification(
    "fixture-review-low-confidence",
    "review_low_confidence",
    {},
  ),
  fixtureNotification("fixture-review-stale", "review_stale", {}),
  // Plan 18 — onboarding checklist (decision super-notif). Mid-flow
  // state: welcome + agent connect ticked, ask still pending, five
  // memories progressing, first_dream locked by five_memories. Mirrors
  // the kitchen §34 SnChecklistCard mid-flow scene.
  fixtureNotification(
    "fixture-onboarding-checklist",
    "checklist",
    {
      title: "Your first week",
      description: "Get oriented in 5 minutes.",
      pin_context: "memories_hero",
      required_ids: [
        "connect_agent",
        "first_memory",
        "first_ask",
        "five_memories",
        "first_dream",
      ],
      items: [
        {
          id: "welcome",
          title: "Read the welcome note",
          description: "90 seconds. Worth it.",
          icon: "Sparkles",
          viewed_at: NOW,
          completed_at: NOW,
        },
        {
          id: "connect_agent",
          title: "Connect your first agent",
          description:
            "Claude Code, Cursor, Codex — one command, memory flows both ways.",
          icon: "Bot",
          cta_label: "Set up",
          cta_url: "/settings/agents",
          viewed_at: NOW,
          completed_at: NOW,
        },
        {
          id: "first_memory",
          title: "Dump your first memory",
          description:
            "Type anything in the bar, hit ⌘↵. A wiki, a link, a half-thought.",
          icon: "Inbox",
          cta_label: "Try the bar",
        },
        {
          id: "first_ask",
          title: "Ask memax a question",
          description:
            "Press ↵ instead of ⌘↵ — memax answers from everything you've dumped.",
          icon: "MessageCircle",
          cta_label: "Try asking",
        },
        {
          id: "five_memories",
          title: "Dump 5 memories",
          description:
            "memax needs a critical mass to start seeing patterns. Unlocks dreams.",
          icon: "Boxes",
          progress: { current: 2, target: 5 },
        },
        {
          id: "first_hub_invite",
          title: "Join or start a team hub",
          description:
            "Shared brain with people you work with. Jump in or start your own.",
          icon: "UsersRound",
          cta_label: "Explore hubs",
          cta_url: "/hubs",
        },
        {
          id: "first_dream",
          title: "Let memax dream",
          description:
            "Dreams run automatically once you cross 5 memories — sit tight.",
          icon: "Moon",
          locked_by: ["five_memories"],
          // v1: no cta_label — dreams auto-trigger via the P2
          // recorder when memory_count==5.
        },
      ],
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
  // Plan 18 — digest (receipt super-notif). Scaffold fixture: full
  // rendering ships with the first digest producer (likely weekly
  // dream digest). Today the row renders as a generic super-notif
  // header + per-item /view trigger.
  fixtureNotification(
    "fixture-weekly-digest",
    "digest",
    {
      title: "Your week in memax",
      description: "Quick recap of what landed last week.",
      pin_context: "",
      items: [
        {
          id: "memories_added",
          title: "12 new memories",
          description: "Mostly from Claude Code sessions.",
          icon: "Inbox",
        },
        {
          id: "topics_organized",
          title: "3 new topics",
          description: "Auth, Pricing, Onboarding.",
          icon: "FolderTree",
        },
        {
          id: "dream_summary",
          title: "Dream merged 2 duplicates",
          description: "Saved you scrolling past the same Redis note twice.",
          icon: "Moon",
        },
      ],
    },
    { audience: "user", recipient_user_id: "user-me" },
  ),
];

export default function InboxFixturesPage() {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const items = FIXTURES.map((notification) =>
    notificationToInboxItem(notification, {
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
  );

  return (
    <IsMobileProvider>
      <InboxSurfaceProvider>
        <div className="mx-auto max-w-3xl px-6 py-16">
          <header className="mb-8 space-y-2">
            <h1 className="text-display-2 font-semibold text-fg-1">
              Inbox fixtures
            </h1>
            <p className="text-[14px] text-fg-3">
              Dev-only preview of every NotificationKind the inbox renderer
              supports. See{" "}
              <code className="rounded bg-surface-2 px-1 py-0.5 text-[12px]">
                docs/plans/17-inbox-notification-framework.md
              </code>{" "}
              §3.
            </p>
          </header>

          <Surface rounded="2xl" className="overflow-hidden">
            {items.map((item) => (
              <InboxRow
                key={item.id}
                item={item}
                isExpanded={expandedId === item.id}
                isResolving={false}
                onToggle={() => {
                  setFocusedId(item.id);
                  setExpandedId((current) =>
                    current === item.id ? null : item.id,
                  );
                }}
                onResolve={() => {
                  /* fixture: no-op */
                }}
                onDismiss={() => {
                  /* fixture: no-op */
                }}
              />
            ))}
          </Surface>
        </div>
      </InboxSurfaceProvider>
    </IsMobileProvider>
  );
}
