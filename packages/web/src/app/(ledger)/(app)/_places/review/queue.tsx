"use client";

import { useEffect, useRef } from "react";
import {
  Button,
  Kbd,
  MemoryList,
  MemoryRow,
  Segmented,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatAge } from "@/lib/v2/copy";
import { gateStatusAt } from "@/lib/v2/data/gates";
import { waitingOnYou } from "@/lib/v2/data/types";
import { useKeycap, useKeycaps } from "@/lib/v2/keymap/react";
import { GateRow } from "../../_components/gate-row";
import { StatementText } from "../../_components/statement-text";
import { useOverlays } from "../../_lib/overlays";
import type { RecordsView } from "../records-view";
import { QueueSkeleton } from "./skeleton";
import {
  matchesFilter,
  type ReviewController,
  type ReviewFilter,
} from "./use-review";
import styles from "./review.module.css";

/**
 * Review.png's left column: the title with what's waiting, the filters
 * (each with its count, in the URL as ?filter=), the queue, and the key
 * legend. The legend follows the card: editing and rejecting have keys
 * of their own.
 */
export function Queue({
  view,
  review,
  filter,
  onFilter,
}: {
  view: RecordsView;
  review: ReviewController;
  filter: ReviewFilter;
  onFilter: (filter: ReviewFilter) => void;
}) {
  const { copy, l, overview, now } = view;
  const { queue, all, visible, selected, selectedGate, gates, state } = review;
  const gatesRead = review.gateCards.query;
  const complete = queue.data !== undefined && !queue.hasNextPage;
  // Exact once the whole queue is loaded; the overview's otherwise. The
  // decision gates count too: they wait on you in Review.
  const waitingGates = review.gateCards.gates.filter(
    (g) => gateStatusAt(g, now) === "waiting",
  ).length;
  const waiting = complete
    ? all.length + waitingGates
    : overview
      ? waitingOnYou(overview)
      : null;
  const counts = complete
    ? {
        conflicts: all.filter((i) => matchesFilter(i, "conflicts")).length,
        external: all.filter((i) => matchesFilter(i, "external")).length,
        stale: all.filter((i) => matchesFilter(i, "stale")).length,
      }
    : (overview?.reviewFilters ?? null);
  // "Oldest" is the oldest proposal still waiting (Review.png: "oldest
  // 3 h"); a kept memory Dream flagged overnight isn't a proposal's wait.
  const oldest = complete
    ? (all
        .filter((i) => i.lifecycle === "proposed")
        .map((i) => i.at)
        .sort((a, b) => Date.parse(a) - Date.parse(b))[0] ?? null)
    : (overview?.oldestWaitingAt ?? null);

  let meta = "";
  if (waiting === 0) meta = copy.review.metaNone;
  else if (waiting !== null && oldest) {
    meta = interpolate(copy.review.meta, {
      n: waiting,
      age: formatAge(copy, oldest, now),
    });
  } else if (waiting !== null) {
    meta = interpolate(copy.review.metaCount, { n: waiting });
  }

  const label = (key: ReviewFilter) => {
    const n = key === "all" ? waiting : counts ? counts[key] : null;
    return n === null
      ? l.review.filtersBare[key]
      : interpolate(copy.review.filters[key], { n });
  };

  // The selected row stays in view as ↓↑ move through a long queue.
  const list = useRef<HTMLDivElement>(null);
  const selectedRef = selectedGate?.ref ?? selected?.ref;
  useEffect(() => {
    list.current
      ?.querySelector<HTMLElement>(".mx-row.is-selected")
      ?.scrollIntoView({ block: "nearest" });
  }, [selectedRef]);

  const editingRef = state.mode.kind === "editing" ? state.mode.ref : null;
  const gateStep = selectedGate
    ? review.gateCards.cardOf(selectedGate.ref).step
    : null;

  return (
    <aside className={styles.queue} aria-label={l.review.queueLabel}>
      <div className={styles.queueHead}>
        <div className={styles.titleRow}>
          <h1 className="mx-page-title">{copy.review.title}</h1>
          <span className="mx-meta">{meta}</span>
        </div>
        <Segmented<ReviewFilter>
          size="sm"
          label={copy.review.filterLabel}
          value={filter}
          onChange={onFilter}
          options={(["all", "conflicts", "external", "stale"] as const).map(
            (value) => ({ value, label: label(value) }),
          )}
        />
      </div>
      <div className={styles.list} ref={list}>
        {filter === "all" &&
        gatesRead.isError &&
        gatesRead.data === undefined ? (
          <div className={styles.gatesFailed} role="status">
            <span>{l.review.gate.failed}</span>
            <Button
              variant="quiet"
              size="sm"
              onClick={() => void gatesRead.refetch()}
            >
              {l.review.gate.retry}
            </Button>
          </div>
        ) : null}
        {queue.data === undefined || review.gatesLoading ? (
          <QueueSkeleton label={l.review.loading} />
        ) : visible.length === 0 && gates.length === 0 ? (
          <div className={styles.filterEmpty}>
            <span>{l.review.emptyFilter}</span>
            <Button variant="quiet" size="sm" onClick={() => onFilter("all")}>
              {l.review.showAll}
            </Button>
          </div>
        ) : (
          <MemoryList aria-label={l.review.queueLabel}>
            {gates.map((gate) => (
              <GateRow
                key={gate.ref}
                view={view}
                gate={gate}
                selected={gate.ref === selectedGate?.ref}
                stacked
                onClick={() => review.select(gate.ref)}
              />
            ))}
            {visible.map((item) => {
              const stamp = view.stamp(item.by);
              // The judge's neutral working mark, never a spinner.
              const checking =
                item.judge === "working" || state.waiting === item.ref;
              return (
                <MemoryRow
                  key={item.ref}
                  stacked
                  selected={item.ref === selected?.ref}
                  state={item.state}
                  mark={checking ? "working" : undefined}
                  markLabel={checking ? l.review.judge.checking : undefined}
                  unconfirmed={item.lifecycle === "proposed"}
                  {...(stamp ?? {})}
                  action={
                    l.records.verbs[
                      item.ref === editingRef ? "editing" : item.action
                    ]
                  }
                  time={view.time(item.at)}
                  id={item.ref}
                  space={item.intoSpace ?? undefined}
                  onClick={() => review.select(item.ref)}
                >
                  <StatementText text={item.statement} />
                </MemoryRow>
              );
            })}
          </MemoryList>
        )}
        {queue.hasNextPage ? (
          <div className={styles.more}>
            <Button
              variant="quiet"
              size="sm"
              icon="chevron-down"
              pending={queue.isFetchingNextPage}
              onClick={() => void queue.fetchNextPage()}
            >
              {l.review.more}
            </Button>
          </div>
        ) : null}
      </div>
      <Legend
        view={view}
        mode={
          state.waiting
            ? "waiting"
            : !selectedGate
              ? state.mode.kind
              : gateStatusAt(selectedGate, now) !== "waiting"
                ? "gateEnded"
                : gateStep === "confirm"
                  ? "gateConfirm"
                  : gateStep === "withdraw"
                    ? "gateWithdraw"
                    : "gate"
        }
        options={selectedGate?.options.length ?? 0}
      />
    </aside>
  );
}

