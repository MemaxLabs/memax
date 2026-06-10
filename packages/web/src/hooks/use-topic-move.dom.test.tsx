// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Topic, TopicListResponse, TopicTree } from "memax-sdk";
import { MemaxError } from "memax-sdk";
import { queryClient } from "@/lib/query-client";
import { topicQueryKey } from "./use-topics";

// ── Mocks ─────────────────────────────────────────────────────────────────

const topicsUpdateMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    topics: {
      update: (...args: unknown[]) => topicsUpdateMock(...args),
    },
  }),
}));

const toastSuccess = vi.fn();
const toastError = vi.fn();
const toastInfo = vi.fn();
const toastDismiss = vi.fn();

vi.mock("./use-bar-toast", () => ({
  useBarToast: () => ({
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
    info: (...args: unknown[]) => toastInfo(...args),
    dismiss: (...args: unknown[]) => toastDismiss(...args),
  }),
}));

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      topics: {
        topicMovedUnderParent: "Moved {name} under {parent}.",
        topicMovedToHub: "Moved {name} to {hub}.",
        topicMoved: "Moved {name}.",
        topicMoveFailed: "topicMoveFailed",
        topicCycleDetected: "topicCycleDetected",
        topicMaxDepthReached: "topicMaxDepthReached",
        topicInvalidParent: "topicInvalidParent",
        topicMoveUndone: "topicMoveUndone",
        topicMoveUndoFailed: "topicMoveUndoFailed",
      },
      toast: {
        undoing: "toast.undoing",
      },
      import: {
        undo: "import.undo",
      },
      errors: {
        action: {
          moveTopic: "move that topic",
        },
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
}));

// Defer import so mocks apply before module eval.
// eslint-disable-next-line import/first
import { useTopicMove, patchTopicListForMove } from "./use-topic-move";

// ── Test fixtures ─────────────────────────────────────────────────────────

const HUB_ID = "11111111-1111-4111-8111-111111111111";
const ROOT_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const PARENT_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const CHILD_ID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
const OTHER_ID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";

function makeTopicNode(overrides: Partial<TopicTree>): TopicTree {
  return {
    id: "",
    owner_id: "u1",
    hub_id: HUB_ID,
    parent_id: null,
    name: "",
    description: "",
    icon: "folder",
    position: 0,
    pinned: false,
    user_modified: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    memory_count: 0,
    total_memory_count: 0,
    children: [],
    ...overrides,
  } as TopicTree;
}

