"use client";

import { MemoryList, MemoryRow } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count } from "@/lib/v2/copy";
import type { ReviewCardData, ReviewItem } from "@/lib/v2/data/review";
import { StatementText } from "../../_components/statement-text";
import { TargetRow } from "../../_components/target-row";
import type { RecordsView } from "../records-view";
import styles from "./review.module.css";

/**
 * "This touches" (Review.png) under the card: the kept memories it
 * relates to, then "Keeping recompiles" with the compiled files. While
 * editing an update it becomes "This replaces" (ReviewEdit.png) with
 * the one memory it changes. Which memories are related is the
 * source's call (`touches.basis`): the memory an update replaces, or,
 * with no better signal from /v2 yet, kept memories in the same section.
 */
export function Touches({
  view,
  item,
  card,
  editing,
}: {
  view: RecordsView;
  item: ReviewItem;
  card: ReviewCardData | undefined;
  editing: boolean;
}) {
  const { l, space } = view;
  const t = l.review.touches;
  if (!card) return null;
  const replaces =
    editing && item.updates !== null && card.touches.basis === "links";
  const memories = replaces
    ? card.touches.memories.filter((m) => m.ref === item.updates)
    : card.touches.memories;
  const targets = replaces ? null : card.touches.targets;
  const meta = [
    count(t.memoriesOne, t.memories, memories.length),
    targets ? count(t.filesOne, t.files, targets.length) : null,
  ]
    .filter(Boolean)
    .join(" · ");
  const memoryHref = (ref: string) =>
    `/${encodeURIComponent(space.slug)}/memories/${encodeURIComponent(ref)}`;

  return (
    <section className="mx-panel" aria-labelledby={`touches-${item.ref}`}>
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id={`touches-${item.ref}`}>
          {replaces ? t.replaces : t.title}
        </h2>
        <span className="mx-meta">{meta}</span>
      </header>
      {memories.length > 0 ? (
        <MemoryList>
          {memories.map((m) => {
            const stamp = view.stamp(m.by);
            return (
              <MemoryRow
                key={m.ref}
                compact
                {...(stamp ?? {})}
                action={l.records.verbs.kept}
                time={view.time(m.at)}
                id={m.ref}
                href={memoryHref(m.ref)}
                note={
                  replaces && card.touches.replacesOnKeep
                    ? interpolate(t.becomesMerged, { ref: item.ref })
                    : undefined
                }
              >
                <StatementText text={m.statement} />
              </MemoryRow>
            );
          })}
        </MemoryList>
      ) : (
        <p className={styles.panelNote}>{t.nothing}</p>
      )}
      {replaces ? null : (
        <>
          <div className={styles.recompiles}>
            <p className="mx-section-label">{t.recompiles}</p>
          </div>
          {targets?.length ? (
            targets.map((target) => (
              <TargetRow key={target.id} space={space.slug} target={target} />
            ))
          ) : (
            // Nothing compiles yet, or the targets didn't load.
            <p className={styles.panelNote}>{t.targetsLater}</p>
          )}
        </>
      )}
    </section>
  );
}
