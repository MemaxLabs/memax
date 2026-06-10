// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryClient as QCType,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ConnectedAgentWithStats } from "memax-sdk";

// ── Mocks ─────────────────────────────────────────────────────────────────

const disconnectMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    agents: {
      disconnect: (...args: unknown[]) => disconnectMock(...args),
    },
  }),
}));

// Minimal i18n mock — useDisconnectAgent only reads t.toast.updateFailed
// from useUpdateAgent's neighbour hook signature; useDisconnectAgent
// itself no longer imports useLocale. Provide a shape the shared file
// can safely destructure.
vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      toast: {
        updateFailed: "updateFailed",
      },
    },
  }),
  useInterpolate: () => (template: string, vars: Record<string, unknown>) =>
    template.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? "")),
}));

// eslint-disable-next-line import/first
import { useDisconnectAgent } from "./use-connected-agents";

// ── Test fixtures ─────────────────────────────────────────────────────────

function makeAgent(slug: string): ConnectedAgentWithStats {
  return {
    agent_name: slug,
    display_name: slug,
    icon: "",
    key_count: 0,
    config_count: 0,
    memory_count: 0,
    last_active_at: null,
  } as unknown as ConnectedAgentWithStats;
}

function seedAgentsCache(qc: QCType, agents: ConnectedAgentWithStats[]) {
  qc.setQueryData<ConnectedAgentWithStats[]>(["connected-agents"], agents);
}

function makeWrapper(qc: QCType) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function freshClient(): QCType {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

// ── Tests ─────────────────────────────────────────────────────────────────

beforeEach(() => {
  disconnectMock.mockReset();
});

describe("useDisconnectAgent", () => {
  it("reports success with counts on happy path", async () => {
    const qc = freshClient();
    seedAgentsCache(qc, [makeAgent("claude-code"), makeAgent("cursor")]);

    disconnectMock.mockResolvedValueOnce({
      disconnected: true,
      keys_revoked: 3,
      configs_tombstoned: 12,
      skipped: [],
    });

    const { result } = renderHook(() => useDisconnectAgent(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.disconnectAgent("claude-code");
    });

    expect(ok).toBe(true);
    expect(result.current.feedback).toEqual({
      kind: "success",
      slug: "claude-code",
      keysRevoked: 3,
      configsTombstoned: 12,
    });
    // Optimistic removal committed.
    const cached = qc.getQueryData<ConnectedAgentWithStats[]>([
      "connected-agents",
    ]);
    expect(cached?.map((a) => a.agent_name)).toEqual(["cursor"]);
  });

  it("treats not_found as idempotent success without rollback", async () => {
    const qc = freshClient();
    seedAgentsCache(qc, [makeAgent("claude-code")]);

    disconnectMock.mockResolvedValueOnce({
      disconnected: false,
      keys_revoked: 0,
      configs_tombstoned: 0,
      skipped: [{ id: "claude-code", reason: "not_found" }],
    });

    const { result } = renderHook(() => useDisconnectAgent(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.disconnectAgent("claude-code");
    });

    expect(ok).toBe(true);
    expect(result.current.feedback?.kind).toBe("not_found");
    // Optimistic removal must STAY committed — server says the
    // target state is already reached, no rollback.
    const cached = qc.getQueryData<ConnectedAgentWithStats[]>([
      "connected-agents",
    ]);
    expect(cached?.map((a) => a.agent_name)).toEqual([]);
  });

  it("rolls back on cascade_failed and reports error", async () => {
    const qc = freshClient();
    seedAgentsCache(qc, [makeAgent("claude-code"), makeAgent("cursor")]);

    disconnectMock.mockResolvedValueOnce({
      disconnected: false,
      keys_revoked: 0,
      configs_tombstoned: 0,
      skipped: [{ id: "claude-code", reason: "cascade_failed" }],
    });

    const { result } = renderHook(() => useDisconnectAgent(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.disconnectAgent("claude-code");
    });

    expect(ok).toBe(false);
    expect(result.current.feedback?.kind).toBe("error");
    // Snapshot restored — claude-code is back in the list.
    const cached = qc.getQueryData<ConnectedAgentWithStats[]>([
      "connected-agents",
    ]);
    expect(cached?.map((a) => a.agent_name)).toEqual(["claude-code", "cursor"]);
  });

  it("rolls back on generic network error", async () => {
    const qc = freshClient();
    seedAgentsCache(qc, [makeAgent("claude-code")]);

    disconnectMock.mockRejectedValueOnce(new Error("network exploded"));

    const { result } = renderHook(() => useDisconnectAgent(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.disconnectAgent("claude-code");
    });

    expect(ok).toBe(false);
    expect(result.current.feedback?.kind).toBe("error");
    const cached = qc.getQueryData<ConnectedAgentWithStats[]>([
      "connected-agents",
    ]);
    expect(cached?.map((a) => a.agent_name)).toEqual(["claude-code"]);
  });

  it("clearFeedback resets the feedback state", async () => {
    const qc = freshClient();
    seedAgentsCache(qc, [makeAgent("claude-code")]);

    disconnectMock.mockResolvedValueOnce({
      disconnected: true,
      keys_revoked: 0,
      configs_tombstoned: 0,
      skipped: [],
    });

    const { result } = renderHook(() => useDisconnectAgent(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.disconnectAgent("claude-code");
    });
    expect(result.current.feedback?.kind).toBe("success");

    act(() => {
      result.current.clearFeedback();
    });
    expect(result.current.feedback).toBeNull();
  });
});
