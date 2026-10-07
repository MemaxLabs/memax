"use client";

import { useMemo, useRef } from "react";
import { useRouter } from "next/navigation";
import { getMemaxClient } from "@/lib/memax-client";
import { createSdkSource } from "@/lib/v2/data/sdk-source";
import {
  landingHref,
  resolveLanding,
  safeNext,
  type LandingSource,
} from "@/lib/v2/onboarding/routes";

/**
 * Where a person goes once signed in (lib/v2/onboarding/routes.ts):
 * `next` when the sign-in came from a page, else their landing: FirstRun
 * for someone new, ReviewImport for an import still in progress, else a
 * space's Today. Reads the person's own record over the SDK; a read that
 * fails lands on FirstRun, which finds their spaces itself.
 */
export function useLandingRedirect(source?: LandingSource) {
  const router = useRouter();
  const started = useRef(false);
  return useMemo(
    () => ({
      go(next: string | null) {
        if (started.current) return;
        started.current = true;
        const safe = safeNext(next);
        if (safe) {
          router.replace(safe);
          return;
        }
        const from =
          source ?? createSdkSource({ client: getMemaxClient(), viewer: null });
        void resolveLanding(from)
          .catch(() => ({ kind: "first-run" }) as const)
          .then((landing) => router.replace(landingHref(landing)));
      },
    }),
    [router, source],
  );
}
