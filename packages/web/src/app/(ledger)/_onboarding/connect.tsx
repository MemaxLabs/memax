"use client";

import { useRouter } from "next/navigation";
import {
  AgentStamp,
  Button,
  Icon,
  Segmented,
  useLedger,
  type SegmentedOption,
} from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import {
  AUTONOMY_LEVELS,
  autonomyIn,
  type AgentConnectionView,
  type Autonomy,
} from "@/lib/v2/data/agents";
import { agentsOfImport } from "@/lib/v2/data/imports";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { setupHref } from "@/lib/v2/onboarding/routes";
import { AGENT_START, AGENT_WHERE } from "./transcript";
import { useSpaceAgents } from "../(app)/_places/agents/queries";
import { useUnavailable } from "../(app)/_places/agents/agent-row-control";
import { useAutonomy } from "../(app)/_places/agents/use-agent-commands";
import { OnboardingPage } from "./frame";
import { useSetupImport, useSetupSpace } from "./queries";
import { CopyCommandButton } from "./setup-bits";
import styles from "./connect.module.css";

type Copy = ReturnType<
  typeof useLocale
>["t"]["ledger"]["onboarding"]["connect"];

/** The board's order: the agents init finds, then ChatGPT, then the rest. */
const ORDER = [
  "claude-code",
  "codex",
  "cursor",
  "gemini",
  "chatgpt",
  "copilot",
  "opencode",
] as const;

type Row =
  | { kind: "connected"; agent: string; connection: AgentConnectionView }
  | { kind: "found"; agent: string }
  | { kind: "chatgpt" }
  | { kind: "missing"; agent: string };

/** The rows: each agent's connection in the space, else what init found, else how to connect it. */
export function connectRows(
  connections: readonly AgentConnectionView[],
  found: readonly string[],
): Row[] {
  const live = connections.filter((c) => c.state !== "disconnected");
  const rows: Row[] = ORDER.map((agent): Row => {
    const connection = live.find((c) => c.agent === agent);
    if (connection) return { kind: "connected", agent, connection };
    if (agent === "chatgpt") return { kind: "chatgpt" };
    if (found.includes(agent)) return { kind: "found", agent };
    return { kind: "missing", agent };
  });
  // Connections of agents the board doesn't list (Claude, an agent of
  // their own) come after ChatGPT.
  for (const connection of live) {
    if (!(ORDER as readonly string[]).includes(connection.agent)) {
      rows.splice(5, 0, {
        kind: "connected",
        agent: connection.agent,
        connection,
      });
    }
  }
  return rows;
}

/**
 * Connect (Connect.png, step 1 of 3) at /setup/agents: which agents share
 * the space's context, and what each may do. The web can't look at the
 * person's machine, so the rows come from the record: agents connected
 * to the space, with their autonomy live (raising needs the person on
 * the web, which this is), the ones `memax init` found by their files and
 * that connect on their next start, ChatGPT through its connector, and
 * how to connect the rest. Reading their files is init's job, so the
 * primary action goes on to FirstRun.
 */
export function ConnectScreen() {
  const { t } = useLocale();
  const copy = t.ledger.onboarding.connect;
  const frame = t.ledger.onboarding.frame;
  const router = useRouter();
  const setup = useSetupSpace();
  const space = setup.space ?? null;
  const next = setupHref("import", { space: space?.slug });
  const nextKey = useKeycap("setup.next");
  useHotkey("setup.next", () => router.push(next));

  return (
    <OnboardingPage meta={interpolate(frame.step, { n: 1 })}>
      <div className={styles.layout}>
        <section className={styles.main}>
          <div>
            <p className="mx-page-eyebrow">
              {space
                ? interpolate(frame.newSpace, { space: space.slug })
                : frame.firstSpace}
            </p>
            <h1 className={styles.title}>{copy.title}</h1>
            <p className={styles.lede}>{space ? copy.lede : copy.ledeNone}</p>
          </div>
          {space ? (
            <SpaceRows space={space} copy={copy} />
          ) : (
            <section className="mx-panel">
              {connectRows([], []).map((row) => (
                <AgentLine
                  key={rowKey(row)}
                  row={row}
                  copy={copy}
                  space={null}
                />
              ))}
            </section>
          )}
          <div className="mx-inline">
            <Button variant="primary" size="lg" kbd={nextKey} href={next}>
              {copy.primary}
            </Button>
            <CopyCommandButton
              command={
                space
                  ? `npx memax-cli init --space ${space.slug}`
                  : "npx memax-cli init"
              }
              variant="quiet"
              size="lg"
            >
              {copy.terminal}
            </CopyCommandButton>
          </div>
        </section>
        <aside className={styles.side}>
          <Levels copy={copy} />
          <div className={styles.note}>
            <span className={styles.noteIcon}>
              <Icon name="shield" />
            </span>
            <span>{copy.note}</span>
          </div>
        </aside>
      </div>
    </OnboardingPage>
  );
}

function rowKey(row: Row): string {
  return row.kind === "chatgpt" ? "chatgpt" : row.agent;
}

