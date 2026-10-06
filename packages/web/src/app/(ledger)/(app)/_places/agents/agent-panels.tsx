"use client";

import {
  Button,
  Icon,
  MemoryList,
  MemoryRow,
  Redaction,
  Segmented,
  StateMark,
  SyncTarget,
  useLedger,
  type Autonomy,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { agoText } from "@/lib/v2/agents/copy";
import {
  count,
  formatReceiptTime,
  formatShortDate,
  joinList,
} from "@/lib/v2/copy";
import {
  AUTONOMY_LEVELS,
  autonomyIn,
  capabilities,
  type AgentDetailView,
  type AgentWrite,
} from "@/lib/v2/data/agents";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { usePlace } from "../place";
import { useUnavailable } from "./agent-row-control";
import { useAutonomy } from "./use-agent-commands";
import styles from "./agent-detail.module.css";

// The panels of one agent's page (AgentDetail.dc.html).

/** "What Codex may do": the level in this space, what each means, and the quarantine rule. */
export function MayPanel({
  detail,
  space,
}: {
  detail: AgentDetailView;
  space: SpaceSummary;
}) {
  const { t } = useLocale();
  const { strings } = useLedger();
  const copy = t.ledger.agents.detail.may;
  const agent = detail.connection;
  const here = autonomyIn(agent, space.slug);
  const { shown, choose, raising } = useAutonomy({ agent, space });
  const unavailable = useUnavailable(agent, here?.autonomy ?? "read", space);
  const held = detail.week.heldExternal;
  const vars = { agent: agent.name, space: space.name };
  return (
    <section className="mx-panel" aria-labelledby="agent-may">
      <header className="mx-panel-head">
        <h2 id="agent-may" className="mx-panel-title">
          {interpolate(copy.title, vars)}
        </h2>
      </header>
      <div className={styles.may} aria-busy={raising || undefined}>
        <Segmented<Autonomy>
          label={interpolate(copy.label, vars)}
          value={shown}
          onChange={choose}
          options={AUTONOMY_LEVELS.map((level) => ({
            value: level,
            label: strings.autonomy[level],
            hint: strings.autonomy[`${level}Hint`],
            disabled: unavailable[level] !== undefined,
            disabledReason: unavailable[level],
          }))}
        />
        {here ? null : (
          <p className={styles.note}>{interpolate(copy.notConnected, vars)}</p>
        )}
        <dl className={styles.levels}>
          {AUTONOMY_LEVELS.map((level) => {
            const on = here && shown === level;
            return (
              <div key={level} className={on ? styles.levelOn : styles.level}>
                <dt>{strings.autonomy[level]}</dt>
                <dd>{copy[level]}</dd>
              </div>
            );
          })}
        </dl>
        <p className={styles.quarantine}>
          <span className={styles.quarantineIcon}>
            <Icon name="shield" />
          </span>
          <span>
            {interpolate(copy.external, vars)}
            {held !== null ? ` ${count(copy.heldOne, copy.held, held)}` : null}
          </span>
        </p>
      </div>
    </section>
  );
}

function WriteRow({
  write,
  space,
}: {
  write: AgentWrite;
  space: SpaceSummary;
}) {
  const { t, locale } = useLocale();
  const { agents } = useLedger();
  const { now, timeZone } = usePlace();
  const copy = t.ledger.agents;
  const time = agoText(copy, write.at, now, timeZone, locale, { lower: true });
  const href = `/${encodeURIComponent(space.slug)}/memories/${encodeURIComponent(write.ref)}`;
  if (write.state === "forgotten") {
    return (
      <Redaction
        date={formatShortDate(new Date(write.at), timeZone, locale)}
        id={write.ref}
      />
    );
  }
  const note =
    write.note?.kind === "contradicts"
      ? interpolate(copy.detail.writes.contradicts, { ref: write.note.ref })
      : write.note?.kind === "proposed-in"
        ? interpolate(copy.detail.writes.proposedIn, {
            agent: agents[write.note.agent]?.name ?? write.note.agent,
            session: write.note.session,
          })
        : undefined;
  return (
    <MemoryRow
      state={write.state}
      unconfirmed={write.state === "conflict" ? true : undefined}
      agent={write.actor.agent}
      person={write.actor.person}
      action={write.action}
      time={time}
      id={write.ref}
      note={note}
      href={href}
    >
      {write.statement ??
        interpolate(copy.detail.writes.unread, { ref: write.ref })}
    </MemoryRow>
  );
}

/** Recent writes, with what became of them this week. */
export function WritesPanel({
  detail,
  space,
}: {
  detail: AgentDetailView;
  space: SpaceSummary;
}) {
  const { t } = useLocale();
  const copy = t.ledger.agents.detail.writes;
  const { week, connection } = detail;
  return (
    <section className="mx-panel" aria-labelledby="agent-writes">
      <header className="mx-panel-head">
        <h2 id="agent-writes" className="mx-panel-title">
          {copy.title}
        </h2>
        <span className="mx-meta">
          {interpolate(copy.meta, {
            writes: week.writes,
            kept: week.kept,
            rejected: week.rejected,
            waiting: week.waiting,
          })}
        </span>
      </header>
      {detail.recentWrites.length === 0 ? (
        <p className={styles.empty}>{copy.none}</p>
      ) : (
        <MemoryList>
          {detail.recentWrites.map((write) => (
            <WriteRow key={write.receiptId} write={write} space={space} />
          ))}
        </MemoryList>
      )}
      <div className={styles.all}>
        <Button
          variant="quiet"
          size="sm"
          icon="arrow-right"
          href={`/${encodeURIComponent(space.slug)}/activity?agent=${encodeURIComponent(connection.id)}`}
        >
          {interpolate(copy.all, { agent: connection.name })}
        </Button>
      </div>
    </section>
  );
}

/** Its latest sessions: reads, writes and when. */
export function SessionsPanel({ detail }: { detail: AgentDetailView }) {
  const { t, locale } = useLocale();
  const { now, timeZone, copy: app } = usePlace();
  const copy = t.ledger.agents.detail.sessions;
  return (
    <section className="mx-panel" aria-labelledby="agent-sessions">
      <header className="mx-panel-head">
        <h2 id="agent-sessions" className="mx-panel-title">
          {copy.title}
        </h2>
        <span className="mx-meta" aria-hidden="true">
          {copy.meta}
        </span>
      </header>
      {detail.sessions.length === 0 ? (
        <p className={styles.empty}>{copy.none}</p>
      ) : (
        <ul className={styles.sessions}>
          {detail.sessions.map((s) => {
            const base = interpolate(
              s.kind === "cloud"
                ? copy.cloud
                : s.kind === "cli"
                  ? copy.cli
                  : copy.plain,
              { ref: s.ref },
            );
            const label = s.handoff
              ? interpolate(copy.fromHandoff, { session: base, ref: s.handoff })
              : base;
            return (
              <li key={s.ref} className={styles.session}>
                <span className={styles.sessionName}>
                  {s.live ? (
                    <StateMark
                      state="working"
                      label={false}
                      title={copy.running}
                      aria-label={copy.running}
                    />
                  ) : null}
                  <span>{label}</span>
                </span>
                <span className={styles.n}>
                  <span className="mx-sr">{copy.readsLabel} </span>
                  {s.reads ?? "—"}
                </span>
                <span className={styles.n}>
                  <span className="mx-sr">{copy.writesLabel} </span>
                  {s.writes}
                </span>
                <span className="mx-meta">
                  {formatReceiptTime(app, s.lastAt, now, timeZone, locale)}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** How it's connected: when and by whom, through what, what it may do, where. */
export function ConnectionPanel({
  detail,
  space,
}: {
  detail: AgentDetailView;
  space: SpaceSummary;
}) {
  const { t, locale } = useLocale();
  const { now, timeZone } = usePlace();
  const copy = t.ledger.agents.detail.connection;
  const caps = t.ledger.agents.capabilities;
  const a = detail.connection;
  const date = formatShortDate(new Date(a.connectedAt), timeZone, locale);
  const here = autonomyIn(a, space.slug);
  const spaces = a.spaces.map((s) => s.name);
  return (
    <section className="mx-panel" aria-labelledby="agent-connection">
      <header className="mx-panel-head">
        <h2 id="agent-connection" className="mx-panel-title">
          {copy.title}
        </h2>
      </header>
      <dl className={styles.kv}>
        <dt>{copy.connected}</dt>
        <dd>
          {a.connectedBy === "you"
            ? interpolate(copy.byYou, { date })
            : a.connectedBy === "memax"
              ? interpolate(copy.carriedOver, { date })
              : date}
        </dd>
        <dt>{copy.through}</dt>
        <dd>{a.credential.kind === "oauth_grant" ? copy.oauth : copy.key}</dd>
        {a.clientId ? (
          <>
            <dt>{copy.client}</dt>
            <dd className={styles.client} title={a.clientId}>
              {a.clientId.replace(/^https?:\/\//, "")}
            </dd>
          </>
        ) : null}
        <dt>{copy.may}</dt>
        <dd>
          {capabilities(a, here?.autonomy ?? "read")
            .map((c) => caps[c])
            .join(locale === "zh" ? "、" : ", ")}
        </dd>
        <dt>{spaces.length > 1 ? copy.spaces : copy.space}</dt>
        <dd>
          {spaces.length === 0
            ? copy.none
            : spaces.length === 1
              ? interpolate(copy.only, { space: spaces[0]! })
              : joinList(spaces, locale)}
        </dd>
        <dt>{copy.lastUsed}</dt>
        <dd>
          {a.lastSeenAt
            ? agoText(t.ledger.agents, a.lastSeenAt, now, timeZone, locale, {
                lower: false,
              })
            : copy.never}
        </dd>
      </dl>
    </section>
  );
}

/** The file it compiles to, when compile targets are served. */
export function CompilesPanel({ detail }: { detail: AgentDetailView }) {
  const { t } = useLocale();
  const { agents } = useLedger();
  const copy = t.ledger.agents.detail.compiles;
  const target = detail.connection.target;
  return (
    <section className="mx-panel" aria-labelledby="agent-compiles">
      <header className="mx-panel-head">
        <h2 id="agent-compiles" className="mx-panel-title">
          {copy.title}
        </h2>
      </header>
      {target ? (
        <SyncTarget
          compact
          path={target.path}
          tool={
            target.sharedWith
              ? interpolate(copy.sharedWith, {
                  agent: agents[target.sharedWith]?.name ?? target.sharedWith,
                })
              : ""
          }
          status={target.status}
        />
      ) : (
        <p className={styles.empty}>
          {target === null ? copy.mcpOnly : copy.none}
        </p>
      )}
    </section>
  );
}

/** This week's numbers. */
export function WeekPanel({ detail }: { detail: AgentDetailView }) {
  const { t } = useLocale();
  const copy = t.ledger.agents.detail.week;
  const w = detail.week;
  return (
    <section className="mx-panel" aria-labelledby="agent-week">
      <header className="mx-panel-head">
        <h2 id="agent-week" className="mx-panel-title">
          {copy.title}
        </h2>
      </header>
      <dl className={styles.kv}>
        <dt>{copy.reads}</dt>
        <dd>
          {w.reads === null ? (
            <>
              <span aria-hidden="true">—</span>
              <span className="mx-sr">{copy.notRecorded}</span>
            </>
          ) : (
            w.reads.toLocaleString("en-US")
          )}
        </dd>
        <dt>{copy.proposals}</dt>
        <dd>{w.proposals}</dd>
        {w.questions ? (
          <>
            <dt>{copy.questions}</dt>
            <dd>
              {w.questions.waiting > 0
                ? interpolate(copy.questionsWaiting, { n: w.questions.asked })
                : w.questions.asked}
            </dd>
          </>
        ) : null}
        {w.handoffsReceived !== null ? (
          <>
            <dt>{copy.handoffs}</dt>
            <dd>{interpolate(copy.received, { n: w.handoffsReceived })}</dd>
          </>
        ) : null}
      </dl>
    </section>
  );
}
