"use client";

import { Icon, SyncTarget, type IconName } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatWhen } from "@/lib/v2/copy";
import type { MemoryRecord } from "@/lib/v2/data/memories";
import type { RecordsView } from "../records-view";
import styles from "./memory.module.css";

const SOURCE_ICONS: Record<string, IconName> = {
  pr: "commit",
  session: "terminal",
  file: "file",
  url: "globe",
  issue: "chat",
  email: "chat",
  note: "memories",
  import: "file",
};

/** Memory.png's side: Sources, Reaches (the compiled files) and "Stays true while". */
export function MemorySide({
  view,
  record,
}: {
  view: RecordsView;
  record: MemoryRecord;
}) {
  const { l, rc, copy, now, timeZone, locale } = view;
  const p = l.memory.page;
  const reach = record.reach;
  return (
    <aside className={styles.side}>
      <section className="mx-panel">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title">{p.sources}</h2>
          <span className="mx-meta">{record.sources.length}</span>
        </header>
        {record.sources.length === 0 ? (
          <p className={styles.note}>{p.noSources}</p>
        ) : (
          record.sources.map((source) => (
            <div key={source.key} className={styles.source}>
              <span className={styles.sourceIcon}>
                <Icon name={SOURCE_ICONS[source.kind] ?? "file"} />
              </span>
              {source.url ? (
                <a
                  className={styles.sourceLabel}
                  href={source.url}
                  target="_blank"
                  rel="noreferrer"
                >
                  {source.label}
                </a>
              ) : (
                <span className={styles.sourceLabel}>{source.label}</span>
              )}
              <span className="mx-meta">
                {source.provider ??
                  (rc.sourceKinds as Record<string, string>)[source.kind] ??
                  source.kind}
              </span>
            </div>
          ))
        )}
      </section>

      <section className="mx-panel">
        <header className="mx-panel-head">
          <h2 className="mx-panel-title">{p.reaches}</h2>
          {reach ? (
            <span className="mx-meta">
              {interpolate(p.reachesMeta, {
                files: count(p.filesOne, p.files, reach.files),
                agents: count(p.agentsOne, p.agents, reach.agents),
              })}
            </span>
          ) : null}
        </header>
        {record.reaches ? (
          record.reaches.map((target) => (
            <SyncTarget
              key={target.path}
              compact
              path={target.path}
              tool={target.tool}
              status={target.status}
            />
          ))
        ) : (
          // PLACEHOLDER: compile targets aren't served by /v2 yet.
          <p className={styles.note}>{p.reachesLater}</p>
        )}
      </section>

      {record.conditions.length > 0 ? (
        <section className="mx-panel">
          <header className="mx-panel-head">
            <h2 className="mx-panel-title">{p.staysTrue}</h2>
          </header>
          <div className={styles.conditions}>
            {record.conditions.map((condition) => (
              <span key={condition.key}>
                {condition.code ? (
                  <>
                    <code className="mx-code">{condition.code}</code>{" "}
                  </>
                ) : null}
                {condition.text}
              </span>
            ))}
            {record.checked ? (
              <span className={`mx-meta ${styles.checked} ${styles.prose}`}>
                {interpolate(
                  record.conditions.length > 1 ? p.checked : p.checkedOne,
                  {
                    when: formatWhen(
                      copy,
                      record.checked.at,
                      now,
                      timeZone,
                      locale,
                    ),
                    name: view.name(record.checked.by),
                  },
                )}
              </span>
            ) : null}
          </div>
        </section>
      ) : null}
    </aside>
  );
}
