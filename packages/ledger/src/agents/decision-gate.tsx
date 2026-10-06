import {
  useId,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from "react";
import { Seal } from "../brand/seal";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import { useControllableState } from "../lib/use-controllable-state";
import { useRovingRadio } from "../lib/use-roving-radio";
import { Button } from "../primitives/button";
import { Kbd } from "../primitives/kbd";
import { AgentStamp } from "../provenance/agent-stamp";
import { Receipt } from "../provenance/receipt";

export interface DecisionOption {
  label: ReactNode;
  detail?: ReactNode;
}

/** The person's answer, kept: what the seal and the receipt show. */
export interface DecisionAnswered {
  /** The kept decision's ID ("M-0447"). */
  id: string;
  /** The date on the seal ("Oct 5"). */
  date: string;
  /** Initials of the person who answered, for the receipt stamp. */
  by?: string;
  /** Their name when it isn't the reader. Defaults to the locale's "You". */
  byName?: string;
  /** The receipt's time. Defaults to the locale's "just now". */
  time?: ReactNode;
}

export interface DecisionGateProps {
  /** The agent that stopped to ask. */
  agent: string;
  /** One sentence, in the serif. */
  question: ReactNode;
  /** Why it is asking, naming the memory IDs and their true states. */
  context?: ReactNode;
  /** Two to four, numbered for the keyboard. Include a way to defer. */
  options: DecisionOption[];
  /** Controlled: the chosen option's index, or -1. */
  selected?: number;
  defaultSelected?: number;
  onSelect?: (index: number) => void;
  /** Answer was pressed with an option chosen. The answer is kept, authored by the person. */
  onAnswer?: (index: number) => void;
  /** The answer is being kept. */
  pending?: boolean;
  space?: string;
  time?: string;
  /**
   * Why Answer can't be pressed even with an option chosen (a viewer, or
   * a session Memax can't confirm is on the web where decisions need it).
   * Answer stays drawn, disabled, with this as its reason.
   */
  answerDisabledReason?: string;
  /** A note above the footer: what stands in the way of answering here, and what to do. */
  notice?: ReactNode;
  /** Quiet actions before Answer, such as taking the question back. */
  actions?: ReactNode;
  /**
   * Replaces the footer while the gate still waits: the confirmation that
   * names what an answer does, or an inline confirmation of an action.
   */
  footer?: ReactNode;
  /**
   * Answered here: the options lock on the chosen one, the seal stamps
   * with the kept decision's ID, and the footer becomes the person's
   * receipt. The seal stamps only on the change to answered while mounted.
   */
  answered?: DecisionAnswered;
  /**
   * It ended without this person's answer: answered elsewhere, taken back
   * or expired. The options lock (on the answer, if there was one) and
   * this line replaces the footer.
   */
  ended?: ReactNode;
  /** What the head says once the gate isn't waiting ("Expired"), in place of "{name} is waiting on you". */
  status?: ReactNode;
  className?: string;
  ref?: Ref<HTMLElement>;
}

/**
 * An agent stops and asks; the answer becomes a kept memory with the person as
 * its author. The options are a radio group: arrow keys or 1–9 choose, and
 * Answer is a Keep, so it is green. Once answered the seal stamps and the
 * footer is the receipt; once it ended any other way it says how.
 */
export function DecisionGate({
  agent,
  question,
  context,
  options,
  selected,
  defaultSelected = -1,
  onSelect,
  onAnswer,
  pending = false,
  space,
  time,
  answerDisabledReason,
  notice,
  actions,
  footer,
  answered,
  ended,
  status,
  className,
  ref,
}: DecisionGateProps) {
  const { strings, agents, formatList } = useLedger();
  const g = strings.gate;
  const who = resolveAgent(agents, { agent }, strings.agent.fallback);
  const questionId = useId();
  const locked = answered !== undefined || ended != null;
  const [choice, setChoice] = useControllableState(
    selected,
    defaultSelected,
    onSelect,
  );
  const { getItemProps, focusItem } = useRovingRadio({
    count: options.length,
    selected: choice,
    onSelect: (i) => {
      if (!locked && i !== choice) setChoice(i);
    },
    isDisabled: () => locked,
  });

  // The seal stamps only on a change to answered while mounted, never when
  // the gate is first drawn already answered.
  const [prevAnswered, setPrevAnswered] = useState(answered !== undefined);
  const [stamping, setStamping] = useState(false);
  if (prevAnswered !== (answered !== undefined)) {
    setPrevAnswered(answered !== undefined);
    setStamping(answered !== undefined);
  }

  // 1–9 choose while focus is in the options. Global keys belong to the app's
  // keymap; this only covers the group itself. Never while an IME composes.
  const onDigit = (event: KeyboardEvent<HTMLDivElement>) => {
    if (
      locked ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.nativeEvent.isComposing
    )
      return;
    if (!/^[1-9]$/.test(event.key)) return;
    const index = Number(event.key) - 1;
    if (index >= options.length) return;
    event.preventDefault();
    focusItem(index);
    if (index !== choice) setChoice(index);
  };

  const reason = format(g.choose, {
    options: formatList(
      options.map((_, i) => String(i + 1)),
      "disjunction",
    ),
  });

  let foot: ReactNode;
  if (answered) {
    foot = (
      <Receipt
        person={answered.by}
        name={answered.byName ?? (answered.by ? strings.agent.you : undefined)}
        action={strings.receiptVerb.kept}
        time={answered.time ?? strings.time.justNow}
        id={answered.id}
      />
    );
  } else if (ended != null) {
    foot = <span className="mx-meta">{ended}</span>;
  } else if (footer != null) {
    foot = footer;
  } else {
    const blocked = choice < 0 || Boolean(answerDisabledReason);
    const answer = (
      <Button
        variant="keep"
        size="sm"
        kbd="↵"
        disabled={blocked}
        disabledReason={
          answerDisabledReason ?? (choice < 0 ? reason : undefined)
        }
        pending={pending}
        onClick={() => {
          if (choice >= 0 && !answerDisabledReason) onAnswer?.(choice);
        }}
      >
        {g.answer}
      </Button>
    );
    foot = (
      <>
        <span className="mx-meta">
          {space ? format(g.keptIn, { space }) : g.kept}
        </span>
        {actions != null ? (
          <div className="mx-gate-actions">
            {actions}
            {answer}
          </div>
        ) : (
          answer
        )}
      </>
    );
  }

  return (
    <article
      ref={ref}
      className={cx(
        "mx-gate",
        locked && "is-locked",
        answered && "is-answered",
        ended != null && "is-ended",
        className,
      )}
      aria-busy={pending || undefined}
    >
      <header className="mx-gate-head">
        <AgentStamp agent={agent} size="sm" decorative />
        <span className="mx-gate-who">
          {locked && status != null
            ? status
            : format(g.waiting, { name: who.name })}
        </span>
        {time ? <span className="mx-meta">{time}</span> : null}
      </header>
      {answered ? (
        <Seal
          date={answered.date}
          id={answered.id}
          size={76}
          animate={stamping}
          className="mx-gate-seal"
        />
      ) : null}
      <p className="mx-gate-q" id={questionId}>
        {question}
      </p>
      {context != null ? <p className="mx-gate-ctx">{context}</p> : null}
      <div
        className="mx-gate-opts"
        role="radiogroup"
        aria-labelledby={questionId}
        aria-readonly={locked || undefined}
        onKeyDown={onDigit}
      >
        {options.map((option, i) => (
          <button
            key={i}
            type="button"
            role="radio"
            aria-checked={choice === i}
            aria-disabled={locked || undefined}
            aria-keyshortcuts={locked ? undefined : String(i + 1)}
            className={cx("mx-gate-opt", choice === i && "is-on")}
            {...getItemProps(i)}
            // Locked, the arrows are the page's again (a queue moves on).
            {...(locked ? { onKeyDown: undefined } : {})}
          >
            <Kbd aria-hidden="true">{i + 1}</Kbd>
            <span className="mx-gate-opt-text">
              <span className="mx-gate-opt-label">{option.label}</span>
              {option.detail != null ? (
                <span className="mx-gate-opt-detail">{option.detail}</span>
              ) : null}
            </span>
          </button>
        ))}
      </div>
      {notice != null && !locked ? (
        <div className="mx-gate-notice" role="note">
          {notice}
        </div>
      ) : null}
      <footer className="mx-gate-foot">{foot}</footer>
    </article>
  );
}
