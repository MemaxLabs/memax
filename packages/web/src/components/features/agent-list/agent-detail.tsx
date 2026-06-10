"use client";

/**
 * `<AgentDetail>` — single-agent detail surface for `/agents/<slug>`.
 *
 * Renders the same `<AgentCard>` Settings shows, but always-expanded
 * (`forceExpanded`) and without the click-to-toggle behavior. Pulls
 * the agent slice out of `useAgentListController` so the data
 * fetching is shared with the list view (one network round-trip per
 * page; the list and the detail page reuse cache).
 *
 * Loading states:
 *   - Controller still loading → render the same DataCardSkeleton
 *     the list shows so the chrome doesn't flash empty.
 *   - Agent not found in the connected list → show a "not found"
 *     message with a link back to /agents. (Direct navigation to a
 *     stale or wrong slug is the only way this fires; a stale link
 *     after a disconnect lands here.)
 *
 * A back chevron in the page header lives in the route page, not
 * here — keeps this component reusable in non-page contexts (e.g.
 * a future detail-in-modal surface).
 */

import Link from "next/link";
import { ArrowLeft, Bot } from "lucide-react";
import { ContentError, DataCardSkeleton } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { AgentCard } from "@/components/features/agent-card/agent-card";
import { useAgentListController } from "./use-agent-list-controller";

interface AgentDetailProps {
  agentSlug: string;
}

export function AgentDetail({ agentSlug }: AgentDetailProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const ctrl = useAgentListController();

  if (ctrl.isLoading) {
    return (
      <div className="py-3">
        <DataCardSkeleton rows={3} />
      </div>
    );
  }

  // Codex p1 M1: when the underlying fetch failed, agent is undefined
  // because the data never landed — distinct from "fetch succeeded but
  // this slug isn't in the list." Show a real retry surface for the
  // first; the friendly not-found message is only correct when the
  // data DID land.
  if (ctrl.hasPrimaryError) {
    return (
      <ContentError
        plain
        message={t.states.error.network}
        retryLabel={t.states.error.retry}
        onRetry={ctrl.refetchAll}
      />
    );
  }

  const agent = ctrl.agents?.find((a) => a.agent_name === agentSlug);

  if (!agent) {
    return (
      <div
        className="rounded-surface px-5 py-8 text-center animate-content-ready"
        style={{
          border: "1px solid var(--border)",
          background: "var(--card)",
        }}
      >
        <Bot className="mx-auto mb-3 h-8 w-8 text-fg-4" />
        <p className="mb-1 text-[14px] font-medium text-fg-2">
          {t.agentConfigs.notFoundTitle}
        </p>
        <p className="mb-4 text-[12px] text-fg-3">
          {t.agentConfigs.notFoundHint}
        </p>
        <Link
          href="/agents"
          className="inline-flex items-center gap-1 text-[13px] text-fg-2 hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-3 w-3" />
          {t.agentConfigs.backToList}
        </Link>
      </div>
    );
  }

  const agentKeys = ctrl.keysByAgent.get(agent.agent_name) ?? [];
  const revokeFeedback =
    ctrl.revokeFeedbackRaw &&
    agentKeys.some((k) => k.id === ctrl.revokeFeedbackRaw?.id)
      ? ctrl.revokeFeedbackRaw
      : null;

  return (
    <AgentCard
      agent={agent}
      configs={ctrl.configsByAgent.get(agent.agent_name) ?? []}
      keys={agentKeys}
      hubNames={ctrl.hubNames}
      extractionCounts={ctrl.extractionCounts}
      forceExpanded
      onForgetConfigs={ctrl.onForgetConfigs}
      forgetFeedback={ctrl.forgetFeedback}
      clearForgetFeedback={ctrl.clearForgetFeedback}
      onRevokeKey={ctrl.onRevokeKey}
      revokeFeedback={revokeFeedback}
      clearRevokeFeedback={ctrl.clearRevokeFeedback}
      onUpdateAgent={ctrl.onUpdateAgent}
      onDisconnect={ctrl.onDisconnect}
      disconnectFeedback={
        ctrl.disconnectFeedback?.slug === agent.agent_name
          ? ctrl.disconnectFeedback
          : null
      }
      clearDisconnectFeedback={ctrl.clearDisconnectFeedback}
      t={t}
      interpolate={interpolate}
    />
  );
}
