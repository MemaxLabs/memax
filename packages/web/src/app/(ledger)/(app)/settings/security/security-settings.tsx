"use client";

import Link from "next/link";
import { Button, PageHeader, Terminal } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { sealSentences } from "@/lib/v2/activity/seal";
import type { SpaceSummary } from "@/lib/v2/data/types";
import {
  agentSummary,
  placeText,
  processorRows,
  unreachableLines,
} from "@/lib/v2/settings-copy";
import { useSpaces } from "../../_lib/data";
import { useSecurity } from "../../_lib/settings";
import { useSpaceView } from "../../_lib/space-context";
import { PlaceSkeleton } from "../../_components/skeleton";
import { useSeal } from "../../_places/activity/queries";
import { useMyAgents } from "../../_places/agents/queries";
import { usePlace } from "../../_places/place";
import styles from "./security.module.css";

/** One space's seal: how far its receipts are sealed, verified, and the key. */
function SealRow({ space }: { space: SpaceSummary }) {
  const { t, locale } = useLocale();
  const copy = t.ledger.settings.security.receipts;
  const { now, timeZone } = usePlace();
  const seal = useSeal(space).data;
  const lines = seal
    ? sealSentences(t.ledger.activity, t.ledger.app, seal, {
        now,
        timeZone,
        locale,
      })
    : [];
  return (
    <li className={styles.row}>
      <span className={styles.label}>{space.name}</span>
      <span className={styles.text} aria-busy={seal ? undefined : true}>
        {!seal ? (
          <p>{copy.loading}</p>
        ) : lines.length === 0 ? (
          <p>{copy.nothing}</p>
        ) : (
          lines.map((line, i) => (
            <p key={i} className={line.problem ? styles.problem : undefined}>
              {line.text}
            </p>
          ))
        )}
        {seal?.keyId ? (
          <p className={styles.mono}>
            {interpolate(copy.signedWith, { key: seal.keyId })}
          </p>
        ) : null}
      </span>
    </li>
  );
}

/**
 * Settings › Security: what Memax keeps for the person and how it holds,
 * from the server's own configuration (/v2/security) and their spaces'
 * checkpoints and agents. Read-only. The receipts' seal per space; the
 * commands to export and verify a space; what Forget reaches and what it
 * can't (backups for the configured days, git history, agents' own
 * memories, providers that keep what they were sent); where data lives;
 * every processor that sees a memory's words, with Voyage's retention
 * said as unconfirmed until the server says otherwise; the agents'
 * autonomy; and what a Keep counts as.
 */
