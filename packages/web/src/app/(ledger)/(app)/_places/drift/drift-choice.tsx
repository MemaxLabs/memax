"use client";

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Button, Kbd } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { linesWord } from "@/lib/v2/brief-copy";
import { count } from "@/lib/v2/copy";
import {
  TARGET_READERS,
  type DriftChangeView,
  type DriftMode,
  type TargetView,
} from "@/lib/v2/data/targets";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { briefHref } from "../brief";
import type { RecordsView } from "../records-view";
import styles from "./drift.module.css";

const MODES: DriftMode[] = ["pull", "overwrite", "stop"];

/** Whether ↵ on this element is ours to take: the page, or one of the choices. */
function confirmsHere(target: EventTarget | null | undefined): boolean {
  if (!(target instanceof HTMLElement)) return true;
  if (target.dataset.driftOption !== undefined) return true;
  return !target.closest("button, a, input, textarea, select, [role=menu]");
}

/**
 * "What should happen" (DriftResolve.png): 1, 2 or 3 chooses (or the
 * arrows, in the group), ↵ does it. Overwrite asks first, inline: the
 * safe Cancel takes the focus. Anyone in the space may pull; overwrite
 * and stop need someone who may keep.
 */
export function DriftChoice({
  view,
  target,
  changes,
  pending,
  onRun,
}: {
  view: RecordsView;
  target: TargetView;
  changes: DriftChangeView[];
  pending: DriftMode | null;
  onRun: (mode: DriftMode) => void;
}) {
  const { l, space, agentName } = view;
  const r = l.brief.resolve;
  const [mode, setMode] = useState<DriftMode>("pull");
  const [confirming, setConfirming] = useState(false);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const options = useRef<(HTMLButtonElement | null)[]>([]);
  const confirmKey = useKeycap("drift.confirm");
  const canKeep = space.role !== "viewer";
  const proposals = changes.filter((c) => c.kind !== "remove").length;
  const edited = changes.length;
  const tool = agentName(TARGET_READERS[target.kind][0] ?? target.kind);
  const allowed = (m: DriftMode) => m === "pull" || canKeep;

  useEffect(() => {
    if (confirming) cancelRef.current?.focus();
  }, [confirming]);

  const choose = (next: DriftMode) => {
    if (!allowed(next) || pending) return;
    setMode(next);
    setConfirming(false);
  };
  const go = () => {
    if (pending || !allowed(mode)) return;
    if (mode === "overwrite" && !confirming) {
      setConfirming(true);
      return;
    }
    setConfirming(false);
    onRun(mode);
  };

  useHotkey("drift.choose", (_event, match) => {
    const next = MODES[match.index];
    if (next) choose(next);
  });
  useHotkey("drift.confirm", (event) => {
    if (!confirmsHere(event.target as EventTarget | null)) return false;
    go();
  });

  const onGroupKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const i = MODES.indexOf(mode);
    const step =
      event.key === "ArrowDown" || event.key === "ArrowRight"
        ? 1
        : event.key === "ArrowUp" || event.key === "ArrowLeft"
          ? -1
          : 0;
    if (!step) return;
    event.preventDefault();
    for (let n = 1; n <= MODES.length; n++) {
      const next = MODES[(i + step * n + MODES.length) % MODES.length]!;
      if (allowed(next)) {
        choose(next);
        options.current[MODES.indexOf(next)]?.focus();
        return;
      }
    }
  };

  const detail: Record<DriftMode, string> = {
    pull: interpolate(r.pullDetail, { lines: linesWord(r.pullLines, edited) }),
    overwrite: interpolate(r.overwriteDetail, {
      lines: linesWord(r.overwriteLines, edited),
    }),
    stop: interpolate(r.stopDetail, { tool }),
  };
  const title: Record<DriftMode, string> = {
    pull: r.pull,
    overwrite: r.overwrite,
    stop: r.stop,
  };
  const action: Record<DriftMode, string> = {
    pull: count(r.pullDoOne, r.pullDo, Math.max(1, proposals)),
    overwrite: r.overwriteDo,
    stop: r.stopDo,
  };

  return (
    <section className={styles.decide} aria-labelledby="drift-should">
      <p className="mx-section-label" id="drift-should">
        {r.should}
      </p>
      <div
        role="radiogroup"
        aria-label={r.choose}
        className={styles.options}
        onKeyDown={onGroupKey}
      >
        {MODES.map((m, i) => (
          <button
            key={m}
            ref={(el) => {
              options.current[i] = el;
            }}
            type="button"
            role="radio"
            data-drift-option=""
            aria-checked={mode === m}
            aria-disabled={allowed(m) ? undefined : true}
            title={allowed(m) ? undefined : l.brief.page.editViewer}
            tabIndex={mode === m ? 0 : -1}
            className={styles.option}
            onClick={() => choose(m)}
          >
            <Kbd>{String(i + 1)}</Kbd>
            <span
              className={`${styles.optionTitle} ${m === "overwrite" ? styles.danger : ""}`}
            >
              {title[m]}
              <span className={styles.optionDetail}>{detail[m]}</span>
            </span>
          </button>
        ))}
      </div>
      {confirming ? (
        <p className={styles.confirm} role="alert">
          {r.confirm}
        </p>
      ) : null}
      <div
        className={styles.foot}
        onKeyDown={(event) => {
          if (event.key === "Escape" && confirming) {
            event.preventDefault();
            setConfirming(false);
          }
        }}
      >
        {confirming ? (
          <Button
            ref={cancelRef}
            variant="quiet"
            size="sm"
            kbd="Esc"
            onClick={() => setConfirming(false)}
          >
            {r.cancel}
          </Button>
        ) : (
          <Button variant="quiet" size="sm" href={briefHref(space.slug)}>
            {r.later}
          </Button>
        )}
        <Button
          variant={mode === "overwrite" ? "danger" : "primary"}
          size="sm"
          kbd={confirmKey}
          pending={pending !== null}
          onClick={go}
        >
          {action[mode]}
        </Button>
      </div>
    </section>
  );
}
