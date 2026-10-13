"use client";

import { MemoryList, MemoryRow } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count } from "@/lib/v2/copy";
import type { ReviewCardData, ReviewItem } from "@/lib/v2/data/review";
import { groupTargets } from "@/lib/v2/data/home-files";
import { isHome } from "@/lib/v2/data/targets";
import { StatementText } from "../../_components/statement-text";
import { TargetList } from "../../_components/target-list";
import { useSpaces } from "../../_lib/data";
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
  // A proposal going into another space recompiles that space's files.
  const spaces = useSpaces().data;
  const into =
    (item.intoSpace && spaces?.find((s) => s.name === item.intoSpace)) || space;
  if (!card) return null;
  const replaces =
    editing && item.updates !== null && card.touches.basis === "links";
  const memories = replaces
    ? card.touches.memories.filter((m) => m.ref === item.updates)
    : card.touches.memories;
  const targets = replaces ? null : card.touches.targets;
  const meta = [
    count(t.memoriesOne, t.memories, memories.length),
    // A home file counts once, whatever the number of machines it's on.
    targets ? count(t.filesOne, t.files, groupTargets(targets).length) : null,
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
            <TargetList space={into} targets={targets} />
          ) : (
            // Nothing compiles yet, or the targets didn't load.
            <p className={styles.panelNote}>{t.targetsLater}</p>
          )}
          {/* D8: a keep in Personal reaches every agent on every machine. */}
          {into.kind === "personal" && targets?.some(isHome) ? (
            <p className={styles.panelNote}>{l.machines.reach.keep}</p>
          ) : null}
        </>
      )}
    </section>
  );
}
