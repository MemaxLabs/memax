"use client";

import { useEffect, useRef } from "react";
import {
  trackFunnelStep,
  type FunnelCounts,
  type FunnelStep,
} from "@/lib/v2/funnel";
import { useSource } from "../(app)/_lib/data";

/**
 * Sends a funnel step (lib/v2/funnel.ts) once per mount, when `ready`
 * first holds. The demo data (dev fixtures, Playwright) sends nothing.
 */
export function useFunnelStep(
  step: FunnelStep,
  ready: boolean,
  counts?: FunnelCounts,
) {
  const demo = useSource().kind === "demo";
  const sent = useRef(false);
  useEffect(() => {
    if (!ready || demo || sent.current) return;
    sent.current = true;
    trackFunnelStep(step, counts);
  }, [step, ready, demo, counts]);
}
