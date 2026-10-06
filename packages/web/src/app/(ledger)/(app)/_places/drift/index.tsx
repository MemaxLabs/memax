"use client";

import { useState } from "react";
import { Button, PageHeader, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { driftLines, driftTitle, driftWhen } from "@/lib/v2/brief-copy";
import { count, formatClock, formatShortDate } from "@/lib/v2/copy";
import {
  findTarget,
  targetName,
  type DriftItemView,
  type DriftMode,
  type DriftResolution,
  type TargetView,
} from "@/lib/v2/data/targets";
import { placeHref } from "@/lib/v2/places";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { targetHref } from "../../_components/target-row";
import { StatusPage } from "../../../_components/status-page";
import { useDrift, useTargets } from "../../_lib/compile";
import { briefHref } from "../brief";
import { useRecordsView, type RecordsView } from "../records-view";
import { DriftChanges } from "./drift-changes";
import { DriftChoice } from "./drift-choice";
import { useDriftActions } from "./use-drift-actions";
import styles from "./drift.module.css";

/**
 * A hand edit (DriftResolve.png, epic 1.6) at
 * /[space]/brief/targets/[target]/drift: the file as Memax wrote it and
 * as it is now, side by side; what the edit says, change by change; and
 * three choices (1, 2, 3, then ↵): pull it in as proposals, overwrite it
 * (confirmed inline), or stop compiling the file. After a pull, Review
 * is where the proposals wait.
 */
export function DriftPlace({ segment }: { segment: string }) {
  const view = useRecordsView();
  const { space, l } = view;
  const targets = useTargets(space);
  const target = targets.data ? findTarget(targets.data, segment) : undefined;

  if (targets.data === undefined) {
    if (targets.isError) {
      return (
        <PlaceError space={space} onRetry={() => void targets.refetch()} />
      );
    }
    return <Loading view={view} label={segment} />;
  }
  if (!target) {
    const nf = l.brief.target.notFound;
    return (
      <StatusPage
        variant="sheet"
        receipt={segment}
        title={interpolate(nf.title, { target: segment, space: space.name })}
        description={nf.detail}
        actions={
          <Button variant="secondary" size="sm" href={briefHref(space.slug)}>
            {nf.open}
          </Button>
        }
      />
    );
  }
  return <Drift view={view} target={target} />;
}

function Loading({ view, label }: { view: RecordsView; label: string }) {
  const { copy } = view;
  return (
    <div className={`mx-page ${styles.page}`}>
      <HeaderSkeleton />
      <PlaceSkeleton
        title={view.l.brief.resolve.editSays}
        status={copy.empty.loadingMeta}
        label={interpolate(copy.empty.loading, { place: label })}
      />
    </div>
  );
}

function Drift({ view, target }: { view: RecordsView; target: TargetView }) {
  const { space, l, now, timeZone, locale, agentName } = view;
  const r = l.brief.resolve;
  const drift = useDrift(space, target);
  const actions = useDriftActions(space, target);
  const [done, setDone] = useState<{
    mode: DriftMode;
    result: DriftResolution;
  } | null>(null);

  const run = async (mode: DriftMode) => {
    const result = await actions.run(mode);
    if (result) setDone({ mode, result });
  };

  if (done) return <Done view={view} target={target} done={done} />;
  if (target.openDrift === 0 && !drift.data?.items.length) {
    return <Nothing view={view} target={target} />;
  }
  if (!drift.data) {
    if (drift.isError) {
      return <PlaceError space={space} onRetry={() => void drift.refetch()} />;
    }
    return <Loading view={view} label={targetName(target)} />;
  }
  const items = drift.data.items;
  const item: DriftItemView | undefined = items[0];
  if (!item) return <Nothing view={view} target={target} />;
  const changes = items.flatMap((i) => i.changes);
  const when = { now, timeZone, locale };

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(r.eyebrow, { path: item.path })}
        title={driftTitle(l.brief, target, agentName)}
        lede={interpolate(r.lede, {
          when: driftWhen(l.brief, item.observedAt, when),
        })}
        actions={
          <StateMark
            state="proposed"
            label={count(r.statusOne, r.status, changes.length)}
          />
        }
      />
      {items.map((each) => (
        <Files key={each.observationId} view={view} item={each} />
      ))}
      <div className={styles.lower}>
        <DriftChanges view={view} target={target} items={items} />
        <DriftChoice
          view={view}
          target={target}
          changes={changes}
          pending={actions.pending}
          onRun={(mode) => void run(mode)}
        />
      </div>
    </div>
  );
}

