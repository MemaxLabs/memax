// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import {
  QueryClientProvider,
  type QueryClient as QCType,
} from "@tanstack/react-query";
import type { ReactNode } from "react";

// ── Mocks ─────────────────────────────────────────────────────────────────
//
// Mock the SDK client so we can assert the exact arguments our hooks
// forward to it. This test is specifically about the scoping contract
// of the Phase 4 Chunk 1 hooks — the default query must be UNSCOPED
// and a caller must be able to opt into hub-scoped queries
// explicitly. An earlier cut silently passed `hub: activeHubId` on
// every call, which hid user-audience rows (dream_run_completed,
// hub_invite, system_notice) from the inbox whenever the user had
// any active hub context.

const listMock = vi.fn();
const summaryMock = vi.fn();
const resolveMock = vi.fn();
const dismissMock = vi.fn();
const markSeenMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    notifications: {
      list: (...args: unknown[]) => listMock(...args),
      summary: (...args: unknown[]) => summaryMock(...args),
      resolve: (...args: unknown[]) => resolveMock(...args),
      dismiss: (...args: unknown[]) => dismissMock(...args),
      markSeen: (...args: unknown[]) => markSeenMock(...args),
    },
  }),
}));

// The hooks that read `useLocale` need it mocked because vitest's
// jsdom environment does not have a LocaleProvider in scope.
vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      toast: { reviewFailed: "Review failed" },
      errors: {
        action: {
          resolveNotification: "resolve that notification",
          dismissNotification: "dismiss that notification",
        },
      },
    },
  }),
}));

// eslint-disable-next-line import/first
import {
  useNotifications,
  useNotificationSummary,
  useResolveNotification,
  useNotificationDismiss,
  useNotificationMarkSeen,
} from "./use-notifications";
// eslint-disable-next-line import/first
import { queryClient as sharedQueryClient } from "@/lib/query-client";

// ── Scaffolding ───────────────────────────────────────────────────────────

function freshClient(): QCType {
  sharedQueryClient.clear();
  sharedQueryClient.setDefaultOptions({
    queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
    mutations: { retry: false },
  });
  return sharedQueryClient;
}

function makeWrapper(qc: QCType) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

// ── Tests ─────────────────────────────────────────────────────────────────

beforeEach(() => {
  listMock.mockReset();
  summaryMock.mockReset();
  resolveMock.mockReset();
  dismissMock.mockReset();
  markSeenMock.mockReset();
  listMock.mockResolvedValue({
    notifications: [],
    has_more: false,
  });
  summaryMock.mockResolvedValue({
    needs_action_pending: 0,
    updates_pending: 0,
    updates_unseen: 0,
    by_kind: {},
  });
  resolveMock.mockResolvedValue({
    status: "resolved",
    action: "dismiss",
    resolution: "dismissed",
  });
  dismissMock.mockResolvedValue({ status: "dismissed" });
  markSeenMock.mockResolvedValue({ status: "seen" });
});

describe("useNotifications scoping contract", () => {
  it("defaults to unscoped — does not inject a hub filter", async () => {
    const qc = freshClient();
    const { result } = renderHook(() => useNotifications(), {
      wrapper: makeWrapper(qc),
    });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(listMock).toHaveBeenCalledTimes(1);
    // The call MUST NOT receive a hub key by default: user-audience
    // rows (dream_run_completed, hub_invite) must be visible
    // regardless of which hub is active in the UI.
    const arg = listMock.mock.calls[0][0];
    expect(arg === undefined || !("hub" in (arg as object))).toBe(true);
  });

  it("forwards an explicit hub filter when the caller requests one", async () => {
    const qc = freshClient();
    const { result } = renderHook(
      () => useNotifications({ hub: "hub-alpha" }),
      { wrapper: makeWrapper(qc) },
    );

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(listMock).toHaveBeenCalledTimes(1);
    expect(listMock.mock.calls[0][0]).toMatchObject({ hub: "hub-alpha" });
  });

  it("forwards other query params (unseenOnly, kind) without a hub", async () => {
    const qc = freshClient();
    const { result } = renderHook(
      () =>
        useNotifications({
          unseenOnly: true,
          kind: ["review_contradiction"],
        }),
      { wrapper: makeWrapper(qc) },
    );

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(listMock).toHaveBeenCalledTimes(1);
    const arg = listMock.mock.calls[0][0] as Record<string, unknown>;
    expect(arg.unseenOnly).toBe(true);
    expect(arg.kind).toEqual(["review_contradiction"]);
    // Still no hub — caller did not ask for one.
    expect("hub" in arg).toBe(false);
  });
});

