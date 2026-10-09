"use client";

import { MemoryList, MemoryRow } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count } from "@/lib/v2/copy";
import {
  driftAgent,
  type DriftChangeView,
  type DriftItemView,
  type TargetView,
} from "@/lib/v2/data/targets";
import { StatementText } from "../../_components/statement-text";
import type { RecordsView } from "../records-view";
import styles from "./drift.module.css";

/**
 * "What the edit says" (DriftResolve.png): each change the compiler read
 * back, as the proposal a pull would write. An edited cited line would
 * update its memory, a new line would be a new memory, and a removed
 * cited line is a proposal to forget or exclude it, never an automatic
 * forget.
 */
export function DriftChanges({
  view,
  target,
  items,
}: {
  view: RecordsView;
  target: TargetView;
  items: DriftItemView[];
}) {
  const { l } = view;
  const r = l.brief.resolve;
  const proposals = items
    .flatMap((i) => i.changes)
    .filter((c) => c.kind !== "remove").length;
  const hidden = items.reduce((sum, i) => sum + i.hiddenCharacters, 0);
  // The tool the file belongs to signs the hand edit; AGENTS.md has none.
  const agent = driftAgent(target.kind) ?? "repository";

  return (
    <section className="mx-panel" aria-labelledby="drift-says">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="drift-says">
          {r.editSays}
        </h2>
        <span className="mx-meta">
          {count(r.proposalsOne, r.proposals, proposals)}
        </span>
      </header>
      <MemoryList>
        {items.flatMap((item) =>
          item.changes.map((change, i) => (
            <MemoryRow
              key={`${item.observationId}-${i}`}
              state="proposed"
              agent={agent}
              action={r.handEdit}
              time={view.time(item.observedAt)}
              source={
                item.commit
                  ? interpolate(r.commit, { commit: item.commit })
                  : item.path
              }
              note={noteOf(r, change)}
            >
              <StatementText text={statementOf(change)} />
            </MemoryRow>
          )),
        )}
      </MemoryList>
      {hidden > 0 ? (
        <p className={styles.note}>{count(r.hiddenOne, r.hidden, hidden)}</p>
      ) : null}
    </section>
  );
}

function statementOf(change: DriftChangeView): string {
  switch (change.kind) {
    case "edit":
      return change.newText;
    case "new":
      return change.text;
    case "remove":
      return change.oldText;
  }
}

function noteOf(
  r: RecordsView["l"]["brief"]["resolve"],
  change: DriftChangeView,
): string | undefined {
  switch (change.kind) {
    case "edit":
      return interpolate(r.wouldUpdate, { ref: change.ref });
    case "remove":
      return interpolate(r.wouldRemove, { ref: change.ref });
    case "new":
      return undefined;
  }
}
