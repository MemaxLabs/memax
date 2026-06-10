"use client";

import { queryClient } from "@/lib/query-client";
import { getHubSummaryQueryOptions } from "@/hooks/use-hub-management";
import { getMemoriesInfiniteQueryOptions } from "@/hooks/use-memories";
import { getRecentMemoriesInfiniteQueryOptions } from "@/hooks/use-recent-memories";
import { getTopicsQueryOptions } from "@/hooks/use-topics";

export type HubLandingRoute = "/home" | "/memories";

type HubRouteWarmTask = (hubId: string) => Promise<unknown>;

const HUB_ROUTE_READINESS_CONTRACTS: Record<
  HubLandingRoute,
  HubRouteWarmTask[]
> = {
  "/home": [
    (hubId) => queryClient.ensureQueryData(getHubSummaryQueryOptions(hubId)),
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
};

export async function warmHubLandingRoute(
  route: HubLandingRoute,
  hubId: string,
) {
  const tasks = HUB_ROUTE_READINESS_CONTRACTS[route].map((task) => task(hubId));
  await Promise.allSettled(tasks);
}
