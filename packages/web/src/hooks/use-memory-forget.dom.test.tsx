// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { InfiniteData } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type {
  Memory,
  TopicListResponse,
  TopicMemoriesResponse,
} from "memax-sdk";
import { MemaxError } from "memax-sdk";
import { queryClient } from "@/lib/query-client";
import {
  memoryDetailQueryKey,
  memoryListQueryPrefix,
  type MemoriesListResponse,
} from "./use-memories";

// ── Mocks ─────────────────────────────────────────────────────────────────

const batchDeleteMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    memories: {
      batchDelete: (...args: unknown[]) => batchDeleteMock(...args),
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
        forgot: "{n} memories forgotten",
        forgetFailed: "forgetFailed",
        forgetDenied: "forgetDenied",
        forgetNotReady: "forgetNotReady",
        partialForget: "{success} · {skipped} skipped",
      },
      toast: {
        forgot: "Forgot.",
      },
      errors: {
        action: {
          forgetMemory: "forget that memory",
          forgetMemories: "forget those memories",
        },
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
}));

// eslint-disable-next-line import/first
import { useMemoryForget } from "./use-memory-forget";

// ── Test fixtures ─────────────────────────────────────────────────────────

// Valid v1-5 UUIDs (third group starts with [1-5], fourth with [89ab]).
const HUB_ID = "11111111-1111-4111-8111-111111111111";
const TOPIC_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const MEMORY_ID_1 = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
const MEMORY_ID_2 = "dddddddd-dddd-4ddd-8ddd-dddddddddddd";
const MEMORY_ID_3 = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";
const MEMORY_ID_4 = "ffffffff-ffff-4fff-8fff-ffffffffffff";
const MEMORY_ID_5 = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa";

