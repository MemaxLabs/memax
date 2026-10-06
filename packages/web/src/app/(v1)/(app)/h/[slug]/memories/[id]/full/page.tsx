"use client";

/**
 * `/h/<slug>/memories/<id>/full` — the panel's soft escape to the
 * full-page memory view.
 *
 * Why this route exists: while the intercepted panel is open at
 * `/h/<slug>/memories/<id>`, there is no soft navigation that swaps
 * the SAME URL over to the canonical page — pushing it again is a
 * no-op and Next offers no client-side interception bypass, which is
 * why "open full page" used to hard-navigate (window.location) and
 * every back from it was a full document load. `(.)memories/[id]`
 * matches exactly one segment, so this child route is NOT intercepted:
 * pushing it renders the full view inline, client-side, and back from
 * here soft-returns to wherever the user was.
 *
 * Renders the identical component tree as the canonical
 * memories/[id] route — one section structure, two URLs, no drift.
 */

import { use } from "react";
import { MemoryDetail } from "@/components/features/memory-detail/memory-detail";
import { V2HubRoute } from "@/components/shell-v2/v2-hub-route";

interface PageProps {
  params: Promise<{ slug: string; id: string }>;
}

export default function HubMemoryDetailFullPage({ params }: PageProps) {
  const { slug, id } = use(params);
  return (
    <V2HubRoute slug={slug}>
      <MemoryDetail variant="route" memoryId={id} />
    </V2HubRoute>
  );
}
