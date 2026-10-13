"use client";

import {
  AgentStamp,
  Button,
  MemoryList,
  MemoryRow,
  ReviewCard,
  StateMark,
  Terminal,
  type TerminalLine,
} from "@memaxlabs/ledger";
import { interpolate } from "@/i18n/interpolate";
import { useLocale } from "@/i18n";
import { cliCommand } from "@/lib/v2/cli";
import { CommandBox } from "./command-box";
import { HOW_IT_WORKS_ID, LINKS } from "./links";
import { SiteFrame } from "./site-frame";
import styles from "./landing.module.css";
import site from "./site.module.css";

/** The init the Landing offers: this build's CLI (lib/v2/cli.ts). */
const INIT = cliCommand("init");

/**
 * Landing (Landing.png, plan 25 D1): the promise, an example space with a
 * fresh seal, the day-one cleanup, how it works, the three principles.
 *
 * Changes from the board, each for a decision since it was drawn:
 * - `npx memax-cli init` (D12), this build's command.
 * - The personal layer joins the "6 files, 3 conflicts" demo (D1).
 * - Targets read as the app words them (D2/D3): Cursor reads AGENTS.md
 *   unless scoped rules are on, and ChatGPT is live over its connector,
 *   never "in sync".
 * - The example card is kept and still: no Undo or Keep that would do
 *   nothing outside the app.
 */
