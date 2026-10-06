"use client";

import { Button, Receipt, Redaction } from "@memaxlabs/ledger";
import { formatShortDate } from "@/lib/v2/copy";
import type { MemoryRecord } from "@/lib/v2/data/memories";
import { useAriaKeys, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { memoryReadsText, reachText } from "@/lib/v2/reads-copy";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./memory.module.css";

/**
 * Memory.png's head: the statement (its type carries its state), the
 * receipt with reads and reach, and the actions. Move to space and
 * Forget are drawn with why they aren't available yet; Forget never
 * has a key.
 */
export function MemoryHead({
  view,
  record,
  onEdit,
  onCite,
}: {
  view: RecordsView;
  record: MemoryRecord;
  onEdit: () => void;
  onCite: () => void;
}) {
  const { l, rc, space, timeZone, locale } = view;
  const p = l.memory.page;
  const editKey = useKeycap("memory.edit");
  const citeKeys = useAriaKeys("memory.cite");
  const forgotten = record.forgotten;
  // The Keep the seal is for, or the latest receipt while it isn't kept.
  const receipt = record.kept
    ? { ...record.kept, action: "kept" as const }
    : record.latest;
  const stamp = receipt ? view.stamp(receipt.by) : null;
  const meta = [
    memoryReadsText(p, record, locale),
    reachText(p, record.reach, locale),
  ]
    .filter(Boolean)
    .join(" · ");
  const stateClass =
    record.state === "proposed" || record.state === "conflict"
      ? styles.isProposed
      : record.state === "stale"
        ? styles.isStale
        : record.state === "merged"
          ? styles.isMerged
          : "";

  return (
    <>
      {forgotten ? (
        <Redaction
          as="div"
          className={styles.tombstone}
          date={formatShortDate(new Date(forgotten.at), timeZone, locale)}
          by={forgotten.by ?? undefined}
          id={record.ref}
          detail={forgotten.detail ?? undefined}
        />
      ) : (
        <h1 className={`${styles.title} ${stateClass}`}>
          <StatementText text={record.statement} />
        </h1>
      )}
      <div className={styles.meta}>
        {receipt ? (
          <Receipt
            {...(stamp ?? {})}
            action={rc.verbs[receipt.action]}
            time={view.dateTime(receipt.at)}
            id={record.ref}
          />
        ) : null}
        {meta ? (
          <span
            className={`mx-meta ${styles.prose}`}
            title={record.readsUnobserved ? p.readsUnobserved : undefined}
          >
            {meta}
          </span>
        ) : null}
      </div>
      {record.lifecycle === "proposed" ? (
        <p className={styles.waiting}>
          {p.proposed}
          <Button
            variant="quiet"
            size="sm"
            href={placeHref(space.slug, "review")}
          >
            {p.openReview}
          </Button>
        </p>
      ) : null}
      {forgotten ? (
        <p className={styles.waiting}>{p.forgotten}</p>
      ) : (
        <div className={`mx-inline ${styles.actions}`}>
          <Button
            variant="secondary"
            icon="pencil"
            kbd={editKey}
            onClick={onEdit}
          >
            {p.edit}
          </Button>
          <Button
            variant="secondary"
            icon="space"
            disabled
            disabledReason={p.moveLater}
          >
            {p.move}
          </Button>
          {/* Memory.png draws no keycap here; ⌘⇧C is on the ? sheet. */}
          <Button
            variant="quiet"
            icon="link"
            aria-keyshortcuts={citeKeys}
            onClick={onCite}
          >
            {p.cite}
          </Button>
          <span className={styles.spacer} />
          {/* Forget has no key, on purpose (HANDOFF §7). */}
          <Button
            variant="danger"
            icon="forget"
            disabled
            disabledReason={p.forgetLater}
          >
            {p.forget}
          </Button>
        </div>
      )}
    </>
  );
}