function Legend({
  view,
  mode,
  options,
}: {
  view: RecordsView;
  mode:
    | ReviewController["state"]["mode"]["kind"]
    | "waiting"
    | "gate"
    | "gateConfirm"
    | "gateWithdraw"
    | "gateEnded";
  /** A gate's options: the digits that choose. */
  options: number;
}) {
  const { copy, l } = view;
  const { setKeysOpen } = useOverlays();
  const move = useKeycaps("review.move");
  const keep = useKeycap("review.keep");
  const edit = useKeycap("review.edit");
  const reject = useKeycap("review.reject");
  const help = useKeycap("help.keys");
  const keepChord = useKeycap("command.keep");
  const stop = useKeycap("review.stopWaiting");
  const digits = useKeycaps("gate.choose").slice(0, Math.max(1, options));
  const key = (caps: string, text: string) => (
    <span className={styles.key}>
      <Kbd>{caps}</Kbd> {text}
    </span>
  );
  const allKeys = (
    <button
      type="button"
      className={styles.allKeys}
      onClick={() => setKeysOpen(true)}
      aria-haspopup="dialog"
    >
      <Kbd>{help}</Kbd> {copy.review.legend.allKeys}
    </button>
  );
  const moveKeys = (
    <span className={styles.key}>
      {[...move].reverse().map((caps) => (
        <Kbd key={caps.join()}>{caps.join(" ")}</Kbd>
      ))}{" "}
      {copy.review.legend.move}
    </span>
  );
  let keys;
  if (mode === "gate") {
    const g = l.review.gate.legend;
    keys = (
      <>
        {moveKeys}
        <span className={styles.key}>
          {digits.length > 1 ? (
            <>
              <Kbd>{digits[0]!.join(" ")}</Kbd>–
              <Kbd>{digits.at(-1)!.join(" ")}</Kbd>
            </>
          ) : (
            <Kbd>{digits[0]?.join(" ") ?? "1"}</Kbd>
          )}{" "}
          {g.choose}
        </span>
        {key("↵", g.answer)}
        {allKeys}
      </>
    );
  } else if (mode === "gateEnded") {
    keys = (
      <>
        {moveKeys}
        {allKeys}
      </>
    );
  } else if (mode === "gateConfirm" || mode === "gateWithdraw") {
    const g = l.review.gate.legend;
    keys = (
      <>
        {mode === "gateConfirm" ? key("↵", g.answer) : null}
        {key("Esc", g.cancel)}
      </>
    );
  } else if (mode === "editing") {
    keys = (
      <>
        {key(keepChord, l.review.legend.keepEdited)}
        {key("Esc", l.review.legend.stopEditing)}
      </>
    );
  } else if (mode === "waiting") {
    keys = (
      <>
        <span className={styles.key}>
          {[...move].reverse().map((caps) => (
            <Kbd key={caps.join()}>{caps.join(" ")}</Kbd>
          ))}{" "}
          {copy.review.legend.move}
        </span>
        {key(stop, l.review.legend.stopWaiting)}
      </>
    );
  } else if (mode === "rejecting") {
    keys = (
      <>
        {key("↵", l.review.legend.confirmReject)}
        {key("Esc", l.review.legend.cancel)}
      </>
    );
  } else {
    keys = (
      <>
        <span className={styles.key}>
          {[...move].reverse().map((caps) => (
            <Kbd key={caps.join()}>{caps.join(" ")}</Kbd>
          ))}{" "}
          {copy.review.legend.move}
        </span>
        {key(keep, copy.review.legend.keep)}
        {key(edit, copy.review.legend.edit)}
        {key(reject, copy.review.legend.reject)}
        <button
          type="button"
          className={styles.allKeys}
          onClick={() => setKeysOpen(true)}
          aria-haspopup="dialog"
        >
          <Kbd>{help}</Kbd> {copy.review.legend.allKeys}
        </button>
      </>
    );
  }
  return (
    <div
      className={styles.legend}
      aria-label={copy.review.legendLabel}
      role="group"
    >
      {keys}
    </div>
  );
}
