"use client";

import { Button, Lineage, StateMark } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { MemoryRecord } from "@/lib/v2/data/memories";
import {
  forgottenLabel,
  goneLines,
  reachLines,
  tombstoneLines,
  tombstoneMeta,
  type TombstoneContext,
} from "@/lib/v2/tombstone-copy";
import { HeaderSkeleton } from "../../_components/skeleton";
import { useTombstone } from "../../_lib/records";
import type { RecordsView } from "../records-view";
import styles from "./memory.module.css";

function Panel({ title, lines }: { title: string; lines: string[] }) {
  return (
    <section className="mx-panel">
      <header className="mx-panel-head">
        <h2 className="mx-panel-title">{title}</h2>
      </header>
      <ul className={styles.panelList}>
        {lines.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
    </section>
  );
}

/**
 * A forgotten memory's page (Tombstone.png): the redaction, who asked
 * and when, how it was forgotten step by step (removed from Memax, each
 * file rewritten, each agent told) with where each stands, what is gone
 * and what is kept, and, said plainly, the copies Memax can't reach.
 * Never words. While the Forget propagates, it reads the tombstone again.
 */
export function Tombstone({
  view,
  record,
}: {
  view: RecordsView;
  record: MemoryRecord;
}) {
  const { l, rc, space, timeZone, locale, viewer } = view;
  const c = l.memory.tombstone;
  const tombstone = useTombstone(space, record.ref);
  const eyebrow = interpolate(c.eyebrow, {
    space: space.name,
    ref: record.ref,
  });

  const head = (label: string | null, meta: string | null, lede: string) => (
    <div className={styles.tombHead}>
      <div className="mx-page-eyebrow">{eyebrow}</div>
      <h1 className="mx-sr">{c.bar}</h1>
      <div className={styles.bars}>
        <span
          className={styles.bar}
          style={{ width: "78%" }}
          role="img"
          aria-label={c.bar}
        />
        <span
          className={styles.bar}
          style={{ width: "46%" }}
          aria-hidden="true"
        />
      </div>
      {label ? (
        <div className={styles.tombMeta}>
          <StateMark state="forgotten" label={label} />
          {meta ? <span className="mx-meta">{meta}</span> : null}
        </div>
      ) : null}
      <p className={styles.lede}>{lede}</p>
    </div>
  );

  const t = tombstone.data;
  if (!t) {
    return (
      <div className={`mx-page ${styles.page}`}>
        {head(null, null, c.lede)}
        {tombstone.isError ? (
          <p className={styles.waiting} role="alert">
            {c.failed}
            <Button
              variant="quiet"
              size="sm"
              onClick={() => void tombstone.refetch()}
            >
              {c.retry}
            </Button>
          </p>
        ) : t === undefined ? (
          <HeaderSkeleton />
        ) : null}
      </div>
    );
  }

  const ctx: TombstoneContext = {
    rc,
    app: view.copy,
    timeZone,
    locale,
    you: viewer ?? undefined,
    agentName: view.agentName,
  };
  const reach = reachLines(c, t, ctx);
  return (
    <div className={`mx-page ${styles.page}`}>
      {head(
        forgottenLabel(c, t, ctx),
        tombstoneMeta(c, t, ctx),
        t.status === "done" ? c.lede : c.ledeWorking,
      )}
      <div className={styles.body}>
        <section className={styles.main} aria-busy={t.status !== "done"}>
          <h2 className="mx-section-label">{c.how}</h2>
          <Lineage events={tombstoneLines(c, t, ctx)} />
        </section>
        <aside className={styles.side}>
          <Panel title={c.gone.title} lines={goneLines(c, t, ctx)} />
          <Panel
            title={c.keptBox.title}
            lines={[c.keptBox.id, c.keptBox.tombstone]}
          />
          {reach.length ? <Panel title={c.reach.title} lines={reach} /> : null}
          <p className={`mx-meta ${styles.footnote}`}>{c.footer}</p>
        </aside>
      </div>
    </div>
  );
}
