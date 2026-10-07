"use client";

/**
 * v2 topic detail route — `/h/<slug>/topics/<id>`.
 *
 * Mounts the existing production `<TopicDetail>` under v2 chrome.
 * `<V2HubRoute>` resolves slug → hub UUID and gates render until
 * the active hub catches up so TopicDetail (which reads from
 * useActiveHub() internally) sees the right hub.
 *
 * Wrapped in `<PageShell>` (plan-26 phase 1) so v1 + v2 topic-detail
 * routes share the canonical max-w-4xl + paddingTop wrapper. Without
 * this wrap the v2 path rendered TopicDetail bare against the shell
 * chrome, which left a visible padding gap vs the v1 layout that
 * always wrapped it.
 */

import { use } from "react";
import { TopicDetail } from "@/components/features/topic/topic-detail";
import { SelectionProvider } from "@/contexts/selection-context";
import { V2HubRoute } from "@/components/shell-v2/v2-hub-route";
import { PageShell } from "@/components/layout";

interface PageProps {
  params: Promise<{ slug: string; id: string }>;
}

export default function HubTopicDetailPage({ params }: PageProps) {
  const { slug, id } = use(params);
  return (
    <V2HubRoute slug={slug}>
      <PageShell>
        <SelectionProvider>
          <TopicDetail topicId={id} />
        </SelectionProvider>
      </PageShell>
    </V2HubRoute>
  );
}
