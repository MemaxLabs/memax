"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button, Diff, Receipt } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { commandReason } from "@/lib/v2/brief-copy";
import { count, formatReceiptTime } from "@/lib/v2/copy";
import { toFailure } from "@/lib/v2/data/command-error";
import type {
  BriefMemory,
  BriefStructure,
  BriefVersionView,
} from "@/lib/v2/data/brief";
import { IntentKeys, intentOf } from "@/lib/v2/intent-keys";
import { StatementText } from "../../_components/statement-text";
import { useToast } from "../../_components/toasts";
import { useAfterCompile } from "../../_lib/compile";
import { useSource } from "../../_lib/data";
import type { RecordsView } from "../records-view";
import {
  diffFromParent,
  diffVersions,
  type HistoryChange,
} from "./history-diff";
import styles from "./brief-history.module.css";

/** A version as it can be restored now: what's no longer kept can't be placed. */
export function restorable(
  structure: BriefStructure,
  memories: Readonly<Record<string, BriefMemory>>,
): BriefStructure {
  const kept = (ref: string) => memories[ref]?.kept === true;
  return {
    ...structure,
    sections: structure.sections
      .map((section) => ({
        ...section,
        items: section.items.filter((item) =>
          "ref" in item ? kept(item.ref) : item.cites.some(kept),
        ),
      }))
      .filter((section) => section.items.length > 0),
  };
}