function makeMemory(id: string, overrides: Partial<Memory> = {}): Memory {
  return {
    id,
    hub_id: HUB_ID,
    owner_id: "u1",
    title: "note " + id,
    content: "content",
    content_type: "markdown",
    content_hash: "hash-" + id,
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

function makeMemoryListPage(
  memories: Memory[],
): InfiniteData<MemoriesListResponse> {
  return {
    pages: [
      {
        memories,
        total: memories.length,
        next_cursor: "",
        has_more: false,
      } as MemoriesListResponse,
    ],
    pageParams: [null],
  };
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  queryClient.clear();
  batchDeleteMock.mockReset();
  toastSuccess.mockReset();
  toastError.mockReset();
  toastInfo.mockReset();
  toastDismiss.mockReset();
});

afterEach(() => {
  queryClient.clear();
});

// ── Tests ─────────────────────────────────────────────────────────────────

describe("useMemoryForget", () => {
  describe("cache-shape drift regression", () => {
    it("does not crash when the topics prefix holds a TopicListResponse", async () => {
      // Same regression class as useMemoryMove's HOTFIX-1: a cached
      // ["topics", hubId] entry is a TopicListResponse (no .memories),
      // and the cache walk must filter by query-key shape before
      // accessing inner fields.
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
      queryClient.setQueryData(
        memoryListQueryPrefix,
        makeMemoryListPage([makeMemory(MEMORY_ID_1)]),
      );

      batchDeleteMock.mockResolvedValueOnce({ deleted: 1, skipped: [] });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = false;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      expect(forgot).toBe(true);
      expect(batchDeleteMock).toHaveBeenCalledTimes(1);
      expect(toastError).not.toHaveBeenCalled();
    });
  });

  describe("optimistic cache strategy", () => {
    it("removes ids from memory list caches on mutate", async () => {
      const mem1 = makeMemory(MEMORY_ID_1);
      const mem2 = makeMemory(MEMORY_ID_2);
      const listKey = [...memoryListQueryPrefix, HUB_ID] as const;
      queryClient.setQueryData(listKey, makeMemoryListPage([mem1, mem2]));

      // Resolve slowly so we can inspect in-flight cache state.
      let resolve!: (value: unknown) => void;
      const pending = new Promise((r) => {
        resolve = r;
      });
      batchDeleteMock.mockImplementationOnce(() => pending);

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgetPromise!: Promise<boolean>;
      act(() => {
        forgetPromise = result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // onMutate has applied optimistic removal synchronously by now.
      await waitFor(() => {
        const live =
          queryClient.getQueryData<InfiniteData<MemoriesListResponse>>(listKey);
        expect(live?.pages[0].memories.map((m) => m.id)).toEqual([MEMORY_ID_2]);
      });

      await act(async () => {
        resolve({ deleted: 1, skipped: [] });
        await forgetPromise;
      });
    });

    it("does NOT touch memoryDetailQueryKey during the optimistic path", async () => {
      // Regression lock-in for Codex correction: the detail cache is
      // owned by useMemory(id), and removing it optimistically would
      // blank /memories/[id] during the in-flight window whenever the
      // server ends up rejecting the id (partial-skip scenarios).
      const mem = makeMemory(MEMORY_ID_1);
      queryClient.setQueryData(memoryDetailQueryKey(MEMORY_ID_1), mem);

      // Slow mutation so we can inspect the mid-flight cache.
      let resolve!: (value: unknown) => void;
      const pending = new Promise((r) => {
        resolve = r;
      });
      batchDeleteMock.mockImplementationOnce(() => pending);

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgetPromise!: Promise<boolean>;
      act(() => {
        forgetPromise = result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // During the in-flight window, detail cache must still have the
      // memory — list-only optimistic strategy.
      const stillThere = queryClient.getQueryData<Memory>(
        memoryDetailQueryKey(MEMORY_ID_1),
      );
      expect(stillThere).toBeDefined();
      expect(stillThere?.id).toBe(MEMORY_ID_1);

      await act(async () => {
        resolve({ deleted: 1, skipped: [] });
        await forgetPromise;
      });
    });

    it("does not clobber concurrent detail refetches on error rollback", async () => {
      // Regression lock-in for Codex correction: v3 planned to snapshot
      // and restore detail caches, which would overwrite any concurrent
      // refetch that landed newer data during the mutation window. The
      // final design OMITS detail caches from snapshot/restore entirely.
      const staleMemory = makeMemory(MEMORY_ID_1, { title: "stale" });
      const refetchedMemory = makeMemory(MEMORY_ID_1, { title: "refetched" });
      queryClient.setQueryData(memoryDetailQueryKey(MEMORY_ID_1), staleMemory);

      // Expected console.error from the hook's unknown-error branch —
      // suppress the noise so the test output stays clean.
      const consoleError = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});

      // Deferred rejection — we need to land the "refetched" value into
      // the cache between onMutate (snapshot time) and the rejection.
      let reject!: (reason: unknown) => void;
      const pending = new Promise((_, r) => {
        reject = r;
      });
      batchDeleteMock.mockImplementationOnce(() => pending);

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgetPromise!: Promise<boolean>;
      act(() => {
        forgetPromise = result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // Simulate a concurrent detail refetch landing newer data.
      act(() => {
        queryClient.setQueryData(
          memoryDetailQueryKey(MEMORY_ID_1),
          refetchedMemory,
        );
      });

      // Now reject the mutation — onError runs rollback.
      await act(async () => {
        reject(new Error("server down"));
        await forgetPromise;
      });

      // Detail cache must still hold the refetched value, NOT be
      // restored to the stale snapshot. onSettled invalidation will
      // eventually refresh it further.
      const afterRollback = queryClient.getQueryData<Memory>(
        memoryDetailQueryKey(MEMORY_ID_1),
      );
      expect(afterRollback?.title).toBe("refetched");
      consoleError.mockRestore();
    });
  });

  describe("forgetWithConfirm success paths", () => {
    it("builder receives effectiveDeleted, not ids.length or result.deleted", async () => {
      // Selection of 5 where 3 are really deleted + 1 not_found + 1 not_owned.
      // effectiveDeleted = 3 + 1 = 4. realSkipped = 1 (not_owned).
      // Builder must receive 4, not 5 (ids.length) or 3 (result.deleted).
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 3,
        skipped: [
          { id: MEMORY_ID_4, reason: "not_found" },
          { id: MEMORY_ID_5, reason: "not_owned" },
        ],
      });

      const builder = vi.fn((count: number) => `Forgot ${count} memories.`);
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm(
          [MEMORY_ID_1, MEMORY_ID_2, MEMORY_ID_3, MEMORY_ID_4, MEMORY_ID_5],
          builder,
        );
      });

      expect(builder).toHaveBeenCalledTimes(1);
      expect(builder).toHaveBeenCalledWith(4);
      // Toast uses the builder output + realSkipped suffix.
      expect(toastSuccess).toHaveBeenCalledWith(
        "Forgot 4 memories. · 1 skipped",
      );
    });

    it("all-not_found passes ids.length to builder (every id in desired end state)", async () => {
      // Select 5, all 5 come back as not_found (race: every memory was
      // deleted in another session). effectiveDeleted = 0 + 5 = 5.
      // User selected 5 memories and all 5 are gone — success copy
      // matches their intent, NO partial-skip suffix.
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 0,
        skipped: [
          { id: MEMORY_ID_1, reason: "not_found" },
          { id: MEMORY_ID_2, reason: "not_found" },
          { id: MEMORY_ID_3, reason: "not_found" },
          { id: MEMORY_ID_4, reason: "not_found" },
          { id: MEMORY_ID_5, reason: "not_found" },
        ],
      });

      const builder = vi.fn((count: number) => `Forgot ${count} memories.`);
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = false;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm(
          [MEMORY_ID_1, MEMORY_ID_2, MEMORY_ID_3, MEMORY_ID_4, MEMORY_ID_5],
          builder,
        );
      });

      expect(forgot).toBe(true);
      expect(builder).toHaveBeenCalledWith(5);
      expect(toastSuccess).toHaveBeenCalledWith("Forgot 5 memories.");
      // No error toast fires for all-not_found — it's effective success.
      expect(toastError).not.toHaveBeenCalled();
    });

    it("uses default builder when none is passed (singular vs plural)", async () => {
      batchDeleteMock.mockResolvedValueOnce({ deleted: 1, skipped: [] });
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // Singular — uses t.toast.forgot = "Forgot."
      expect(toastSuccess).toHaveBeenCalledWith("Forgot.");
    });

    it("uses plural default builder for multi-id batches", async () => {
      batchDeleteMock.mockResolvedValueOnce({ deleted: 3, skipped: [] });
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([
          MEMORY_ID_1,
          MEMORY_ID_2,
          MEMORY_ID_3,
        ]);
      });

      // Plural — uses t.batch.forgot = "{n} memories forgotten" interpolated.
      expect(toastSuccess).toHaveBeenCalledWith("3 memories forgotten");
    });

    it("partial success with not_owned adds ' · N skipped' suffix", async () => {
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 2,
        skipped: [{ id: MEMORY_ID_3, reason: "not_owned" }],
      });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([
          MEMORY_ID_1,
          MEMORY_ID_2,
          MEMORY_ID_3,
        ]);
      });

      // 2 deleted → plural builder → "2 memories forgotten" + " · 1 skipped"
      expect(toastSuccess).toHaveBeenCalledWith(
        "2 memories forgotten · 1 skipped",
      );
    });
  });

  describe("forgetWithConfirm full-skip error branches", () => {
    it("full-skip all-not_owned → forgetDenied error toast", async () => {
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 0,
        skipped: [
          { id: MEMORY_ID_1, reason: "not_owned" },
          { id: MEMORY_ID_2, reason: "not_owned" },
        ],
      });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = true;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm([
          MEMORY_ID_1,
          MEMORY_ID_2,
        ]);
      });

      expect(forgot).toBe(false);
      expect(toastError).toHaveBeenCalledWith("forgetDenied");
      expect(toastSuccess).not.toHaveBeenCalled();
    });

    it("full-skip all-delete_failed → forgetFailed error toast (distinct from denied)", async () => {
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 0,
        skipped: [
          { id: MEMORY_ID_1, reason: "delete_failed" },
          { id: MEMORY_ID_2, reason: "delete_failed" },
        ],
      });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = true;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm([
          MEMORY_ID_1,
          MEMORY_ID_2,
        ]);
      });

      expect(forgot).toBe(false);
      // MUST be forgetFailed, not forgetDenied — different retry semantics.
      expect(toastError).toHaveBeenCalledWith("forgetFailed");
    });

    it("full-skip mixed delete_failed + not_owned → forgetFailed wins", async () => {
      // delete_failed takes precedence because infra issues are retryable
      // and the user may fix them; not_owned is a permission dead-end.
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 0,
        skipped: [
          { id: MEMORY_ID_1, reason: "not_owned" },
          { id: MEMORY_ID_2, reason: "delete_failed" },
        ],
      });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1, MEMORY_ID_2]);
      });

      expect(toastError).toHaveBeenCalledWith("forgetFailed");
    });

    it("full-skip mixed not_found + not_owned → forgetDenied (not_found removed from count)", async () => {
      // not_found collapses into effectiveDeleted, but here the one
      // remaining real skip is not_owned. effectiveDeleted = 0 + 1 = 1,
      // so this is NOT a full-skip branch — it's partial success.
      batchDeleteMock.mockResolvedValueOnce({
        deleted: 0,
        skipped: [
          { id: MEMORY_ID_1, reason: "not_found" },
          { id: MEMORY_ID_2, reason: "not_owned" },
        ],
      });

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = false;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm([
          MEMORY_ID_1,
          MEMORY_ID_2,
        ]);
      });

      // Partial success: effectiveDeleted=1, realSkipped=1
      expect(forgot).toBe(true);
      // Default builder for count=1 → "Forgot." + " · 1 skipped"
      expect(toastSuccess).toHaveBeenCalledWith("Forgot. · 1 skipped");
    });
  });

  describe("guards and error surface", () => {
    it("single-flight guard: second call returns false without hitting SDK", async () => {
      let resolve!: (value: unknown) => void;
      const pending = new Promise((r) => {
        resolve = r;
      });
      batchDeleteMock.mockImplementationOnce(() => pending);

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let first!: Promise<boolean>;
      act(() => {
        first = result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // Wait for isPending to propagate, then fire a second call.
      await waitFor(() => {
        expect(result.current.isPending).toBe(true);
      });

      let second = true;
      await act(async () => {
        second = await result.current.forgetWithConfirm([MEMORY_ID_2]);
      });
      expect(second).toBe(false);
      // SDK called exactly once (for the first call).
      expect(batchDeleteMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        resolve({ deleted: 1, skipped: [] });
        await first;
      });
    });

    it("UUID guard rejects non-UUID ids without calling SDK", async () => {
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = true;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm(["optimistic-123"]);
      });

      expect(forgot).toBe(false);
      expect(batchDeleteMock).not.toHaveBeenCalled();
      expect(toastError).toHaveBeenCalledWith("forgetNotReady");
    });

    it("maps MemaxError permission codes to forgetDenied", async () => {
      batchDeleteMock.mockRejectedValueOnce(
        new MemaxError("raw server message", "permission_denied", 403),
      );

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      let forgot = true;
      await act(async () => {
        forgot = await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      expect(forgot).toBe(false);
      expect(toastError).toHaveBeenCalledWith("forgetDenied");
      // Raw server message must NOT leak to the user.
      expect(toastError).not.toHaveBeenCalledWith(
        expect.stringContaining("raw server message"),
      );
    });

    it("classifies 5xx MemaxError via status class (surfaces server copy, not generic)", async () => {
      // An unmapped error code with a known HTTP status class must fall into
      // the classifier branch — "memax had a hiccup…" is strictly friendlier
      // than the old generic forgetFailed fallback, and skips the console
      // error path because the error IS classifiable.
      const consoleError = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});
      batchDeleteMock.mockRejectedValueOnce(
        new MemaxError("weird error", "some_weird_code", 500),
      );

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      expect(toastError).toHaveBeenCalledWith(
        "memax had a hiccup. Try to forget that memory again in a moment.",
        { autoDismissMs: 5000 },
      );
      expect(consoleError).not.toHaveBeenCalled();
      consoleError.mockRestore();
    });

    it("falls through to forgetFailed when status is not in a known class", async () => {
      // 418 is deliberately outside every classifier predicate
      // (isServerError is 500-599, not 418). The error is a MemaxError but
      // has no known code and no classifiable status class → generic
      // forgetFailed with console.error for devtools inspection.
      const consoleError = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});
      batchDeleteMock.mockRejectedValueOnce(
        new MemaxError("teapot", "some_weird_code", 418),
      );

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      expect(toastError).toHaveBeenCalledWith("forgetFailed");
      expect(consoleError).toHaveBeenCalled();
      consoleError.mockRestore();
    });

    it("maps raw TypeError to forgetFailed and logs", async () => {
      const consoleError = vi
        .spyOn(console, "error")
        .mockImplementation(() => {});
      batchDeleteMock.mockRejectedValueOnce(new TypeError("undefined is not"));

      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      expect(toastError).toHaveBeenCalledWith("forgetFailed");
      // Raw JS error must NOT leak to the user.
      expect(toastError).not.toHaveBeenCalledWith(
        expect.stringContaining("undefined is not"),
      );
      expect(consoleError).toHaveBeenCalled();
      consoleError.mockRestore();
    });

    it("success toast has NO undo action (regression guard for no-undo contract)", async () => {
      batchDeleteMock.mockResolvedValueOnce({ deleted: 1, skipped: [] });
      const { result } = renderHook(() => useMemoryForget(), { wrapper });

      await act(async () => {
        await result.current.forgetWithConfirm([MEMORY_ID_1]);
      });

      // toast.success was called exactly once, and the second arg (if
      // any) must not carry actionLabel or onAction. Forget is
      // irreversible by design — any future refactor that adds an undo
      // affordance here should fail this test loudly.
      expect(toastSuccess).toHaveBeenCalledTimes(1);
      const [, opts] = toastSuccess.mock.calls[0];
      if (opts !== undefined) {
        expect(opts).not.toHaveProperty("actionLabel");
        expect(opts).not.toHaveProperty("onAction");
      }
    });
  });
});
