"use client";

import { useEffect, useSyncExternalStore } from "react";
import { useParams, usePathname } from "next/navigation";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { isPlaceRoute, type PlaceRoute } from "@/lib/v2/places";
import { useSpaces } from "./data";

/** Where the frame is: the space in the URL (if any), the place, settings. */
export function useFrameLocation(): {
  slug: string | undefined;
  route: PlaceRoute | undefined;
  inSettings: boolean;
} {
  const params = useParams<{ space?: string }>();
  const pathname = usePathname() ?? "";
  const segments = pathname.split("/").filter(Boolean);
  const slug = params?.space;
  return {
    slug,
    route: slug && isPlaceRoute(segments[1]) ? segments[1] : undefined,
    inSettings: segments[0] === "settings",
  };
}

// The last space visited, so pages outside a space (settings) keep the
// rail on it. Per device, like the theme.
const LAST_SPACE_KEY = "memax.v2.space";
const listeners = new Set<() => void>();

function readLastSpace(): string | null {
  try {
    return localStorage.getItem(LAST_SPACE_KEY);
  } catch {
    return null;
  }
}

function writeLastSpace(slug: string) {
  try {
    if (localStorage.getItem(LAST_SPACE_KEY) === slug) return;
    localStorage.setItem(LAST_SPACE_KEY, slug);
    for (const listener of listeners) listener();
  } catch {
    // Private mode: the rail falls back to the first space.
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export type CurrentSpace =
  | { status: "loading" }
  | { status: "error"; retry: () => void }
  | { status: "missing"; slug: string; fallback: SpaceSummary | undefined }
  | { status: "ready"; space: SpaceSummary };

/**
 * The space the frame shows: the URL's, or (outside a space) the last
 * one visited, or the first. `missing` when the URL names a space the
 * person can't open.
 */
export function useCurrentSpace(): {
  current: CurrentSpace;
  spaces: SpaceSummary[];
} {
  const { slug } = useFrameLocation();
  const query = useSpaces();
  const last = useSyncExternalStore(subscribe, readLastSpace, () => null);
  const spaces = query.data ?? [];
  const fromUrl = slug ? spaces.find((s) => s.slug === slug) : undefined;

  useEffect(() => {
    if (fromUrl) writeLastSpace(fromUrl.slug);
  }, [fromUrl]);

  if (!query.data) {
    return {
      spaces,
      current: query.isError
        ? { status: "error", retry: () => void query.refetch() }
        : { status: "loading" },
    };
  }
  if (slug) {
    return {
      spaces,
      current: fromUrl
        ? { status: "ready", space: fromUrl }
        : { status: "missing", slug, fallback: spaces[0] },
    };
  }
  const remembered = spaces.find((s) => s.slug === last) ?? spaces[0];
  return {
    spaces,
    current: remembered
      ? { status: "ready", space: remembered }
      : { status: "missing", slug: "", fallback: undefined },
  };
}
