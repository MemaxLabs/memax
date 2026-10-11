"use client";

import { createContext, use, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuth, type User } from "@/lib/auth";
import { getMemaxClient } from "@/lib/memax-client";
import { demoSource } from "@/lib/v2/data/demo-source";
import { createSdkSource } from "@/lib/v2/data/sdk-source";
import type { LedgerDataSource } from "@/lib/v2/data/source";
import {
  SPACE_ON_V1_OVERVIEW,
  type SpaceOverview,
  type SpaceSummary,
  type Viewer,
} from "@/lib/v2/data/types";
import { webSessionOf } from "@/lib/v2/web-session";

/**
 * Which source the frame reads (decided once, in (app)/layout.tsx):
 * - `sdk`: memax.v2, for a browser with a session.
 * - `demo`: the handoff dataset, for dev fixtures and Playwright.
 */
export type DataMode = "sdk" | "demo";

const SourceContext = createContext<LedgerDataSource>(demoSource);

function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const letters =
    words.length >= 2
      ? `${[...words[0]][0]}${[...words[words.length - 1]][0]}`
      : [...(words[0] ?? "?")].slice(0, 2).join("");
  return letters.toUpperCase();
}

function viewerFromUser(user: User): Viewer {
  const name = user.display_name || user.name || user.email;
  return {
    id: user.id,
    initials: initials(name),
    name,
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  };
}

export function LedgerDataProvider({
  mode,
  children,
}: {
  mode: DataMode;
  children: ReactNode;
}) {
  const { user, session } = useAuth();
  const source = useMemo(
    () =>
      mode === "demo"
        ? demoSource
        : createSdkSource({
            client: getMemaxClient(),
            viewer: user ? viewerFromUser(user) : null,
            // D15: whether this session was issued to the web app, as the
            // web app's server read it from the session's token.
            webSession: () => webSessionOf(session),
          }),
    [mode, user, session],
  );
  return <SourceContext value={source}>{children}</SourceContext>;
}

export function useSource(): LedgerDataSource {
  return use(SourceContext);
}

const keys = {
  spaces: (kind: string) => ["v2", kind, "spaces"] as const,
  overview: (kind: string, slug: string) =>
    ["v2", kind, "spaces", slug, "overview"] as const,
};

export const ledgerQueryKeys = keys;

export function useViewer(): Viewer | null {
  return useSource().viewer;
}

export function useSpaces() {
  const source = useSource();
  return useQuery({
    queryKey: keys.spaces(source.kind),
    queryFn: ({ signal }) => source.spaces(signal),
    initialData: source.peek?.spaces(),
    staleTime: 60_000,
  });
}

/** The overview the frame shows, as it reads it: the data, a failure and a retry. */
export interface OverviewRead {
  data: SpaceOverview | undefined;
  isError: boolean;
  refetch: () => unknown;
}

/**
 * A space's overview: read from the source for a space on V2, and known
 * without reading for one still on V1 (SPACE_ON_V1_OVERVIEW), which has
 * no V2 record to read. Keyed by the space alone, so once a space
 * switches, the query that never ran reads its first V2 overview.
 */
export function useOverview(space: SpaceSummary | undefined): OverviewRead {
  const source = useSource();
  const onV1 = space?.onV2 === false;
  const query = useQuery({
    queryKey: keys.overview(source.kind, space?.slug ?? ""),
    queryFn: ({ signal }) => source.overview(space!, signal),
    enabled: space !== undefined && !onV1,
    initialData: space && !onV1 ? source.peek?.overview(space.slug) : undefined,
    staleTime: 30_000,
  });
  if (onV1) return ON_V1_READ;
  return { data: query.data, isError: query.isError, refetch: query.refetch };
}

const ON_V1_READ: OverviewRead = {
  data: SPACE_ON_V1_OVERVIEW,
  isError: false,
  refetch: () => undefined,
};