/** One version's changes (BriefHistory.png's right panel), against the one before it or against now. */
export function VersionDetail({
  view,
  version,
  versions,
  memories,
}: {
  view: RecordsView;
  version: BriefVersionView;
  versions: BriefVersionView[];
  memories: Readonly<Record<string, BriefMemory>>;
}) {
  const { l, space, copy, now, timeZone, locale } = view;
  const h = l.brief.history;
  const source = useSource();
  const toast = useToast();
  const afterCompile = useAfterCompile(space);
  const [keys] = useState(() => new IntentKeys());
  const [withNow, setWithNow] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const cancel = useRef<HTMLElement>(null);
  const current = versions.find((v) => v.current) ?? versions[0]!;
  const isCurrent = version.ref === current.ref;
  const diff =
    withNow && !isCurrent
      ? diffVersions(version.structure, current.structure)
      : diffFromParent(version, versions);
  const stamp = view.stamp(version.by);

  useEffect(() => {
    if (confirming) cancel.current?.focus();
  }, [confirming]);

  const restore = async () => {
    if (pending) return;
    setPending(true);
    const structure = restorable(version.structure, memories);
    const intent = intentOf("brief.restore", version.ref, current.version);
    try {
      const result = await source.brief.revise({
        space,
        base: current.version,
        structure,
        reason: interpolate(h.restoreReason, { ref: version.ref }),
        idempotencyKey: keys.keyFor(intent),
      });
      keys.settle(intent);
      afterCompile();
      setConfirming(false);
      toast({
        state: "kept",
        text: interpolate(l.brief.toast.restored, {
          from: version.ref,
          ref: result.ref,
        }),
      });
    } catch (err) {
      toast({
        state: "proposed",
        text: interpolate(l.brief.edit.failed, {
          reason: commandReason(l.records, l.brief, toFailure(err), space.name),
        }),
      });
    } finally {
      setPending(false);
    }
  };

  const text = (ref: string) => memories[ref]?.text ?? null;

  return (
    <section className={`mx-panel ${styles.detail}`} aria-label={version.ref}>
      <header className="mx-panel-head">
        <span className={styles.detailHead}>
          <Receipt
            {...(stamp ?? {})}
            action={version.by?.kind === "dream" ? h.rewrote : h.revised}
            time={formatReceiptTime(copy, version.at, now, timeZone, locale)}
            id={version.ref}
          />
        </span>
        <span className={styles.actions}>
          {isCurrent ? null : (
            <Button
              variant="quiet"
              size="sm"
              aria-pressed={withNow}
              onClick={() => setWithNow((v) => !v)}
            >
              {withNow ? h.compareBefore : h.compareNow}
            </Button>
          )}
          <Button
            variant="secondary"
            size="sm"
            disabled={isCurrent || space.role === "viewer"}
            disabledReason={
              isCurrent ? h.restoreCurrent : l.brief.page.editViewer
            }
            onClick={() => setConfirming(true)}
          >
            {h.restore}
          </Button>
        </span>
      </header>
      {confirming ? (
        <div
          className={styles.confirm}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              setConfirming(false);
            }
          }}
        >
          <p>{interpolate(h.restoreConfirm, { ref: version.ref })}</p>
          <Button
            ref={cancel}
            variant="quiet"
            size="sm"
            kbd="Esc"
            onClick={() => setConfirming(false)}
          >
            {h.cancel}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            pending={pending}
            onClick={() => void restore()}
          >
            {h.restoreDo}
          </Button>
        </div>
      ) : null}
      {version.reason ? (
        <p className={styles.why}>
          {interpolate(h.why, { reason: version.reason })}
        </p>
      ) : null}
      {withNow && !isCurrent ? (
        <p className={styles.why}>
          {interpolate(h.since, { ref: version.ref })}
        </p>
      ) : null}
      {diff.count === 0 && version.parent !== null ? (
        <p className={styles.nothing}>{h.nothing}</p>
      ) : null}
      {version.parent === null && !withNow ? (
        <FirstVersion view={view} version={version} text={text} />
      ) : (
        diff.sections.map((section) => (
          <div key={section.key}>
            <h3 className={styles.sec}>{section.heading}</h3>
            {section.changes.map((change, i) => (
              <Change
                key={`${section.key}-${i}`}
                view={view}
                change={change}
                heading={section.heading}
                text={text}
              />
            ))}
          </div>
        ))
      )}
      {diff.unchanged.length > 0 ? (
        <>
          <div className={styles.foot}>
            <span className="mx-meta">
              {count(h.unchangedOne, h.unchanged, diff.unchanged.length)}
            </span>
            <Button
              variant="quiet"
              size="sm"
              icon="chevron-down"
              aria-expanded={showAll}
              onClick={() => setShowAll((v) => !v)}
            >
              {showAll ? h.hide : h.show}
            </Button>
          </div>
          {showAll ? (
            <ul className={styles.unchanged}>
              {diff.unchanged.map((ref) => (
                <li key={ref}>
                  <span className={styles.unchangedRef}>{ref}</span>
                  <span>
                    <StatementText text={text(ref) ?? ""} />
                  </span>
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : null}
    </section>
  );
}

/** The first version: everything it placed, by section. */
function FirstVersion({
  view,
  version,
  text,
}: {
  view: RecordsView;
  version: BriefVersionView;
  text: (ref: string) => string | null;
}) {
  const h = view.l.brief.history;
  return (
    <>
      {version.structure.sections.map((section) => (
        <div key={section.key}>
          <h3 className={styles.sec}>{section.heading}</h3>
          {section.items.map((item, i) => (
            <div key={i} className={styles.change}>
              <span className={styles.kind}>{h.kinds.added}</span>
              <div>
                <p>
                  <ins className="mx-diff-add">
                    <StatementText
                      text={
                        "ref" in item ? (text(item.ref) ?? item.ref) : item.text
                      }
                    />
                  </ins>
                </p>
                <span className="mx-meta">
                  {"ref" in item ? item.ref : item.cites.join(", ")}
                </span>
              </div>
            </div>
          ))}
        </div>
      ))}
    </>
  );
}

function Change({
  view,
  change,
  heading,
  text,
}: {
  view: RecordsView;
  change: HistoryChange;
  heading: string;
  text: (ref: string) => string | null;
}) {
  const h = view.l.brief.history;
  const k = h.kinds;
  const meta = (ref: string, note: string | null) =>
    note
      ? `${ref} · ${note}`
      : text(ref) === null
        ? interpolate(h.notInBrief, { ref })
        : ref;
  let kind: string;
  let body: ReactNode;
  let line: string | null = null;
  switch (change.kind) {
    case "reworded":
      kind = k.reworded;
      body = <Diff before={change.before} after={change.after} />;
      line = change.ref ? meta(change.ref, change.note) : null;
      break;
    case "flagged":
      kind = k.flagged;
      body = (
        <span className={styles.stale}>
          <StatementText text={text(change.ref) ?? change.ref} />
        </span>
      );
      line = meta(change.ref, change.note);
      break;
    case "added":
      kind = k.added;
      body = (
        <ins className="mx-diff-add">
          <StatementText text={text(change.ref) ?? change.ref} />
        </ins>
      );
      line = meta(change.ref, change.note);
      break;
    case "removed":
      kind = k.removed;
      body = (
        <del className="mx-diff-del">
          <StatementText text={text(change.ref) ?? change.ref} />
        </del>
      );
      line = meta(change.ref, change.note);
      break;
    case "moved":
      kind = k.moved;
      body = <StatementText text={text(change.ref) ?? change.ref} />;
      line = interpolate(h.movedFrom, {
        ref: change.ref,
        section: change.from,
      });
      break;
    case "proseAdded":
      kind = k.added;
      body = (
        <ins className="mx-diff-add">
          <StatementText text={change.text} />
        </ins>
      );
      line = change.cites.join(", ");
      break;
    case "proseRemoved":
      kind = k.removed;
      body = (
        <del className="mx-diff-del">
          <StatementText text={change.text} />
        </del>
      );
      line = change.cites.join(", ");
      break;
    case "renamed":
      kind = k.renamed;
      body = <span className={styles.heading}>{heading}</span>;
      line = interpolate(h.renamedFrom, { heading: change.from });
      break;
  }
  return (
    <div className={styles.change}>
      <span className={styles.kind}>{kind}</span>
      <div>
        <p>{body}</p>
        {line ? <span className="mx-meta">{line}</span> : null}
      </div>
    </div>
  );
}