function SpaceRows({ space, copy }: { space: SpaceSummary; copy: Copy }) {
  const agents = useSpaceAgents(space);
  const { view } = useSetupImport(space);
  const found = view.data ? agentsOfImport(view.data.summary) : [];
  const rows = connectRows(agents.data ?? [], found);
  return (
    <section className="mx-panel" aria-busy={agents.isPending || undefined}>
      {rows.map((row) => (
        <AgentLine key={rowKey(row)} row={row} copy={copy} space={space} />
      ))}
    </section>
  );
}

function useAutonomyOptions(): SegmentedOption<Autonomy>[] {
  const { strings } = useLedger();
  return AUTONOMY_LEVELS.map((level) => ({
    value: level,
    label: strings.autonomy[level],
  }));
}

function AgentLine({
  row,
  copy,
  space,
}: {
  row: Row;
  copy: Copy;
  space: SpaceSummary | null;
}) {
  const { agents } = useLedger();
  const options = useAutonomyOptions();
  const key = row.kind === "chatgpt" ? "chatgpt" : row.agent;
  const name = agents[key]?.name ?? key;
  const stamp = <AgentStamp agent={key} showName surface />;
  switch (row.kind) {
    case "connected":
      return space ? (
        <ConnectedLine
          connection={row.connection}
          space={space}
          copy={copy}
          stamp={stamp}
        />
      ) : null;
    case "found":
      return (
        <div className={styles.row}>
          <Check label={interpolate(copy.foundLabel, { agent: name })} />
          {stamp}
          <span className={styles.where}>
            <code>{AGENT_WHERE[key] ?? ""}</code>
            <span className="mx-meta">{copy.startsAt}</span>
          </span>
          <Segmented
            size="sm"
            label={interpolate(copy.autonomy, { agent: name })}
            options={options}
            value={AGENT_START[key] ?? "propose"}
            disabled
          />
        </div>
      );
    case "chatgpt":
      return (
        <div className={styles.row}>
          <span className={styles.box} aria-hidden="true" />
          {stamp}
          <span className="mx-meta">{copy.chatgpt}</span>
          <span>
            <Button
              size="sm"
              icon="arrow-up-right"
              href={
                space
                  ? `/${encodeURIComponent(space.slug)}/agents?overlay=connect`
                  : undefined
              }
              disabled={!space}
            >
              {copy.connectChatgpt}
            </Button>
          </span>
        </div>
      );
    case "missing":
      return (
        <div className={`${styles.row} ${styles.off}`}>
          <span />
          {stamp}
          <span className="mx-meta">{copy.notConnected}</span>
          <span className="mx-meta">
            <RunIt copy={copy} agent={key} />
          </span>
        </div>
      );
  }
}

/**
 * "Run npx memax-cli setup --mcp --only copilot where it's installed": what
 * init does for each agent it finds, for one, with the command as code.
 */
function RunIt({ copy, agent }: { copy: Copy; agent: string }) {
  const command = `npx memax-cli setup --mcp --only ${agent}`;
  const [before, after] = copy.runIt.split("{command}");
  return (
    <>
      {before}
      <code className={`mx-code ${styles.wrap}`}>{command}</code>
      {after}
    </>
  );
}

function ConnectedLine({
  connection,
  space,
  copy,
  stamp,
}: {
  connection: AgentConnectionView;
  space: SpaceSummary;
  copy: Copy;
  stamp: React.ReactNode;
}) {
  const options = useAutonomyOptions();
  const here = autonomyIn(connection, space.slug);
  const { shown, raising, choose } = useAutonomy({ agent: connection, space });
  const unavailable = useUnavailable(
    connection,
    here?.autonomy ?? "read",
    space,
  );
  return (
    <div className={`${styles.row} ${raising ? styles.raising : ""}`}>
      <Check
        label={interpolate(copy.connectedLabel, { agent: connection.name })}
      />
      {stamp}
      <span className={styles.where}>
        <code>{AGENT_WHERE[connection.agent] ?? connection.name}</code>
        <span className="mx-meta">{copy.connected}</span>
      </span>
      <Segmented
        size="sm"
        label={interpolate(copy.autonomy, { agent: connection.name })}
        options={options.map((o) => ({
          ...o,
          disabled: Boolean(unavailable[o.value]),
          disabledReason: unavailable[o.value],
        }))}
        value={shown}
        onChange={choose}
      />
    </div>
  );
}

/**
 * The board's checked box for an agent that is (or will be) connected. Not
 * a control: connecting is init's (or the agent's own sign-in), and the
 * level beside it is what the person changes here.
 */
function Check({ label }: { label: string }) {
  return (
    <span
      className={`${styles.box} ${styles.checked}`}
      role="img"
      aria-label={label}
    >
      <Icon name="check" size={12} />
    </span>
  );
}

function Levels({ copy }: { copy: Copy }) {
  const l = copy.levels;
  return (
    <section className="mx-panel" aria-labelledby="levels-title">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title" id="levels-title">
          {l.title}
        </h2>
      </header>
      <dl className={styles.levels}>
        <dt>{l.read}</dt>
        <dd>{l.readDetail}</dd>
        <dt>{l.propose}</dt>
        <dd>{l.proposeDetail}</dd>
        <dt>{l.write}</dt>
        <dd>{l.writeDetail}</dd>
      </dl>
    </section>
  );
}
