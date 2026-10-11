"use client";

import { useState } from "react";
import Link from "next/link";
import { AgentStamp, Button } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { driftNotice } from "@/lib/v2/brief-copy";
import { cliPrefix } from "@/lib/v2/cli";
import { count } from "@/lib/v2/copy";
import type { BriefView } from "@/lib/v2/data/brief";
import { knownMachines } from "@/lib/v2/data/home-files";
import { machineName } from "@/lib/v2/data/machines";
import type { TargetView } from "@/lib/v2/data/targets";
import { homeDriftNotice } from "@/lib/v2/machines-copy";
import { LinkHome } from "../../_components/link-home";
import { MachinesPanel } from "../../_components/machines-panel";
import { TargetList } from "../../_components/target-list";
import { targetHref } from "../../_components/target-row";
import { useDrift } from "../../_lib/compile";
import { useMachines } from "../../_lib/machines";
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
          <TargetList space={space} targets={targets} />
        ) : space.kind === "personal" ? (
          <LinkHome lead={l.machines.panel.none} />
        ) : (
          <p className={styles.panelNote}>
            {interpolate(s.noTargets, { cli: cliPrefix() })}
          </p>
        )}
        {drifted ? <DriftNotice view={view} target={drifted} /> : null}
      </section>

      {space.kind === "personal" && targets?.length ? (
        <MachinesPanel space={space} targets={targets} variant="side" />
      ) : null}

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
          // PLACEHOLDER: /v2 doesn't count the Brief's reads by agent.
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
  const machines = useMachines(target.machine ? space : null);
  const [confirming, setConfirming] = useState(false);
  const when = { now, timeZone, locale };
  const item = drift.data?.items[0];
  const notice = target.machine
    ? homeDriftNotice(
        l.machines,
        l.brief,
        target,
        machineName(target.machine, knownMachines([target], machines.data)),
        item,
        agentName,
        when,
      )
    : driftNotice(l.brief, target, item, agentName, when);
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