export function SecuritySettings() {
  const { t, locale } = useLocale();
  const copy = t.ledger.settings.security;
  const security = useSecurity();
  const spaces = (useSpaces().data ?? []).filter((s) => s.onV2 !== false);
  const agents = useMyAgents().data;
  const { space } = useSpaceView();
  const s = security.data;

  if (!s) {
    return (
      <>
        <PageHeader
          title={copy.title}
          lede={copy.lede}
          className={styles.head}
        />
        {security.isError ? (
          <section className="mx-panel" role="alert">
            <header className="mx-panel-head">
              <h2 className="mx-panel-title">{copy.loadFailed}</h2>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void security.refetch()}
              >
                {copy.retry}
              </Button>
            </header>
          </section>
        ) : (
          <PlaceSkeleton
            title={copy.title}
            status={copy.loading}
            label={copy.loading}
          />
        )}
      </>
    );
  }

  const slug = (space.onV2 !== false ? space : spaces[0])?.slug ?? space.slug;
  const processors = processorRows(copy, s.processors, locale);
  const p = copy.processors;

  return (
    <>
      <PageHeader title={copy.title} lede={copy.lede} className={styles.head} />

      <section className="mx-panel" aria-labelledby="sec-receipts">
        <header className="mx-panel-head">
          <h2 id="sec-receipts" className="mx-panel-title">
            {copy.receipts.title}
          </h2>
          <span className="mx-meta">{copy.receipts.meta}</span>
        </header>
        {spaces.length === 0 ? (
          <div className={styles.body}>
            <p>{copy.receipts.noSpaces}</p>
          </div>
        ) : (
          <ul className={styles.rows} aria-label={copy.receipts.label}>
            {spaces.map((sp) => (
              <SealRow key={sp.id} space={sp} />
            ))}
          </ul>
        )}
        <p className={styles.footer}>{t.ledger.activity.seal.hint}</p>
      </section>

      <section className="mx-panel" aria-labelledby="sec-export">
        <header className="mx-panel-head">
          <h2 id="sec-export" className="mx-panel-title">
            {copy.export.title}
          </h2>
        </header>
        <div className={styles.body}>
          <p>{copy.export.body}</p>
          <Terminal
            lines={[
              { kind: "cmd", text: `memax export --space ${slug}` },
              { kind: "cmd", text: `memax verify-export memax-export/${slug}` },
            ]}
          />
          <p className={styles.note}>{copy.export.note}</p>
        </div>
      </section>

      <section className="mx-panel" aria-labelledby="sec-forget">
        <header className="mx-panel-head">
          <h2 id="sec-forget" className="mx-panel-title">
            {copy.forget.title}
          </h2>
        </header>
        <div className={styles.reach}>
          <div>
            <h3 className={styles.reachTitle}>{copy.forget.reaches}</h3>
            <ul>
              <li>{copy.forget.reach.words}</li>
              <li>{copy.forget.reach.files}</li>
              <li>{copy.forget.reach.agents}</li>
              <li>{copy.forget.reach.tombstone}</li>
            </ul>
          </div>
          <div>
            <h3 className={styles.reachTitle}>{copy.forget.out}</h3>
            <ul>
              {unreachableLines(copy, s, locale).map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </div>
        </div>
      </section>

      <section className="mx-panel" aria-labelledby="sec-residency">
        <header className="mx-panel-head">
          <h2 id="sec-residency" className="mx-panel-title">
            {copy.residency.title}
          </h2>
        </header>
        <div role="table" aria-labelledby="sec-residency">
          <div role="row" className={`${styles.table} ${styles.th}`}>
            <span role="columnheader">{copy.residency.what}</span>
            <span role="columnheader">{copy.residency.where}</span>
          </div>
          {s.residency.map((place) => (
            <div role="row" key={place.holds} className={styles.table}>
              <span role="rowheader">{copy.residency.holds[place.holds]}</span>
              <span role="cell">{placeText(copy, place)}</span>
            </div>
          ))}
        </div>
        <p className={styles.footer}>{copy.residency.encryption}</p>
      </section>

      <section className="mx-panel" aria-labelledby="sec-processors">
        <header className="mx-panel-head">
          <h2 id="sec-processors" className="mx-panel-title">
            {p.title}
          </h2>
          <span className="mx-meta">{p.meta}</span>
        </header>
        {processors.length === 0 ? (
          <div className={styles.body}>
            <p>{p.none}</p>
          </div>
        ) : (
          <div role="table" aria-labelledby="sec-processors">
            <div
              role="row"
              className={`${styles.table} ${styles.three} ${styles.th}`}
            >
              <span role="columnheader">{p.service}</span>
              <span role="columnheader">{p.gets}</span>
              <span role="columnheader">{p.keeps}</span>
            </div>
            {processors.map((row) => (
              <div
                role="row"
                key={row.name}
                className={`${styles.table} ${styles.three}`}
              >
                <span role="rowheader" className={styles.label}>
                  {row.name}
                </span>
                <span role="cell">
                  <ul className={styles.gets}>
                    {row.gets.map((g) => (
                      <li key={g}>{g}</li>
                    ))}
                  </ul>
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
        )}
      </section>

      <section className="mx-panel" aria-labelledby="sec-agents">
        <header className="mx-panel-head">
          <h2 id="sec-agents" className="mx-panel-title">
            {copy.agents.title}
          </h2>
          <Link href="/settings/keys" className={styles.link}>
            {copy.agents.link}
          </Link>
        </header>
        <div className={styles.body}>
          {agents ? <p>{agentSummary(copy, agents, locale)}</p> : null}
          <p>{copy.agents.rule}</p>
        </div>
      </section>

      <section className="mx-panel" aria-labelledby="sec-assurance">
        <header className="mx-panel-head">
          <h2 id="sec-assurance" className="mx-panel-title">
            {copy.assurance.title}
          </h2>
          <span className={styles.session}>
            <span className="mx-meta">{copy.assurance.session}</span>
            <span className={styles.mono}>{s.assurance}</span>
          </span>
        </header>
        <div className={styles.body}>
          <p>{copy.assurance.body}</p>
          <p>{copy.assurance.needs}</p>
        </div>
      </section>
    </>
  );
}