describe("useNotificationSummary scoping contract", () => {
  it("defaults to unscoped — passes undefined to the SDK", async () => {
    const qc = freshClient();
    const { result } = renderHook(() => useNotificationSummary(), {
      wrapper: makeWrapper(qc),
    });

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(summaryMock).toHaveBeenCalledTimes(1);
    expect(summaryMock.mock.calls[0][0]).toBeUndefined();
  });

  it("forwards an explicit hub when the caller requests one", async () => {
    const qc = freshClient();
    const { result } = renderHook(
      () => useNotificationSummary({ hub: "hub-beta" }),
      { wrapper: makeWrapper(qc) },
    );

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(summaryMock).toHaveBeenCalledTimes(1);
    expect(summaryMock.mock.calls[0][0]).toBe("hub-beta");
  });

  it("caches unscoped and scoped summaries under different keys", async () => {
    const qc = freshClient();

    const { result: unscoped } = renderHook(() => useNotificationSummary(), {
      wrapper: makeWrapper(qc),
    });
    await waitFor(() => expect(unscoped.current.isSuccess).toBe(true));

    const { result: scoped } = renderHook(
      () => useNotificationSummary({ hub: "hub-gamma" }),
      { wrapper: makeWrapper(qc) },
    );
    await waitFor(() => expect(scoped.current.isSuccess).toBe(true));

    // Both must have actually fetched — if the unscoped key leaked
    // into the scoped key (or vice versa) one of them would have
    // been cache-hit and the mock would only see one call.
    expect(summaryMock).toHaveBeenCalledTimes(2);
    expect(summaryMock.mock.calls[0][0]).toBeUndefined();
    expect(summaryMock.mock.calls[1][0]).toBe("hub-gamma");
  });
});

