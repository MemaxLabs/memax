"use client";

import { AgentStamp, Button, MemoryText, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { ReviewItem } from "@/lib/v2/data/review";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./review.module.css";

/**
 * A kept memory in the queue because it was flagged: stale (Dream saw
 * its source change) or in conflict. It isn't a proposal, so there's
 * nothing to keep: stale facts are verified (V, from the memory's page,
 * once Verify lands) and conflicts are compared (C).
 */
export function FlagCard({
  view,
  item,
  compareHref,
}: {
  view: RecordsView;
  item: ReviewItem;
  compareHref: string | null;
}) {
  const { l, space } = view;
  const f = l.review.flag;
  const stamp = view.stamp(item.by);
  const stale = item.state === "stale";
  const when = view.time(item.at);
  const memoryHref = `/${encodeURIComponent(space.slug)}/memories/${encodeURIComponent(item.ref)}`;
  return (
    <article className="mx-review is-focused" tabIndex={-1}>
      <header className="mx-review-head">
        <StateMark
          state={stale ? "stale" : "conflict"}
          label={stale ? f.stale : f.conflict}
        />
        <span className="mx-review-by">
          {stamp ? <AgentStamp {...stamp} size="sm" decorative /> : null}
          <span>{view.name(item.by)}</span>
          <span className="mx-meta">· {when}</span>
        </span>
      </header>
      <MemoryText state={stale ? "stale" : "conflict"} size="lg">
        <StatementText text={item.statement} />
      </MemoryText>
      <p className={styles.flagNote}>
        {stale
          ? interpolate(f.staleDetail, { name: view.name(item.by), when })
          : f.conflictDetail}
      </p>
      <footer className="mx-review-foot">
        <span className="mx-receipt">{item.ref}</span>
        <div className="mx-review-actions">
          <Button variant="quiet" size="sm" href={memoryHref}>
            {f.open}
          </Button>
          {stale ? (
            <Button
              variant="secondary"
              size="sm"
              kbd="V"
              disabled
              disabledReason={f.verifyLater}
            >
              {f.verify}
            </Button>
          ) : compareHref ? (
            <Button variant="secondary" size="sm" kbd="C" href={compareHref}>
              {l.review.compareBoth}
            </Button>
          ) : null}
        </div>
      </footer>
    </article>
  );
}
