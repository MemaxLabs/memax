import { useId, type MouseEvent, type ReactNode, type Ref } from "react";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import type { MarkState, StatementState } from "../lib/types";
import { AgentStamp } from "../provenance/agent-stamp";
import { StateMark } from "./state-mark";

export interface MemoryRowProps {
  /** The statement. It is set in the serif; its state sets the type. */
  children: ReactNode;
  /** Kept rows carry no mark. A forgotten memory has no words left: render `Redaction`. */
  state?: StatementState;
  /**
   * The mark to draw instead of the state's, while the type still follows
   * `state`: a proposal Memax is still checking wears `working`.
   */
  mark?: MarkState;
  /** The mark's word for assistive technology and the tooltip, when it isn't the state's. */
  markLabel?: string;
  agent?: string;
  /** A person's initials. */
  person?: string;
  /** The actor's name, for the tooltip and assistive technology. */
  name?: string;
  /** The receipt's verb ("kept", "proposed", "flagged"). Defaults to the state's word. */
  action?: string;
  time?: string;
  /** The memory's ID. It never truncates. */
  id?: string;
  space?: string;
  /** Truncates first when the rail is narrow. */
  source?: string;
  /** A line under the statement. */
  note?: ReactNode;
  /** Buttons that float over the rail on hover and focus, so the rule never moves. */
  actions?: ReactNode;
  /** Accessible name of the actions group. Defaults to "Actions for {id}". */
  actionsLabel?: string;
  selected?: boolean;
  /** Italic in ink-2 even when the state isn't `proposed` (a conflicting proposal). */
  unconfirmed?: boolean;
  /** Moves the receipt under the statement, for narrow panes. */
  stacked?: boolean;
  compact?: boolean;
  /** Makes the statement a link to the memory, stretched over the whole row. */
  href?: string;
  /** Makes the statement a button, stretched over the whole row (e.g. to select it). */
  onClick?: (event: MouseEvent<HTMLElement>) => void;
  /** The statement's language, when it differs from the page's. */
  lang?: string;
  /** `li` inside a `MemoryList` (the default), `div` when the row stands alone. */
  as?: "li" | "div";
  className?: string;
  ref?: Ref<HTMLElement>;
}

/**
 * One memory: the statement in serif, its receipt on the ledger rule at the
 * right, and a mark only when the memory is something other than simply kept.
 * The receipt is two strict lines that never wrap: "verb · when" and
 * "ID · space · source".
 */
export function MemoryRow({
  children,
  state = "kept",
  mark,
  markLabel,
  agent,
  person,
  name,
  action,
  time,
  id,
  space,
  source,
  note,
  actions,
  actionsLabel,
  selected,
  unconfirmed,
  stacked,
  compact,
  href,
  onClick,
  lang,
  as = "li",
  className,
  ref,
}: MemoryRowProps) {
  const { strings, agents, Link } = useLedger();
  const uid = useId();
  const markId = `${uid}-mark`;
  const railId = `${uid}-rail`;
  const hasActor = Boolean(agent || person);
  const actor = hasActor
    ? resolveAgent(agents, { agent, person, name }, strings.agent.fallback)
    : undefined;
  const verb = action ?? strings.receiptVerb[state];
  const shown = mark ?? state;
  const marked = shown !== "kept";
  // Line two: the ID, which never truncates, then "space · source" in one box
  // that ends in an ellipsis, so the source is cut first and then the space.
  const rest = [space, source].filter(Boolean).join(" · ");
  const fullReceipt = [actor?.name, verb, time, id, space, source]
    .filter(Boolean)
    .join(" · ");
  const describedBy = cx(marked && markId, railId);
  const interactive = href !== undefined || onClick !== undefined;

  let statement: ReactNode = children;
  if (href !== undefined) {
    statement = (
      <Link
        className="mx-row-link"
        href={href}
        aria-describedby={describedBy}
        title={fullReceipt}
      >
        {children}
      </Link>
    );
  } else if (onClick) {
    statement = (
      <button
        type="button"
        className="mx-row-link"
        onClick={onClick}
        aria-describedby={describedBy}
        title={fullReceipt}
      >
        {children}
      </button>
    );
  }

  const Tag = as;
  return (
    <Tag
      ref={ref as Ref<HTMLLIElement & HTMLDivElement>}
      className={cx(
        "mx-row",
        `is-${state}`,
        (unconfirmed ?? state === "proposed") && "is-unconfirmed",
        selected && "is-selected",
        compact && "is-compact",
        stacked && "is-stacked",
        interactive && "is-clickable",
        className,
      )}
      aria-current={selected ? "true" : undefined}
    >
      <div className="mx-row-mark">
        {marked ? (
          <StateMark
            id={markId}
            state={shown}
            label={false}
            {...(markLabel
              ? { "aria-label": markLabel, title: markLabel }
              : {})}
          />
        ) : null}
      </div>
      <div className="mx-row-body">
        <p
          className="mx-row-text"
          lang={lang}
          aria-describedby={marked ? markId : undefined}
        >
          {statement}
        </p>
        {note ? <p className="mx-row-note">{note}</p> : null}
      </div>
      <div className="mx-row-rail" id={railId} title={fullReceipt}>
        {actor ? (
          <AgentStamp agent={agent} person={person} name={name} size="sm" />
        ) : (
          <span />
        )}
        <div className="mx-row-rail-lines">
          <span className="mx-receipt">
            <span className="mx-receipt-action">{verb}</span>
            {time ? (
              <>
                <span className="mx-receipt-dot" aria-hidden="true">
                  ·
                </span>
                <span>{time}</span>
              </>
            ) : null}
          </span>
          {id || rest ? (
            <span className="mx-receipt mx-row-rail-2">
              {id ? <span className="mx-row-rail-id">{id}</span> : null}
              {rest ? (
                <span className="mx-row-rail-rest">
                  {id ? ` · ${rest}` : rest}
                </span>
              ) : null}
            </span>
          ) : null}
        </div>
      </div>
      {actions ? (
        <div
          className="mx-row-actions"
          role="group"
          aria-label={
            actionsLabel ??
            (id
              ? format(strings.memoryRow.actionsFor, { id })
              : strings.memoryRow.actions)
          }
        >
          {actions}
        </div>
      ) : null}
    </Tag>
  );
}