describe("notification mutations do not pass a hub", () => {
  it("useResolveNotification calls the SDK without a hub hint", async () => {
    const qc = freshClient();
    const { result } = renderHook(() => useResolveNotification(), {
      wrapper: makeWrapper(qc),
    });

    await result.current.mutateAsync({ id: "n-1", action: "dismiss" });

    expect(resolveMock).toHaveBeenCalledTimes(1);
    // The mutation must pass (id, action) only — no third hub
    // argument, no hub key smuggled through. Pinning a hub here
    // would imply resolve is hub-scoped when the server visibility
    // check walks the full membership list.
    expect(resolveMock.mock.calls[0]).toEqual(["n-1", "dismiss"]);
  });

  it("useNotificationMarkSeen calls the SDK with the id only", async () => {
    const qc = freshClient();
    const { result } = renderHook(() => useNotificationMarkSeen(), {
      wrapper: makeWrapper(qc),
    });

    await result.current.mutateAsync("n-2");

    expect(markSeenMock).toHaveBeenCalledTimes(1);
    expect(markSeenMock.mock.calls[0]).toEqual(["n-2"]);
  });

  it("optimistically marks a notification seen in list and summary caches", async () => {
    const qc = freshClient();
    qc.setQueryData(["notifications", "list", "all", {}], {
      notifications: [
        {
          id: "n-seen",
          audience: "user",
          hub_id: "hub-1",
          kind: "system_notice",
          status: "pending",
          priority: 0,
          source_kind: "system_notice",
          source_id: "notice-1",
          created_at: new Date().toISOString(),
        },
      ],
      has_more: false,
    });
    qc.setQueryData(["notifications", "summary", "all"], {
      needs_action_pending: 0,
      updates_pending: 1,
      updates_unseen: 1,
      by_kind: {
        system_notice: { pending: 1, unseen: 1 },
      },
    });

    let releaseMarkSeen: (() => void) | undefined;
    markSeenMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseMarkSeen = () => resolve({ status: "seen" });
        }),
    );

    const { result } = renderHook(() => useNotificationMarkSeen(), {
      wrapper: makeWrapper(qc),
    });

    const mutation = result.current.mutateAsync("n-seen");

    await waitFor(() => {
      expect(
        qc.getQueryData<{
          notifications: Array<{ id: string; seen_at?: string }>;
        }>(["notifications", "list", "all", {}])?.notifications[0]?.seen_at,
      ).toBeTruthy();
    });
    expect(
      qc.getQueryData<{
        updates_pending: number;
        updates_unseen: number;
        by_kind: Record<string, { pending: number; unseen: number }>;
      }>(["notifications", "summary", "all"]),
    ).toMatchObject({
      updates_pending: 1,
      updates_unseen: 0,
      by_kind: { system_notice: { pending: 1, unseen: 0 } },
    });

    if (releaseMarkSeen) {
      releaseMarkSeen();
    }
    await mutation;
  });

  it("optimistically removes a resolved notification from list and summary caches", async () => {
    const qc = freshClient();
    qc.setQueryData(["notifications", "list", "all", {}], {
      notifications: [
        {
          id: "n-optimistic",
          audience: "hub_member",
          hub_id: "hub-1",
          kind: "review_topic_merge",
          status: "pending",
          priority: 0,
          source_kind: "topic_review",
          source_id: "dream-1",
          created_at: new Date().toISOString(),
        },
      ],
      has_more: false,
    });
    qc.setQueryData(["notifications", "summary", "all"], {
      needs_action_pending: 1,
      updates_pending: 0,
      updates_unseen: 0,
      by_kind: {
        review_topic_merge: { pending: 1, unseen: 1 },
      },
    });

    let releaseResolve: (() => void) | undefined;
    resolveMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseResolve = () =>
            resolve({
              status: "resolved",
              action: "merge",
              resolution: "accepted",
            });
        }),
    );

    const { result } = renderHook(() => useResolveNotification(), {
      wrapper: makeWrapper(qc),
    });

    const mutation = result.current.mutateAsync({
      id: "n-optimistic",
      action: "merge",
    });

    await waitFor(() => {
      const cached = qc.getQueryData<{
        notifications: Array<{ id: string }>;
      }>(["notifications", "list", "all", {}]);
      expect(cached?.notifications).toEqual([]);
    });
    expect(
      qc.getQueryData<{
        needs_action_pending: number;
        by_kind: Record<string, { pending: number }>;
      }>(["notifications", "summary", "all"]),
    ).toMatchObject({
      needs_action_pending: 0,
      by_kind: { review_topic_merge: { pending: 0 } },
    });

    if (releaseResolve) {
      releaseResolve();
    }
    await mutation;
  });

  it("restores caches if a dismiss mutation fails", async () => {
    const qc = freshClient();
    qc.setQueryData(["notifications", "list", "all", {}], {
      notifications: [
        {
          id: "n-dismiss",
          audience: "user",
          hub_id: "hub-1",
          kind: "hub_member_joined",
          status: "pending",
          priority: 0,
          source_kind: "hub_member",
          source_id: "invite-1",
          created_at: new Date().toISOString(),
        },
      ],
      has_more: false,
    });
    qc.setQueryData(["notifications", "summary", "all"], {
      needs_action_pending: 0,
      updates_pending: 1,
      updates_unseen: 1,
      by_kind: {
        hub_member_joined: { pending: 1, unseen: 1 },
      },
    });
    dismissMock.mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => useNotificationDismiss(), {
      wrapper: makeWrapper(qc),
    });

    await expect(result.current.mutateAsync("n-dismiss")).rejects.toThrow(
      "boom",
    );

    expect(
      qc.getQueryData<{
        notifications: Array<{ id: string }>;
      }>(["notifications", "list", "all", {}])?.notifications,
    ).toHaveLength(1);
    expect(
      qc.getQueryData<{
        updates_pending: number;
        updates_unseen: number;
        by_kind: Record<string, { pending: number; unseen: number }>;
      }>(["notifications", "summary", "all"]),
    ).toMatchObject({
      updates_pending: 1,
      updates_unseen: 1,
      by_kind: { hub_member_joined: { pending: 1, unseen: 1 } },
    });
  });
});
