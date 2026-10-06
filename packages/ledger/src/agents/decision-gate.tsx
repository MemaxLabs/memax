import { useId, type KeyboardEvent, type ReactNode, type Ref } from "react";
import { useLedger } from "../i18n/provider";
import { resolveAgent } from "../lib/agents";
import { cx } from "../lib/cx";
import { format } from "../lib/format";
import { useControllableState } from "../lib/use-controllable-state";
import { useRovingRadio } from "../lib/use-roving-radio";
import { Button } from "../primitives/button";
import { Kbd } from "../primitives/kbd";
import { AgentStamp } from "../provenance/agent-stamp";

export interface DecisionOption {
  label: ReactNode;
  detail?: ReactNode;
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
  className?: string;
  ref?: Ref<HTMLElement>;
}

/**
 * An agent stops and asks; the answer becomes a kept memory with the person as
 * its author. The options are a radio group: arrow keys or 1–9 choose, and
 * Answer is a Keep, so it is green.
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
  className,
  ref,
}: DecisionGateProps) {
  const { strings, agents, formatList } = useLedger();
  const g = strings.gate;
  const who = resolveAgent(agents, { agent }, strings.agent.fallback);
  const questionId = useId();
  const [choice, setChoice] = useControllableState(
    selected,
    defaultSelected,
    onSelect,
  );
  const { getItemProps, focusItem } = useRovingRadio({
    count: options.length,
    selected: choice,
    onSelect: (i) => {
      if (i !== choice) setChoice(i);
    },
  });

  // 1–9 choose while focus is in the options. Global keys belong to the app's
  // keymap; this only covers the group itself. Never while an IME composes.
  const onDigit = (event: KeyboardEvent<HTMLDivElement>) => {
    if (
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

  return (
    <article ref={ref} className={cx("mx-gate", className)}>
      <header className="mx-gate-head">
        <AgentStamp agent={agent} size="sm" decorative />
        <span className="mx-gate-who">
          {format(g.waiting, { name: who.name })}
        </span>
        {time ? <span className="mx-meta">{time}</span> : null}
      </header>
      <p className="mx-gate-q" id={questionId}>
        {question}
      </p>
      {context != null ? <p className="mx-gate-ctx">{context}</p> : null}
      <div
        className="mx-gate-opts"
        role="radiogroup"
        aria-labelledby={questionId}
        onKeyDown={onDigit}
      >
        {options.map((option, i) => (
          <button
            key={i}
            type="button"
            role="radio"
            aria-checked={choice === i}
            aria-keyshortcuts={String(i + 1)}
            className={cx("mx-gate-opt", choice === i && "is-on")}
            {...getItemProps(i)}
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
      <footer className="mx-gate-foot">
        <span className="mx-meta">
          {space ? format(g.keptIn, { space }) : g.kept}
        </span>
        <Button
          variant="keep"
          size="sm"
          kbd="↵"
          disabled={choice < 0}
          disabledReason={choice < 0 ? reason : undefined}
          pending={pending}
          onClick={() => {
            if (choice >= 0) onAnswer?.(choice);
          }}
        >
          {g.answer}
        </Button>
      </footer>
    </article>
  );
}
