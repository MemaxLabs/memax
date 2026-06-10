"use client";

/**
 * `<AgentList>` — presentational list of `<AgentCard>` rows. Plan 24
 * §4.2.
 *
 * Two consumers:
 *   - Settings (`<AgentConfigsSection>`) renders this with
 *     `expandable={true}` + `showConnectNew={true}` — current
 *     behavior, click expands inline detail in place.
 *   - `/agents` route renders this with `expandable={false}` — click
 *     on a card navigates to `/agents/<slug>` instead of expanding.
 *
 * Data + grouping comes from `useAgentListController` so settings and
 * the route stay in lockstep on loading semantics + retry behavior.
 *
 * `showConnectNew` controls whether the "add more agents" disclosure
 * (which mounts `<ConnectAgentsSection>`) renders below the list.
 */

import { Bot, ChevronDown, ChevronRight, Copy, Check } from "lucide-react";
import { useState } from "react";
import { DataCardSkeleton, DataSectionCard } from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { ConnectAgentsSection } from "@/components/features/connect-agents-section";
import { AgentCard } from "@/components/features/agent-card/agent-card";
import { useAgentListController } from "./use-agent-list-controller";

const SETUP_CMD =
  "npx memax-cli@latest login && npx memax-cli@latest setup --all";
const EMPTY_AGENT_NAMES = [
  "Claude Code",
  "Cursor",
  "Windsurf",
  "Copilot",
  "Codex",
  "Gemini",
].join(", ");

export interface AgentListProps {
  /**
   * When false (default), `<AgentCard>` renders in non-expandable
   * mode and the consumer's `onAgentClick` fires on tap. When true,
   * the card toggles expanded inline (Settings disclosure pattern).
   */
  expandable?: boolean;
  /**
   * When true, mounts the "add more agents" disclosure below the
   * list (renders `<ConnectAgentsSection>` when expanded). Settings
   * sets this to true; the `/agents` route omits it.
   */
  showConnectNew?: boolean;
  /**
   * When true, per-agent key lists are hidden inside AgentCard. Used
   * when API key management lives in a separate dedicated surface.
   */
  hideKeyManagement?: boolean;
  /**
   * Click handler when `expandable=false`. Receives the agent slug.
   * Owned by the consumer (the route page passes a `router.push`
   * call) so this component stays router-free and unit-testable
   * without next/navigation context.
   */
  onAgentClick?: (slug: string) => void;
}

export function AgentList({
  expandable = false,
  showConnectNew = true,
  hideKeyManagement,
  onAgentClick,
}: AgentListProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const ctrl = useAgentListController();
  const hasAgents = ctrl.agents && ctrl.agents.length > 0;

  if (!ctrl.isLoading && !ctrl.hasPrimaryError && !hasAgents) {
    return (
      <section className="mb-8">
        <div
          className="rounded-surface px-5 py-5 text-center animate-content-ready"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <Bot className="mx-auto mb-3 h-8 w-8 text-fg-4" />
          <p className="mb-1 text-[14px] font-medium text-fg-2">
            {t.agentConfigs.emptyTitle}
          </p>
          <p className="mb-4 text-[12px] text-fg-3">{EMPTY_AGENT_NAMES}</p>
          <div className="mx-auto max-w-xs">
            <CopyBlock text={SETUP_CMD} mono />
          </div>
        </div>
      </section>
    );
  }

  return (
    <>
      <section className="mb-8">
        <DataSectionCard
          icon={Bot}
          label={t.agentConfigs.title}
          phase={
            ctrl.isLoading
              ? "loading"
              : ctrl.hasPrimaryError
                ? "error"
                : hasAgents
                  ? "loaded"
                  : "empty"
          }
          skeleton={<DataCardSkeleton rows={3} />}
          errorCopy={{
            title: t.states.error.network,
            retryLabel: t.states.error.retry,
            onRetry: ctrl.refetchAll,
          }}
          emptyCopy={{
            title: t.agentConfigs.emptyTitle,
            hint: t.agentConfigs.emptyDesc,
          }}
        >
          <div className="space-y-3 py-3 animate-content-ready">
            {ctrl.agents?.map((agent) => {
              const agentKeys = ctrl.keysByAgent.get(agent.agent_name) ?? [];
              const revokeFeedback =
                ctrl.revokeFeedbackRaw &&
                agentKeys.some((k) => k.id === ctrl.revokeFeedbackRaw?.id)
                  ? ctrl.revokeFeedbackRaw
                  : null;
              return (
                <div key={agent.agent_name} className="px-3">
                  <AgentCard
                    agent={agent}
                    configs={ctrl.configsByAgent.get(agent.agent_name) ?? []}
                    keys={agentKeys}
                    hideKeyManagement={hideKeyManagement}
                    hubNames={ctrl.hubNames}
                    extractionCounts={ctrl.extractionCounts}
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
                    onCardClick={
                      expandable
                        ? undefined
                        : onAgentClick
                          ? () => onAgentClick(agent.agent_name)
                          : undefined
                    }
                    t={t}
                    interpolate={interpolate}
                  />
                </div>
              );
            })}
          </div>
        </DataSectionCard>
      </section>

      {showConnectNew && hasAgents && (
        <SetupDisclosure label={t.agentConfigs.addMore} />
      )}
    </>
  );
}

/* ── Local CopyBlock — empty-state command renderer ── */

function CopyBlock({ text, mono }: { text: string; mono?: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={async () => {
        await navigator.clipboard.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }}
      className="relative w-full text-left rounded-lg bg-foreground/3 hover:bg-foreground/5 transition-colors cursor-pointer px-3.5 py-3 group/copy"
    >
      <p
        className={`text-[13px] text-fg-2 leading-relaxed pr-8 ${mono ? "font-mono" : ""}`}
      >
        {text}
      </p>
      <span className="absolute top-2.5 right-3">
        {copied ? (
          <Check className="h-3 w-3 text-emerald-500/70" />
        ) : (
          <Copy className="h-3 w-3 text-fg-3" />
        )}
      </span>
    </button>
  );
}

/* ── Local SetupDisclosure — "add more agents" footer ── */

function SetupDisclosure({ label }: { label: string }) {
  const [open, setOpen] = useState(false);

  return (
    <section className="mb-8">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 text-[13px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors px-1 mb-2"
      >
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        {label}
      </button>
      {open && <ConnectAgentsSection />}
    </section>
  );
}
