import type { Ref } from "react";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import type { Autonomy } from "../lib/types";
import { StateMark } from "../memory/state-mark";
import { Segmented } from "../primitives/segmented";
import { AgentStamp } from "../provenance/agent-stamp";

export interface AgentRowProps {
  agent: string;
  /** The connection's name, when it differs from the registry's ("Codex (laptop)"). */
  name?: string;
  /** Controlled. Read never writes; Propose sends writes to Review; Write keeps them, still receipted. */
  autonomy: Autonomy;
  /** Without it the autonomy control can't be changed. */
  onAutonomyChange?: (autonomy: Autonomy) => void;
  /**
   * Levels that can't be chosen, each with why ("API keys propose at most").
   * They are greyed, skipped by the arrow keys, and keep the reason as their tooltip.
   */
  unavailable?: Partial<Record<Autonomy, string>>;
  /** Reads this week; `null` when they aren't recorded yet (shown as "—"). */
  reads: number | null;
  /** Writes this week; `null` when they aren't recorded yet. */
  writes: number | null;
  lastSeen?: string;
  /** The compiled file, if any ("CLAUDE.md"). */
  target?: string;
  /** What to say when there is no target. Defaults to "MCP only". */
  noTarget?: string;
  paused?: boolean;
  /** Makes the stamp and name a link to the agent's page. */
  href?: string;
  className?: string;
  ref?: Ref<HTMLDivElement>;
}

/**
 * An agent connection: who it is, how much it may do, and what it did this
 * week. Put rows under an `AgentListHead` inside a `.mx-panel`. Each value
 * carries its column name for assistive technology, since the head is visual.
 */
export function AgentRow({
  agent,
  name,
  autonomy,
  onAutonomyChange,
  unavailable,
  reads,
  writes,
  lastSeen,
  target,
  noTarget,
  paused,
  href,
  className,
  ref,
}: AgentRowProps) {
  const { strings, agents, formatNumber, Link } = useLedger();
  const a = strings.autonomy;
  const t = strings.agentList;
  const who = resolveAgent(agents, { agent, name }, strings.agent.fallback);
  const level = (value: Autonomy, label: string, hint: string) => ({
    value,
    label,
    hint,
    disabled: unavailable?.[value] !== undefined,
    disabledReason: unavailable?.[value],
  });
  const count = (label: string, n: number | null) => (
    <span className="mx-agent-num">
      <span className="mx-sr">{label} </span>
      {n === null ? (
        <>
          <span aria-hidden="true">—</span>
          <span className="mx-sr">{t.notRecorded}</span>
        </>
      ) : (
        formatNumber(n)
      )}
    </span>
  );
  const stamp = <AgentStamp agent={agent} name={name} showName surface />;
  return (
    <div
      ref={ref}
      className={cx("mx-agent", paused && "is-paused", className)}
      role="group"
      aria-label={who.name}
    >
      <div className="mx-agent-id">
        {href !== undefined ? (
          <Link className="mx-agent-link" href={href}>
            {stamp}
          </Link>
        ) : (
          stamp
        )}
      </div>
      <Segmented<Autonomy>
        size="sm"
        label={format(a.label, { name: who.name })}
        value={autonomy}
        onChange={onAutonomyChange}
        options={[
          level("read", a.read, a.readHint),
          level("propose", a.propose, a.proposeHint),
          level("write", a.write, a.writeHint),
        ]}
      />
      {count(t.readsLabel, reads)}
      {count(t.writesLabel, writes)}
      <span className="mx-agent-seen">
        {paused ? (
          <StateMark state="off" label={t.paused} />
        ) : lastSeen ? (
          <>
            <span className="mx-sr">{t.lastSeen} </span>
            {lastSeen}
          </>
        ) : (
          <>
            <span aria-hidden="true">—</span>
            <span className="mx-sr">{t.notSeen}</span>
          </>
        )}
      </span>
      <span className="mx-agent-target">
        {target ? (
          <>
            <span className="mx-sr">{t.target} </span>
            <code className="mx-code">{target}</code>
          </>
        ) : (
          <span className="mx-meta">{noTarget ?? t.mcpOnly}</span>
        )}
      </span>
    </div>
  );
}

export interface AgentListHeadProps {
  className?: string;
}

/** The column names over a list of `AgentRow`s. Visual only; each row labels its own values. */
export function AgentListHead({ className }: AgentListHeadProps) {
  const { strings } = useLedger();
  const t = strings.agentList;
  return (
    <div className={cx("mx-agent-head", className)} aria-hidden="true">
      <span>{t.agent}</span>
      <span>{t.autonomy}</span>
      <span className="is-num">{t.reads}</span>
      <span className="is-num">{t.writes}</span>
      <span>{t.lastSeen}</span>
      <span>{t.target}</span>
    </div>
  );
}
