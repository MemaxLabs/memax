import type { ReactNode } from "react";
import type { MarkState } from "../lib/types";
import { AgentStamp } from "../provenance/agent-stamp";
import { StateMark } from "./state-mark";

export interface LineageEvent {
  /** Stable React key; the index is used when omitted. */
  key?: string;
  /** The mark on the line. Without one, a neutral dot. */
  state?: MarkState;
  agent?: string;
  person?: string;
  /** Starts with the actor: "Kept by you", "Dream merged 9 notes into it". */
  title: ReactNode;
  /** Shown as written ("Oct 2, 10:41"). */
  time: string;
  /** Machine-readable time for the `<time>` element. */
  dateTime?: string;
  detail?: ReactNode;
}

export interface LineageProps {
  /** Oldest first. */
  events: LineageEvent[];
  className?: string;
}

/** The life of one memory, oldest first: proposed, kept, merged, handed off, verified, forgotten. */
export function Lineage({ events, className }: LineageProps) {
  return (
    <ol className={className ? `mx-lineage ${className}` : "mx-lineage"}>
      {events.map((event, i) => (
        <li key={event.key ?? i} className="mx-lineage-item">
          <span className="mx-lineage-mark">
            {event.state ? (
              <StateMark state={event.state} label={false} />
            ) : (
              <span className="mx-lineage-dot" />
            )}
          </span>
          <div className="mx-lineage-body">
            <div className="mx-lineage-head">
              {event.agent || event.person ? (
                // The title already names the actor, so the stamp is decorative.
                <AgentStamp
                  agent={event.agent}
                  person={event.person}
                  size="sm"
                  decorative
                />
              ) : (
                <span className="mx-stamp-gap" />
              )}
              <span className="mx-lineage-title">{event.title}</span>
              <time className="mx-lineage-time" dateTime={event.dateTime}>
                {event.time}
              </time>
            </div>
            {event.detail ? (
              <div className="mx-lineage-detail">{event.detail}</div>
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}
