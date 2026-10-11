"use client";

import {
  useEffect,
  useRef,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from "react";
import { AgentStamp, Diff, Icon, Kbd } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate } from "@/lib/v2/copy";
import type {
  ForgetPreview,
  StaleCause,
  StaleWhy,
  VerifyAnswer,
} from "@/lib/v2/data/memories";
import { isComposing } from "@/lib/v2/keymap/keymap";
import { useKeycap } from "@/lib/v2/keymap/react";
import { failureText } from "@/lib/v2/records-copy";
import type { RecordsView } from "../_places/records-view";
import type { VerifyAnswerState } from "./use-verify-answer";
import styles from "./verify-stale.module.css";

/**
 * VerifyStale's parts (VerifyStale.png): what changed, and the three
 * answers. The dialog is verify-stale.tsx.
 */

const date = (view: RecordsView, iso: string) =>
  formatShortDate(new Date(iso), view.timeZone, view.locale);

/** "What changed": a pull request and its hunk, or the date that passed. */
export function WhatChanged({
  view,
  why,
}: {
  view: RecordsView;
  why: StaleWhy;
}) {
  const v = view.l.verify;
  return (
    <div className={styles.block}>
      <p className="mx-section-label">{v.changed}</p>
      {why.causes.length === 0 ? (
        <p className={styles.plain}>{v.noCause}</p>
      ) : (
        why.causes.map((cause, i) => (
          <Cause key={i} view={view} why={why} cause={cause} />
        ))
      )}
    </div>
  );
}

function Cause({
  view,
  why,
  cause,
}: {
  view: RecordsView;
  why: StaleWhy;
  cause: StaleCause;
}) {
  const v = view.l.verify;
  if (cause.kind === "date") {
    const stamp = view.stamp(why.flaggedBy);
    return (
      <div className={styles.change}>
        <span className={styles.changeIcon}>
          <Icon name="clock" />
        </span>
        <span>
          {interpolate(v.datePassed, { date: date(view, cause.staleAfter) })}
        </span>
        {why.since ? (
          <span className={`mx-meta ${styles.changeBy}`}>
            {interpolate(stamp ? v.flaggedBy : v.flaggedBare, {
              date: date(view, why.since),
            })}
            {stamp ? <AgentStamp {...stamp} size="sm" /> : null}
          </span>
        ) : null}
      </div>
    );
  }
  const stamp = view.stamp(cause.by);
  return (
    <>
      <div className={styles.change}>
        <span className={styles.changeIcon}>
          <Icon name="commit" />
        </span>
        <span>
          <b className={styles.changeRef}>{cause.ref}</b>
          {cause.title ? ` ${cause.title}` : null}
        </span>
        <span className={`mx-meta ${styles.changeBy}`}>
          {interpolate(stamp ? v.merged : v.mergedBare, {
            date: date(view, cause.at),
          })}
          {stamp ? <AgentStamp {...stamp} size="sm" /> : null}
        </span>
      </div>
      {cause.hunk ? (
        <pre
          className={styles.code}
          aria-label={interpolate(v.hunkLabel, { path: cause.hunk.path })}
        >
          <span className={styles.path}>{cause.hunk.path}</span>
          {cause.hunk.lines.map((line, i) => (
            <span
              key={i}
              className={
                line.op === "del"
                  ? styles.del
                  : line.op === "add"
                    ? styles.add
                    : undefined
              }
            >
              {`${line.op === "del" ? "-" : line.op === "add" ? "+" : " "}${line.text}`}
            </span>
          ))}
        </pre>
      ) : null}
    </>
  );
}

/**
 * "Removed from 3 files and every agent. A tombstone stays." The compiled
 * files; a copy-out (ChatGPT) is one of the agents that's told.
 */
export function removesText(view: RecordsView, preview: ForgetPreview): string {
  const v = view.l.verify;
  const files = preview.files;
  const where =
    files > 0
      ? count(v.removesFilesOne, v.removesFiles, files)
      : v.removesAgents;
  return interpolate(v.removes, { where });
}

/** Why this person can't forget it, from the preview; null when they can. */
export function forgetRefusalText(
  view: RecordsView,
  why: StaleWhy,
  preview: ForgetPreview | undefined,
): string | null {
  const { rc, space, locale } = view;
  return preview?.refusal
    ? failureText(
        rc,
        {
          kind: "refused",
          code: preview.refusal.code,
          message: preview.refusal.message,
        },
        { command: "forget", ref: why.ref, space: space.name, locale },
      )
    : null;
}

