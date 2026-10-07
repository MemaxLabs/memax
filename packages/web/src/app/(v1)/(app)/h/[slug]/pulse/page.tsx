"use client";

/**
 * /h/<slug>/pulse — the pulse board's own surface (plan 25 P4),
 * hub-scoped.
 *
 * The board was already embedded on the memories page
 * (`<BoardSection>`); this is the same board for the hub in the URL,
 * mounted full-page with the pieces the embedded variant deliberately
 * leaves out — the custom-board composer and the 最近 receipts strip
 * that absorbed the retired inbox.
 *
 * Route choice: this used to be a STATIC `/pulse` path that read hub
 * identity from active-hub state. That was wrong — a board belongs to
 * exactly ONE hub, so a user who created a board in team hub X (at
 * `/h/X/memories`) and then opened Pulse got whichever hub localStorage
 * last held, usually personal, and their board looked like it had
 * vanished. The board now takes its hub from the URL like memories
 * does, gated by `<V2HubRoute>` so nothing renders against the previous
 * active hub during the slug → `switchHub` window. `/pulse` survives as
 * a client-side forwarder to this path for old deep links (and for
 * `/inbox`, which still points at it).
 */

import { use } from "react";
import { BoardPage } from "@/components/features/board/board-view";
import { PageShell } from "@/components/layout";
import { V2HubRoute } from "@/components/shell-v2/v2-hub-route";
import { useAuth } from "@/lib/auth";
import { resolveHubFromSlug } from "@/lib/hub-from-slug";

interface PageProps {
  params: Promise<{ slug: string }>;
}

export default function HubPulsePage({ params }: PageProps) {
  const { slug } = use(params);
  const { hubs } = useAuth();
  // The URL is the hub identity on this route, so resolve the board's
  // hub from the slug rather than letting `<BoardPage>` fall back to
  // active-hub context. `<V2HubRoute>` keeps the two in sync anyway;
  // passing it explicitly means the board can never render another
  // hub's cards under this URL even if that gate regresses. `null`
  // (slug matched no hub) never reaches the child — V2HubRoute renders
  // its not-found surface instead.
  const hubId = resolveHubFromSlug({ slug, hubs });
  return (
    <V2HubRoute slug={slug}>
      <PageShell>{hubId ? <BoardPage hubId={hubId} /> : null}</PageShell>
    </V2HubRoute>
  );
}