/** The two files side by side: lines the edit removed struck, lines it added marked. */
function Files({ view, item }: { view: RecordsView; item: DriftItemView }) {
  const { l, timeZone, locale } = view;
  const r = l.brief.resolve;
  const { removed, added } = driftLines(item.changes);
  const stamp = (iso: string) =>
    interpolate(r.when, {
      date: formatShortDate(new Date(iso), timeZone, locale),
      time: formatClock(iso, timeZone, locale),
    });
  return (
    <div className={styles.files}>
      <FileSide
        title={r.compiled}
        meta={item.compiledAt ? stamp(item.compiledAt) : null}
        content={item.compiled}
        marked={removed}
        mark="del"
      />
      <FileSide
        title={r.repository}
        meta={
          item.commit
            ? interpolate(r.repositoryMeta, {
                when: stamp(item.observedAt),
                commit: item.commit,
              })
            : stamp(item.observedAt)
        }
        content={item.observed}
        marked={added}
        mark="add"
      />
    </div>
  );
}

function FileSide({
  title,
  meta,
  content,
  marked,
  mark,
}: {
  title: string;
  meta: string | null;
  content: string;
  marked: ReadonlySet<number>;
  mark: "del" | "add";
}) {
  const lines = content.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  let front = lines[0] === "---";
  return (
    <section className={`mx-panel ${styles.filePanel}`} aria-label={title}>
      <header className="mx-panel-head">
        <h2 className={`mx-panel-title ${styles.fileTitle}`}>{title}</h2>
        {meta ? <span className="mx-meta">{meta}</span> : null}
      </header>
      <pre className={styles.code} tabIndex={0} aria-label={title}>
        <code className={styles.lines}>
          {lines.map((line, i) => {
            const quiet = front || line.startsWith("<!--");
            if (front && i > 0 && line === "---") front = false;
            const n = i + 1;
            const cls = marked.has(n)
              ? styles[mark]
              : quiet
                ? styles.quiet
                : "";
            return (
              <span key={n} className={`${styles.line} ${cls}`}>
                {line}
              </span>
            );
          })}
        </code>
      </pre>
    </section>
  );
}

function Nothing({ view, target }: { view: RecordsView; target: TargetView }) {
  const { space, l } = view;
  const r = l.brief.resolve;
  return (
    <StatusPage
      variant="sheet"
      receipt={targetName(target)}
      title={r.none}
      description={r.noneDetail}
      actions={
        <Button
          variant="secondary"
          size="sm"
          href={targetHref(space.slug, target)}
        >
          {interpolate(r.openTarget, { file: targetName(target) })}
        </Button>
      }
    />
  );
}

/** After a choice: a pull's proposals wait in Review; otherwise, back to the file. */
function Done({
  view,
  target,
  done,
}: {
  view: RecordsView;
  target: TargetView;
  done: { mode: DriftMode; result: DriftResolution };
}) {
  const { space, l } = view;
  const r = l.brief.resolve;
  const { mode, result } = done;
  const refs = result.proposals.map((p) => p.ref);
  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        className={styles.head}
        eyebrow={interpolate(r.eyebrow, { path: targetName(target) })}
        title={driftTitle(l.brief, target, view.agentName)}
      />
      <section className={styles.result} aria-live="polite">
        <h2>
          {mode === "pull"
            ? r.pulled
            : mode === "overwrite"
              ? interpolate(l.brief.toast.overwritten, {
                  file: targetName(target),
                })
              : interpolate(l.brief.toast.stopped, {
                  file: targetName(target),
                })}
        </h2>
        {mode === "pull" && refs.length > 0 ? (
          <p>
            {interpolate(
              refs.length === 1 ? r.pulledDetailOne : r.pulledDetail,
              {
                refs: refs.join(", "),
              },
            )}
          </p>
        ) : null}
        {mode === "pull" && result.waiting > 0 ? (
          <p>{count(r.waitingOne, r.waiting, result.waiting)}</p>
        ) : null}
        <div className={styles.actions}>
          {mode === "pull" ? (
            <Button
              variant="primary"
              size="sm"
              href={placeHref(space.slug, "review")}
            >
              {r.openReview}
            </Button>
          ) : null}
          <Button
            variant={mode === "pull" ? "quiet" : "secondary"}
            size="sm"
            href={targetHref(space.slug, target)}
          >
            {interpolate(r.openTarget, { file: targetName(target) })}
          </Button>
        </div>
      </section>
    </div>
  );
}
