"use client";

import { MemoryRow } from "@memaxlabs/ledger";
import { gateStatusAt, type GateView } from "@/lib/v2/data/gates";
import type { RecordsView } from "../_places/records-view";

/**
 * A decision gate as a row (Review's queue, Today's "Waiting on you"):
 * the agent's question in roman serif (a question, not a proposal) with
 * the ochre waiting mark, and the receipt rail "asked · 2 min ago",
 * "G-0012". Once it isn't waiting it carries no ochre: answered reads as
 * kept, taken back or expired as off.
 */
export function GateRow({
  view,
  gate,
  selected,
  stacked,
  space,
  note,
  href,
  onClick,
}: {
  view: RecordsView;
  gate: GateView;
  selected?: boolean;
  stacked?: boolean;
  /** The space's name on the rail, where the list spans one (Today). */
  space?: string;
  note?: string;
  href?: string;
  onClick?: () => void;
}) {
  const { rc, l, now } = view;
  const status = gateStatusAt(gate, now);
  const verb =
    status === "answered"
      ? rc.verbs.answered
      : status === "withdrawn"
        ? rc.verbs.withdrawn
        : rc.verbs.asked;
  return (
    <MemoryRow
      stacked={stacked}
      selected={selected}
      state={status === "waiting" ? "proposed" : "kept"}
      // A question isn't a proposal: roman, with the waiting mark.
      unconfirmed={false}
      mark={status === "withdrawn" || status === "expired" ? "off" : undefined}
      markLabel={
        status === "waiting"
          ? l.today.waiting.title
          : status === "answered"
            ? undefined
            : l.review.gate.status[status]
      }
      agent={gate.agent}
      action={verb}
      time={view.time(gate.askedAt)}
      id={gate.ref}
      space={space}
      note={note}
      href={href}
      onClick={onClick}
    >
      {gate.question}
    </MemoryRow>
  );
}
