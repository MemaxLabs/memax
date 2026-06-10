"use client";

/**
 * `<AgentGridCard>` — vertical, compact card for the `/agents` grid
 * (plan 28 §B-3).
 *
 * Why a separate component (not a `compact` prop on `<AgentCard>`):
 * codex review (gpt-5.5) flagged that the existing AgentCard header
 * is row-shaped and horizontally dense (`px-5`, flex-row, status
 * label, metrics wrap, chevron). A 3-col grid needs a real
 * vertically-stacked card layout, not just "hide the body". Forcing
 * AgentCard into both shapes via props heads toward "mode soup". So
 * this is a sibling component sharing the same data inputs.
 *
 * Visual rhythm matches `<MemoryCardLayout>`:
 *   - `glass-card` material (matches memory-card v2).
 *   - rounded-surface (20px) — same as memory cards.
 *   - vertical stack: avatar+status (header) → name → metrics → footer.
 *
 * Real `<Link>` (not `<button>` + router.push) so:
 *   - middle-click / cmd-click opens in a new tab,
 *   - browser-native focus + hover semantics work,
 *   - the keyboard contract is "tab + enter", not a custom button
 *     handler.
 *
 * `needs_reconnect` surfaces as a destructive-tinted footer badge so
 * the grid doesn't bury an actionable lifecycle state. Click still
 * drills into `/agents/<slug>` where the existing reconnect dialog
 * lives — the badge is signal, not action, here.
 */

import Link from "next/link";
import { Activity, AlertTriangle } from "lucide-react";
import type { ConnectedAgentWithStats } from "memax-sdk";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { getAgentStatus } from "@/components/features/settings/shared/agent-status";
import { StatusDot, statusLabelKey } from "./agent-status-dot";

interface AgentGridCardProps {
  agent: ConnectedAgentWithStats;
}

export function AgentGridCard({ agent }: AgentGridCardProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const identity = resolveAgentIdentity(agent.agent_name, agent);
  const AgentIcon = identity.icon;
  const status = getAgentStatus(agent);
  const lastObservedAt = agent.last_observed_at ?? agent.last_active_at;
  const displayName = agent.needs_reconnect
    ? t.agentConfigs.unnamedAgent
    : identity.displayName;
  // Memax is the always-on platform actor — route its tile to /brain
  // (where the Memax agent's chat surface lives) instead of the
  // /agents/<slug> detail page. The detail page still resolves
  // for direct-URL access (the store synthesizes the row), but the
  // grid affordance points at the place a user actually wants to
  // visit when they tap "Memax".
  const isMemax = agent.agent_name === "memax";
  const href = isMemax
    ? "/brain"
    : `/agents/${encodeURIComponent(agent.agent_name)}`;

  return (
    <Link
      href={href}
      // glass-card material matches `<MemoryCardLayout>` — keeps
      // `/agents` and the memory feed visually consistent.
      // `min-h-[140px]` matches `<ConnectAgentTile>` and
      // `<AgentGridCardSkeleton>` so the skeleton → loaded
      // transition has zero layout shift even on agents with no
      // metrics.
      // Hover: `bg-surface-1` neutral elevation — design system
      // explicitly forbids `shadow-md`-style generic shadows on
      // cards (memax-design-system.md "What NOT to Generate"),
      // and memory-card uses neutral surface affordance, not
      // shadow lift.
      className="glass-card flex h-full min-h-35 flex-col rounded-surface p-4 transition-colors hover:bg-surface-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fg-3/40"
    >
      {/* Header: avatar + status pill (top-right) */}
      <div className="mb-3 flex items-start gap-3">
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-chrome"
          style={{ background: `oklch(from ${identity.color} l c h / 0.12)` }}
        >
          {agent.icon ? (
            <span className="text-[16px]">{agent.icon}</span>
          ) : (
            <AgentIcon
              className="h-4.5 w-4.5"
              style={{ color: identity.color }}
              strokeWidth={1.8}
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[14px] font-medium text-fg-1">
            {displayName}
          </p>
          <div className="mt-0.5 flex items-center gap-1.5">
            <StatusDot status={status} />
            <span className="truncate text-[11px] text-fg-3">
              {t.agentConfigs[statusLabelKey(status)]}
            </span>
          </div>
        </div>
      </div>

      {/* Metrics: just the two most useful. Keys + configs live in the
          detail page; surfacing them here would crowd the card. */}
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-3">
        {agent.memory_count > 0 && (
          <span className="flex items-center gap-1 tabular-nums">
            <Activity className="h-2.5 w-2.5" />
            {interpolate(t.agentConfigs.memoriesPushed, {
              n: String(agent.memory_count),
            })}
          </span>
        )}
        {lastObservedAt && (
          <span className="tabular-nums">
            {formatAge(lastObservedAt, t, interpolate)}
          </span>
        )}
      </div>

      {/* Footer fills remaining height so all cards align. Reconnect
          badge appears only when needs_reconnect; otherwise the spacer
          keeps the bottom edge consistent across the grid. */}
      <div className="mt-auto flex items-end">
        {agent.needs_reconnect ? (
          <span
            className="flex items-center gap-1 rounded-chrome px-2 py-1 text-[11px] font-medium text-destructive"
            style={{
              backgroundColor: "oklch(from var(--destructive) l c h / 0.08)",
            }}
          >
            <AlertTriangle className="h-3 w-3" />
            {t.agentConfigs.reconnect}
          </span>
        ) : (
          <span className="text-[11px] text-fg-4">
            {t.agentConfigs.viewDetails}
          </span>
        )}
      </div>
    </Link>
  );
}
