// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryClient as QCType,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ApiKeyListItem } from "memax-sdk";

// ── Mocks ─────────────────────────────────────────────────────────────────

const revokeKeyMock = vi.fn();
const updateKeyMock = vi.fn();
const toastErrorMock = vi.fn();

vi.mock("@/lib/memax-client", () => ({
  getMemaxClient: () => ({
    auth: {
      revokeKey: (...args: unknown[]) => revokeKeyMock(...args),
      updateKey: (...args: unknown[]) => updateKeyMock(...args),
    },
  }),
}));

// useUpdateApiKey now shows a toast on failure via useBarToast, which
// requires BarProvider. For these unit tests we mock the toast hook
// so renderHook doesn't need the full provider tree.
vi.mock("@/hooks/use-bar-toast", () => ({
  useBarToast: () => ({
    error: toastErrorMock,
    success: vi.fn(),
    info: vi.fn(),
    dismiss: vi.fn(),
  }),
}));

vi.mock("@/i18n", () => ({
  useLocale: () => ({
    t: {
      apiKeys: { updateFailed: "Couldn't update key — retry" },
      errors: {
        action: {
          updateApiKey: "update that key",
        },
      },
    },
  }),
}));

// eslint-disable-next-line import/first
import { useRevokeApiKey, useUpdateApiKey } from "./use-api-keys";

// ── Test fixtures ─────────────────────────────────────────────────────────

function makeKey(id: string): ApiKeyListItem {
  return {
    id,
    prefix: `mk_${id}`,
    name: `key-${id}`,
    agent_name: "",
    standalone: false,
    last_used: null,
    expires_at: null,
    created_at: new Date().toISOString(),
  } as unknown as ApiKeyListItem;
}

function seedKeysCache(qc: QCType, keys: ApiKeyListItem[]) {
  qc.setQueryData<ApiKeyListItem[]>(["api-keys"], keys);
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
  revokeKeyMock.mockReset();
});

describe("useRevokeApiKey", () => {
  it("reports success on happy path", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1"), makeKey("k2")]);

    revokeKeyMock.mockResolvedValueOnce({ revoked: true, skipped: [] });

    const { result } = renderHook(() => useRevokeApiKey(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.revokeKey("k1");
    });

    expect(ok).toBe(true);
    expect(result.current.feedback).toEqual({ kind: "success", id: "k1" });
    // Optimistic removal committed.
    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.map((k) => k.id)).toEqual(["k2"]);
  });

  it("treats not_found as idempotent success (no rollback)", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1")]);

    revokeKeyMock.mockResolvedValueOnce({
      revoked: false,
      skipped: [{ id: "k1", reason: "not_found" }],
    });

    const { result } = renderHook(() => useRevokeApiKey(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.revokeKey("k1");
    });

    expect(ok).toBe(true);
    expect(result.current.feedback?.kind).toBe("not_found");
    // Optimistic removal stays — server says the key is already gone.
    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.map((k) => k.id)).toEqual([]);
  });

  it("rolls back on revoke_failed and reports error", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1"), makeKey("k2")]);

    revokeKeyMock.mockResolvedValueOnce({
      revoked: false,
      skipped: [{ id: "k1", reason: "revoke_failed" }],
    });

    const { result } = renderHook(() => useRevokeApiKey(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.revokeKey("k1");
    });

    expect(ok).toBe(false);
    expect(result.current.feedback?.kind).toBe("error");
    // Snapshot restored — k1 is back in the list.
    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.map((k) => k.id)).toEqual(["k1", "k2"]);
  });

  it("rolls back on generic network error", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1")]);

    revokeKeyMock.mockRejectedValueOnce(new Error("network down"));

    const { result } = renderHook(() => useRevokeApiKey(), {
      wrapper: makeWrapper(qc),
    });

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.revokeKey("k1");
    });

    expect(ok).toBe(false);
    expect(result.current.feedback?.kind).toBe("error");
    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.map((k) => k.id)).toEqual(["k1"]);
  });

  it("clearFeedback resets the feedback state", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1")]);

    revokeKeyMock.mockResolvedValueOnce({ revoked: true, skipped: [] });

    const { result } = renderHook(() => useRevokeApiKey(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.revokeKey("k1");
    });
    expect(result.current.feedback?.kind).toBe("success");

    act(() => {
      result.current.clearFeedback();
    });
    expect(result.current.feedback).toBeNull();
  });
});

describe("useUpdateApiKey", () => {
  beforeEach(() => {
    updateKeyMock.mockReset();
  });

  it("optimistically applies agent_name on assign", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1")]);

    updateKeyMock.mockResolvedValueOnce({
      id: "k1",
      agent_name: "hatch-agent-v4",
      standalone: false,
    });

    const { result } = renderHook(() => useUpdateApiKey(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.updateKey("k1", { agent_name: "hatch-agent-v4" });
    });

    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.[0]?.agent_name).toBe("hatch-agent-v4");
    // Invariant: assigning an agent forces standalone=false even when
    // the caller didn't send it explicitly. Prevents the contradictory
    // "assigned + standalone" state.
    expect(cached?.[0]?.standalone).toBe(false);
    expect(updateKeyMock).toHaveBeenCalledWith("k1", {
      agent_name: "hatch-agent-v4",
    });
  });

  it("optimistically toggles standalone", async () => {
    const qc = freshClient();
    seedKeysCache(qc, [makeKey("k1")]);

    updateKeyMock.mockResolvedValueOnce({
      id: "k1",
      agent_name: "",
      standalone: true,
    });

    const { result } = renderHook(() => useUpdateApiKey(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.updateKey("k1", { standalone: true });
    });

    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.[0]?.standalone).toBe(true);
  });

  it("marking standalone on a linked row clears agent_name in cache", async () => {
    // Invariant: setting standalone=true forces agent_name=''. Without
    // this optimistic rule the classifier's first-branch-wins logic
    // would keep showing the agent badge until the server round-trip.
    const qc = freshClient();
    seedKeysCache(qc, [
      {
        ...makeKey("k1"),
        agent_name: "claude-code",
        standalone: false,
      },
    ]);

    updateKeyMock.mockResolvedValueOnce({
      id: "k1",
      agent_name: "",
      standalone: true,
    });

    const { result } = renderHook(() => useUpdateApiKey(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.updateKey("k1", {
        agent_name: "",
        standalone: true,
      });
    });

    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.[0]?.agent_name).toBe("");
    expect(cached?.[0]?.standalone).toBe(true);
  });

  it("rolls back on server error", async () => {
    const qc = freshClient();
    const original = makeKey("k1");
    seedKeysCache(qc, [original]);

    updateKeyMock.mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => useUpdateApiKey(), {
      wrapper: makeWrapper(qc),
    });

    await act(async () => {
      await result.current.updateKey("k1", { agent_name: "some-agent" });
    });

    const cached = qc.getQueryData<ApiKeyListItem[]>(["api-keys"]);
    expect(cached?.[0]?.agent_name).toBe("");
    expect(cached?.[0]?.standalone).toBe(false);
  });
});
