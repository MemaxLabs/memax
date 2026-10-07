"use client";

import { useId, type KeyboardEvent } from "react";
import { Button, Field } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, joinList, joinSentences } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import type { ForgetPreview, MemoryRecord } from "@/lib/v2/data/memories";
import { failureText } from "@/lib/v2/records-copy";
import { useForgetPreview } from "../../_lib/records";
import type { RecordsView } from "../records-view";
import type { MemoryForget } from "./use-memory-forget";
import styles from "./memory.module.css";

/** "Removes the words from Memax, 4 compiled files and 5 agents." */
export function removesText(
  copy: RecordsView["l"]["memory"]["page"]["forgetConfirm"],
  preview: ForgetPreview,
  locale: RecordsView["locale"],
): string {
  const where = [copy.memax];
  if (preview.files)
    where.push(count(copy.filesOne, copy.files, preview.files));
  if (preview.copies)
    where.push(count(copy.copiesOne, copy.copies, preview.copies));
  if (preview.agents)
    where.push(count(copy.agentsOne, copy.agents, preview.agents));
  return interpolate(copy.removes, { where: joinList(where, locale) });
}

/** "It takes M-0202 (cites it) with it." */
export function carriesText(
  copy: RecordsView["l"]["memory"]["page"]["forgetConfirm"],
  preview: ForgetPreview,
  locale: RecordsView["locale"],
): string | null {
  if (preview.carries.length === 0) return null;
  const list = preview.carries.map((c) =>
    interpolate(copy.carry, { ref: c.ref, why: copy.why[c.reason] }),
  );
  return interpolate(copy.carries, { list: joinList(list, locale) });
}

/**
 * The States board's inline confirmation: "Forget this everywhere?",
 * what it removes (from the server's preview, so the counts are what the
 * Forget will do), what goes with it, a note for the tombstone, then
 * Cancel (Esc) and Forget M-0201. Forget has no key; it is never the
 * default, so Cancel takes focus.
 */
export function ForgetConfirm({
  view,
  record,
  forget,
}: {
  view: RecordsView;
  record: MemoryRecord;
  forget: MemoryForget;
}) {
  const { l, rc, space, locale } = view;
  const copy = l.memory.page.forgetConfirm;
  const titleId = useId();
  const preview = useForgetPreview(space, record.ref, record.version, true);
  const p = preview.data;
  const failed = preview.isError ? toFailure(preview.error) : null;
  const refusal = p?.refusal
    ? failureText(
        rc,
        { kind: "refused", code: p.refusal.code, message: p.refusal.message },
        { command: "forget", ref: record.ref, space: space.name, locale },
      )
    : failed
      ? failureText(rc, failed, {
          command: "forget",
          ref: record.ref,
          space: space.name,
          locale,
        })
      : null;
  const detail = p
    ? joinSentences(
        [
          removesText(copy, p, locale),
          carriesText(copy, p, locale),
          copy.after,
        ],
        locale,
      )
    : failed
      ? copy.after
      : copy.checking;

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      forget.cancel();
    }
  };

  return (
    <div
      className={styles.forgetConfirm}
      role="group"
      aria-labelledby={titleId}
      onKeyDown={onKeyDown}
    >
      <div className={styles.forgetText}>
        <strong id={titleId} className={styles.forgetTitle}>
          {copy.title}
        </strong>
        <span className={styles.forgetDetail} aria-live="polite">
          {detail}
        </span>
        {refusal ? (
          <span className={styles.forgetRefusal} role="alert">
            {refusal}
          </span>
        ) : null}
      </div>
      {p && !refusal ? (
        <Field
          className={styles.forgetNote}
          label={copy.note}
          hint={copy.noteHint}
          value={forget.note}
          maxLength={500}
          onChange={(e) => forget.setNote(e.target.value)}
        />
      ) : null}
      <div className={styles.forgetButtons}>
        <Button
          variant="quiet"
          size="sm"
          kbd="Esc"
          autoFocus
          onClick={forget.cancel}
        >
          {copy.cancel}
        </Button>
        <Button
          variant="danger"
          size="sm"
          icon="forget"
          pending={forget.pending}
          disabled={!p || Boolean(refusal)}
          disabledReason={refusal ?? (p ? undefined : copy.checking)}
          onClick={() => p && void forget.submit(p)}
        >
          {interpolate(forget.pending ? copy.forgetting : copy.submit, {
            ref: record.ref,
          })}
        </Button>
      </div>
    </div>
  );
}
