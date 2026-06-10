"use client";

/**
 * v2 memory detail route — `/h/<slug>/memories/<id>`.
 *
 * Mounts `<MemoryDetail variant="route">` (plan 21 phase 1 unified
 * component) under v2 chrome via `<V2HubRoute>`. The same component
 * renders in `variant="modal"` mode for transient inspect from the
 * grid, so click vs. permalink-open share one section structure.
 */

import { use } from "react";
import { MemoryDetail } from "@/components/features/memory-detail/memory-detail";
import { V2HubRoute } from "@/components/shell-v2/v2-hub-route";

interface PageProps {
  params: Promise<{ slug: string; id: string }>;
}

export default function HubMemoryDetailPage({ params }: PageProps) {
  const { slug, id } = use(params);
  return (
    <V2HubRoute slug={slug}>
      <MemoryDetail variant="route" memoryId={id} />
    </V2HubRoute>
  );
}