/** "What's true now?": three answers on keys 1, 2 and 3. */
export function Answers({
  view,
  why,
  state,
  preview,
  previewFailed,
  firstRef,
  onSubmit,
}: {
  view: RecordsView;
  why: StaleWhy;
  state: VerifyAnswerState;
  preview: ForgetPreview | undefined;
  previewFailed: boolean;
  /** The option focus starts on. */
  firstRef: Ref<HTMLButtonElement>;
  onSubmit: () => void;
}) {
  const v = view.l.verify;
  const fromSource = why.causes.some((c) => c.kind === "source");
  const forgetRefusal = forgetRefusalText(view, why, preview);
  const option = (
    answer: VerifyAnswer,
    n: number,
    title: string,
    detail: ReactNode,
    serif = false,
  ) => {
    const checked = state.answer === answer;
    return (
      <button
        type="button"
        role="radio"
        aria-checked={checked}
        className={styles.option}
        onClick={() => state.choose(answer)}
        ref={
          (state.answer ? checked : answer === "still_true")
            ? firstRef
            : undefined
        }
        tabIndex={(state.answer ? checked : answer === "still_true") ? 0 : -1}
        data-answer={answer}
      >
        <Kbd className={styles.optionKey}>{n}</Kbd>
        <span
          className={`${styles.optionTitle} ${answer === "forget" ? styles.forget : ""}`}
        >
          {title}
          <span className={serif ? styles.optionWords : styles.optionDetail}>
            {detail}
          </span>
        </span>
      </button>
    );
  };

  const changed = state.words && state.words !== why.statement.trim();
  return (
    <div
      role="radiogroup"
      aria-label={v.trueNowLabel}
      className={styles.answers}
      onKeyDown={onArrows}
    >
      <p className={`mx-section-label ${styles.answersLabel}`}>{v.trueNow}</p>
      {option(
        "still_true",
        1,
        v.stillTrue,
        fromSource ? v.stillTrueSource : v.stillTrueDate,
      )}
      {option(
        "update",
        2,
        v.update,
        changed ? (
          <Diff before={why.statement} after={state.words} />
        ) : (
          v.updateWrite
        ),
        Boolean(changed),
      )}
      {state.answer === "update" ? (
        state.editing ? (
          <UpdateEditor view={view} state={state} onSubmit={onSubmit} />
        ) : (
          <button
            type="button"
            className={styles.editWords}
            onClick={state.edit}
          >
            <Icon name="pencil" />
            {v.updateEdit}
          </button>
        )
      ) : null}
      {option(
        "forget",
        3,
        v.forget,
        forgetRefusal ??
          (preview
            ? removesText(view, preview)
            : previewFailed
              ? v.forgetFailed
              : v.forgetChecking),
      )}
    </div>
  );
}

/** ↓↑ move between the answers, as a radio group's arrows do. */
function onArrows(event: KeyboardEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (target.getAttribute("role") !== "radio") return;
  if (!["ArrowDown", "ArrowUp", "ArrowRight", "ArrowLeft"].includes(event.key))
    return;
  const radios = [
    ...event.currentTarget.querySelectorAll<HTMLElement>("[role=radio]"),
  ];
  const i = radios.indexOf(target);
  const step = event.key === "ArrowDown" || event.key === "ArrowRight" ? 1 : -1;
  const next = radios[(i + step + radios.length) % radios.length];
  event.preventDefault();
  next?.focus();
  next?.click();
}

/** "Update it", in the person's words: the serif field, ⌘↵ keeps it. */
function UpdateEditor({
  view,
  state,
  onSubmit,
}: {
  view: RecordsView;
  state: VerifyAnswerState;
  onSubmit: () => void;
}) {
  const v = view.l.verify;
  const keepKey = useKeycap("command.keep");
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = field.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (isComposing(event.nativeEvent)) return;
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      onSubmit();
    }
  };
  return (
    <div className={styles.editor}>
      <textarea
        ref={field}
        className={styles.textarea}
        aria-label={v.updateLabel}
        aria-describedby={state.problem ? "verify-problem" : "verify-hint"}
        aria-invalid={state.problem ? true : undefined}
        value={state.draft}
        rows={2}
        maxLength={2000}
        onChange={(e) => state.setDraft(e.target.value)}
        onKeyDown={onKeyDown}
      />
      {state.problem ? (
        <span id="verify-problem" className={styles.problem} role="alert">
          {state.problem}
        </span>
      ) : (
        <span id="verify-hint" className="mx-meta">
          {interpolate(v.updateHint, { key: keepKey })}
        </span>
      )}
    </div>
  );
}
