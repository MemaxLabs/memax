"use client";

import { useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import type { ImportSummary, ImportView } from "@/lib/v2/data/imports";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { v2Spaces } from "@/lib/v2/onboarding/routes";
import { useSource, useSpaces } from "../(app)/_lib/data";
import { recordKeys } from "../(app)/_lib/records";
import { agentKeys } from "../(app)/_places/agents/queries";

/**
 * The first session's queries, keyed under the space's records (so a
 * Keep anywhere refreshes them) and polled while something is still
 * happening on the person's machine: until init's import arrives, and
 * until its judge and conflict check are done. Polling stands in for the
 * server push of plan §5.14.
 */

export const importKeys = {
  list: (kind: string, slug: string) =>
    [...recordKeys.space(kind, slug), "imports"] as const,
  view: (kind: string, slug: string, id: string) =>
    [...recordKeys.space(kind, slug), "imports", id] as const,
};

/** While init may still be running on the machine. */
export const WAIT_FOR_INIT_MS = 3000;
/** While the judge and the conflict check read an import. */
export const WAIT_FOR_CHECK_MS = 2000;

/** The space's imports, newest first; polled while `poll` (until one arrives). */
export function useImports(
  space: SpaceSummary | null | undefined,
  { poll = false }: { poll?: boolean } = {},
) {
  const source = useSource();
  return useQuery<ImportSummary[]>({
    queryKey: importKeys.list(source.kind, space?.slug ?? ""),
    queryFn: ({ signal }) =>
      source.imports.list({ space: space!, limit: 10, signal }),
    enabled: Boolean(space),
    initialData: space ? source.imports.peekList?.(space.slug) : undefined,
    refetchInterval: (q) =>
      poll && (q.state.data?.length ?? 0) === 0 ? WAIT_FOR_INIT_MS : false,
  });
}

/** One import in full, polled until it's ready. */
export function useImportView(
  space: SpaceSummary | null | undefined,
  id: string | null | undefined,
) {
  const source = useSource();
  return useQuery<ImportView | null>({
    queryKey: importKeys.view(source.kind, space?.slug ?? "", id ?? ""),
    queryFn: ({ signal }) =>
      source.imports.get({ space: space!, id: id!, signal }),
    enabled: Boolean(space && id),
    initialData:
      space && id ? source.imports.peekView?.(space.slug, id) : undefined,
    refetchInterval: (q) =>
      q.state.data && !q.state.data.progress.ready ? WAIT_FOR_CHECK_MS : false,
  });
}

/**
 * The space a setup screen is about: `?space=` when it names one of the
 * person's spaces, else their first project space on the V2 record, else
 * Personal there; null for someone with no space on the V2 record yet
 * (a first run). `poll` keeps looking while there is none, so FirstRun
 * notices the space init creates.
 */
export function useSetupSpace({ poll = false }: { poll?: boolean } = {}) {
  const params = useSearchParams();
  const asked = params?.get("space") ?? null;
  const spaces = useSpaces();
  const source = useSource();
  const list = spaces.data;
  const space = useMemo(() => {
    if (!list) return undefined;
    if (asked) {
      const named = list.find((s) => s.slug === asked || s.id === asked);
      if (named) return named;
    }
    const onV2 = v2Spaces(list);
    return (
      onV2.find((s) => s.kind === "project") ??
      onV2.find((s) => s.kind === "personal") ??
      null
    );
  }, [list, asked]);
  // Poll the spaces list until init creates (or moves) a space.
  useQuery({
    queryKey: ["v2", source.kind, "spaces", "first-run-poll"],
    queryFn: async () => {
      await spaces.refetch();
      return Date.now();
    },
    enabled: poll && space === null,
    refetchInterval: WAIT_FOR_INIT_MS,
  });
  return {
    space,
    spaces: list ?? [],
    loading: spaces.isPending,
    failed: spaces.isError && !list,
    retry: () => void spaces.refetch(),
  };
}

/** The space's agent connections, when there is a space (the Agents place's query). */
export function useAgentsOf(space: SpaceSummary | null | undefined) {
  const source = useSource();
  return useQuery({
    queryKey: agentKeys.space(source.kind, space?.slug ?? ""),
    queryFn: ({ signal }) => source.spaceAgents(space!, signal),
    enabled: Boolean(space),
    initialData: space ? source.agentsPeek?.spaceAgents(space.slug) : undefined,
    staleTime: 30_000,
  });
}

/** The import a screen is about: `?import=` when given, else the space's newest. */
export function useSetupImport(
  space: SpaceSummary | null | undefined,
  { poll = false }: { poll?: boolean } = {},
) {
  const params = useSearchParams();
  const asked = params?.get("import") ?? null;
  const imports = useImports(space, { poll });
  const id = asked ?? imports.data?.[0]?.id ?? null;
  const view = useImportView(space, id);
  return { id, imports, view };
}
