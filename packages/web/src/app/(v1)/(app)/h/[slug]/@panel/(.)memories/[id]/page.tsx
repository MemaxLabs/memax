"use client";

/**
 * Intercepted memory detail — renders the memory as a right-side
 * slide-in panel when the user soft-navigates to a memory URL from
 * ANY route inside /h/<slug>/ (memories list, topic detail, …).
 * Hard navigations (refresh, deep link, share) bypass this intercept
 * and fall through to the canonical memories/[id]/page.tsx full route.
 *
 * Behavior:
 *   - Mobile: full-screen takeover (panel collapses to w-full on
 *     narrow viewports).
 *   - Desktop: ~50vw right-anchored slide-in; underlying view stays
 *     visible behind a faint backdrop.
 *   - Close: router.back() pops the intercept; URL returns to the
 *     previous route with the panel sliding out. Browser back too.
 *
 * URL semantics: while the panel is open the address bar shows
 * /h/<slug>/memories/<id>, so share / refresh land on the full route.
 * "Open full page" inside the panel triggers a slide-out animation
 * then hard-navigates so the canonical route renders inline.
 */

import { use } from "react";
import { useRouter } from "next/navigation";
import { MemoryDetail } from "@/components/features/memory-detail/memory-detail";

interface PageProps {
  params: Promise<{ slug: string; id: string }>;
}

export default function InterceptedMemoryDetailPanel({ params }: PageProps) {
  const { slug, id } = use(params);
  const router = useRouter();
  return (
    <MemoryDetail
      variant="panel"
      memoryId={id}
      onClose={() => router.back()}
      fullPagePath={`/h/${slug}/memories/${id}`}
    />
  );
}
