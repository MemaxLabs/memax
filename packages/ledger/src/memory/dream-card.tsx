import type { ReactNode, Ref } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { format, formatNodes, plural } from "../lib/format";
import type { DreamItemKind, MarkState } from "../lib/types";
import { Glyph } from "./glyph";

export interface DreamItem {
  /** Stable React key; the index is used when omitted. */
  key?: string;
  kind: DreamItemKind;
  text: ReactNode;
  /** "9 notes → M-0219", "needs you", "restorable". */
  meta?: ReactNode;
}

export interface DreamCardProps {
  /** The edition number. */
  issue?: number;
  /** Already formatted ("Mon 5 Oct"). */
  date: string;
  time?: string;
  duration?: string;
  /** How many notes the night read. */
  notes: number;
  /** How many facts they became. */
  facts: number;
  /** The night's real note IDs; the first 35 are drawn. */
  noteIds: string[];
  /** The facts they folded into. */
  factIds: string[];
  /** At most three are shown, conflicts first. */
  items?: DreamItem[];
  /** Buttons; they restyle themselves for night. */
  action?: ReactNode;
  /**
   * The fold plays once per edition. Pass false once this edition has been
   * seen, so it doesn't replay on every visit.
   */
  animate?: boolean;
  /** The headline's heading level. */
  headingLevel?: 2 | 3 | 4;
  className?: string;
  ref?: Ref<HTMLElement>;
}

const KIND_STATE: Record<DreamItemKind, MarkState> = {
  merged: "merged",
  conflict: "conflict",
  faded: "faded",
  kept: "kept",
};

/**
 * The overnight edition, on the one inverted surface: a masthead, the night's
 * headline, and the real note IDs folding into the facts they became.
 */
export function DreamCard({
  issue,
  date,
  time,
  duration,
  notes,
  facts,
  noteIds,
  factIds,
  items = [],
  action,
  animate = true,
  headingLevel = 3,
  className,
  ref,
}: DreamCardProps) {
  const { strings, formatNumber } = useLedger();
  const d = strings.dream;
  const notesText = plural(d.notes, notes, formatNumber);
  const factsText = plural(d.facts, facts, formatNumber);
  const shown = [...items]
    .sort(
      (a, b) => Number(b.kind === "conflict") - Number(a.kind === "conflict"),
    )
    .slice(0, 3);
  const Title = `h${headingLevel}` as const;
  const issueLine = [
    issue != null ? format(d.issue, { n: issue }) : null,
    date,
    time,
    duration,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <section
      ref={ref}
      className={cx("mx-dream", !animate && "is-static", className)}
    >
      <header className="mx-dream-mast">
        <span className="mx-dream-name">{d.masthead}</span>
        <span className="mx-receipt is-night">{issueLine}</span>
      </header>
      <div className="mx-dream-main">
        <Title className="mx-dream-title">
          {formatNodes(d.title, {
            notes: notesText,
            facts: <span className="mx-dream-n">{factsText}</span>,
          })}
        </Title>
        <figure
          className="mx-dream-fold"
          aria-label={format(d.fold, { notes: notesText, facts: factsText })}
        >
          <div className="mx-dream-notes" aria-hidden="true">
            {noteIds.slice(0, 35).map((noteId, i) => (
              <span key={noteId} style={{ animationDelay: `${i * 14}ms` }}>
                {noteId}
              </span>
            ))}
          </div>
          <svg
            className="mx-dream-brace"
            width={18}
            height={100}
            viewBox="0 0 18 100"
            aria-hidden="true"
          >
            <path d="M2 2 C10 2 9 14 9 30 C9 44 10 50 16 50 C10 50 9 56 9 70 C9 86 10 98 2 98" />
          </svg>
          <ol className="mx-dream-facts">
            {factIds.map((factId, i) => (
              <li key={factId} style={{ animationDelay: `${520 + i * 70}ms` }}>
                <span className="mx-dream-dot" />
                {factId}
              </li>
            ))}
          </ol>
        </figure>
      </div>
      {shown.length ? (
        <ul className="mx-dream-list">
          {shown.map((item, i) => (
            <li
              key={item.key ?? i}
              className={cx("mx-dream-item", `is-${item.kind}`)}
            >
              <span className="mx-dream-kind">
                <Glyph state={KIND_STATE[item.kind]} size={10} />
                {d.kind[item.kind]}
              </span>
              <span className="mx-dream-text">{item.text}</span>
              {item.meta != null ? (
                <span className="mx-dream-meta">{item.meta}</span>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {action ? <footer className="mx-dream-foot">{action}</footer> : null}
    </section>
  );
}
