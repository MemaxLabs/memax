"use client";

import { Button, formatNodes, Icon, type IconName } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import { CLOUD_POSTURE } from "@/lib/v2/cloud-posture";
import {
  placeText,
  processorRows,
  unreachableLines,
} from "@/lib/v2/settings-copy";
import { CONTACT, docsPage } from "./links";
import { SiteFrame } from "./site-frame";
import site from "./site.module.css";
import styles from "./security.module.css";

const RULES: { key: RuleKey; icon: IconName }[] = [
  { key: "receipts", icon: "receipt" },
  { key: "propose", icon: "agents" },
  { key: "outside", icon: "shield" },
  { key: "forget", icon: "forget" },
  { key: "secrets", icon: "alert" },
  { key: "leave", icon: "file" },
];
/** What the CLI runs to take a space and check it (docs: memax export, verify-export). */
const EXPORT_COMMANDS = [
  "memax export --space my-project",
  "memax verify-export ./my-project",
];

type RuleKey =
  | "receipts"
  | "propose"
  | "outside"
  | "forget"
  | "secrets"
  | "leave";

/**
 * Trust and security (Security.png). The board's six rules and "Your
 * data", with its placeholders filled from what is true (plan 25 §5.16):
 * the region, encryption and certifications, the sub-processors with what
 * each sees, their pinned zero-retention hosts and fp8 floor, what Forget
 * can't reach, sealed receipts, export, passkeys, and where to report a
 * problem. The facts are Memax cloud's (lib/v2/cloud-posture.ts, from the
 * server's defaults) in Settings › Security's own words, so the two pages
 * say the same thing.
 */
