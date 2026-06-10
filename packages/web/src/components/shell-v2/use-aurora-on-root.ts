"use client";

/**
 * useAuroraOnRoot — sets `--page-aurora-gradient` on the document
 * element so the v2 shell page background can render the same aurora as
 * the hub-header banner. One source of truth via `@memaxlabs/ui` aurora
 * tokens.
 *
 * Behavior:
 *   - `mode = "signature"` → SIGNATURE_AURORA (brand-color radial)
 *   - `mode = "time"`      → AURORA_GRADIENTS[bucket(now)], re-evaluated
 *                            every 15 minutes so a long-running session
 *                            rolls forward through the time-of-day
 *                            buckets without a refresh.
 *   - `mode = "none"`      → "none" (solid `var(--background)`)
 *
 * Cleanup: on unmount or mode change, clears the property so v1 routes
 * (which don't render under v2 chrome) don't inherit a stale gradient.
 *
 * Reduced-motion / reduced-transparency aren't checked here because the
 * aurora is a static gradient — no animation. CSS-side @media handles
 * the visual fallback.
 */

import { useEffect, useState } from "react";
import {
  AURORA_GRADIENTS,
  SIGNATURE_AURORA,
  getHubHeaderTimeBucket,
  type HubHeaderAuroraMode,
} from "@memaxlabs/ui";

const REFRESH_INTERVAL_MS = 15 * 60_000;

export function useAuroraOnRoot(mode: HubHeaderAuroraMode | undefined) {
  const [bucket, setBucket] = useState(() =>
    typeof window === "undefined"
      ? "morning"
      : getHubHeaderTimeBucket(new Date()),
  );

  // Roll the time-bucket forward periodically when mode is `time`.
  // Effect resubscribes when mode changes.
  useEffect(() => {
    if (mode !== "time") return;
    const handle = setInterval(() => {
      setBucket(getHubHeaderTimeBucket(new Date()));
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(handle);
  }, [mode]);

  useEffect(() => {
    const root = document.documentElement;
    let value: string;
    if (!mode || mode === "none") {
      value = "none";
    } else if (mode === "signature") {
      value = SIGNATURE_AURORA;
    } else {
      // mode === "time"
      value = AURORA_GRADIENTS[bucket as keyof typeof AURORA_GRADIENTS];
    }
    root.style.setProperty("--page-aurora-gradient", value);
    // NO cleanup that removes the var on unmount. Previously this
    // hook cleared `--page-aurora-gradient` on every cleanup,
    // including the dev-mode double-fire (setup → cleanup → setup)
    // and any transient ShellLayoutV2 remount triggered by a parent
    // re-render. Between cleanup and the next setup, the var falls
    // back to the `none` default in globals.css and the aurora
    // briefly disappears across the whole shell. The "explicitly
    // clear so v1 routes don't inherit" rationale doesn't apply
    // post plan-24 phase 4b (v1 was retired), so leaving the value
    // sticky is safe — the next ShellLayoutV2 mount will overwrite
    // it with the correct mode anyway.
  }, [mode, bucket]);
}
