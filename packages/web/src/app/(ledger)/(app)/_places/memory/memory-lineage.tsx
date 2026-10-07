"use client";

import { useState } from "react";
import {
  Button,
  Lineage,
  MemoryList,
  MemoryRow,
  type LineageEvent,
  type MarkState,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatShortDate } from "@/lib/v2/copy";
import type {
  FoldUndo,
  LineageEntry,
  MemoryRecord,
} from "@/lib/v2/data/memories";
import { isYou } from "@/lib/v2/records-copy";
import { StatementText } from "../../_components/statement-text";
import { useUndo } from "../../_lib/undo";
import type { RecordsView } from "../records-view";
import styles from "./memory.module.css";

/** The mark a lineage line wears; handing off and checking are plain dots. */
const MARKS: Partial<Record<LineageEntry["action"], MarkState>> = {
  proposed: "proposed",
  kept: "kept",
  merged: "merged",
  faded: "faded",
  forgot: "forgotten",
  restored: "kept",
  returned: "conflict",
};

/** A flag's line wears the flag it set: stale, or a conflict. */
function markOf(entry: LineageEntry): MarkState | undefined {
  if (entry.action === "flagged") return entry.flag?.kind;
  return MARKS[entry.action];
}

function title(view: RecordsView, entry: LineageEntry): string {
  const t = view.l.memory.page.titles;
  const name = view.name(entry.by);
  const you = isYou(entry.by);
  switch (entry.action) {
    case "proposed":
      return interpolate(t.proposed, { name });
    case "kept":
      return you ? t.keptYou : interpolate(t.kept, { name });
    case "edited":
      return you ? t.editedYou : interpolate(t.edited, { name });
    case "merged":
      if (entry.into) return interpolate(t.folded, { name, ref: entry.into });
      return entry.count === null
        ? interpolate(t.mergedSome, { name })
        : count(t.mergedOne, t.merged, entry.count, { name });
    case "handed_off":
      return entry.to
        ? interpolate(t.handedTo, {
            agent: view.agentName(entry.to.agent),
            ref: entry.to.ref,
          })
        : interpolate(t.handedOff, { name });
    case "flagged":
      // Which flag it set: a stale fact, or a conflict (with the memory
      // it contradicts, when the receipt names it).
      switch (entry.flag?.kind) {
        case "stale":
          return interpolate(t.flagged, { name });
        case "conflict":
          return entry.flag.with
            ? interpolate(t.flaggedConflict, { name, ref: entry.flag.with })
            : interpolate(t.flaggedConflictBare, { name });
        default:
          return interpolate(t.flaggedBare, { name });
      }
    case "verified":
      return t.verified;
    case "faded":
      return t.faded;
    case "compiled":
      return t.compiled;
    default:
      return interpolate(t[entry.action], { name });
  }
}

const SHOWN_NOTES = 2;

/** Memory.png's left column: the lineage, oldest first, and the notes merged into it. */
export function MemoryLineage({
  view,
  record,
}: {
  view: RecordsView;
  record: MemoryRecord;
}) {
  const p = view.l.memory.page;
  const [allNotes, setAllNotes] = useState(false);
  const undo = useUndo();
  // Anyone who may keep can undo one of the judge's folds, for 14 days.
  const canUnfold = view.space.role !== "viewer";
  const unfold = (ref: string, fold: FoldUndo) => (
    <span className={styles.unfold}>
      <span className="mx-meta">
        {interpolate(p.foldedUntil, {
          date: formatShortDate(
            new Date(fold.until),
            view.timeZone,
            view.locale,
          ),
        })}
      </span>
      <Button
        variant="quiet"
        size="sm"
        aria-label={interpolate(p.unfoldFor, { ref })}
        onClick={() => void undo.unfold(view.space, ref, fold.receipt)}
      >
        {p.unfold}
      </Button>
    </span>
  );
  const events: LineageEvent[] = record.lineage.map((entry) => {
    const stamp = view.stamp(entry.by);
    const fold = canUnfold && entry.undo ? entry.undo : null;
    return {
      key: entry.key,
      state: markOf(entry),
      agent: stamp?.agent,
      person: stamp?.person,
      title: title(view, entry),
      time: view.dateTime(entry.at),
      dateTime: entry.at,
      detail: fold ? (
        <>
          {entry.detail ? <span>{entry.detail} </span> : null}
          {unfold(record.ref, fold)}
        </>
      ) : (
        (entry.detail ?? undefined)
      ),
    };
  });
  const merged = record.merged;
  const notes = merged
    ? allNotes
      ? merged.notes
      : merged.notes.slice(0, SHOWN_NOTES)
    : [];
  const hidden = merged ? merged.total - SHOWN_NOTES : 0;

  return (
    <section className={styles.main}>
      <h2 className="mx-section-label">{p.lineage}</h2>
      <Lineage events={events} />
      {merged && merged.notes.length > 0 ? (
        <>
          <h2 className={`mx-section-label ${styles.mergedLabel}`}>
            {p.mergedInto}
          </h2>
          <div className="mx-panel">
            <MemoryList>
              {notes.map((note) => {
                const stamp = view.stamp(note.by);
                const fold = canUnfold && note.undo ? note.undo : null;
                return (
                  <MemoryRow
                    key={note.ref}
                    compact
                    state="merged"
                    {...(stamp ?? {})}
                    action={view.rc.verbs.merged}
                    time={view.time(note.at)}
                    id={note.ref}
                    actions={
                      fold ? (
                        <Button
                          variant="secondary"
                          size="sm"
                          aria-label={interpolate(p.unfoldFor, {
                            ref: note.ref,
                          })}
                          title={interpolate(p.foldedUntil, {
                            date: formatShortDate(
                              new Date(fold.until),
                              view.timeZone,
                              view.locale,
                            ),
                          })}
                          onClick={() =>
                            void undo.unfold(view.space, note.ref, fold.receipt)
                          }
                        >
                          {p.unfold}
                        </Button>
                      ) : undefined
                    }
                  >
                    <StatementText text={note.statement} />
                  </MemoryRow>
                );
              })}
            </MemoryList>
            {hidden > 0 && merged.notes.length > SHOWN_NOTES ? (
              <div className={styles.more}>
                <Button
                  size="sm"
                  variant="quiet"
                  icon="chevron-down"
                  aria-expanded={allNotes}
                  onClick={() => setAllNotes((open) => !open)}
                >
                  {allNotes
                    ? p.fewerNotes
                    : count(p.moreNotesOne, p.moreNotes, hidden)}
                </Button>
              </div>
            ) : null}
          </div>
        </>
      ) : null}
    </section>
  );
}
