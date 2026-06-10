// @vitest-environment jsdom

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { HTMLAttributes, ReactNode } from "react";
import type { Notification } from "memax-sdk";
import type { InboxItem } from "./inbox-control";

const { notificationsState } = vi.hoisted(() => {
  const state: {
    data: { notifications: unknown[] } | undefined;
    isLoading: boolean;
    isError: boolean;
    refetch: ReturnType<typeof vi.fn>;
    calls: unknown[];
  } = {
    data: { notifications: [] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    calls: [],
  };
  return { notificationsState: state };
});

const { markSeenState } = vi.hoisted(() => ({
  markSeenState: {
    mutate: vi.fn(),
  },
}));

const prefetchNotificationsMock = vi.fn();

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      reviews: {
        kindContradiction: "Contradiction",
        kindStale: "Stale",
        kindLowConfidence: "Low confidence",
        kindTopicMerge: "Topic merge",
        kindTopicRestructure: "Topic restructure",
        title: "Review",
        kindUnsupportedYet: "Not supported yet",
        dismiss: "Dismiss",
        keepA: "Keep A",
        keepB: "Keep B",
        keepBoth: "Keep both",
        merge: "Merge",
        keepSeparate: "Keep separate",
        apply: "Apply",
        keep: "Keep",
        accept: "Accept",
        decline: "Decline",
        topicMergeBody: "Merge review",
        topicRestructureBody: "Restructure review",
        topicMemoryCount: "{n} memories",
        topicMemoryCountOne: "1 memory",
        topicUntitled: "(untitled topic)",
        topicRemoved: "(removed topic)",
      },
      dreams: {
        similarity: "{n}%",
        report: "Dream report",
        notificationTitle: "memax dreamed",
        notificationCleanTitle: "memax dreamed — everything held together",
        notificationCleanBody:
          "Nothing needed merging, restructuring, or review this time.",
        notificationMerged: "{n} merged",
        notificationContradictions: "{n} tangled",
        notificationArchived: "{n} archived",
        notificationOrganized: "{n} organized",
        scanned: "{n} memories scanned",
        restructured: "{n} topics restructured",
      },
      topics: {
        dreamingNow: "Dreaming now",
        dreamingNowHint: "Organizing in the background",
      },
      states: {
        error: {
          retry: "Retry",
        },
      },
      inbox: {
        title: "Inbox",
        pageSubtitle: "Things memax needs you to decide",
        viewAll: "{n} waiting · open full inbox",
        emptyTitle: "Inbox is quiet",
        emptySubtitle: "Dreams are out wandering",
        lastRun: "Last run {time} · {n} processed",
        loadFailed: "Couldn't open Inbox right now.",
        loadFailedHint: "Reviews are temporarily unavailable. Try again.",
        collapseReview: "Collapse review",
        expandReview: "Expand review",
        hubInviteAnonymous: "Someone",
        hubInviteBody: "invited you to",
        hubInviteRoleLabel: "Role",
        hubMemberJoinedBody: "joined",
        hubOwnershipTransferBody: "wants to transfer",
        hubOwnershipTransferHint: "Please review",
        hubOwnershipTransferredSelfTitle: "You are now the owner of {hub}",
        hubOwnershipTransferredOtherTitle: "{owner} is now the owner of {hub}",
        hubOwnershipTransferredBody: "now owns",
        hubOwnershipTransferredSelfBody: "You are now the owner of",
        giftInviteBody: "sent an invite",
        giftInviteOpenLink: "Open invite",
        reviewStaleNote: "Stale note",
        reviewLowConfidenceNote: "Low confidence note",
        kindHubInvite: "Hub invite",
        kindHubMemberJoined: "Member joined",
        kindHubOwnershipTransfer: "Ownership transfer",
        kindHubOwnershipTransferred: "Ownership transferred",
        kindSystemNotice: "System notice",
        kindGiftInvite: "Gift invite",
        topicMergeOneTitle: "Merge {source} into {target}?",
        topicMergeManyTitle: "Merge {sources} into {target}?",
        topicMergeFallbackTitle: "Merge topics into {target}?",
        topicRestructureTitle: "Move {child} under {parent}?",
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
  pluralize: (one: string, other: string, n: number) =>
    n === 1
      ? one
      : other.replace(/\{(\w+)\}/g, (_, key) => (key === "n" ? String(n) : "")),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: vi.fn(),
  }),
  usePathname: () => "/inbox",
}));

