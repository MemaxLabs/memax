"use client";

/**
 * `/home` — the app's neutral entry resolver.
 *
 * Every "take me to the app" flow funnels here (login, OAuth callback,
 * brand link, error-page home buttons, invite/share landings). It
 * routes to ONE destination: the active hub's memories overview.
 *
 * History: this page used to branch — activated users landed on
 * `/brain` (Ask memax), everyone else on memories — with a
 * localStorage landing-surface hint to make the branch instant.
 * Founder call (2026-08): memories IS the home surface for everyone;
 * Ask memax is a tab you choose, not a place entry links teleport you
 * to. The branch, the hint machinery (`lib/landing-surface.ts`), and
 * the activation-query wait were all removed with that decision — this
 * page now waits only for auth (which the destination needs anyway)
 * so it can route to the ACTIVE hub instead of guessing personal and
 * silently reverting a team-hub selection.
 *
 * Why router.replace: `/home` must never land in browser history —
 * back from the destination should leave the app, not bounce through
 * the resolver.
 *
 * A person who sees V2 (the V2 UI flag, `ui` from /auth/me) starts in
 * V2 instead, at its landing (lib/ui-gate.ts V2_LANDING_PATH). The proxy
 * sends a browser carrying the `memax_ui` hint there before this page
 * loads; this covers the load whose /auth/me just set the hint, say
 * after an operator turned the flag on while the person was in V1. A
 * full navigation, since V2 is the other root layout.
 */

import { useLayoutEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { useAuth, useActiveHub } from "@/lib/auth";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildMemoriesPath } from "@/lib/route-helpers";
import { V2_LANDING_PATH } from "@/lib/ui-gate";

export default function HomePage() {
  const router = useRouter();
  const { user, loading, ui } = useAuth();
  const { activeHub } = useActiveHub();
  const resolvedRef = useRef(false);

  useLayoutEffect(() => {
    if (resolvedRef.current) return;
    // Wait for /auth/me: the hubs list is what lets us resolve the
    // active hub's slug, so routing earlier would always guess
    // personal. If auth FAILS the app shell bounces to /login on its
    // own — this page only handles the signed-in path.
    if (loading || !user) return;
    resolvedRef.current = true;
    if (ui === "v2") {
      window.location.replace(V2_LANDING_PATH);
      return;
    }
    const slug = activeHub?.hub
      ? hubRouteSlug(activeHub.hub, user.id)
      : "personal";
    router.replace(buildMemoriesPath(slug));
  }, [loading, user, ui, activeHub, router]);

  // Neutral by design: the app shell around this page shows the rail
  // with no active tab and no bar. Painting a destination skeleton
  // here would just recreate the flick this page exists to remove.
  return null;
}
