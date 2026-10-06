"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { GateView } from "@/lib/v2/data/gates";
import type { SpaceOverview, SpaceSummary } from "@/lib/v2/data/types";
import { ledgerQueryKeys, useSource } from "./data";
import { recordKeys } from "./records";

/**
 * TanStack Query over the source's decision gates (lib/v2/data/gates.ts).
 * The keys sit under the space's, so a decision anywhere in the space
 * refreshes them, and a gate's answer refreshes the overview, Today and
 * Memories with it. The demo's come from `peekWaiting`, so the first
 * render (and every screenshot) has them.
 */

export const gateKeys = {
  waiting: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "gates", "waiting"] as const,
  one: (kind: string, slug: string, ref: string) =>
    [...recordKeys.space(kind, slug), "gates", "one", ref] as const,
};

/**
 * No `gate.changed` events yet (plan §5.12's SSE): while questions wait,
 * look again this often, so one answered or withdrawn elsewhere shows.
 */
export const GATES_POLL_MS = 30_000;

export function useWaitingGates(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: gateKeys.waiting(source.kind, space.slug),
    queryFn: ({ signal }) => source.gates.waiting({ space, signal }),
    initialData: source.gates.peekWaiting?.(space.slug),
    staleTime: 10_000,
    refetchInterval: (query) =>
      query.state.data?.length ? GATES_POLL_MS : false,
  });
}

/** One gate however it ended (a link from Activity or Today); null when the space has none by that ref. */
export function useGate(space: SpaceSummary, ref: string | null) {
  const source = useSource();
  return useQuery({
    queryKey: gateKeys.one(source.kind, space.slug, ref ?? ""),
    queryFn: ({ signal }) =>
      source.gates.get({ space, ref: ref ?? "", signal }),
    enabled: Boolean(ref),
    staleTime: 10_000,
  });
}

/**
 * After a gate ended here (answered, withdrawn, or found ended): it
 * leaves the cached list and the rail's count drops at once, then
 * everything the space shows refetches.
 */
export function useAfterGate(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    (ref: string) => {
      let removed = false;
      queryClient.setQueryData<GateView[]>(
        gateKeys.waiting(source.kind, space.slug),
        (list) => {
          if (!list?.some((g) => g.ref === ref)) return list;
          removed = true;
          return list.filter((g) => g.ref !== ref);
        },
      );
      if (removed) {
        queryClient.setQueryData<SpaceOverview>(
          ledgerQueryKeys.overview(source.kind, space.slug),
          (overview) =>
            overview && overview.gatesWaiting !== null
              ? {
                  ...overview,
                  gatesWaiting: Math.max(0, overview.gatesWaiting - 1),
                }
              : overview,
        );
      }
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
    },
    [queryClient, source.kind, space.slug],
  );
}
