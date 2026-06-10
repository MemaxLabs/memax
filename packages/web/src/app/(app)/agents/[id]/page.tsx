"use client";

/**
 * /agents/<slug> — plan 24 P1-2 single-agent detail route.
 *
 * Renders `<AgentDetail>` (always-expanded `<AgentCard>`) under a
 * route header that includes a back chevron to /agents. Shares the
 * `useAgentListController` hook with /agents and Settings, so
 * navigation between the list and the detail reuses the same query
 * cache (no second fetch).
 */

import { use } from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useLocale } from "@/i18n";
import { AgentDetail } from "@/components/features/agent-list/agent-detail";
import { PageShell } from "@/components/layout";

export default function AgentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { t } = useLocale();
  return (
    <PageShell>
      <header className="mb-6">
        <Link
          href="/agents"
          className="inline-flex items-center gap-1 text-[13px] text-fg-3 hover:text-fg-2 transition-colors"
        >
          <ArrowLeft className="h-3 w-3" />
          {t.agentConfigs.backToList}
        </Link>
      </header>
      <AgentDetail agentSlug={id} />
    </PageShell>
  );
}
