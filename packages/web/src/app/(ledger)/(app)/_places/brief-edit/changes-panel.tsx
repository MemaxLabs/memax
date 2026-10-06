"use client";

import { Diff } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, joinSentences, spell } from "@/lib/v2/copy";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import type { EditorChange } from "./editor-state";
import styles from "./brief-edit.module.css";

/** What a change's first line says: "Edited · M-0102", "Moved · M-0112 to Decisions". */
function changeLabel(
  k: RecordsView["l"]["brief"]["edit"]["kinds"],
  change: EditorChange,
): string {
  switch (change.kind) {
    case "edited":
      return interpolate(k.edited, { ref: change.ref });
    case "prose":
      return interpolate(k.prose, { section: change.section });
    case "moved":
      return interpolate(k.moved, {
        ref: change.ref ?? "",
        section: change.section,
      });
    case "added":
      return interpolate(k.added, { section: change.section });
    case "removed":
      return interpolate(k.removed, { ref: change.ref ?? "" });
    case "reordered":
      return interpolate(k.reordered, { section: change.section });
    case "renamed":
      return interpolate(k.renamed, { from: change.from, to: change.to });
    case "section":
      return interpolate(k.section, { section: change.section });
    case "cited":
      return interpolate(k.cited, { ref: change.ref });
    case "uncited":
      return interpolate(k.uncited, { ref: change.ref });
  }
}

/**
 * BriefEdit.png's Changes panel: everything Done will keep, one line
 * each with its words (a word diff for an edit), then what Done does.
 */
export function ChangesPanel({
  view,
  changes,
  files,
}: {
  view: RecordsView;
  changes: EditorChange[];
  /** Files a revision recompiles. */
  files: number;
}) {
  const { l, copy, locale } = view;
  const e = l.brief.edit;
  const note =
    changes.length > 0
      ? joinSentences(
          [
            count(e.doneNoteOne, e.doneNote, changes.length, {
              n: spell(copy, changes.length),
              files: count(e.filesOne, e.files, files),
            }),
            e.dreamNote,
          ],
          locale,
        )
      : null;
  return (
    <>
      <section className="mx-panel" aria-labelledby="brief-changes">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title" id="brief-changes">
            {e.changes} <span className="mx-meta">{changes.length}</span>
          </h2>
        </header>
        {changes.length === 0 ? (
          <p className={styles.empty}>{e.noChanges}</p>
        ) : (
          changes.map((change) => (
            <div key={change.key} className={styles.change}>
              <span className="mx-meta">{changeLabel(e.kinds, change)}</span>
              {change.kind === "edited" || change.kind === "prose" ? (
                <p>
                  <Diff before={change.before} after={change.after} />
                </p>
              ) : "text" in change && change.text ? (
                <p>
                  <StatementText text={change.text} />
                </p>
              ) : null}
            </div>
          ))
        )}
      </section>
      {note ? <p className={`mx-meta ${styles.foot}`}>{note}</p> : null}
    </>
  );
}
