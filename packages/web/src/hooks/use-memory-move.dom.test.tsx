// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, render, waitFor, act } from "@testing-library/react";
import { QueryClientProvider, type InfiniteData } from "@tanstack/react-query";
import { useSyncExternalStore, type ReactNode } from "react";
import type { HubWithRole, Memory, TopicListResponse } from "memax-sdk";
import { MemaxError } from "memax-sdk";
import { queryClient } from "@/lib/query-client";
import {
  memoryDetailQueryKey,
  memoryListQueryKey,
  memoryListQueryPrefix,
  type MemoriesListResponse,
} from "./use-memories";
import { hubListQueryKey } from "./use-hubs";

// ── Mocks ─────────────────────────────────────────────────────────────────

const batchMoveMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    memories: {
      batchMove: (...args: unknown[]) => batchMoveMock(...args),
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

// Minimal i18n mock — returns the keys the hook reads.
vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      batch: {
        moved: "Moved {n} memories.",
        movedToDestination: "Moved {n} memories to {name}.",
        moveFailed: "moveFailed",
        moveNotReady: "moveNotReady",
        moveSourceDenied: "moveSourceDenied",
        noWriteAccess: "noWriteAccess",
        targetNotFound: "targetNotFound",
        partialMove: "{success} · {skipped} skipped",
      },
      toast: {
        moved: "Moved.",
        movedTo: "Moved to {name}.",
        topicCleared: "Topic cleared.",
        undoing: "toast.undoing",
        moveUndone: "toast.moveUndone",
        moveUndoFailed: "toast.moveUndoFailed",
      },
      import: {
        undo: "import.undo",
      },
      errors: {
        action: {
          moveMemory: "move that memory",
          moveMemories: "move those memories",
        },
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

// Defer import so mocks apply before module eval.
// eslint-disable-next-line import/first
import { useMemoryMove } from "./use-memory-move";

// ── Test fixtures ─────────────────────────────────────────────────────────

// Memory ID must pass isServerMovableSnapshot's v1-5 UUID regex
// (third group starts with [1-5], fourth with [89ab]).
const HUB_ID = "11111111-1111-4111-8111-111111111111";
const OTHER_HUB_ID = "22222222-2222-4222-8222-222222222222";
const TOPIC_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const TOPIC_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const MEMORY_ID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";

function makeMemory(overrides: Partial<Memory> = {}): Memory {
  return {
    id: MEMORY_ID,
    hub_id: HUB_ID,
    owner_id: "u1",
    title: "note",
    content: "content",
    content_type: "markdown",
    content_hash: "hash",
    summary: "",
    kind: "semantic",
    stability: "evolving",
    retrieval_weight: 1,
    tags: [],
    boundary: "",
    state: "active",
    pinned: false,
    source: "test",
    topic_id: TOPIC_A,
    version: 1,
    access_count: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    accessed_at: new Date().toISOString(),
    ...overrides,
  } as Memory;
}

function makeHubs(): HubWithRole[] {
  return [
    {
      hub: {
        id: HUB_ID,
        name: "Personal",
        slug: "personal",
        icon: "",
        accent: "",
        hub_type: "personal",
        plan: "",
        owner_id: "u1",
        allow_contributor_topics: true,
        allow_contributor_dreams: true,
        contributor_delete_policy: "own",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      role: "owner",
    },
  ] as unknown as HubWithRole[];
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  queryClient.clear();
  batchMoveMock.mockReset();
  toastSuccess.mockReset();
  toastError.mockReset();
  toastInfo.mockReset();
  toastDismiss.mockReset();
  // Seed the memory detail cache so collectKnownMemories finds the source.
  queryClient.setQueryData(memoryDetailQueryKey(MEMORY_ID), makeMemory());
  queryClient.setQueryData(hubListQueryKey, makeHubs());
});

afterEach(() => {
  queryClient.clear();
});

// ── Tests ─────────────────────────────────────────────────────────────────

describe("useMemoryMove", () => {
  describe("cache-shape drift (HOTFIX-1 regression)", () => {
    it("does not crash when the topics prefix holds a TopicListResponse", async () => {
      // This is the exact regression the TypeError bug manifested: the
      // ["topics", hubId] cache entry is a TopicListResponse shape (no
      // `.memories` field), and collectKnownMemories used to blow up on it
      // via `data.memories.forEach` on undefined.
      const topicList: TopicListResponse = {
        topics: [
          {
            id: TOPIC_A,
            name: "Alpha",
            icon: "folder",
            children: [],
            memory_count: 1,
            total_memory_count: 1,
          },
        ],
      } as unknown as TopicListResponse;
      queryClient.setQueryData(["topics", HUB_ID], topicList);

      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let moved = false;
      await act(async () => {
        moved = await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "custom-success",
        );
      });

      expect(moved).toBe(true);
      expect(batchMoveMock).toHaveBeenCalledTimes(1);
      expect(toastError).not.toHaveBeenCalled();
    });
  });

  describe("lifecycle", () => {
    it("applies optimistic hub + topic change then resolves on success", async () => {
      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const detail = queryClient.getQueryData<Memory>(
        memoryDetailQueryKey(MEMORY_ID),
      );
      expect(detail?.topic_id).toBe(TOPIC_B);
      expect(toastSuccess).toHaveBeenCalledWith(
        "success-msg",
        expect.objectContaining({ actionLabel: "import.undo" }),
      );
    });

    // Codex flagged the stale-row-chip symptom as still unverified after the
    // SDK normalization fix: the detail-cache assertion above proves the
    // memory-detail `<TopicLocation>` path updates, but said nothing about
    // the `["memory-lists", hubId, sort]` infinite-data cache the `/memories`
    // grid and unassigned inbox subscribe to. Both surfaces read
    // `memory.topic_id` off that cache and resolve it to a chip label via
    // `topicPathLookup.get(...)`, so if the optimistic patch misses the
    // infinite-data shape the visible chip stays pinned to the old topic
    // even after the move succeeds. Assertion below is a direct cache probe.
    it("patches the memory-lists infinite-data cache so row chips read the new topic_id", async () => {
      const baseline = makeMemory({ topic_id: TOPIC_A });
      queryClient.setQueryData<InfiniteData<MemoriesListResponse>>(
        memoryListQueryKey(HUB_ID, "recent"),
        {
          pages: [
            {
              memories: [baseline],
              next_cursor: "",
              has_more: false,
              total: 1,
            },
          ],
          pageParams: [""],
        },
      );

      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const patched = queryClient.getQueryData<
        InfiniteData<MemoriesListResponse>
      >(memoryListQueryKey(HUB_ID, "recent"));
      const firstMemory = patched?.pages?.[0]?.memories?.[0];
      expect(firstMemory?.id).toBe(MEMORY_ID);
      expect(firstMemory?.topic_id).toBe(TOPIC_B);
    });

    // Rendered-DOM probe — the direct regression guard Codex asked for. The
    // cache assertion above proves the data shape is right, but not that a
    // subscriber re-renders. This mounts a component that subscribes to the
    // memory-lists cache via `useSyncExternalStore` (avoids React Query's
    // refetch lifecycle entirely — invalidation can't clobber the patched
    // state on an observer) and displays the memory's current `topic_id`.
    // If the optimistic patch and notification pipeline both work, the
    // rendered text flips from TOPIC_A to TOPIC_B after the move.
    it("re-renders the visible row-chip path against the patched memory-lists cache after move", async () => {
      const baseline = makeMemory({ topic_id: TOPIC_A });
      queryClient.setQueryData<InfiniteData<MemoriesListResponse>>(
        memoryListQueryKey(HUB_ID, "recent"),
        {
          pages: [
            {
              memories: [baseline],
              next_cursor: "",
              has_more: false,
              total: 1,
            },
          ],
          pageParams: [""],
        },
      );

      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      function ChipProbe() {
        const data = useSyncExternalStore(
          (notify) =>
            queryClient.getQueryCache().subscribe(() => {
              notify();
            }),
          () =>
            queryClient.getQueryData<InfiniteData<MemoriesListResponse>>(
              memoryListQueryKey(HUB_ID, "recent"),
            ),
          () => undefined,
        );
        const first = data?.pages?.[0]?.memories?.[0];
        return <div data-testid="chip-topic">{first?.topic_id ?? "none"}</div>;
      }

      const { getByTestId } = render(<ChipProbe />, { wrapper });
      expect(getByTestId("chip-topic").textContent).toBe(TOPIC_A);

      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      await waitFor(() => {
        expect(getByTestId("chip-topic").textContent).toBe(TOPIC_B);
      });
    });

    it("surfaces skipped counts when the server returns a partial BatchMoveResult", async () => {
      // Server accepted 2 of 3; the third was not owned by the caller.
      batchMoveMock.mockResolvedValueOnce({
        moved: 2,
        skipped: [{ id: "not-mine", reason: "not_owned" }],
      });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "Moved 2 memories.",
        );
      });

      expect(toastSuccess).toHaveBeenCalledWith(
        "Moved 2 memories. · 1 skipped",
        expect.objectContaining({ actionLabel: "import.undo" }),
      );
    });

    it("rolls back the optimistic patch when the mutation rejects", async () => {
      batchMoveMock.mockRejectedValueOnce(
        new MemaxError("Not a member", "not_member", 403),
      );

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const detail = queryClient.getQueryData<Memory>(
        memoryDetailQueryKey(MEMORY_ID),
      );
      expect(detail?.topic_id).toBe(TOPIC_A);
      expect(detail?.hub_id).toBe(HUB_ID);
      expect(toastError).toHaveBeenCalledWith("noWriteAccess");
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it("invalidates caches on settled regardless of outcome", async () => {
      const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const invalidatedKeys = invalidateSpy.mock.calls.map((call) =>
        JSON.stringify(call[0]?.queryKey),
      );
      expect(invalidatedKeys.some((k) => k.includes("memory-lists"))).toBe(
        true,
      );
      expect(invalidatedKeys.some((k) => k.includes("topics"))).toBe(true);
      expect(invalidatedKeys.some((k) => k.includes("hub-summary"))).toBe(true);

      invalidateSpy.mockRestore();
    });
  });

  describe("single-flight guard", () => {
    it("blocks re-entry while a move is in flight", async () => {
      type PendingResult = { moved: number; skipped: never[] };
      let resolveFirst: (value: PendingResult) => void = () => {};
      const firstPending = new Promise<PendingResult>((resolve) => {
        resolveFirst = resolve;
      });
      batchMoveMock.mockImplementationOnce(() => firstPending);

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let firstPromise!: Promise<boolean>;
      await act(async () => {
        firstPromise = result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "first",
        );
      });

      await waitFor(() => {
        expect(result.current.isPending).toBe(true);
      });

      let secondResult = true;
      await act(async () => {
        secondResult = await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "second",
        );
      });
      expect(secondResult).toBe(false);
      // Second call should not have hit the SDK.
      expect(batchMoveMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        resolveFirst({ moved: 1, skipped: [] });
        await firstPromise;
      });
    });
  });

  describe("error surface hygiene", () => {
    it("maps MemaxError topic_not_found to targetNotFound toast", async () => {
      batchMoveMock.mockRejectedValueOnce(
        new MemaxError("Topic gone", "topic_not_found", 404),
      );

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(toastError).toHaveBeenCalledWith("targetNotFound");
    });

    it("maps memory_move_incomplete plain Error to noWriteAccess", async () => {
      batchMoveMock.mockImplementationOnce(async () => ({
        moved: 0,
        skipped: [],
      }));

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(toastError).toHaveBeenCalledWith("noWriteAccess");
    });

    it("never leaks raw JS error messages to the toast", async () => {
      const consoleSpy = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});
      batchMoveMock.mockRejectedValueOnce(
        new TypeError(
          "Cannot read properties of undefined (reading 'forEach')",
        ),
      );

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(toastError).toHaveBeenCalledWith("moveFailed");
      expect(toastError).not.toHaveBeenCalledWith(
        expect.stringContaining("Cannot read"),
      );
      expect(consoleSpy).toHaveBeenCalled();
      consoleSpy.mockRestore();
    });

    it("rejects snapshots with non-UUID ids via moveNotReady", async () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let moved = true;
      await act(async () => {
        moved = await result.current.moveWithUndo(
          [{ id: "optimistic-123", hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(moved).toBe(false);
      expect(toastError).toHaveBeenCalledWith("moveNotReady");
      expect(batchMoveMock).not.toHaveBeenCalled();
    });
  });

  describe("undo path", () => {
    it("only undoes the subset the server actually moved (skipped ids are NOT replayed)", async () => {
      // Forward batch: 2 memories requested, server moved one and reported
      // the other as already_at_target. Undo must not touch the
      // already_at_target one — otherwise the user would see a phantom
      // move back to its pre-existing location.
      const MOVED_ID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd";
      const SKIPPED_ID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
      queryClient.setQueryData(
        memoryDetailQueryKey(MOVED_ID),
        makeMemory({ id: MOVED_ID }),
      );
      queryClient.setQueryData(
        memoryDetailQueryKey(SKIPPED_ID),
        makeMemory({ id: SKIPPED_ID }),
      );

      batchMoveMock.mockResolvedValueOnce({
        moved: 1,
        skipped: [{ id: SKIPPED_ID, reason: "already_at_target" }],
      });
      batchMoveMock.mockResolvedValueOnce({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [
            { id: MOVED_ID, hubId: HUB_ID, topicId: TOPIC_A },
            { id: SKIPPED_ID, hubId: HUB_ID, topicId: TOPIC_A },
          ],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const [, options] = toastSuccess.mock.calls[0] as [
        string,
        { onAction: () => void },
      ];

      await act(async () => {
        options.onAction();
        await new Promise((r) => setTimeout(r, 0));
      });

      await waitFor(() => {
        expect(batchMoveMock).toHaveBeenCalledTimes(2);
      });
      // Second call (undo) must carry only the MOVED id — never the skipped one.
      const undoIds = batchMoveMock.mock.calls[1][0] as string[];
      expect(undoIds).toEqual([MOVED_ID]);
      expect(undoIds).not.toContain(SKIPPED_ID);
    });

    it("guards undo against synchronous double-click (single-flight latch)", async () => {
      // Regression: the toast action button is a closed-over callback that
      // is not gated by React state — two rapid clicks in the same frame
      // both enter before any re-render. A useState-based guard would let
      // both through; only a useRef latch short-circuits the second call.
      // This test fires onAction twice inside the same act() block with
      // no await between them, then asserts exactly one undo mutation ran.
      batchMoveMock.mockResolvedValue({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      // Forward move consumed call #1; any subsequent calls are undo replays.
      expect(batchMoveMock).toHaveBeenCalledTimes(1);

      const [, options] = toastSuccess.mock.calls[0] as [
        string,
        { onAction: () => void },
      ];

      // Synchronous double-click — both calls happen before React can
      // re-render between them. The useRef latch must make the second
      // call a no-op; the useState isPending flag from React Query is
      // not sufficient because it flips asynchronously.
      act(() => {
        options.onAction();
        options.onAction();
      });

      // Let the single undo mutation settle.
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });

      await waitFor(() => {
        // Exactly one additional batchMove call (the undo replay).
        expect(batchMoveMock).toHaveBeenCalledTimes(2);
      });
      // Belt-and-suspenders: flush another tick and assert the second
      // click never landed late.
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
      expect(batchMoveMock).toHaveBeenCalledTimes(2);

      // The synchronous dismissal in onAction must fire at least once so
      // the stale success toast cannot render a second click target.
      expect(toastDismiss).toHaveBeenCalled();
      // The persistent in-flight notification must appear exactly once —
      // the second onAction was latched out before reaching toast.info.
      expect(toastInfo).toHaveBeenCalledTimes(1);
      expect(toastInfo).toHaveBeenCalledWith("toast.undoing");
    });

    it("wires the success toast with an undo action that replays with inverted target", async () => {
      batchMoveMock.mockResolvedValue({ moved: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      const [, options] = toastSuccess.mock.calls[0] as [
        string,
        { onAction: () => void },
      ];
      expect(options.onAction).toBeTypeOf("function");

      await act(async () => {
        options.onAction();
        // Let the undo mutation settle.
        await new Promise((r) => setTimeout(r, 0));
      });

      await waitFor(() => {
        // 1 forward + 1 undo
        expect(batchMoveMock).toHaveBeenCalledTimes(2);
      });
      // Second call (undo) must target the original topic, not the destination.
      const undoCall = batchMoveMock.mock.calls[1];
      expect(undoCall[1]).toEqual(
        expect.objectContaining({ hubId: HUB_ID, topicId: TOPIC_A }),
      );
    });
  });

  describe("harmonized success messages", () => {
    // North-star UX: single / batch / drag-drop all produce the same
    // "Moved to {name}." family of strings. The hook exposes three helpers
    // (moveOneSuccess / moveManySuccess / moveOneCleared) that all three
    // user surfaces share, so the voice is uniform regardless of which
    // control the user interacted with.

    it("moveOneSuccess with a destination returns 'Moved to {name}.'", () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveOneSuccess("Design")).toBe("Moved to Design.");
    });

    it("moveOneSuccess with no destination falls back to 'Moved.'", () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveOneSuccess()).toBe("Moved.");
    });

    it("moveOneSuccess with explicit undefined destination also falls back to 'Moved.'", () => {
      // Regression for drag/drop fallback path: when the droppable doesn't
      // carry topicName, topic-dnd-provider passes `undefined` through to
      // moveOneSuccess(). This must yield the generic "Moved." string,
      // never stringify an unrelated label as if it were the destination.
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveOneSuccess(undefined)).toBe("Moved.");
    });

    it("moveManySuccess with a destination returns 'Moved N memories to {name}.'", () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveManySuccess(3, "Design")).toBe(
        "Moved 3 memories to Design.",
      );
    });

    it("moveManySuccess with no destination falls back to the generic count string", () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveManySuccess(3)).toBe("Moved 3 memories.");
    });

    it("moveOneCleared returns the dedicated 'Topic cleared.' string", () => {
      const { result } = renderHook(() => useMemoryMove(), { wrapper });
      expect(result.current.moveOneCleared()).toBe("Topic cleared.");
    });
  });

  describe("source_delete_forbidden handling", () => {
    // Regression coverage for the server's new source-hub delete
    // permission check (commit 1bec0efd). The hook must:
    //   1. Route full-skip batches with all source_delete_forbidden
    //      reasons to the new moveSourceDenied toast, not the generic
    //      noWriteAccess fallback.
    //   2. Fall through to noWriteAccess for mixed-reason full skips
    //      or pure not_owned.
    //   3. Still surface partial-success (moved > 0) with the skip
    //      suffix, without triggering the error branch.
    //   4. Preserve onError rollback — the custom
    //      MemoryMoveIncompleteError still extends Error, so React
    //      Query's mutation cache sees it as failure and restores the
    //      optimistic snapshot.

    it("full-skip with all source_delete_forbidden → moveSourceDenied toast", async () => {
      batchMoveMock.mockResolvedValueOnce({
        moved: 0,
        skipped: [{ id: MEMORY_ID, reason: "source_delete_forbidden" }],
      });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let moved = true;
      await act(async () => {
        moved = await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(moved).toBe(false);
      expect(toastError).toHaveBeenCalledWith("moveSourceDenied");
      // MUST NOT fall through to the generic noWriteAccess branch.
      expect(toastError).not.toHaveBeenCalledWith("noWriteAccess");
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it("full-skip with mixed reasons falls through to noWriteAccess", async () => {
      // One source_delete_forbidden + one not_owned — the reason-aware
      // branch only fires when EVERY skip is source_delete_forbidden.
      // Mixed cases use the existing generic copy.
      const OTHER_ID = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa";
      batchMoveMock.mockResolvedValueOnce({
        moved: 0,
        skipped: [
          { id: MEMORY_ID, reason: "source_delete_forbidden" },
          { id: OTHER_ID, reason: "not_owned" },
        ],
      });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let moved = true;
      await act(async () => {
        moved = await result.current.moveWithUndo(
          [
            { id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A },
            { id: OTHER_ID, hubId: HUB_ID, topicId: TOPIC_A },
          ],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(moved).toBe(false);
      expect(toastError).toHaveBeenCalledWith("noWriteAccess");
      expect(toastError).not.toHaveBeenCalledWith("moveSourceDenied");
    });

    it("partial success with source_delete_forbidden skipped contributes to suffix", async () => {
      // 1 moved + 1 blocked by source policy → partial success toast
      // with " · 1 skipped" suffix, NOT the full-skip error branch.
      const OTHER_ID = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa";
      batchMoveMock.mockResolvedValueOnce({
        moved: 1,
        skipped: [{ id: OTHER_ID, reason: "source_delete_forbidden" }],
      });
      // Seed the moved memory's snapshot cache so applyOptimisticMove
      // doesn't bail.
      queryClient.setQueryData(memoryDetailQueryKey(MEMORY_ID), makeMemory());
      queryClient.setQueryData(
        memoryDetailQueryKey(OTHER_ID),
        makeMemory({ id: OTHER_ID }),
      );

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      let moved = false;
      await act(async () => {
        moved = await result.current.moveWithUndo(
          [
            { id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A },
            { id: OTHER_ID, hubId: HUB_ID, topicId: TOPIC_A },
          ],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      expect(moved).toBe(true);
      // Partial-success path fires, NOT the error branch.
      expect(toastError).not.toHaveBeenCalled();
      expect(toastSuccess).toHaveBeenCalled();
      const [message] = toastSuccess.mock.calls[0] as [string, ...unknown[]];
      expect(message).toContain("success-msg");
      expect(message).toContain("1 skipped");
    });

    it("full-skip with all source_delete_forbidden rolls back the optimistic snapshot", async () => {
      // The MemoryMoveIncompleteError must still extend Error so React
      // Query's onError path fires and restores the list-cache snapshot.
      // Seed a list cache with the memory, trigger a full-skip move,
      // verify the memory is still in the cache after the mutation
      // settles (optimistic removal was rolled back).
      const listKey = memoryListQueryKey(HUB_ID, "recent");
      queryClient.setQueryData(listKey, {
        pages: [
          {
            memories: [makeMemory()],
            total: 1,
            next_cursor: "",
            has_more: false,
          },
        ],
        pageParams: [null],
      } as InfiniteData<MemoriesListResponse>);

      batchMoveMock.mockResolvedValueOnce({
        moved: 0,
        skipped: [{ id: MEMORY_ID, reason: "source_delete_forbidden" }],
      });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      // After rollback, the memory must still be in the cache.
      const restored =
        queryClient.getQueryData<InfiniteData<MemoriesListResponse>>(listKey);
      expect(restored?.pages[0].memories).toHaveLength(1);
      expect(restored?.pages[0].memories[0]?.id).toBe(MEMORY_ID);
    });

    it("empty skipped array with moved=0 (edge) falls through to noWriteAccess", async () => {
      // Defensive guard: if the server ever returns {moved: 0, skipped: []}
      // (shouldn't happen in practice, but the reason-aware branch
      // requires skipped.length > 0 to short-circuit), we must not
      // mis-classify the empty array as "all source_delete_forbidden"
      // and show the wrong copy. The .every() predicate on an empty
      // array returns true, so an explicit length guard is required.
      batchMoveMock.mockResolvedValueOnce({ moved: 0, skipped: [] });

      const { result } = renderHook(() => useMemoryMove(), { wrapper });

      await act(async () => {
        await result.current.moveWithUndo(
          [{ id: MEMORY_ID, hubId: HUB_ID, topicId: TOPIC_A }],
          { hubId: OTHER_HUB_ID, topicId: TOPIC_B },
          "success-msg",
        );
      });

      // Empty skipped + moved=0 must NOT route to moveSourceDenied
      // (false "all of empty set satisfies" trap). Falls through to
      // the generic full-skip copy.
      expect(toastError).toHaveBeenCalledWith("noWriteAccess");
      expect(toastError).not.toHaveBeenCalledWith("moveSourceDenied");
    });
  });
});
