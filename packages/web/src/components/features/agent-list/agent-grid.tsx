"use client";

/**
 * `<AgentGrid>` — top-level grid for the `/agents` route (plan 28 §B-5).
 *
 * Sibling to `<AgentList>` — Settings keeps the existing list+chrome
 * pattern, `/agents` uses this grid. Codex review (gpt-5.5) flagged
 * that overloading `<AgentList>` with grid/compact/connect-card/empty
 * props heads toward "mode soup"; sharing data plumbing via
 * `useAgentListController` keeps both surfaces in lockstep without a
 * single component owning every shape.
 *
 * Renders:
 *   - phase=loading: `<ConnectAgentTile>` first + N grid skeletons
 *   - phase=error: an inline error tile with retry; the connect tile
 *     stays available so the operator can still reach the install
 *     instructions even if the agent fetch is broken
 *   - phase=loaded: `<ConnectAgentTile>` first + one
 *     `<AgentGridCard>` per agent
 *   - phase=empty (no agents): `<ConnectAgentTile>` solo + a hint
 *     line above ("No agents yet — connect your first below.")
 *
 * Grid breakpoints: 1 col mobile, 2 col sm, 3 col lg — same shape as
 * the memory-card grid so the visual rhythm matches.
 */

import { AlertTriangle } from "lucide-react";
import { useLocale } from "@/i18n";
import { AgentGridCard } from "@/components/features/agent-card/agent-grid-card";
import { ConnectAgentTile } from "@/components/features/agent-card/connect-agent-tile";
import { AgentGridCardSkeletonList } from "@/components/features/agent-card/agent-grid-card-skeleton";
import { useAgentListController } from "./use-agent-list-controller";

export function AgentGrid() {
  const { t } = useLocale();
  const ctrl = useAgentListController();
  const hasAgents = !!ctrl.agents && ctrl.agents.length > 0;

  if (ctrl.isLoading) {
    return (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <ConnectAgentTile />
        <AgentGridCardSkeletonList count={5} />
      </div>
    );
  }

  if (ctrl.hasPrimaryError) {
    return (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <ConnectAgentTile />
        <ErrorTile
          title={t.states.error.network}
          retryLabel={t.states.error.retry}
          onRetry={ctrl.refetchAll}
        />
      </div>
    );
  }

  if (!hasAgents) {
    return (
      <div className="space-y-3">
        <p className="text-[13px] text-fg-3">{t.agentConfigs.gridEmptyHint}</p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <ConnectAgentTile />
        </div>
      </div>
    );
  }

  return (
    // animate-content-ready wraps the loaded grid so the skeleton →
    // loaded swap fades in instead of popping (design system: skeleton
    // exits, content enters via animate-content-ready).
    //
    // The server synthesizes a "memax" entry into `ctrl.agents` as the
    // always-on platform actor (postgres_agent_sync.go: ListConnected
    // AgentsWithStats prepends it), so this component just renders
    // through AgentGridCard for every row — including Memax — instead
    // of injecting a parallel UI affordance. AgentGridCard special-
    // cases the link target for memax (→ /brain).
    <div className="grid grid-cols-1 gap-3 animate-content-ready sm:grid-cols-2 lg:grid-cols-3">
      <ConnectAgentTile />
      {ctrl.agents?.map((agent) => (
        <AgentGridCard key={agent.agent_name} agent={agent} />
      ))}
    </div>
  );
}

function ErrorTile({
  title,
  retryLabel,
  onRetry,
}: {
  title: string;
  retryLabel: string;
  onRetry: () => void;
}) {
  return (
    <div
      className="flex h-full min-h-35 flex-col items-center justify-center gap-2 rounded-surface border border-border/50 bg-surface-1/40 p-4 text-center"
      role="status"
    >
      <AlertTriangle className="h-5 w-5 text-destructive" />
      <p className="text-[13px] text-fg-2">{title}</p>
      {/* Retry: visible focus ring + 44×44 hit target via padding/min
          dimensions so keyboard + touch reach the standard. */}
      <button
        type="button"
        onClick={onRetry}
        className="inline-flex min-h-11 items-center justify-center rounded-chrome px-4 py-2 text-[12px] font-medium text-fg-1 transition-colors hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40 cursor-pointer"
      >
        {retryLabel}
      </button>
    </div>
  );
}
