"use client";

/**
 * v2 memories overview route — `/h/<slug>/memories`.
 *
 * Renders the production `<TopicGrid>` — same surface v1 ships at
 * `/memories` — wrapped in `<SelectionProvider>` for batch-row
 * selection scoping. The `<V2HubRoute>` wrapper resolves the URL
 * slug to the active hub and gates children until sync completes.
 *
 * Plan-20 card-grid replaces TopicGrid's recent rows when that lands.
 */

import { use } from "react";
import { BoardSection } from "@/components/features/board/board-view";
import { TopicGrid } from "@/components/features/topic/topic-grid";
import { SelectionProvider } from "@/contexts/selection-context";
import { V2HubRoute } from "@/components/shell-v2/v2-hub-route";

interface PageProps {
  params: Promise<{ slug: string }>;
}

export default function HubMemoriesPage({ params }: PageProps) {
  const { slug } = use(params);
  return (
    <V2HubRoute slug={slug}>
      {/* Pulse board (plan 25): renders nothing until the hub's nightly
          producers have cards, then leads the page. */}
      <BoardSection />
      <SelectionProvider>
        <TopicGrid />
      </SelectionProvider>
    </V2HubRoute>
  );
}
