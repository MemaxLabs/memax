"use client";

import { useMemo, useRef } from "react";
import { useRouter } from "next/navigation";
import type { WebUi } from "memax-sdk";
import { getMemaxClient } from "@/lib/memax-client";
import { createSdkSource } from "@/lib/v2/data/sdk-source";
import {
  afterSignIn,
  landingHref,
  resolveLanding,
  type LandingSource,
} from "@/lib/v2/onboarding/routes";

/**
 * Where a person goes once signed in (lib/v2/onboarding/routes.ts), given
 * the web UI the API says they see: with the V2 UI flag, `next` when the
 * sign-in came from a page, else their landing (FirstRun for someone new,
 * ReviewImport for an import still in progress, else a space's Today);
 * without it, `next` only when it opens without the flag, else V1's home.
 * Reads the person's own record over the SDK; a read that fails lands on
 * FirstRun, which finds their spaces itself.
 */
export function useLandingRedirect(source?: LandingSource) {
  const router = useRouter();
  const started = useRef(false);
  return useMemo(
    () => ({
      go(next: string | null, ui: WebUi | null | undefined) {
        if (started.current) return;
        started.current = true;
        const after = afterSignIn(next, ui);
        if (after.kind !== "landing") {
          router.replace(after.href);
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