vi.mock("@/contexts/inbox-surface-context", () => ({
  useInboxSurface: () => ({
    open: false,
    setOpen: vi.fn(),
    openInbox: vi.fn(),
    closeInbox: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-notifications", () => ({
  prefetchNotifications: (...args: unknown[]) =>
    prefetchNotificationsMock(...args),
  useNotifications: (query?: unknown) => {
    notificationsState.calls.push(query);
    return notificationsState;
  },
  useNotificationSummary: () => ({
    data: { needs_action_pending: 0, updates_unseen: 0 },
  }),
  useResolveNotification: () => ({ mutate: vi.fn() }),
  useNotificationDismiss: () => ({ mutate: vi.fn() }),
  useNotificationMarkSeen: () => markSeenState,
}));

vi.mock("@/hooks/use-dreams", () => ({
  useDreamReport: () => ({ data: undefined }),
}));

vi.mock("@/hooks/use-dream-run-status", () => ({
  useDreamRunStatus: () => ({ status: "idle" }),
}));

vi.mock("@/hooks/use-is-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@/lib/auth", () => ({
  useAuth: () => ({
    user: { id: "user-active" },
    hubs: [
      {
        hub: {
          id: "hub-active",
          name: "Active hub",
          icon: "A",
          accent: "violet",
          hub_type: "team",
        },
      },
      {
        hub: {
          id: "hub-other",
          name: "Other hub",
          icon: "O",
          accent: "blue",
          hub_type: "team",
        },
      },
    ],
  }),
}));

vi.mock("@/lib/format-age", () => ({
  formatAge: () => "now",
}));

vi.mock("./memax-star", () => ({
  MemaxStar: (props: HTMLAttributes<HTMLSpanElement>) => (
    <span {...props}>*</span>
  ),
}));

vi.mock("@memaxlabs/ui", () => ({
  ContentError: () => null,
  getHubAccentStyle: () => ({
    background: "transparent",
    foreground: "currentColor",
  }),
  Popover: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  SectionHeader: () => null,
  Skeleton: ({ className }: { className?: string }) => (
    <div data-slot="skeleton" className={className} />
  ),
  Surface: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

// eslint-disable-next-line import/first
import {
  InboxControl,
  InboxRow,
  InboxView,
  notificationToInboxItem,
} from "./inbox-control";

afterEach(() => {
  notificationsState.data = { notifications: [] };
  notificationsState.isLoading = false;
  notificationsState.isError = false;
  notificationsState.calls = [];
  markSeenState.mutate.mockReset();
  prefetchNotificationsMock.mockReset();
  cleanup();
});

const inboxItemLabels = {
  topicUntitled: "（未命名主题）",
  topicRemoved: "（已删除主题）",
  dreamRunCompletedTitle: "memax 做了个梦",
  dreamRunCompletedCleanTitle: "memax 整理好了",
  dreamRunCompletedPartialTitle: "memax 做了个梦——中途遇到一些问题",
  topicMergeOne: ({ source, target }: { source: string; target: string }) =>
    `把 ${source} 合并进 ${target}？`,
  topicMergeMany: ({ sources, target }: { sources: string; target: string }) =>
    `把 ${sources} 合并进 ${target}？`,
  topicMergeFallback: ({ target }: { target: string }) =>
    `把这些主题合并进 ${target}？`,
  topicRestructure: ({ child, parent }: { child: string; parent: string }) =>
    `把 ${child} 移到 ${parent} 下面？`,
  hubOwnershipTransferredSelfTitle: ({ hub }: { hub: string }) =>
    `你现在是 ${hub} 的所有者`,
  hubOwnershipTransferredOtherTitle: ({
    owner,
    hub,
  }: {
    owner: string;
    hub: string;
  }) => `${owner} 现在是 ${hub} 的所有者`,
};

function makeItem(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "notif-1",
    audience: "hub_member",
    kind: "topic_merge",
    status: "pending",
    seen: false,
    title: "Merge topics",
    description: "Suggested merge",
    similarity: 0,
    created_at: new Date(0).toISOString(),
    ...overrides,
  };
}

describe("InboxRow payload guards", () => {
  it("does not throw when a topic merge payload is malformed", () => {
    const renderRow = () =>
      render(
        <InboxRow
          item={makeItem({
            kind: "topic_merge",
            payload: { target_topic: null, source_topics: null },
          })}
          isExpanded
          isResolving={false}
          onToggle={() => {}}
          onResolve={() => {}}
          onDismiss={() => {}}
        />,
      );

    expect(renderRow).not.toThrow();
    expect(screen.getByText("Suggested merge")).toBeTruthy();
  });

  it("does not throw when a contradiction payload is malformed in expanded state", () => {
    const renderRow = () =>
      render(
        <InboxRow
          item={makeItem({
            kind: "contradiction",
            title: "Conflicting memories",
            description: "Needs review",
            payload: {},
          })}
          isExpanded
          isResolving={false}
          onToggle={() => {}}
          onResolve={() => {}}
          onDismiss={() => {}}
        />,
      );

    expect(renderRow).not.toThrow();
    expect(screen.getByText("Needs review")).toBeTruthy();
  });

  it("still toggles the row chrome without throwing", () => {
    render(
      <InboxRow
        item={makeItem({ kind: "topic_merge" })}
        isExpanded={false}
        isResolving={false}
        onToggle={() => {}}
        onResolve={() => {}}
        onDismiss={() => {}}
      />,
    );

    expect(() =>
      fireEvent.click(screen.getByRole("button", { name: /topic merge/i })),
    ).not.toThrow();
  });
});

describe("notificationToInboxItem", () => {
  it("localizes collapsed topic review titles", () => {
    const mergeNotification = {
      id: "n-merge",
      audience: "hub_member",
      hub_id: "hub-1",
      kind: "review_topic_merge",
      status: "pending",
      priority: 0,
      source_kind: "dream",
      source_id: "dream-1",
      created_at: new Date().toISOString(),
      payload: {
        target_topic: { id: "topic-target", name: "" },
        source_topics: [{ id: "topic-source", name: "(removed topic)" }],
      },
    } satisfies Notification;
    const restructureNotification = {
      ...mergeNotification,
      id: "n-restructure",
      kind: "review_topic_restructure",
      payload: {
        child_topic: { id: "topic-child", name: "" },
        parent_topic: { id: "topic-parent", name: "平台" },
      },
    } satisfies Notification;

    expect(
      notificationToInboxItem(mergeNotification, inboxItemLabels).title,
    ).toBe('把 "（已删除主题）" 合并进 "（未命名主题）"？');
    expect(
      notificationToInboxItem(restructureNotification, inboxItemLabels).title,
    ).toBe('把 "（未命名主题）" 移到 "平台" 下面？');
  });

  it("renders ownership-change self receipts with recipient-aware copy", () => {
    const notification = {
      id: "n-ownership",
      audience: "user",
      hub_id: "hub-1",
      kind: "hub_ownership_transferred",
      status: "pending",
      priority: 0,
      source_kind: "hub_ownership_transferred",
      source_id: "transfer-1:new_owner",
      created_at: new Date().toISOString(),
      payload: {
        hub: { id: "hub-1", name: "Alpha" },
        new_owner: { id: "user-active", display: "Jiahao" },
        old_owner: { id: "user-old", display: "Old owner" },
      },
    } satisfies Notification;

    expect(
      notificationToInboxItem(notification, inboxItemLabels, "user-active")
        .title,
    ).toBe("你现在是 Alpha 的所有者");
    expect(
      notificationToInboxItem(notification, inboxItemLabels, "user-other")
        .title,
    ).toBe("Jiahao 现在是 Alpha 的所有者");
  });

  it("uses the changed dream title when a receipt carries outcomes", () => {
    const notification = {
      id: "n-dream-outcome",
      audience: "user",
      hub_id: "hub-1",
      kind: "dream_run_completed",
      status: "pending",
      priority: 0,
      source_kind: "dream_run",
      source_id: "dream-1",
      created_at: new Date().toISOString(),
      payload: {
        run_id: "dream-1",
        mode: "maintenance",
        counts: {
          merged: 2,
          contradictions: 0,
          archived: 0,
          organized: 0,
          restructures: 0,
        },
        memories_scanned: 9,
        finished_at: new Date().toISOString(),
      },
    } satisfies Notification;

    expect(notificationToInboxItem(notification, inboxItemLabels).title).toBe(
      "memax 做了个梦",
    );
  });

  it("uses the clean dream title when a receipt carries no outcomes", () => {
    const notification = {
      id: "n-dream-clean",
      audience: "user",
      hub_id: "hub-1",
      kind: "dream_run_completed",
      status: "pending",
      priority: 0,
      source_kind: "dream_run",
      source_id: "dream-2",
      created_at: new Date().toISOString(),
      payload: {
        run_id: "dream-2",
        mode: "maintenance",
        counts: {
          merged: 0,
          contradictions: 0,
          archived: 0,
          organized: 0,
          restructures: 0,
        },
        memories_scanned: 6,
        finished_at: new Date().toISOString(),
      },
    } satisfies Notification;

    expect(notificationToInboxItem(notification, inboxItemLabels).title).toBe(
      "memax 整理好了",
    );
  });
});

describe("Inbox branded loading", () => {
  it("shows the dedicated memax inbox loading state instead of the empty state", () => {
    notificationsState.data = undefined;
    notificationsState.isLoading = true;

    render(<InboxView />);

    expect(screen.queryByText("Inbox is quiet")).toBeNull();
    expect(
      document.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBeGreaterThan(0);
  });
});

describe("Inbox scope", () => {
  it("keeps the compact inbox panel unscoped so user-audience invites stay visible", () => {
    render(<InboxControl />);

    expect(notificationsState.calls).toHaveLength(1);
    expect(notificationsState.calls[0]).toEqual({ status: "pending" });
  });

  it("keeps one global inbox list and shows a hub chip on hub-scoped rows", () => {
    notificationsState.data = {
      notifications: [
        {
          id: "n-hub",
          audience: "hub_member",
          hub_id: "hub-active",
          kind: "review_topic_merge",
          status: "pending",
          priority: 0,
          source_kind: "topic_review",
          source_id: "dream-1",
          created_at: new Date().toISOString(),
          payload: {
            target_topic: { id: "topic-target", name: "Alpha" },
            source_topics: [{ id: "topic-source", name: "Beta" }],
          },
        },
        {
          id: "n-other-hub",
          audience: "hub_member",
          hub_id: "hub-other",
          kind: "review_topic_merge",
          status: "pending",
          priority: 0,
          source_kind: "topic_review",
          source_id: "dream-2",
          created_at: new Date().toISOString(),
          payload: {
            target_topic: { id: "topic-target-2", name: "Gamma" },
            source_topics: [{ id: "topic-source-2", name: "Delta" }],
          },
        },
        {
          id: "n-account",
          audience: "user",
          hub_id: "hub-other",
          kind: "hub_invite",
          status: "pending",
          priority: 0,
          source_kind: "hub_invite",
          source_id: "invite-1",
          created_at: new Date().toISOString(),
          payload: {
            hub: { id: "hub-other", name: "New hub" },
            inviter: { id: "user-2", display: "Someone" },
          },
        },
      ],
    };

    render(<InboxControl />);

    expect(screen.getByText('Merge "Beta" into "Alpha"?')).toBeTruthy();
    expect(screen.getByText("Someone invited you to New hub")).toBeTruthy();
    expect(screen.getByText('Merge "Delta" into "Gamma"?')).toBeTruthy();
    expect(screen.getByText("Active hub")).toBeTruthy();
    expect(screen.getByText("Other hub")).toBeTruthy();
  });

  it("marks an unseen row seen when the user expands it", () => {
    notificationsState.data = {
      notifications: [
        {
          id: "n-notice",
          audience: "user",
          hub_id: "hub-active",
          kind: "system_notice",
          status: "pending",
          priority: 0,
          source_kind: "system_notice",
          source_id: "notice-1",
          created_at: new Date().toISOString(),
          payload: {
            title: "Heads up",
            body: "Something changed",
          },
        },
      ],
    };

    render(<InboxControl />);

    fireEvent.click(screen.getByRole("button", { name: /system notice/i }));

    expect(markSeenState.mutate).toHaveBeenCalledTimes(1);
    expect(markSeenState.mutate.mock.calls[0][0]).toBe("n-notice");
  });
});

describe("DreamRunCompletedBody", () => {
  it("renders the clean receipt body when the dream made no changes", () => {
    render(
      <InboxRow
        item={makeItem({
          kind: "dream_run_completed",
          title: "memax 整理好了",
          payload: {
            run_id: "dream-clean",
            mode: "maintenance",
            counts: {
              merged: 0,
              contradictions: 0,
              archived: 0,
              organized: 0,
              restructures: 0,
            },
            memories_scanned: 7,
            finished_at: new Date().toISOString(),
          },
        })}
        isExpanded
        isResolving={false}
        onToggle={() => {}}
        onResolve={() => {}}
        onDismiss={() => {}}
      />,
    );

    expect(screen.getByText("7 memories scanned")).toBeTruthy();
    expect(
      screen.getByText(
        "Nothing needed merging, restructuring, or review this time.",
      ),
    ).toBeTruthy();
  });
});
