"use client";

import { Receipt, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate } from "@/lib/v2/copy";
import type { ConflictData } from "@/lib/v2/data/review";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./compare.module.css";

/** ReviewConflict.png's two cards: what's kept and in force, and the proposal that contradicts it. */
export function ConflictSides({
  view,
  conflict,
}: {
  view: RecordsView;
  conflict: ConflictData;
}) {
  const { l, rc, timeZone, locale } = view;
  const c = l.review.compare;
  const { kept, proposal } = conflict;
  const date = (iso: string) =>
    formatShortDate(new Date(iso), timeZone, locale);
  const keptStamp = view.stamp(kept.by);
  const proposalStamp = view.stamp(proposal.by);
  return (
    <div className={styles.sides}>
      <article className={styles.side}>
        <StateMark
          state="kept"
          label={interpolate(c.keptSide, { date: date(kept.at) })}
        />
        <p className={styles.statement}>
          <StatementText text={kept.statement} />
        </p>
        <Receipt
          {...(keptStamp ?? {})}
          action={rc.verbs.kept}
          time={view.time(kept.at)}
          id={kept.ref}
        />
        <dl className={styles.kv}>
          {kept.why ? (
            <>
              <dt>{c.rows.why}</dt>
              <dd>{kept.why}</dd>
            </>
          ) : null}
          {kept.source ? (
            <>
              <dt>{c.rows.source}</dt>
              <dd>{kept.source}</dd>
            </>
          ) : null}
          {kept.reaches ? (
            <>
              <dt>{c.rows.reaches}</dt>
              <dd>
                {interpolate(c.reaches, {
                  files: count(c.filesOne, c.files, kept.reaches.files),
                  reads: kept.reaches.reads,
                })}
              </dd>
            </>
          ) : null}
        </dl>
      </article>
      <article className={`${styles.side} ${styles.proposalSide}`}>
        <StateMark
          state="conflict"
          label={interpolate(c.proposedSide, {
            agent: view.name(proposal.by),
            ref: kept.ref,
          })}
        />
        <p className={styles.statement}>
          <StatementText text={proposal.statement} />
        </p>
        <Receipt
          {...(proposalStamp ?? {})}
          action={rc.verbs.proposed}
          time={view.time(proposal.at)}
          id={proposal.ref}
        />
        <dl className={styles.kv}>
          {proposal.why ? (
            <>
              <dt>{c.rows.why}</dt>
              <dd>{proposal.why}</dd>
            </>
          ) : null}
          {proposal.evidence ? (
            <>
              <dt>{c.rows.evidence}</dt>
              <dd>
                <code className="mx-code">{proposal.evidence.code}</code>
                {interpolate(c.changed, {
                  date: date(proposal.evidence.changedAt),
                })}
              </dd>
            </>
          ) : null}
          {proposal.session ? (
            <>
              <dt>{c.rows.session}</dt>
              <dd>{proposal.session}</dd>
            </>
          ) : null}
        </dl>
      </article>
    </div>
  );
}
