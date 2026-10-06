"use client";

import {
  AgentStamp,
  Button,
  DreamCard,
  Icon,
  MemoryList,
  MemoryRow,
  StateMark,
  useLedger,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatAge, formatClock } from "@/lib/v2/copy";
import { noteText } from "@/lib/v2/records-copy";
import type { ReviewItem } from "@/lib/v2/data/review";
import type { TargetView } from "@/lib/v2/data/targets";
import {
  activeToday,
  pickWaiting,
  startOfDay,
  type TodayData,
} from "@/lib/v2/data/today";
import { agentDay, dreamDate } from "@/lib/v2/today-copy";
import { placeHref } from "@/lib/v2/places";
import { StatementText } from "../../_components/statement-text";
import { TargetRow } from "../../_components/target-row";
import type { RecordsView } from "../records-view";
import styles from "./today.module.css";

const ROW_STATE = {
  proposed: "proposed",
  conflict: "conflict",
  stale: "stale",
} as const;

/** Main.png's Dream card; the designed placeholder while Dream editions aren't built. */
export function TodayDream({
  view,
  data,
}: {
  view: RecordsView;
  data: TodayData;
}) {
  const { l, timeZone, locale } = view;
  const { strings } = useLedger();
  const d = l.today.dream;
  const dream = data.dream;
  if (dream.kind !== "edition") {
    return (
      <section className={`mx-dream is-static ${styles.dreamQuiet}`}>
        <header className="mx-dream-mast">
          <span className="mx-dream-name">{strings.dream.masthead}</span>
        </header>
        <h2 className={styles.dreamQuietTitle}>
          {dream.kind === "quiet" ? d.quiet : d.none}
        </h2>
        <p className={styles.dreamQuietDetail}>
          {dream.kind === "quiet" ? d.quietDetail : d.noneDetail}
        </p>
      </section>
    );
  }
  const e = dream.edition;
  return (
    <DreamCard
      headingLevel={2}
      issue={e.n}
      date={dreamDate(e.at, timeZone, locale)}
      time={formatClock(e.at, timeZone, locale)}
      duration={
        e.seconds === null
          ? undefined
          : interpolate(d.seconds, { n: e.seconds })
      }
      notes={e.notes}
      facts={e.facts}
      noteIds={e.noteIds}
      factIds={e.factIds}
      items={e.lines.map((line, i) => ({
        key: String(i),
        kind: line.kind,
        text: line.text,
        meta:
          line.meta.kind === "folded"
            ? interpolate(d.folded, { n: line.meta.notes, ref: line.meta.into })
            : line.meta.kind === "needs-you"
              ? d.needsYou
              : d.restorable,
      }))}
    />
  );
}

/** "Waiting on you": a few of Review's items, then "N more in Review". */
export function WaitingPanel({
  view,
  data,
}: {
  view: RecordsView;
  data: TodayData;
}) {
  const { l, space, rc, agentName, timeZone, locale } = view;
  const w = l.today.waiting;
  const waiting = data.waiting;
  const shown = pickWaiting(waiting.items);
  const more = Math.max(0, waiting.total - shown.length);
  const meta = [
    waiting.proposals > 0
      ? count(w.proposalsOne, w.proposals, waiting.proposals)
      : null,
    waiting.stale > 0 ? interpolate(w.verify, { n: waiting.stale }) : null,
  ]
    .filter(Boolean)
    .join(" · ");
  const href = (item: ReviewItem) =>
    `${placeHref(space.slug, "memories")}/${encodeURIComponent(item.ref)}`;

  return (
    <section
      className={`mx-panel ${styles.waiting}`}
      aria-labelledby="today-waiting"
    >
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="today-waiting">
          {w.title}
          {waiting.total > 0 ? (
            <span
              className="mx-count is-pending"
              aria-label={interpolate(w.count, { n: waiting.total })}
            >
              {waiting.total}
            </span>
          ) : null}
        </h2>
        {meta ? <span className="mx-meta">{meta}</span> : null}
      </header>
      {shown.length === 0 ? (
        <p className={styles.panelNote}>{w.nothing}</p>
      ) : (
        <MemoryList>
          {shown.map((item) => {
            const stamp = view.stamp(item.by);
            const note = waiting.notes[item.ref];
            return (
              <MemoryRow
                key={item.ref}
                state={ROW_STATE[item.state]}
                unconfirmed={item.state === "conflict" ? true : undefined}
                {...(stamp ?? {})}
                // An update is a proposal too (Main.png: "proposed").
                action={
                  rc.verbs[item.action === "updated" ? "proposed" : item.action]
                }
                time={view.time(item.at)}
                id={item.ref}
                space={item.intoSpace ?? space.name}
                href={href(item)}
                note={
                  note
                    ? noteText(rc, note, agentName, timeZone, locale)
                    : undefined
                }
              >
                <StatementText text={item.statement} />
              </MemoryRow>
            );
          })}
        </MemoryList>
      )}
      {more > 0 ? (
        <div className={styles.more}>
          <Button
            variant="quiet"
            size="sm"
            icon="arrow-right"
            href={placeHref(space.slug, "review")}
          >
            {count(w.moreOne, w.more, more)}
          </Button>
        </div>
      ) : null}
    </section>
  );
}

