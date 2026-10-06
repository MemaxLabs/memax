"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { SpaceSummary } from "@/lib/v2/data/types";
import type { TargetView } from "@/lib/v2/data/targets";
import { useSource } from "./data";
import { recordKeys } from "./records";

/**
 * TanStack Query over the source's Brief, targets and Today
 * (lib/v2/data/brief.ts, targets.ts, today.ts). Every key sits under
 * the space's, so a command that changes what compiles refreshes the
 * overview and the status line with it. The demo's data comes from
 * `peek*`, so the first render (and every screenshot) has it.
 */

export const compileKeys = {
  brief: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "brief"] as const,
  versions: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "brief", "versions"] as const,
  targets: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "targets"] as const,
  preview: (kind: string, slug: string, id: string) =>
    [...recordKeys.space(kind, slug), "targets", id, "preview"] as const,
  drift: (kind: string, slug: string, id: string) =>
    [...recordKeys.space(kind, slug), "targets", id, "drift"] as const,
  today: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "today"] as const,
};

/** While a file compiles, look again this often until it settles. */
const COMPILING_POLL_MS = 1500;

const compiling = (targets: readonly TargetView[] | undefined) =>
  Boolean(targets?.some((t) => t.syncState === "compiling"));

export function useBrief(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.brief(source.kind, space.slug),
    queryFn: ({ signal }) => source.brief.get({ space, signal }),
    initialData: source.brief.peek?.(space.slug),
    staleTime: 30_000,
  });
}

export function useBriefVersions(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.versions(source.kind, space.slug),
    queryFn: ({ signal }) => source.brief.versions({ space, signal }),
    initialData: source.brief.peekVersions?.(space.slug),
    staleTime: 30_000,
  });
}

export function useTargets(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.targets(source.kind, space.slug),
    queryFn: ({ signal }) => source.targets.list({ space, signal }),
    initialData: source.targets.peekList?.(space.slug),
    staleTime: 15_000,
    refetchInterval: (query) =>
      compiling(query.state.data) ? COMPILING_POLL_MS : false,
  });
}

export function useTargetPreview(
  space: SpaceSummary,
  target: TargetView | undefined,
) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.preview(source.kind, space.slug, target?.id ?? ""),
    queryFn: ({ signal }) =>
      source.targets.preview({ space, target: target!, signal }),
    enabled: target !== undefined,
    initialData: target
      ? source.targets.peekPreview?.(space.slug, target.id)
      : undefined,
    staleTime: 15_000,
    refetchInterval: (query) =>
      query.state.data?.target.syncState === "compiling"
        ? COMPILING_POLL_MS
        : false,
  });
}

export function useDrift(space: SpaceSummary, target: TargetView | undefined) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.drift(source.kind, space.slug, target?.id ?? ""),
    queryFn: ({ signal }) =>
      source.targets.drift({ space, target: target!, signal }),
    // Only a drifted target has hand edits to read.
    enabled: target !== undefined && target.openDrift > 0,
    initialData: target
      ? source.targets.peekDrift?.(space.slug, target.id)
      : undefined,
    staleTime: 15_000,
  });
}

export function useToday(space: SpaceSummary) {
  const source = useSource();
  return useQuery({
    queryKey: compileKeys.today(source.kind, space.slug),
    queryFn: ({ signal }) => source.today.get({ space, signal }),
    initialData: source.today.peek?.(space.slug),
    staleTime: 30_000,
  });
}

/**
 * After a command that changes what compiles (a compile, a setting, a
 * resolved hand edit, a new Brief): put the target the server returned
 * in the list at once, then refresh everything the space shows.
 */
export function useAfterCompile(space: SpaceSummary) {
  const source = useSource();
  const queryClient = useQueryClient();
  return useCallback(
    (changed?: TargetView) => {
      if (changed) {
        queryClient.setQueryData<TargetView[]>(
          compileKeys.targets(source.kind, space.slug),
          (list) => list?.map((t) => (t.id === changed.id ? changed : t)),
        );
      }
      void queryClient.invalidateQueries({
        queryKey: recordKeys.space(source.kind, space.slug),
      });
    },
    [queryClient, source.kind, space.slug],
  );
}
