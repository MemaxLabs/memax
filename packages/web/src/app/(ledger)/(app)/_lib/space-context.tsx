"use client";

import { createContext, use } from "react";
import type { SpaceOverview, SpaceSummary } from "@/lib/v2/data/types";

/** The space the frame is showing, for the pages inside it. */
export interface SpaceView {
  space: SpaceSummary;
  /** Undefined while it loads. */
  overview: SpaceOverview | undefined;
  overviewFailed: boolean;
  retryOverview: () => void;
}

export const SpaceViewContext = createContext<SpaceView | null>(null);

export function useSpaceView(): SpaceView {
  const view = use(SpaceViewContext);
  if (!view) throw new Error("useSpaceView needs the app frame");
  return view;
}
