import { beforeEach, describe, expect, it, vi } from "vitest";

const ensureQueryData = vi.fn();
const fetchInfiniteQuery = vi.fn();
const getHubSummaryQueryOptions = vi.fn((hubId: string) => ({
  queryKey: ["hub-summary", hubId],
}));
const getTopicsQueryOptions = vi.fn((hubId: string) => ({
  queryKey: ["topics", hubId],
}));
const getMemoriesInfiniteQueryOptions = vi.fn(
  (input: Record<string, unknown>) => ({
    queryKey: ["memories", input],
  }),
);
const getRecentMemoriesInfiniteQueryOptions = vi.fn(
  (input: Record<string, unknown>) => ({
    queryKey: ["recent", input],
  }),
);

vi.mock("@/lib/query-client", () => ({
  queryClient: {
    ensureQueryData,
    fetchInfiniteQuery,
  },
}));

vi.mock("@/hooks/use-hub-management", () => ({
  getHubSummaryQueryOptions,
}));

vi.mock("@/hooks/use-topics", () => ({
  getTopicsQueryOptions,
}));

vi.mock("@/hooks/use-memories", () => ({
  getMemoriesInfiniteQueryOptions,
}));

vi.mock("@/hooks/use-recent-memories", () => ({
  getRecentMemoriesInfiniteQueryOptions,
}));

describe("hub route readiness", () => {
  beforeEach(() => {
    ensureQueryData.mockReset();
    fetchInfiniteQuery.mockReset();
    getHubSummaryQueryOptions.mockClear();
    getTopicsQueryOptions.mockClear();
    getMemoriesInfiniteQueryOptions.mockClear();
    getRecentMemoriesInfiniteQueryOptions.mockClear();
    ensureQueryData.mockResolvedValue(undefined);
    fetchInfiniteQuery.mockResolvedValue(undefined);
  });

  it("warms the /memories contract with summary, topics, memories, and recent data", async () => {
    const { warmHubLandingRoute } = await import("@/lib/hub-route-readiness");

    await warmHubLandingRoute("/memories", "hub-2");

    expect(getHubSummaryQueryOptions).toHaveBeenCalledWith("hub-2");
    expect(getTopicsQueryOptions).toHaveBeenCalledWith("hub-2");
    expect(getMemoriesInfiniteQueryOptions).toHaveBeenCalledWith({
      hubId: "hub-2",
      sort: "recent",
    });
    expect(getRecentMemoriesInfiniteQueryOptions).toHaveBeenCalledWith({
      hubId: "hub-2",
      window: "7d",
      actor: "all",
      expanded: false,
    });
    expect(ensureQueryData).toHaveBeenCalledTimes(2);
    expect(fetchInfiniteQuery).toHaveBeenCalledTimes(2);
  });

  it("settles the contract even when one query fails", async () => {
    const { warmHubLandingRoute } = await import("@/lib/hub-route-readiness");
    fetchInfiniteQuery.mockRejectedValueOnce(new Error("network"));

    await expect(
      warmHubLandingRoute("/memories", "hub-3"),
    ).resolves.toBeUndefined();
  });
});