export function LandingScreen() {
  const { t } = useLocale();
  const c = t.ledger.public.landing;
  const tx = c.dayOne.transcript;
  const lines: TerminalLine[] = [
    { kind: "cmd", text: tx.command },
    { kind: "dim", text: tx.reading },
    { kind: "out", text: tx.claude },
    { kind: "out", text: tx.agents },
    { kind: "out", text: tx.cursor },
    { kind: "out", text: tx.codex },
    { kind: "dim", text: tx.folded },
    { kind: "blank" },
    { kind: "warn", text: tx.testCommand },
    { kind: "warn", text: tx.packageManager },
    { kind: "warn", text: tx.deploy },
    { kind: "blank" },
    { kind: "proposed", text: tx.summary },
    { kind: "proposed", text: tx.personal },
  ];

  return (
    <SiteFrame current="product">
      <section className={styles.hero} aria-labelledby="landing-title">
        <div>
          <p className={styles.eyebrow}>{c.eyebrow}</p>
          <h1 id="landing-title" className={styles.title}>
            <span className={site.line}>{c.titleLine1}</span>{" "}
            <span className={site.line}>{c.titleLine2}</span>
          </h1>
          <p className={styles.lede}>{c.lede}</p>
          <div className={styles.start}>
            <CommandBox command={INIT} />
            {/* A plain fragment link, so focus moves on with the scroll. */}
            <Button
              variant="quiet"
              size="lg"
              icon="arrow-right"
              render={<a href={`#${HOW_IT_WORKS_ID}`} />}
            >
              {c.howItWorks}
            </Button>
          </div>
          <p className={`mx-meta ${styles.worksWith}`}>{c.worksWith}</p>
        </div>
        <ExampleSpace />
      </section>

      <section className={styles.dayOne} aria-labelledby="day-one">
        <div>
          <p className={styles.kicker}>{c.dayOne.eyebrow}</p>
          <h2 id="day-one" className={styles.dayOneTitle}>
            {c.dayOne.title}
          </h2>
          <p className={styles.body}>{c.dayOne.body}</p>
          <p className={styles.body}>{c.dayOne.personal}</p>
          <dl className={styles.stats} aria-label={c.dayOne.stats.label}>
            <div>
              <dt>6</dt>
              <dd className="mx-meta">{c.dayOne.stats.files}</dd>
            </div>
            <div>
              <dt>3</dt>
              <dd className="mx-meta">{c.dayOne.stats.conflicts}</dd>
            </div>
            <div>
              <dt>1</dt>
              <dd className="mx-meta">{c.dayOne.stats.brief}</dd>
            </div>
          </dl>
        </div>
        <Terminal title={tx.title} lines={lines} />
      </section>

      <section
        id={HOW_IT_WORKS_ID}
        className={styles.how}
        aria-labelledby="how-title"
      >
        <div className={styles.howHead}>
          <p className={styles.kicker}>{c.how.eyebrow}</p>
          <h2 id="how-title" className={styles.howTitle}>
            {c.how.title}
          </h2>
        </div>
        <div className={styles.howGrid}>
          <div className={styles.column}>
            <h3 className={styles.columnTitle}>{c.how.compiled}</h3>
            <ul className={`mx-panel ${styles.targets}`}>
              <li className={styles.target}>
                <AgentStamp agent="claude-code" showName />
                <code>CLAUDE.md</code>
                <StateMark state="kept" label={c.how.inSync} />
              </li>
              <li className={styles.target}>
                <AgentStamp agent="codex" showName />
                <code>AGENTS.md</code>
                <StateMark state="kept" label={c.how.inSync} />
              </li>
              <li className={styles.target}>
                <AgentStamp agent="cursor" showName />
                <code>AGENTS.md</code>
                <StateMark state="kept" label={c.how.inSync} />
              </li>
              <li className={styles.target}>
                <AgentStamp agent="chatgpt" showName />
                <code>{c.how.projectInstructions}</code>
                <span className={styles.delivery}>{c.how.overConnector}</span>
              </li>
            </ul>
          </div>
          <div className={styles.column}>
            <h3 className={styles.columnTitle}>{c.how.live}</h3>
            <ul className={`mx-panel ${styles.targets}`}>
              {(["claude", "gemini", "opencode", "copilot"] as const).map(
                (agent) => (
                  <li key={agent} className={styles.liveRow}>
                    <AgentStamp agent={agent} showName />
                    <span className="mx-meta">{c.how.readsAndProposes}</span>
                  </li>
                ),
              )}
            </ul>
          </div>
        </div>
      </section>

      <section className={styles.principles} aria-label={c.principles.label}>
        {(["write", "review", "forget"] as const).map((key, i) => (
          <div key={key}>
            <span className={styles.number} aria-hidden="true">
              {String(i + 1).padStart(2, "0")}
            </span>
            <h3 className={styles.principleTitle}>{c.principles[key].title}</h3>
            <p className={styles.principleBody}>{c.principles[key].body}</p>
          </div>
        ))}
      </section>

      <section className={styles.cta} aria-labelledby="cta-title">
        <h2 id="cta-title" className={styles.ctaTitle}>
          {c.cta.title}
        </h2>
        <div className={styles.ctaActions}>
          <Button variant="primary" size="lg" href={LINKS.signIn}>
            {c.cta.start}
          </Button>
          <Button variant="secondary" size="lg" href={LINKS.pricing}>
            {c.cta.pricing}
          </Button>
        </div>
      </section>
    </SiteFrame>
  );
}

/**
 * The hero's example: a space's ledger with two kept memories and one
 * just kept, its seal fresh. It is an illustration, labelled as one, and
 * nothing in it acts.
 */
function ExampleSpace() {
  const { t } = useLocale();
  const e = t.ledger.public.landing.example;
  return (
    <figure className={styles.example} aria-label={e.label}>
      <div className={styles.exampleHead}>
        <span className={styles.exampleSpace}>memax-v2</span>
        <span className="mx-meta">
          {interpolate(e.counts, { kept: 214, waiting: 5 })}
        </span>
      </div>
      <MemoryList>
        <MemoryRow person="ZZ" time={e.oct2} id="M-0219">
          {e.river}
        </MemoryRow>
        <MemoryRow person="JY" time={e.sep12} id="M-0098">
          {e.errors}
        </MemoryRow>
      </MemoryList>
      <div className={styles.exampleCard}>
        <ReviewCard
          kept
          agent="claude-code"
          time={e.hourAgo}
          id="M-0430"
          keptBy="ZZ"
          keptDate={e.keptDate}
          statement={e.confirm}
        />
      </div>
    </figure>
  );
}
