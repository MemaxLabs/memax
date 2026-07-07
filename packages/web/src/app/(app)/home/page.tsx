"use client";

/**
 * `/home` — the app's neutral entry resolver.
 *
 * Every "take me to the app" flow funnels here (login, OAuth callback,
 * brand link, error-page home buttons, invite/share landings). This
 * page owns ONE decision — activated users land on `/brain`, everyone
 * else lands on the personal memories overview — and makes it before
 * any destination chrome paints.
 *
 * History: `/home` used to server-redirect to `/brain`, and `/brain`
 * then client-redirected non-activated users to
 * `/h/personal/memories`. That two-hop chain painted the brain
 * surface (centered bar, conversations panel, Brain rail tab) for
 * ~300–600ms before flicking to memories — the "bar renders then
 * flicks into memory view" defect. `/home` is intentionally NOT a
 * brain-view route anymore (see route-helpers.ts): while resolving,
 * the shell renders neutral chrome — no bar, no active rail tab, no
 * secondary panel — so there is no wrong surface to flash.
 *
 * Resolution order:
 *   1. Local landing-surface hint (written by useOnboardingActivation
 *      whenever the server signal resolves) — instant, no network.
 *   2. No hint (first entry on this browser): wait for the activation
 *      query, then route. The shell's neutral state stands in; this
 *      only happens once per browser.
 *
 * Why router.replace: `/home` must never land in browser history —
 * back from the destination should leave the app, not bounce through
 * the resolver.
 */

import { useLayoutEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { useOnboardingActivation } from "@/hooks/use-onboarding-activation";
import {
  getLandingSurfaceHint,
  landingPathFor,
  setLandingSurfaceHint,
} from "@/lib/landing-surface";

export default function HomePage() {
  const router = useRouter();
  const { isActivated, isLoading } = useOnboardingActivation();
  const resolvedRef = useRef(false);

  useLayoutEffect(() => {
    if (resolvedRef.current) return;
    const hint = getLandingSurfaceHint();
    if (hint) {
      resolvedRef.current = true;
      router.replace(landingPathFor(hint));
      return;
    }
    if (isLoading) return;
    resolvedRef.current = true;
    const surface = isActivated ? "brain" : "memories";
    // Persist here as well as in the hook: the redirect unmounts this
    // page before the hook's passive effect gets a chance to run.
    setLandingSurfaceHint(surface);
    router.replace(landingPathFor(surface));
  }, [isActivated, isLoading, router]);

  // Neutral by design: the app shell around this page shows the rail
  // with no active tab and no bar. Painting a destination skeleton
  // here would just recreate the flick this page exists to remove.
  return null;
}
