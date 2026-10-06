"use client";

import type { KeyboardEvent } from "react";
import { Button, Field } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { ReviewItem } from "@/lib/v2/data/review";
import { isComposing } from "@/lib/v2/keymap/keymap";
import type { RecordsView } from "../records-view";
import styles from "./review.module.css";

/**
 * X: reject, with an optional reason that goes into the receipt. The
 * field takes focus, so ↵ rejects and Esc goes back to the card; the
 * keys are the field's own, and never fire while an IME composes.
 */
export function RejectPanel({
  view,
  item,
  reason,
  pending,
  onReason,
  onReject,
  onCancel,
}: {
  view: RecordsView;
  item: ReviewItem;
  reason: string;
  pending: boolean;
  onReason: (reason: string) => void;
  onReject: () => void;
  onCancel: () => void;
}) {
  const r = view.l.review.reject;
  const title = interpolate(r.title, { ref: item.ref });
  const hint =
    item.by?.kind === "agent" || item.by?.kind === "dream"
      ? interpolate(r.hint, { agent: view.name(item.by) })
      : r.hintPerson;
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (isComposing(event.nativeEvent)) return;
    if (event.key === "Enter") {
      event.preventDefault();
      onReject();
    } else if (event.key === "Escape") {
      event.preventDefault();
      onCancel();
    }
  };
  return (
    <section className={styles.reject} aria-label={title}>
      <p className={styles.rejectTitle}>{title}</p>
      <Field
        label={r.why}
        hint={hint}
        value={reason}
        onChange={(event) => onReason(event.target.value)}
        onKeyDown={onKeyDown}
        readOnly={pending}
        autoFocus
      />
      <div className={styles.rejectActions}>
        <Button variant="quiet" size="sm" kbd="Esc" onClick={onCancel}>
          {r.cancel}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          kbd="↵"
          onClick={onReject}
          pending={pending}
        >
          {r.confirm}
        </Button>
      </div>
    </section>
  );
}
