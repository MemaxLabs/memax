// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryClient as QCType,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { AgentConfigListResult } from "memax-sdk";

// ── Mocks ─────────────────────────────────────────────────────────────────

const batchDeleteMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    configs: {
      batchDelete: (...args: unknown[]) => batchDeleteMock(...args),
    },
  }),
}));

// eslint-disable-next-line import/first
import { useAgentConfigForget } from "./use-agent-config-forget";

// ── Test fixtures ─────────────────────────────────────────────────────────

function makeConfig(
  id: string,
  agent: string,
): AgentConfigListResult["configs"][number] {
  return {
    id,
    agent,
    file_path: `${id}.md`,
    scope: "global",
    content: `content-${id}`,
    content_hash: `hash-${id}`,
    version: 1,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  } as AgentConfigListResult["configs"][number];
}

function seedConfigsCache(
  qc: QCType,
  configs: AgentConfigListResult["configs"],
) {
  qc.setQueryData<AgentConfigListResult>(["agent-configs"], {
    configs,
    extraction_counts: {},
  });
}

function makeWrapper(qc: QCType) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function freshClient(): QCType {
  return new QueryClient({
    defaultOptions: {
      // gcTime: Infinity keeps seeded cache entries alive across the
      // mutation lifecycle so rollback assertions can read the
      // restored snapshot without an active observer. Using gcTime: 0
      // causes the cache to be dropped immediately when no hook is
      // observing it, which invalidates the rollback assertion.
      queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

// ── Tests ─────────────────────────────────────────────────────────────────

beforeEach(() => {
  batchDeleteMock.mockReset();
});

describe("useAgentConfigForget", () => {
  it("reports full success inline when every id is deleted", async () => {
    const qc = freshClient();
    seedConfigsCache(qc, [
      makeConfig("a", "claude"),
      makeConfig("b", "cursor"),
    ]);

    batchDeleteMock.mockResolvedValueOnce({ deleted: 2, skipped: [] });

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.forgetConfigs(["a", "b"]);
    });

    expect(ok).toBe(true);
    expect(result.current.feedback).toEqual({
      kind: "success",
      deleted: 2,
      failed: 0,
      attemptedIds: ["a", "b"],
    });
    // Optimistic removal + onSettled invalidate should have fired,
    // but since the mock resolves success, the list stays empty
    // until a refetch — which uses the same batchDelete mock setup
    // (no refetch triggered in this unit test because onSettled
    // invalidates and the next fetch is not mocked; the setQueryData
    // from onMutate is the load-bearing assertion).
    const cached = qc.getQueryData<AgentConfigListResult>(["agent-configs"]);
    expect(cached?.configs.map((c) => c.id)).toEqual([]);
  });

  it("reports partial success when some ids return delete_failed", async () => {
    const qc = freshClient();
    seedConfigsCache(qc, [
      makeConfig("a", "claude"),
      makeConfig("b", "cursor"),
      makeConfig("c", "codex"),
    ]);

    batchDeleteMock.mockResolvedValueOnce({
      deleted: 2,
      skipped: [{ id: "c", reason: "delete_failed" }],
    });

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.forgetConfigs(["a", "b", "c"]);
    });

    expect(ok).toBe(true);
    expect(result.current.feedback).toEqual({
      kind: "partial",
      deleted: 2,
      failed: 1,
      attemptedIds: ["a", "b", "c"],
    });
  });

  it("treats full skip as effective success when every reason is not_found", async () => {
    const qc = freshClient();
    seedConfigsCache(qc, [
      makeConfig("a", "claude"),
      makeConfig("b", "cursor"),
    ]);

    batchDeleteMock.mockResolvedValueOnce({
      deleted: 0,
      skipped: [
        { id: "a", reason: "not_found" },
        { id: "b", reason: "not_found" },
      ],
    });

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.forgetConfigs(["a", "b"]);
    });

    // not_found is "target reached" — counts as effective deleted
    // so kind is "success" and `deleted` carries the effective count.
    expect(ok).toBe(true);
    expect(result.current.feedback).toEqual({
      kind: "success",
      deleted: 2,
      failed: 0,
      attemptedIds: ["a", "b"],
    });
  });

  it("throws + rolls back + reports error on full-skip with delete_failed", async () => {
    const qc = freshClient();
    const original = [makeConfig("a", "claude"), makeConfig("b", "cursor")];
    seedConfigsCache(qc, original);

    batchDeleteMock.mockResolvedValueOnce({
      deleted: 0,
      skipped: [
        { id: "a", reason: "delete_failed" },
        { id: "b", reason: "delete_failed" },
      ],
    });

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.forgetConfigs(["a", "b"]);
    });

    expect(ok).toBe(false);
    expect(result.current.feedback).toEqual({
      kind: "error",
      deleted: 0,
      failed: 2,
      attemptedIds: ["a", "b"],
    });
    // onError restored the snapshot — both configs are back in the
    // list, no waiting for onSettled refetch.
    const cached = qc.getQueryData<AgentConfigListResult>(["agent-configs"]);
    expect(cached?.configs.map((c) => c.id)).toEqual(["a", "b"]);
  });

  it("rolls back on generic mutation failure (network, parse)", async () => {
    const qc = freshClient();
    const original = [makeConfig("a", "claude")];
    seedConfigsCache(qc, original);

    batchDeleteMock.mockRejectedValueOnce(new Error("network exploded"));

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.forgetConfigs(["a"]);
    });

    expect(ok).toBe(false);
    expect(result.current.feedback?.kind).toBe("error");
    expect(result.current.feedback?.failed).toBe(1);
    const cached = qc.getQueryData<AgentConfigListResult>(["agent-configs"]);
    expect(cached?.configs.map((c) => c.id)).toEqual(["a"]);
  });

  it("single-flight guard: second call while pending resolves false without firing batchDelete twice", async () => {
    const qc = freshClient();
    seedConfigsCache(qc, [makeConfig("a", "claude")]);

    // First call resolves on-demand so we can start a second one
    // mid-flight.
    let releaseFirst: (v: unknown) => void = () => {};
    batchDeleteMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          releaseFirst = resolve;
        }),
    );

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    let first: Promise<boolean> | undefined;
    act(() => {
      first = result.current.forgetConfigs(["a"]);
    });
    await waitFor(() => {
      expect(result.current.isPending).toBe(true);
    });

    let secondResult: boolean | undefined;
    await act(async () => {
      secondResult = await result.current.forgetConfigs(["a"]);
    });
    expect(secondResult).toBe(false);
    expect(batchDeleteMock).toHaveBeenCalledTimes(1);

    act(() => {
      releaseFirst({ deleted: 1, skipped: [] });
    });
    await act(async () => {
      await first;
    });
    expect(result.current.feedback?.kind).toBe("success");
  });

  it("clearFeedback() resets the feedback state", async () => {
    const qc = freshClient();
    seedConfigsCache(qc, [makeConfig("a", "claude")]);

    batchDeleteMock.mockResolvedValueOnce({ deleted: 1, skipped: [] });

    const { result } = renderHook(() => useAgentConfigForget(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.forgetConfigs(["a"]);
    });
    expect(result.current.feedback?.kind).toBe("success");

    act(() => {
      result.current.clearFeedback();
    });
    expect(result.current.feedback).toBeNull();
  });
});
