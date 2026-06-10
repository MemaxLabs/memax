"use client";

/**
 * Legacy `/memories/<id>` route — kept around because middleware
 * intentionally does NOT redirect this path (hub identity is encoded
 * in the memory's data, not the URL — see middleware.ts header).
 * Old shared/email links land here. Renders the same
 * `<MemoryDetail variant="route">` as the v2-canonical
 * `/h/<slug>/memories/<id>` route.
 */

import { use } from "react";
import { MemoryDetail } from "@/components/features/memory-detail/memory-detail";

export default function MemoryDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <MemoryDetail variant="route" memoryId={id} />;
}
