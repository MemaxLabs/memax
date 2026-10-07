"use client";

import {
  useEffect,
  useId,
  useRef,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { Button, Diff, Field, StateMark } from "@memaxlabs/ledger";
import { isComposing } from "@/lib/v2/keymap/keymap";
import styles from "./statement-editor.module.css";

/**
 * Editing a statement (ReviewEdit.png): the words in the serif, the
 * word diff against what it was, an optional reason for the receipt,
 * and Cancel (Esc) / Keep edited (⌘↵). The keys are the form's own, so
 * nothing fires while an IME composes, and single-key shortcuts never
 * reach the page while typing (the keymap ignores text fields).
 */
export function StatementEditor({
  stateLabel,
  by,
  base,
  draft,
  reason,
  onDraft,
  onReason,
  onCancel,
  onSubmit,
  labels,
  receipt,
  pending,
  error,
  submitKey,
}: {
  /** The header's state word ("Editing a proposal"). */
  stateLabel: string;
  /** Who it's from, beside the state (stamp, name, time). */
  by?: ReactNode;
  /** What the diff compares against. */
  base: string;
  draft: string;
  reason: string;
  onDraft: (value: string) => void;
  onReason: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
  labels: {
    statement: string;
    diff: string;
    why: string;
    whyHint: string;
    cancel: string;
    submit: string;
  };
  /** The receipt the edit will leave, in the footer. */
  receipt: ReactNode;
  pending: boolean;
  error: string | null;
  /** The submit chord's keycap ("⌘↵"). */
  submitKey: string;
}) {
  const id = useId();
  const field = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const el = field.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (isComposing(event.nativeEvent)) return;
    if (event.key === "Escape") {
      event.preventDefault();
      onCancel();
      return;
    }
    if (event.key !== "Enter") return;
    const chord = event.metaKey || event.ctrlKey;
    // ⌘↵ anywhere; a plain ↵ in the one-line reason too.
    if (chord || (event.target as HTMLElement).tagName === "INPUT") {
      event.preventDefault();
      onSubmit();
    }
  };

  const changed = draft.trim() !== base.trim();
  return (
    <article
      className={styles.editor}
      onKeyDown={onKeyDown}
      aria-busy={pending || undefined}
    >
      <header className={styles.head}>
        <StateMark state="proposed" label={stateLabel} />
        {by ? <span className={styles.by}>{by}</span> : null}
      </header>
      <div className={styles.field}>
        <label className={styles.label} htmlFor={`${id}-stmt`}>
          {labels.statement}
        </label>
        <textarea
          ref={field}
          id={`${id}-stmt`}
          className={styles.textarea}
          rows={3}
          value={draft}
          onChange={(event) => onDraft(event.target.value)}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${id}-err` : undefined}
          readOnly={pending}
        />
        {error ? (
          <p id={`${id}-err`} className={styles.error} role="alert">
            {error}
          </p>
        ) : null}
      </div>
      {changed ? (
        <div className={styles.diff}>
          <span className="mx-meta">{labels.diff}</span>
          <span className={styles.diffText}>
            <Diff before={base} after={draft} />
          </span>
        </div>
      ) : null}
      <Field
        label={labels.why}
        hint={labels.whyHint}
        value={reason}
        onChange={(event) => onReason(event.target.value)}
        readOnly={pending}
      />
      <footer className={styles.foot}>
        {receipt}
        <span className={styles.spacer} />
        <Button variant="quiet" size="sm" kbd="Esc" onClick={onCancel}>
          {labels.cancel}
        </Button>
        <Button
          variant="keep"
          size="sm"
          kbd={submitKey}
          onClick={onSubmit}
          pending={pending}
        >
          {labels.submit}
        </Button>
      </footer>
    </article>
  );
}