export function SecurityScreen() {
  const { t, locale } = useLocale();
  const c = t.ledger.public.security;
  const words = t.ledger.settings.security;
  const posture = CLOUD_POSTURE;
  const place = (holds: string) => {
    const found = posture.residency.find((p) => p.holds === holds);
    return found ? placeText(words, found) : "";
  };
  const rows = processorRows(words, posture.processors, locale);
  const unreachable = unreachableLines(words, posture, locale);
  const days = posture.backupDays;

  return (
    <SiteFrame current="trust">
      <section className={site.hero} aria-labelledby="security-title">
        <p className={site.eyebrow}>{c.eyebrow}</p>
        <h1 id="security-title" className={site.title}>
          <span className={site.line}>{c.titleLine1}</span>{" "}
          <span className={site.line}>{c.titleLine2}</span>
        </h1>
        <p className={site.lede}>{c.lede}</p>
      </section>

      <section aria-labelledby="rules-title">
        <h2 id="rules-title" className="mx-sr">
          {c.rules.label}
        </h2>
        <div className={styles.rules}>
          {RULES.map(({ key, icon }) => (
            <div key={key} className={styles.rule}>
              <Icon name={icon} size={20} />
              <h3>{c.rules[key].title}</h3>
              <p>{c.rules[key].body}</p>
              <small>{c.rules[key].where}</small>
            </div>
          ))}
        </div>
      </section>

      <section className={styles.section} aria-labelledby="data-title">
        <h2 id="data-title" className={site.sectionTitle}>
          {c.data.title}
        </h2>
        <div className={`mx-panel ${styles.table}`} role="table">
          <div className={styles.head} role="row">
            <span role="columnheader">{c.data.what}</span>
            <span role="columnheader">{c.data.where}</span>
            <span role="columnheader">{c.data.howLong}</span>
          </div>
          <DataRow
            what={c.data.memories.what}
            where={interpolate(c.data.cloud, { place: place("database") })}
            howLong={c.data.memories.howLong}
          />
          <DataRow
            what={c.data.notes.what}
            where={c.data.notes.where}
            howLong={c.data.notes.howLong}
          />
          <DataRow
            what={c.data.compiled.what}
            where={interpolate(c.data.compiled.where, {
              place: place("objects"),
            })}
            howLong={c.data.compiled.howLong}
          />
          <DataRow
            what={c.data.reads.what}
            where={c.data.reads.where}
            howLong={c.data.reads.howLong}
          />
          <DataRow
            what={c.data.code.what}
            where={c.data.code.where}
            howLong={c.data.code.howLong}
          />
          <DataRow
            what={c.data.backups.what}
            where={place("database")}
            howLong={
              days === 1
                ? c.data.backups.howLongOne
                : interpolate(c.data.backups.howLong, { days })
            }
          />
        </div>

        <h3 className={styles.subTitle}>{c.data.runs}</h3>
        <dl className={styles.places}>
          {posture.residency.map((p) => (
            <div key={p.holds}>
              <dt>{words.residency.holds[p.holds]}</dt>
              <dd>{placeText(words, p)}</dd>
            </div>
          ))}
        </dl>
        <p className={`mx-meta ${styles.note}`}>
          {words.residency.encryption} {c.data.certifications}
        </p>
      </section>

      <section className={styles.section} aria-labelledby="processors-title">
        <h2 id="processors-title" className={site.sectionTitle}>
          {words.processors.title}
        </h2>
        <p className={styles.lede}>{c.processors.lede}</p>
        <div className={`mx-panel ${styles.table}`} role="table">
          <div className={styles.head} role="row">
            <span role="columnheader">{words.processors.service}</span>
            <span role="columnheader">{words.processors.gets}</span>
            <span role="columnheader">{words.processors.keeps}</span>
          </div>
          {rows.map((row) => (
            <div key={row.name} className={styles.row} role="row">
              <span role="cell" className={styles.strong}>
                {row.name}
              </span>
              <span role="cell">
                {row.gets.map((line) => (
                  <span key={line} className={styles.line}>
                    {line}
                  </span>
                ))}
              </span>
              <span
                role="cell"
                className={row.unconfirmed ? styles.unconfirmed : undefined}
              >
                {row.keeps}
              </span>
            </div>
          ))}
        </div>
        <p className={`mx-meta ${styles.note}`}>{c.processors.source}</p>
        <p className={`mx-meta ${styles.note}`}>{c.processors.original}</p>
      </section>

      <section className={styles.section} aria-labelledby="forget-title">
        <h2 id="forget-title" className={site.sectionTitle}>
          {words.forget.title}
        </h2>
        <p className={styles.lede}>{c.forget.lede}</p>
        <div className={styles.reach}>
          <div>
            <h3 className={styles.subTitle}>{words.forget.reaches}</h3>
            <ul className={styles.list}>
              {(["words", "files", "agents", "tombstone"] as const).map(
                (key) => (
                  <li key={key}>
                    <Icon name="check" size={16} />
                    <span>{words.forget.reach[key]}</span>
                  </li>
                ),
              )}
            </ul>
          </div>
          <div>
            <h3 className={styles.subTitle}>{words.forget.out}</h3>
            <ul className={styles.list}>
              {unreachable.map((line) => (
                <li key={line}>
                  <Icon name="alert" size={16} />
                  <span>{line}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </section>

      <section className={styles.section} aria-labelledby="check-title">
        <h2 id="check-title" className={site.sectionTitle}>
          {c.check.title}
        </h2>
        <div className={styles.checks}>
          <div className={styles.check}>
            <h3>{c.check.receipts.title}</h3>
            <p>{c.check.receipts.body}</p>
          </div>
          <div className={styles.check}>
            <h3>{c.check.export.title}</h3>
            <p>{c.check.export.body}</p>
            <pre className={styles.cmd}>
              <code>
                {EXPORT_COMMANDS.map((command) => (
                  <span key={command} className={styles.cmdLine}>
                    <span aria-hidden="true">› </span>
                    {command}
                  </span>
                ))}
              </code>
            </pre>
          </div>
          <div className={styles.check}>
            <h3>{c.check.passkeys.title}</h3>
            <p>{c.check.passkeys.body}</p>
          </div>
          <div className={styles.check}>
            <h3>{c.check.sessions.title}</h3>
            <p>{c.check.sessions.body}</p>
          </div>
        </div>
      </section>

      <section className={styles.report} aria-labelledby="report-title">
        <div>
          <h2 id="report-title" className={styles.reportTitle}>
            {c.report.title}
          </h2>
          <p className={styles.reportBody}>
            {formatNodes(c.report.body, {
              email: (
                <a href={`mailto:${CONTACT.security}`}>{CONTACT.security}</a>
              ),
            })}
          </p>
        </div>
        <Button
          variant="secondary"
          size="lg"
          render={<a href={docsPage("/concepts/security")} />}
        >
          {c.report.docs}
        </Button>
      </section>
    </SiteFrame>
  );
}

function DataRow({
  what,
  where,
  howLong,
}: {
  what: string;
  where: string;
  howLong: string;
}) {
  return (
    <div className={styles.row} role="row">
      <span role="rowheader" className={styles.strong}>
        {what}
      </span>
      <span role="cell">{where}</span>
      <span role="cell">{howLong}</span>
    </div>
  );
}
