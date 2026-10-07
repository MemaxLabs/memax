import { Fragment, type ReactNode } from "react";
import { cx } from "../lib/cx";
import { AgentStamp } from "./agent-stamp";

export interface ReceiptProps {
  agent?: string;
  /** A person's initials. */
  person?: string;
  /** The person's or agent's name, read instead of the monogram. */
  name?: string;
  /** A lower-case past-tense verb: proposed, kept, merged, read, flagged. */
  action?: ReactNode;
  /** Relative within 24 hours ("14 min ago"), otherwise absolute ("Oct 3, 09:14"). */
  time?: ReactNode;
  source?: ReactNode;
  /** M-0000 memory, H-0000 handoff, PR #000 for code. */
  id?: ReactNode;
  /** `night` on the night surface. */
  tone?: "default" | "night";
  className?: string;
}

/** The provenance line every memory carries: stamp · action · time · source · ID, in mono. */
export function Receipt({
  agent,
  person,
  name,
  action,
  time,
  source,
  id,
  tone = "default",
  className,
}: ReceiptProps) {
  const parts: Array<{ key: string; node: ReactNode }> = [];
  if (action != null) {
    parts.push({
      key: "a",
      node: <span className="mx-receipt-action">{action}</span>,
    });
  }
  if (time != null) parts.push({ key: "t", node: <span>{time}</span> });
  if (source != null) {
    parts.push({
      key: "s",
      node: <span className="mx-receipt-source">{source}</span>,
    });
  }
  if (id != null)
    parts.push({ key: "i", node: <span className="mx-receipt-id">{id}</span> });
  return (
    <span
      className={cx("mx-receipt", tone === "night" && "is-night", className)}
    >
      {agent || person ? (
        <AgentStamp agent={agent} person={person} name={name} size="sm" />
      ) : null}
      {parts.map((part, i) => (
        <Fragment key={part.key}>
          {i > 0 ? (
            <span className="mx-receipt-dot" aria-hidden="true">
              ·
            </span>
          ) : null}
          {part.node}
        </Fragment>
      ))}
    </span>
  );
}
