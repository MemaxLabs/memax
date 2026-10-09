"use client";

import { AgentRow, type Autonomy } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { unavailableLevels } from "@/lib/v2/agents/autonomy";
import { agoText, unavailableText } from "@/lib/v2/agents/copy";
import { autonomyIn, type AgentConnectionView } from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { usePlace } from "../place";
import { useAutonomy } from "./use-agent-commands";
import styles from "./agents.module.css";

export function agentHref(space: string, agent: string): string {
  return `/${encodeURIComponent(space)}/agents/${encodeURIComponent(agent)}`;
}

/** The greyed levels' tooltips for an agent at `current`. */
export function useUnavailable(
  agent: AgentConnectionView,
  current: Autonomy,
  space: SpaceSummary,
): Partial<Record<Autonomy, string>> {
  const { t } = useLocale();
  const reasons = unavailableLevels(agent, current, space.role);
  return Object.fromEntries(
    Object.entries(reasons).map(([level, reason]) => [
      level,
      unavailableText(t.ledger.agents, reason, agent.name),
    ]),
  );
}

/** One row of the Agents table, with its autonomy control live. */
export function AgentRowControl({
  agent,
  space,
}: {
  agent: AgentConnectionView;
  space: SpaceSummary;
}) {
  const { t, locale } = useLocale();
  const { now, timeZone } = usePlace();
  const here = autonomyIn(agent, space.slug);
  const { shown, raising, choose } = useAutonomy({ agent, space });
  const unavailable = useUnavailable(agent, here?.autonomy ?? "read", space);
  const copy = t.ledger.agents;
  return (
    <AgentRow
      agent={agent.agent}
      name={agent.name}
      autonomy={shown}
      onAutonomyChange={choose}
      unavailable={unavailable}
      reads={here?.reads7d ?? null}
      writes={here?.writes7d ?? 0}
      lastSeen={
        agent.lastSeenAt
          ? agoText(copy, agent.lastSeenAt, now, timeZone, locale)
          : undefined
      }
      target={agent.target?.path}
      noTarget={agent.target === undefined ? copy.noTarget : undefined}
      paused={agent.state === "paused"}
      href={agentHref(space.slug, agent.id)}
      // A raise waits on the server: no spinner, just the cursor.
      className={raising ? styles.raising : undefined}
    />
  );
}
