"use client";

import { AgentStamp, Button, Diff } from "@memaxlabs/ledger";
import styles from "./edit-clash.module.css";

/**
 * Someone kept a change while you were editing (States2 "Edit clash"):
 * who and when, their change as a word diff from the version you
 * started from, yours under it, and three ways on: Keep theirs, Combine
 * both (edit again from theirs), Keep mine (your words on their version).
 */
export function EditClash({
  title,
  stamp,
  before,
  theirs,
  yours,
  labels,
  onKeepTheirs,
  onCombine,
  onKeepMine,
  pending,
}: {
  /** "Jiahao kept a change to this fact 1 minute ago." */
  title: string;
  stamp: { person?: string; agent?: string; name?: string } | null;
  /** The words you started editing from. */
  before: string;
  theirs: string;
  /** "Yours: “…”" */
  yours: string;
  labels: {
    region: string;
    keepTheirs: string;
    combine: string;
    keepMine: string;
  };
  onKeepTheirs: () => void;
  onCombine: () => void;
  onKeepMine: () => void;
  pending: boolean;
}) {
  return (
    <section
      className={`mx-panel ${styles.clash}`}
      aria-label={labels.region}
      role="alert"
    >
      <div className={styles.body}>
        <p className={styles.title}>
          {stamp ? <AgentStamp {...stamp} size="sm" decorative /> : null}
          <b>{title}</b>
        </p>
        <p className={styles.diff}>
          <Diff before={before} after={theirs} />
        </p>
        <span className={`mx-meta ${styles.yours}`}>{yours}</span>
      </div>
      <div className={styles.actions}>
        <Button
          variant="quiet"
          size="sm"
          onClick={onKeepTheirs}
          disabled={pending}
        >
          {labels.keepTheirs}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={onCombine}
          disabled={pending}
        >
          {labels.combine}
        </Button>
        <Button variant="keep" size="sm" onClick={onKeepMine} pending={pending}>
          {labels.keepMine}
        </Button>
      </div>
    </section>
  );
}
