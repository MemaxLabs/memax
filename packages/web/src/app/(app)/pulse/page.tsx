"use client";

/**
 * /pulse — the pulse board's own surface (plan 25 P4).
 *
 * This route replaced `/inbox` as the fourth shell tab. The board was
 * already embedded on the memories page (`<BoardSection>`); this is the
 * same board for the active hub, mounted full-page with the pieces the
 * embedded variant deliberately leaves out — board tabs, the
 * custom-board composer, and the 最近 receipts strip that absorbed the
 * retired inbox.
 *
 * Route choice: a STATIC `/pulse` path rather than mirroring the
 * memories tab's active-hub path. Both tabs would otherwise resolve to
 * `/h/<slug>/memories` and the pathname-derived active-tab lookup
 * (`getShellTabForPath`) could not tell them apart. `/pulse` also gives
 * the retired `/inbox` route a stable redirect target for old email
 * deep links. Hub identity comes from the active-hub context, which is
 * how every other static-path tab (`/brain`, `/agents`) already works —
 * so no slug and no `<V2HubRoute>` gate is needed here.
 */

import { BoardPage } from "@/components/features/board/board-view";
import { PageShell } from "@/components/layout";

export default function PulsePage() {
  return (
    <PageShell>
      <BoardPage />
    </PageShell>
  );
}
