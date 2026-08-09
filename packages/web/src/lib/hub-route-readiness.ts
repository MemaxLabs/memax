"use client";

import { queryClient } from "@/lib/query-client";
import { getHubBoardQueryOptions } from "@/hooks/use-board";
import { getHubSummaryQueryOptions } from "@/hooks/use-hub-management";
import { getMemoriesInfiniteQueryOptions } from "@/hooks/use-memories";
import { getRecentMemoriesInfiniteQueryOptions } from "@/hooks/use-recent-memories";
import { getTopicsQueryOptions } from "@/hooks/use-topics";

// `/home` used to have its own warm contract when it rendered the v1
// brain landing; it is now the neutral entry resolver (renders no hub
// data). `/memories` warms the hub home; `/pulse` warms the board
// surface for surface-preserving hub switches (switching hubs while
// on pulse lands on the target hub's pulse).
export type HubLandingRoute = "/memories" | "/pulse";

type HubRouteWarmTask = (hubId: string) => Promise<unknown>;

const HUB_ROUTE_READINESS_CONTRACTS: Record<
  HubLandingRoute,
  HubRouteWarmTask[]
> = {
  "/memories": [
    (hubId) => queryClient.ensureQueryData(getHubSummaryQueryOptions(hubId)),
    (hubId) => queryClient.ensureQueryData(getTopicsQueryOptions(hubId)),
    (hubId) =>
      queryClient.fetchInfiniteQuery(
        getMemoriesInfiniteQueryOptions({
          hubId,
          sort: "recent",
        }),
      ),
    (hubId) =>
      queryClient.fetchInfiniteQuery(
        getRecentMemoriesInfiniteQueryOptions({
          hubId,
          window: "7d",
          actor: "all",
          expanded: false,
        }),
      ),
  ],
  // The pulse surface reads the system board (cards + slots) and the
  // hub summary (header identity). Custom boards fetch per-board after
  // the boards list resolves — warming the system board covers the
  // above-the-fold paint.
  "/pulse": [
    (hubId) => queryClient.ensureQueryData(getHubSummaryQueryOptions(hubId)),
    (hubId) => queryClient.ensureQueryData(getHubBoardQueryOptions(hubId)),
  ],
};

export async function warmHubLandingRoute(
  route: HubLandingRoute,
  hubId: string,
) {
  const tasks = HUB_ROUTE_READINESS_CONTRACTS[route].map((task) => task(hubId));
  await Promise.allSettled(tasks);
}
