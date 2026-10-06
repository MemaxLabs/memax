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
  /** Controlled. Read never writes; Propose sends writes to Review; Write keeps them, still receipted. */
  autonomy: Autonomy;
  /** Without it the autonomy control can't be changed. */
  onAutonomyChange?: (autonomy: Autonomy) => void;
  /** Reads this week. */
  reads: number;
  /** Writes this week. */
  writes: number;
  lastSeen?: string;
  /** The compiled file, if any ("CLAUDE.md"). */
  target?: string;
  paused?: boolean;
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
  autonomy,
  onAutonomyChange,
  reads,
  writes,
  lastSeen,
  target,
  paused,
  className,
  ref,
}: AgentRowProps) {
  const { strings, agents, formatNumber } = useLedger();
  const a = strings.autonomy;
  const t = strings.agentList;
  const who = resolveAgent(agents, { agent }, strings.agent.fallback);
  return (
    <div
      ref={ref}
      className={cx("mx-agent", paused && "is-paused", className)}
      role="group"
      aria-label={who.name}
    >
      <div className="mx-agent-id">
        <AgentStamp agent={agent} showName surface />
      </div>
      <Segmented<Autonomy>
        size="sm"
        label={format(a.label, { name: who.name })}
        value={autonomy}
        onChange={onAutonomyChange}
        options={[
          { value: "read", label: a.read, hint: a.readHint },
          { value: "propose", label: a.propose, hint: a.proposeHint },
          { value: "write", label: a.write, hint: a.writeHint },
        ]}
      />
      <span className="mx-agent-num">
        <span className="mx-sr">{t.readsLabel} </span>
        {formatNumber(reads)}
      </span>
      <span className="mx-agent-num">
        <span className="mx-sr">{t.writesLabel} </span>
        {formatNumber(writes)}
      </span>
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
          <span className="mx-meta">{t.mcpOnly}</span>
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