// Tree shape for tests:
//   Root
//    └─ Parent
//         └─ Child
//   Other (sibling of Root)
function makeTopicList(): TopicListResponse {
  return {
    topics: [
      makeTopicNode({
        id: ROOT_ID,
        name: "Root",
        position: 0,
        children: [
          makeTopicNode({
            id: PARENT_ID,
            name: "Parent",
            parent_id: ROOT_ID,
            position: 0,
            children: [
              makeTopicNode({
                id: CHILD_ID,
                name: "Child",
                parent_id: PARENT_ID,
                position: 0,
              }),
            ],
          }),
        ],
      }),
      makeTopicNode({ id: OTHER_ID, name: "Other", position: 1 }),
    ],
    unassigned_count: 0,
  } as unknown as TopicListResponse;
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

function findTopic(forest: readonly TopicTree[], id: string): TopicTree | null {
  for (const node of forest) {
    if (node.id === id) return node;
    const deeper = findTopic(node.children, id);
    if (deeper) return deeper;
  }
  return null;
}

beforeEach(() => {
  queryClient.clear();
  topicsUpdateMock.mockReset();
  toastSuccess.mockReset();
  toastError.mockReset();
  toastInfo.mockReset();
  toastDismiss.mockReset();
  queryClient.setQueryData(["topics", HUB_ID], makeTopicList());
});

afterEach(() => {
  queryClient.clear();
});

// ── Tests ─────────────────────────────────────────────────────────────────

describe("useTopicMove", () => {
  describe("optimistic apply", () => {
    it("patches the topic list tree when moving a node under a new parent", async () => {
      topicsUpdateMock.mockResolvedValueOnce({
        id: CHILD_ID,
        parent_id: OTHER_ID,
        position: 0,
        user_modified: true,
      } as Topic);

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "Moved Child under Other.",
        );
      });

      const patched = queryClient.getQueryData<TopicListResponse>([
        "topics",
        HUB_ID,
      ]);
      expect(patched).toBeDefined();
      // Child should no longer live under Parent.
      const parent = findTopic(patched!.topics, PARENT_ID);
      expect(parent?.children.map((c) => c.id)).not.toContain(CHILD_ID);
      // Child should now live under Other.
      const other = findTopic(patched!.topics, OTHER_ID);
      expect(other?.children.map((c) => c.id)).toContain(CHILD_ID);
      // Moved node has fresh parent_id + user_modified flag.
      const movedChild = findTopic(patched!.topics, CHILD_ID);
      expect(movedChild?.parent_id).toBe(OTHER_ID);
      expect(movedChild?.user_modified).toBe(true);
      expect(toastSuccess).toHaveBeenCalledWith(
        "Moved Child under Other.",
        expect.objectContaining({ actionLabel: "import.undo" }),
      );
    });

    it("patches the topic detail cache when moving a topic that has a cached detail", async () => {
      const detail: Topic = {
        id: CHILD_ID,
        owner_id: "u1",
        hub_id: HUB_ID,
        parent_id: PARENT_ID,
        name: "Child",
        description: "",
        icon: "folder",
        position: 0,
        pinned: false,
        user_modified: false,
        created_at: new Date(0).toISOString(),
        updated_at: new Date(0).toISOString(),
      } as Topic;
      queryClient.setQueryData(topicQueryKey(CHILD_ID), detail);

      topicsUpdateMock.mockResolvedValueOnce({
        ...detail,
        parent_id: OTHER_ID,
        position: 3,
        user_modified: true,
      } as Topic);

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID, position: 3 },
          "Moved Child under Other.",
        );
      });

      const patched = queryClient.getQueryData<Topic>(topicQueryKey(CHILD_ID));
      expect(patched?.parent_id).toBe(OTHER_ID);
      expect(patched?.position).toBe(3);
      expect(patched?.user_modified).toBe(true);
    });

    it("rolls back the topic detail cache on failed reparent (Codex review)", async () => {
      // Regression guard: restoreTopicCaches must rewind the topic detail
      // cache, not just the list cache. Previously a failed reparent left
      // the detail page stuck at the optimistic parent/position until
      // refetch.
      const originalDetail: Topic = {
        id: CHILD_ID,
        owner_id: "u1",
        hub_id: HUB_ID,
        parent_id: PARENT_ID,
        name: "Child",
        description: "",
        icon: "folder",
        position: 7,
        pinned: false,
        user_modified: false,
        created_at: new Date(0).toISOString(),
        updated_at: new Date(0).toISOString(),
      } as Topic;
      queryClient.setQueryData(topicQueryKey(CHILD_ID), originalDetail);

      topicsUpdateMock.mockRejectedValueOnce(
        new MemaxError("Cycle detected", "cycle_detected", 400),
      );

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 7,
            name: "Child",
          },
          { parentId: OTHER_ID, position: 99 },
          "Moved Child under Other.",
        );
      });

      // Detail cache must match the pre-mutation state — parent_id back to
      // PARENT_ID, position back to 7, user_modified false.
      const restoredDetail = queryClient.getQueryData<Topic>(
        topicQueryKey(CHILD_ID),
      );
      expect(restoredDetail?.parent_id).toBe(PARENT_ID);
      expect(restoredDetail?.position).toBe(7);
      expect(restoredDetail?.user_modified).toBe(false);
      expect(toastError).toHaveBeenCalledWith("topicCycleDetected");
    });

    it("rolls back the optimistic patch when the mutation rejects", async () => {
      topicsUpdateMock.mockRejectedValueOnce(
        new MemaxError("Cycle detected", "cycle_detected", 400),
      );

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "Moved Child under Other.",
        );
      });

      // The tree should be restored — Child back under Parent, Other has no
      // children, no user_modified flag.
      const restored = queryClient.getQueryData<TopicListResponse>([
        "topics",
        HUB_ID,
      ]);
      const parent = findTopic(restored!.topics, PARENT_ID);
      expect(parent?.children.map((c) => c.id)).toContain(CHILD_ID);
      const other = findTopic(restored!.topics, OTHER_ID);
      expect(other?.children).toEqual([]);
      expect(toastError).toHaveBeenCalledWith("topicCycleDetected");
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it("invalidates the topics prefix on settled, even after a rejection", async () => {
      const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
      topicsUpdateMock.mockRejectedValueOnce(
        new MemaxError("Max depth", "max_depth_subtree", 400),
      );

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "Moved Child under Other.",
        );
      });

      const invalidatedKeys = invalidateSpy.mock.calls.map((call) =>
        JSON.stringify(call[0]?.queryKey),
      );
      expect(invalidatedKeys.some((k) => k?.includes("topics"))).toBe(true);
      invalidateSpy.mockRestore();
    });
  });

  describe("single-flight guard", () => {
    it("blocks re-entry while a move is in flight", async () => {
      let resolveFirst: (value: Topic) => void = () => {};
      const firstPending = new Promise<Topic>((resolve) => {
        resolveFirst = resolve;
      });
      topicsUpdateMock.mockImplementationOnce(() => firstPending);

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      let firstPromise!: Promise<boolean>;
      await act(async () => {
        firstPromise = result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "first",
        );
      });

      await waitFor(() => {
        expect(result.current.isPending).toBe(true);
      });

      let secondResult = true;
      await act(async () => {
        secondResult = await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: ROOT_ID },
          "second",
        );
      });
      expect(secondResult).toBe(false);
      // Second call must not have hit the SDK.
      expect(topicsUpdateMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        resolveFirst({
          id: CHILD_ID,
          parent_id: OTHER_ID,
          position: 0,
          user_modified: true,
        } as Topic);
        await firstPromise;
      });
    });
  });

  describe("no-op guard", () => {
    it("short-circuits without a server call when target parent equals current parent", async () => {
      const { result } = renderHook(() => useTopicMove(), { wrapper });

      let ok = true;
      await act(async () => {
        ok = await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: PARENT_ID },
          "should not fire",
        );
      });

      expect(ok).toBe(false);
      expect(topicsUpdateMock).not.toHaveBeenCalled();
      expect(toastSuccess).not.toHaveBeenCalled();
      expect(toastError).not.toHaveBeenCalled();
    });
  });

  describe("error mapping", () => {
    it.each([
      ["cycle_detected", "topicCycleDetected"],
      ["max_depth_subtree", "topicMaxDepthReached"],
      ["max_depth", "topicMaxDepthReached"],
      ["invalid_parent", "topicInvalidParent"],
      ["duplicate_name", "topicMoveFailed"],
    ])("maps MemaxError.%s to the %s toast", async (code, expectedToast) => {
      topicsUpdateMock.mockRejectedValueOnce(
        new MemaxError("server error", code, 400),
      );

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "success",
        );
      });

      expect(toastError).toHaveBeenCalledWith(expectedToast);
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it("never leaks raw JS error messages into toasts", async () => {
      const consoleSpy = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});
      topicsUpdateMock.mockRejectedValueOnce(new Error("internal: oops"));

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 0,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "success",
        );
      });

      expect(toastError).toHaveBeenCalledWith("topicMoveFailed");
      // Must not stringify the Error onto the toast.
      expect(toastError).not.toHaveBeenCalledWith(
        expect.stringContaining("internal: oops"),
      );
      expect(consoleSpy).toHaveBeenCalled();
      consoleSpy.mockRestore();
    });
  });

  describe("undo", () => {
    it("replays the move with force=true and restores exact prior parent + position", async () => {
      // First call: move Child from Parent to Other.
      topicsUpdateMock.mockResolvedValueOnce({
        id: CHILD_ID,
        parent_id: OTHER_ID,
        position: 0,
        user_modified: true,
      } as Topic);

      const { result } = renderHook(() => useTopicMove(), { wrapper });

      await act(async () => {
        await result.current.moveTopicWithUndo(
          {
            id: CHILD_ID,
            hubId: HUB_ID,
            parentId: PARENT_ID,
            position: 7,
            name: "Child",
          },
          { parentId: OTHER_ID },
          "Moved Child under Other.",
        );
      });

      // Grab the onAction callback from the success toast to simulate undo.
      const successCall = toastSuccess.mock.calls[0];
      const onAction = (
        successCall[1] as {
          onAction: () => void;
        }
      ).onAction;

      // Second call is the undo — parent_id back to original PARENT_ID,
      // position back to original 7, force=true so the no-op guard doesn't
      // short-circuit (snapshot's prior state now matches what we're
      // asking the server to restore).
      topicsUpdateMock.mockResolvedValueOnce({
        id: CHILD_ID,
        parent_id: PARENT_ID,
        position: 7,
        user_modified: true,
      } as Topic);

      await act(async () => {
        onAction();
      });
      await waitFor(() => {
        expect(topicsUpdateMock).toHaveBeenCalledTimes(2);
      });

      // The second call must have targeted parent PARENT_ID at position 7.
      const undoCall = topicsUpdateMock.mock.calls[1];
      expect(undoCall[0]).toBe(CHILD_ID);
      expect(undoCall[1]).toMatchObject({
        parent_id: PARENT_ID,
        position: 7,
      });
      // Undo's success toast fired.
      await waitFor(() => {
        expect(toastSuccess).toHaveBeenCalledWith("topicMoveUndone");
      });
    });
  });

  // ── Position-aware optimistic insert (Codex review) ──
  //
  // These tests cover patchTopicListForMove as a pure function. The
  // hook-level tests above already exercise the full lifecycle; these
  // lock in the specific ordering contract that was broken before the
  // Codex-review fix.
  describe("position-aware optimistic insert", () => {
    function mkNode(
      id: string,
      name: string,
      overrides: Partial<TopicTree> = {},
    ): TopicTree {
      return {
        id,
        owner_id: "u1",
        hub_id: HUB_ID,
        parent_id: null,
        name,
        description: "",
        icon: "folder",
        position: 0,
        pinned: false,
        user_modified: false,
        created_at: new Date(0).toISOString(),
        updated_at: new Date(0).toISOString(),
        memory_count: 0,
        total_memory_count: 0,
        children: [],
        ...overrides,
      } as TopicTree;
    }

    it("appends to the end when target.position is undefined (forward drag/drop)", () => {
      // Forest: Other has two children at positions 0 and 1. Moving Child
      // without an explicit position should land it at the end.
      const list = {
        topics: [
          mkNode("other", "Other", {
            id: OTHER_ID,
            children: [
              mkNode("o-a", "O-A", { parent_id: OTHER_ID, position: 0 }),
              mkNode("o-b", "O-B", { parent_id: OTHER_ID, position: 1 }),
            ],
          }),
          mkNode(CHILD_ID, "Child", { position: 5 }),
        ],
        unassigned_count: 0,
      } as unknown as TopicListResponse;

      const next = patchTopicListForMove(
        list,
        {
          id: CHILD_ID,
          hubId: HUB_ID,
          parentId: null,
          position: 5,
          name: "Child",
        },
        { parentId: OTHER_ID }, // no explicit position
      );

      const other = next.topics.find((n) => n.id === OTHER_ID)!;
      expect(other.children.map((c) => c.id)).toEqual(["o-a", "o-b", CHILD_ID]);
    });

    it("inserts at the correct slot when target.position is explicit (undo replay)", () => {
      // Forest: Other has three children at positions 0, 1, 2. Moving Child
      // in with target.position = 1 should place Child between o-a (pos 0)
      // and o-b (pos 1) — the sorted-insert tiebreak places `moving` before
      // the first child with a strictly greater position, or at an equal
      // position tie if the moving topic is older.
      const list = {
        topics: [
          mkNode("other", "Other", {
            id: OTHER_ID,
            children: [
              mkNode("o-a", "O-A", {
                parent_id: OTHER_ID,
                position: 0,
                created_at: new Date(1_000).toISOString(),
              }),
              mkNode("o-b", "O-B", {
                parent_id: OTHER_ID,
                position: 1,
                created_at: new Date(2_000).toISOString(),
              }),
              mkNode("o-c", "O-C", {
                parent_id: OTHER_ID,
                position: 2,
                created_at: new Date(3_000).toISOString(),
              }),
            ],
          }),
          mkNode(CHILD_ID, "Child", {
            position: 99,
            created_at: new Date(500).toISOString(),
          }),
        ],
        unassigned_count: 0,
      } as unknown as TopicListResponse;

      const next = patchTopicListForMove(
        list,
        {
          id: CHILD_ID,
          hubId: HUB_ID,
          parentId: null,
          position: 99,
          name: "Child",
        },
        { parentId: OTHER_ID, position: 1 },
      );

      const other = next.topics.find((n) => n.id === OTHER_ID)!;
      // With moving.position = 1 and created_at older than o-b, the sort
      // order is: o-a (0), Child (1, older), o-b (1), o-c (2).
      expect(other.children.map((c) => c.id)).toEqual([
        "o-a",
        CHILD_ID,
        "o-b",
        "o-c",
      ]);
      // And the moved node itself has the target position applied.
      const moved = other.children.find((c) => c.id === CHILD_ID)!;
      expect(moved.position).toBe(1);
    });

    it("inserts at root at the correct slot when parentId is null with explicit position", () => {
      // Forest: three root-level topics at positions 0, 1, 2. Moving Child
      // to root with position 1 should slot between them.
      const list = {
        topics: [
          mkNode("a", "A", {
            position: 0,
            created_at: new Date(1_000).toISOString(),
          }),
          mkNode("b", "B", {
            position: 1,
            created_at: new Date(2_000).toISOString(),
          }),
          mkNode("c", "C", {
            position: 2,
            created_at: new Date(3_000).toISOString(),
          }),
          mkNode("parent", "Parent", {
            id: PARENT_ID,
            position: 10,
            children: [
              mkNode(CHILD_ID, "Child", {
                parent_id: PARENT_ID,
                position: 0,
                created_at: new Date(500).toISOString(),
              }),
            ],
          }),
        ],
        unassigned_count: 0,
      } as unknown as TopicListResponse;

      const next = patchTopicListForMove(
        list,
        {
          id: CHILD_ID,
          hubId: HUB_ID,
          parentId: PARENT_ID,
          position: 0,
          name: "Child",
        },
        { parentId: null, position: 1 },
      );

      // At root level, Child (pos 1, older than B) should land between A
      // and B.
      expect(next.topics.map((n) => n.id)).toEqual([
        "a",
        CHILD_ID,
        "b",
        "c",
        PARENT_ID,
      ]);
    });

    it("places the moved node first when target.position is smaller than every sibling", () => {
      const list = {
        topics: [
          mkNode("other", "Other", {
            id: OTHER_ID,
            children: [
              mkNode("o-a", "O-A", { parent_id: OTHER_ID, position: 5 }),
              mkNode("o-b", "O-B", { parent_id: OTHER_ID, position: 6 }),
            ],
          }),
          mkNode(CHILD_ID, "Child", { position: 99 }),
        ],
        unassigned_count: 0,
      } as unknown as TopicListResponse;

      const next = patchTopicListForMove(
        list,
        {
          id: CHILD_ID,
          hubId: HUB_ID,
          parentId: null,
          position: 99,
          name: "Child",
        },
        { parentId: OTHER_ID, position: 0 },
      );

      const other = next.topics.find((n) => n.id === OTHER_ID)!;
      expect(other.children.map((c) => c.id)).toEqual([CHILD_ID, "o-a", "o-b"]);
    });

    it("places the moved node last when target.position is larger than every sibling", () => {
      const list = {
        topics: [
          mkNode("other", "Other", {
            id: OTHER_ID,
            children: [
              mkNode("o-a", "O-A", { parent_id: OTHER_ID, position: 0 }),
              mkNode("o-b", "O-B", { parent_id: OTHER_ID, position: 1 }),
            ],
          }),
          mkNode(CHILD_ID, "Child", { position: 99 }),
        ],
        unassigned_count: 0,
      } as unknown as TopicListResponse;

      const next = patchTopicListForMove(
        list,
        {
          id: CHILD_ID,
          hubId: HUB_ID,
          parentId: null,
          position: 99,
          name: "Child",
        },
        { parentId: OTHER_ID, position: 50 },
      );

      const other = next.topics.find((n) => n.id === OTHER_ID)!;
      expect(other.children.map((c) => c.id)).toEqual(["o-a", "o-b", CHILD_ID]);
    });
  });
});
