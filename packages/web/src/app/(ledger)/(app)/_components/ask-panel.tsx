"use client";

import { Button, Cite, Highlight, Receipt, useLedger } from "@memaxlabs/ledger";
import { interpolate, useLocale, type Locale } from "@/i18n";
import { count, formatReceiptTime } from "@/lib/v2/copy";
import type {
  AnswerPart,
  ReceiptLine,
  SpaceSummary,
  Viewer,
} from "@/lib/v2/data/types";
import type { AskState } from "../_lib/use-ask";
import { useSource } from "../_lib/data";
import styles from "./command-center.module.css";

/**
 * Ask's result (Ask.png): the answer in the serif with its highlight
 * and citations, the numbered sources with their receipts, then Send,
 * Copy with citations and Keep as memory (⌘↵).
 */
export function AskPanel({
  state,
  space,
  viewer,
  keepKey,
  keeping,
  onKeep,
  onCopy,
  onRemember,
  onRetry,
}: {
  state: AskState;
  space: SpaceSummary;
  viewer: Viewer | null;
  keepKey: string;
  keeping: boolean;
  onKeep: () => void;
  onCopy: () => void;
  onRemember: () => void;
  onRetry: () => void;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.app.command;
  const { agents } = useLedger();
  const source = useSource();

  if (state.status === "idle") return null;

  if (state.status === "none" || state.status === "unavailable") {
    const text = state.status === "none" ? copy.none : copy.unavailable;
    return (
      <div className={styles.quiet} role="status">
        <p className={styles.quietTitle}>
          {interpolate(text.title, { space: space.name })}
        </p>
        <p className={styles.quietDetail}>{text.detail}</p>
        <div>
          <Button
            variant="secondary"
            size="sm"
            icon="plus"
            onClick={onRemember}
          >
            {copy.rememberInstead}
          </Button>
        </div>
      </div>
    );
  }

  if (state.status === "failed") {
    return (
      <div className={styles.quiet} role="alert">
        <p className={styles.quietTitle}>{copy.failed}</p>
        <div>
          <Button variant="secondary" size="sm" icon="sync" onClick={onRetry}>
            {copy.tryAgain}
          </Button>
        </div>
      </div>
    );
  }

  const streaming = state.status === "streaming";
  const sources = count(copy.sourcesOne, copy.sources, state.sources.length);
  const label = interpolate(copy.answerFrom, { space: space.name, sources });
  const now = source.now();
  const timeZone =
    viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;

  return (
    <>
      <p className="mx-sr" role="status">
        {streaming ? interpolate(copy.answering, { space: space.name }) : label}
      </p>
      {state.sources.length > 0 ? (
        <p className="mx-section-label" aria-hidden="true">
          {label}
        </p>
      ) : null}
      <p className="mx-answer" aria-busy={streaming || undefined}>
        <AnswerParts parts={state.parts} sources={state.sources} />
      </p>
      {state.sources.length > 0 ? (
        <div className={styles.sources}>
          <p className="mx-section-label">{copy.sourcesLabel}</p>
          <ol className={styles.sourceList}>
            {state.sources.map((src) => (
              <li key={src.n} className={styles.sourceRow}>
                <Cite n={src.n} title={src.receipt.ref} />
                <p
                  className={
                    src.state === "merged"
                      ? `${styles.sourceText} ${styles.sourceMerged}`
                      : styles.sourceText
                  }
                >
                  {src.statement}
                </p>
                <span className={styles.sourceReceipt}>
                  <SourceReceipt
                    receipt={src.receipt}
                    now={now}
                    timeZone={timeZone}
                    locale={locale}
                  />
                </span>
              </li>
            ))}
          </ol>
        </div>
      ) : null}
      {state.status === "done" ? (
        <div className={styles.actions}>
          <Button
            variant="secondary"
            size="sm"
            icon="terminal"
            disabled
            disabledReason={t.ledger.app.notYet}
          >
            {interpolate(copy.send, {
              agent: agents["claude-code"]?.name ?? "Claude Code",
            })}
          </Button>
          <Button variant="quiet" size="sm" icon="link" onClick={onCopy}>
            {copy.copy}
          </Button>
          <span className={styles.spacer} />
          <Button
            variant="keep"
            size="sm"
            kbd={keepKey}
            pending={keeping}
            onClick={onKeep}
          >
            {copy.keepAnswer}
          </Button>
        </div>
      ) : null}
    </>
  );
}

function AnswerParts({
  parts,
  sources,
}: {
  parts: AnswerPart[];
  sources: { n: number; receipt: ReceiptLine }[];
}) {
  return (
    <>
      {parts.map((part, i) => {
        if (part.kind === "highlight") {
          return <Highlight key={i}>{part.text}</Highlight>;
        }
        if (part.kind === "cite") {
          const ref = sources.find((s) => s.n === part.n)?.receipt.ref;
          return <Cite key={i} n={part.n} title={ref} />;
        }
        return <span key={i}>{part.text}</span>;
      })}
    </>
  );
}

function SourceReceipt({
  receipt,
  now,
  timeZone,
  locale,
}: {
  receipt: ReceiptLine;
  now: Date;
  timeZone: string;
  locale: Locale;
}) {
  const { t } = useLocale();
  const copy = t.ledger.app;
  return (
    <Receipt
      person={receipt.person}
      agent={receipt.agent}
      action={copy.receipt[receipt.action]}
      time={formatReceiptTime(copy, receipt.at, now, timeZone, locale)}
      id={receipt.ref}
    />
  );
}