/** "In flight": the handoff in progress; a plain note until handoffs are served. */
export function InFlightPanel({
  view,
  data,
}: {
  view: RecordsView;
  data: TodayData;
}) {
  const { l, agentName } = view;
  const f = l.today.inFlight;
  const flight = data.inFlight;
  return (
    <section className="mx-panel" aria-labelledby="today-flight">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="today-flight">
          {f.title}
        </h2>
        {flight ? <span className="mx-receipt">{flight.ref}</span> : null}
      </header>
      {flight ? (
        <div className={styles.flight}>
          <div
            className={styles.route}
            aria-label={interpolate(f.from, {
              from: agentName(flight.from),
              to: agentName(flight.to),
            })}
          >
            <AgentStamp agent={flight.from} showName />
            <Icon name="arrow-right" size={14} />
            <AgentStamp agent={flight.to} showName />
          </div>
          <p className={styles.flightTitle}>{flight.title}</p>
          <div className={styles.flightState}>
            <StateMark
              state="working"
              label={interpolate(f.working, { agent: agentName(flight.to) })}
            />
            {flight.questions > 0 ? (
              <StateMark
                state="proposed"
                label={count(f.questionsOne, f.questions, flight.questions)}
              />
            ) : null}
          </div>
        </div>
      ) : (
        <p className={styles.panelNote}>{flight === null ? f.none : f.later}</p>
      )}
    </section>
  );
}

/** "Agents today": who worked here today, what they read and wrote, and when last. */
export function AgentsPanel({
  view,
  data,
}: {
  view: RecordsView;
  data: TodayData;
}) {
  const { l, space, now, timeZone, copy } = view;
  const a = l.today.agents;
  const agents = data.agents;
  const dayStart = startOfDay(now, timeZone);
  const active = agents?.rows.filter((row) => activeToday(row, dayStart)) ?? [];
  return (
    <section className="mx-panel" aria-labelledby="today-agents">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="today-agents">
          {a.title}
        </h2>
        {agents && agents.connected > 0 ? (
          <span className="mx-meta">
            {interpolate(a.active, {
              n: active.length,
              total: agents.connected,
            })}
          </span>
        ) : null}
      </header>
      {!agents ? (
        <p className={styles.panelNote}>{a.unavailable}</p>
      ) : agents.connected === 0 ? (
        <div className={styles.flight}>
          <p className={styles.flightNote}>{a.noAgents}</p>
          <span>
            <Button
              variant="secondary"
              size="sm"
              icon="plus"
              href={`${placeHref(space.slug, "agents")}?overlay=connect`}
            >
              {a.connect}
            </Button>
          </span>
        </div>
      ) : active.length === 0 ? (
        <p className={styles.panelNote}>{a.nobody}</p>
      ) : (
        active.map((row) => (
          <div key={row.id} className={styles.agent}>
            <AgentStamp agent={row.agent} name={row.name} showName />
            <span className={`mx-meta ${styles.agentDay}`}>
              {agentDay(l.today, row)}
            </span>
            <span className={`mx-meta ${styles.agentSeen}`}>
              {row.lastSeenAt ? formatAge(copy, row.lastSeenAt, now) : ""}
            </span>
          </div>
        ))
      )}
    </section>
  );
}

/** "Compiled context": each file and where it stands; the icon recompiles them all. */
export function CompiledPanel({
  view,
  targets,
  onCompile,
  compiling,
}: {
  view: RecordsView;
  targets: TargetView[] | undefined;
  onCompile: () => void;
  compiling: boolean;
}) {
  const { l, space } = view;
  const c = l.today.compiled;
  return (
    <section className="mx-panel" aria-labelledby="today-compiled">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="today-compiled">
          {c.title}
        </h2>
        <Button
          variant="quiet"
          size="sm"
          icon="sync"
          aria-label={c.recompile}
          title={c.recompile}
          pending={compiling}
          disabled={space.role === "viewer" || !targets?.length}
          onClick={onCompile}
        />
      </header>
      {targets?.length ? (
        targets.map((target) => (
          <TargetRow
            key={target.id}
            space={space.slug}
            target={target}
            variant="plain"
          />
        ))
      ) : (
        <p className={styles.panelNote}>{c.none}</p>
      )}
    </section>
  );
}
