"use client";

/**
 * /pulse — forwarder to the active hub's pulse board.
 *
 * The board itself moved to `/h/<slug>/pulse` (hub identity belongs in
 * the URL — see that route's docstring for the bug this fixed). This
 * path stays mounted because it is the old deep-link target: bookmarks,
 * the retired `/inbox` + `/inbox/<id>` routes, and `/discover` all
 * redirect here.
 *
 * It has to be a CLIENT component: the active hub lives in client auth
 * state (there is no server-readable hub preference), so a server
 * `redirect()` could only ever guess `personal` — which is exactly the
 * wrong-hub bug we just removed. We render nothing until auth hydrates,
 * then `router.replace` so the forwarder never lands in history.
 */

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useActiveHub, useAuth } from "@/lib/auth";
import { hubRouteSlug } from "@/lib/hub-from-slug";
import { buildPulsePath } from "@/lib/route-helpers";

export default function PulseForwarderPage() {
  const router = useRouter();
  const { user } = useAuth();
  const { activeHub } = useActiveHub();
  const slug =
    activeHub?.hub && user ? hubRouteSlug(activeHub.hub, user.id) : null;

  useEffect(() => {
    if (!slug) return;
    router.replace(buildPulsePath(slug));
  }, [router, slug]);

  return null;
}
