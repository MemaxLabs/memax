"use client";

import { useState } from "react";
import Link from "next/link";
import { AgentStamp, Button } from "@memaxlabs/ledger";
import { driftNotice } from "@/lib/v2/brief-copy";
import { count } from "@/lib/v2/copy";
import type { BriefView } from "@/lib/v2/data/brief";
import type { TargetView } from "@/lib/v2/data/targets";
import { targetHref, TargetRow } from "../../_components/target-row";
import { useDrift } from "../../_lib/compile";
import { useDriftActions } from "../drift/use-drift-actions";
import type { RecordsView } from "../records-view";
import styles from "./brief.module.css";

/** Brief.png's side: Compiled to (with the drift notice), Read this week and Sources. */
export function BriefSide({
  view,
  brief,
  targets,
  numbers,
  onCompile,
  compiling,
}: {
  view: RecordsView;
  brief: BriefView;
  targets: TargetView[] | undefined;
  numbers: ReadonlyMap<string, number>;
  onCompile: () => void;
  compiling: boolean;
}) {
  const { l, space } = view;
  const s = l.brief.side;
  const drifted = targets?.find((t) => t.syncState === "drifted");
  const canCompile = space.role !== "viewer";
  const sources = [...numbers.entries()];

  return (
    <aside className={styles.side}>
      <section className="mx-panel" aria-labelledby="brief-compiled-to">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title" id="brief-compiled-to">
            {s.compiledTo}
          </h2>
          <Button
            variant="quiet"
            size="sm"
            icon="sync"
            aria-label={s.compileLabel}
            onClick={onCompile}
            pending={compiling}
            disabled={!canCompile || !targets?.length}
            disabledReason={!canCompile ? l.brief.page.editViewer : undefined}
          >
            {s.compile}
          </Button>
        </header>
        {targets?.length ? (
          targets.map((target) => (
            <TargetRow key={target.id} space={space.slug} target={target} />
          ))
        ) : (
          <p className={styles.panelNote}>{s.noTargets}</p>
        )}
        {drifted ? <DriftNotice view={view} target={drifted} /> : null}
      </section>

      <section className="mx-panel" aria-labelledby="brief-reads">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title" id="brief-reads">
            {s.readThisWeek}
          </h2>
          {brief.reads ? (
            <span className="mx-meta">
              {count(s.readsOne, s.reads, brief.reads.total)}
            </span>
          ) : null}
        </header>
        {brief.reads ? (
          <div className={styles.reads}>
            {brief.reads.agents.map((row) => (
              <div key={row.agent} className={styles.read}>
                <AgentStamp agent={row.agent} showName />
                <b>{row.reads}</b>
              </div>
            ))}
          </div>
        ) : (
          // PLACEHOLDER: reads aren't recorded yet.
          <p className={styles.panelNote}>{s.readsLater}</p>
        )}
      </section>

      <section className="mx-panel" aria-labelledby="brief-sources">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title" id="brief-sources">
            {s.sources}
          </h2>
        </header>
        {sources.length > 0 ? (
          <ol className={styles.sources}>
            {sources.map(([ref]) => (
              <li key={ref}>
                <span className={styles.sourceId}>{ref}</span>
                <span className={styles.sourceLabel}>
                  {brief.titles[ref] ?? brief.memories[ref]?.text ?? ""}
                </span>
              </li>
            ))}
          </ol>
        ) : (
          <p className={styles.panelNote}>{s.noSources}</p>
        )}
      </section>
    </aside>
  );
}

/**
 * Brief.png's ochre notice under the targets: a file was edited by hand.
 * Pull edit into Brief turns it into proposals at once; Overwrite asks
 * first, inline. The title opens both files side by side.
 */
function DriftNotice({
  view,
  target,
}: {
  view: RecordsView;
  target: TargetView;
}) {
  const { l, space, now, timeZone, locale, agentName } = view;
  const d = l.brief.drift;
  const drift = useDrift(space, target);
  const actions = useDriftActions(space, target);
  const [confirming, setConfirming] = useState(false);
  const notice = driftNotice(l.brief, target, drift.data?.items[0], agentName, {
    now,
    timeZone,
    locale,
  });
  const canOverwrite = space.role !== "viewer";

  return (
    <div className={styles.drift}>
      <p>
        <strong>
          <Link
            className={styles.driftLink}
            href={`${targetHref(space.slug, target)}/drift`}
          >
            {notice.title}
          </Link>
        </strong>{" "}
        {notice.body}
      </p>
      {confirming ? (
        <>
          <p>{d.confirm}</p>
          <div className="mx-inline">
            <Button
              size="sm"
              variant="quiet"
              kbd="Esc"
              onClick={() => setConfirming(false)}
            >
              {d.cancel}
            </Button>
            <Button
              size="sm"
              variant="danger"
              pending={actions.pending === "overwrite"}
              onClick={async () => {
                await actions.run("overwrite");
                setConfirming(false);
              }}
            >
              {d.overwrite}
            </Button>
          </div>
        </>
      ) : (
        <div className="mx-inline">
          <Button
            size="sm"
            variant="secondary"
            pending={actions.pending === "pull"}
            onClick={() => void actions.run("pull")}
          >
            {d.pull}
          </Button>
          <Button
            size="sm"
            variant="danger"
            disabled={!canOverwrite}
            disabledReason={l.brief.page.editViewer}
            onClick={() => setConfirming(true)}
          >
            {d.overwrite}
          </Button>
        </div>
      )}
    </div>
  );
}
