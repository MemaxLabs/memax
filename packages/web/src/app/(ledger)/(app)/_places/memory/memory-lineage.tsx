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
import { count } from "@/lib/v2/copy";
import type { LineageEntry, MemoryRecord } from "@/lib/v2/data/memories";
import { isYou } from "@/lib/v2/records-copy";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./memory.module.css";

/** The mark a lineage line wears; handing off and checking are plain dots. */
const MARKS: Partial<Record<LineageEntry["action"], MarkState>> = {
  proposed: "proposed",
  kept: "kept",
  merged: "merged",
  flagged: "stale",
  faded: "faded",
  forgot: "forgotten",
  restored: "kept",
};

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
  const events: LineageEvent[] = record.lineage.map((entry) => {
    const stamp = view.stamp(entry.by);
    return {
      key: entry.key,
      state: MARKS[entry.action],
      agent: stamp?.agent,
      person: stamp?.person,
      title: title(view, entry),
      time: view.dateTime(entry.at),
      dateTime: entry.at,
      detail: entry.detail ?? undefined,
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
                return (
                  <MemoryRow
                    key={note.ref}
                    compact
                    state="merged"
                    {...(stamp ?? {})}
                    action={view.rc.verbs.merged}
                    time={view.time(note.at)}
                    id={note.ref}
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
