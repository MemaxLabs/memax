import type { ReactNode, Ref } from "react";
import { Icon } from "../brand/icon";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import type { HandoffStatus } from "../lib/types";
import { StateMark } from "../memory/state-mark";
import { AgentStamp } from "../provenance/agent-stamp";

export interface HandoffSlipProps {
  /** "H-0093". */
  id?: string;
  /** The agent handing off. */
  from: string;
  /** The agent receiving it. */
  to: string;
  /** Where the receiving agent runs ("cloud task"). */
  toSurface?: string;
  title: ReactNode;
  done?: ReactNode[];
  next?: ReactNode[];
  /** Neutral questions. One that needs a person becomes a DecisionGate. */
  questions?: ReactNode[];
  /** Which memories go with it ("7 memories"). */
  carries?: string;
  /** `drafted` (off mark), `sent` (with the receiving agent), `accepted` (green disc). */
  status: HandoffStatus;
  time?: string;
  actions?: ReactNode;
  /** The title's heading level; the section labels sit one below it. */
  headingLevel?: 2 | 3 | 4;
  className?: string;
  ref?: Ref<HTMLElement>;
}

/**
 * A transmittal slip: what one agent session hands the next. Done, next, the
 * open questions, and exactly which memories go with it. The dashed top edge
 * is its signature.
 */
export function HandoffSlip({
  id,
  from,
  to,
  toSurface,
  title,
  done = [],
  next = [],
  questions = [],
  carries,
  status,
  time,
  actions,
  headingLevel = 3,
  className,
  ref,
}: HandoffSlipProps) {
  const { strings, agents } = useLedger();
  const h = strings.handoff;
  const receiver = resolveAgent(agents, { agent: to }, strings.agent.fallback);
  const Title = `h${headingLevel}` as const;
  const Label = `h${(headingLevel + 1) as 3 | 4 | 5}` as const;
  const list = (label: string, items: ReactNode[], variant?: string) =>
    items.length ? (
      <div className={cx("mx-slip-sec", variant)}>
        <Label className="mx-slip-label">{label}</Label>
        <ul className="mx-slip-list">
          {items.map((item, i) => (
            <li key={i}>{item}</li>
          ))}
        </ul>
      </div>
    ) : null;
  let mark: ReactNode;
  if (status === "accepted")
    mark = <StateMark state="kept" label={h.accepted} />;
  else if (status === "sent") {
    mark = (
      <StateMark
        state="working"
        label={format(h.withAgent, { name: receiver.name })}
      />
    );
  } else mark = <StateMark state="off" label={h.drafted} />;
  return (
    <article ref={ref} className={cx("mx-slip", className)}>
      <header className="mx-slip-head">
        <div className="mx-slip-route">
          <AgentStamp agent={from} showName />
          <Icon name="arrow-right" size={16} className="mx-slip-arrow" />
          <span className="mx-sr">{h.to}</span>
          <AgentStamp agent={to} showName />
          {toSurface ? (
            <span className="mx-slip-surface">{toSurface}</span>
          ) : null}
        </div>
        <span className={cx("mx-slip-status", `is-${status}`)}>{mark}</span>
      </header>
      <Title className="mx-slip-title">{title}</Title>
      <div className="mx-slip-grid">
        {list(h.done, done)}
        {list(h.next, next)}
      </div>
      {list(h.questions, questions, "is-questions")}
      <footer className="mx-slip-foot">
        <span className="mx-receipt">
          {[id, carries, time].filter(Boolean).join(" · ")}
        </span>
        {actions ? <div className="mx-slip-actions">{actions}</div> : null}
      </footer>
    </article>
  );
}
