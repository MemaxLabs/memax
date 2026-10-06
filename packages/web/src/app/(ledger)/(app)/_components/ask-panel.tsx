"use client";

import { useState } from "react";
import {
  Button,
  Cite,
  Highlight,
  Receipt,
  Seal,
  useLedger,
} from "@memaxlabs/ledger";
import { interpolate, useLocale, type Locale } from "@/i18n";
import { count, formatReceiptTime, formatShortDate } from "@/lib/v2/copy";
import type {
  AnswerPart,
  AskSource,
  ReceiptLine,
  SpaceSummary,
  Viewer,
} from "@/lib/v2/data/types";
import type { AskState } from "../_lib/use-ask";
import { useSource } from "../_lib/data";
import { memoryHref } from "../_places/memories/memory-rows";
import styles from "./command-center.module.css";

/** The tooltip a citation shows: the memory's ID and its words. */
function citeTitle(src: AskSource | undefined): string | undefined {
  if (!src) return undefined;
  const words =
    src.statement.length > 160
      ? `${src.statement.slice(0, 157)}…`
      : src.statement;
  return `${src.receipt.ref} · ${words}`;
}

/**
 * Ask's result (Ask.png): the answer in the serif with its citations,
 * the numbered sources with their receipts, then Send, Copy with
 * citations and Keep as memory (⌘↵). Hovering or focusing a citation
 * marks its source; a citation of a kept memory opens it. Once kept,
 * the seal stamps where Keep was.
 */
export function AskPanel({
  state,
  space,
  viewer,
  keepKey,
  keeping,
  kept,
  onKeep,
  onCopy,
  onRemember,
  onRetry,
  onOpen,
}: {
  state: AskState;
  space: SpaceSummary;
  viewer: Viewer | null;
  keepKey: string;
  keeping: boolean;
  /** The memory the answer was just kept as: the seal shows. */
  kept: { ref: string; at: Date } | null;
  onKeep: () => void;
  onCopy: () => void;
  onRemember: () => void;
  onRetry: () => void;
  /** A citation was followed to its memory: the overlay closes. */
  onOpen: () => void;
}) {
  const { t, locale } = useLocale();
  const copy = t.ledger.app.command;
  const { agents } = useLedger();
  const source = useSource();
  const [active, setActive] = useState<number | null>(null);
  const now = source.now();
  const timeZone =
    viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;

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

  if (state.status === "limit") {
    const date = state.resetAt
      ? formatShortDate(new Date(state.resetAt), timeZone, locale)
      : "";
    return (
      <div className={styles.quiet} role="status">
        <p className={styles.quietTitle}>
          {interpolate(copy.limit.title, { n: String(state.limit) })}
        </p>
        <p className={styles.quietDetail}>
          {interpolate(copy.limit.detail, { date })}
        </p>
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

  const sourceList = (sources: AskSource[]) => (
    <ol className={styles.sourceList}>
      {sources.map((src) => (
        <li
          key={src.n}
          className={styles.sourceRow}
          data-active={active === src.n || undefined}
        >
          <Cite
            n={src.n}
            title={src.receipt.ref}
            href={src.memory ? memoryHref(space.slug, src.memory) : undefined}
          />
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
  );

  if (state.status === "off") {
    return (
      <div onClickCapture={followed(onOpen)}>
        <div className={styles.quiet} role="status">
          <p className={styles.quietTitle}>
            {interpolate(copy.off.title, { space: space.name })}
          </p>
          <p className={styles.quietDetail}>
            {state.sources.length > 0 ? copy.off.detail : copy.off.empty}
          </p>
        </div>
        {state.sources.length > 0 ? (
          <div className={styles.sources}>
            <p className="mx-section-label">{copy.sourcesLabel}</p>
            {sourceList(state.sources)}
          </div>
        ) : null}
      </div>
    );
  }

  const streaming = state.status === "streaming";
  const sources = count(copy.sourcesOne, copy.sources, state.sources.length);
  const label = interpolate(copy.answerFrom, { space: space.name, sources });

  return (
    <div onClickCapture={followed(onOpen)}>
      <p className="mx-sr" role="status">
        {streaming ? interpolate(copy.answering, { space: space.name }) : label}
      </p>
      {state.sources.length > 0 ? (
        <p className="mx-section-label" aria-hidden="true">
          {label}
        </p>
      ) : null}
      <p className="mx-answer" aria-busy={streaming || undefined}>
        <AnswerParts
          parts={state.parts}
          sources={state.sources}
          space={space}
          onActive={setActive}
        />
      </p>
      {state.sources.length > 0 ? (
        <div className={styles.sources}>
          <p className="mx-section-label">{copy.sourcesLabel}</p>
          {sourceList(state.sources)}
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
          {kept ? (
            <span className={styles.keptSeal} role="status">
              <Seal
                size={56}
                animate
                id={kept.ref}
                date={formatShortDate(kept.at, timeZone, locale)}
              />
            </span>
          ) : (
            <Button
              variant="keep"
              size="sm"
              kbd={keepKey}
              pending={keeping}
              onClick={onKeep}
            >
              {copy.keepAnswer}
            </Button>
          )}
        </div>
      ) : null}
    </div>
  );
}

/** Calls open when a click lands on a link (a citation going to its memory). */
function followed(open: () => void) {
  return (event: React.MouseEvent) => {
    if ((event.target as Element | null)?.closest?.("a[href]")) open();
  };
}

function AnswerParts({
  parts,
  sources,
  space,
  onActive,
}: {
  parts: AnswerPart[];
  sources: AskSource[];
  space: SpaceSummary;
  onActive: (n: number | null) => void;
}) {
  return (
    <>
      {parts.map((part, i) => {
        if (part.kind === "highlight") {
          return <Highlight key={i}>{part.text}</Highlight>;
        }
        if (part.kind === "cite") {
          const src = sources.find((s) => s.n === part.n);
          return (
            <span
              key={i}
              onMouseEnter={() => onActive(part.n)}
              onMouseLeave={() => onActive(null)}
              onFocus={() => onActive(part.n)}
              onBlur={() => onActive(null)}
            >
              <Cite
                n={part.n}
                title={citeTitle(src)}
                href={
                  src?.memory ? memoryHref(space.slug, src.memory) : undefined
                }
              />
            </span>
          );
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
      time={
        receipt.at
          ? formatReceiptTime(copy, receipt.at, now, timeZone, locale)
          : undefined
      }
      id={receipt.ref}
    />
  );
}
