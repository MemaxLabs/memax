"use client";

import type { ReactNode } from "react";
import { Button, Icon, formatNodes, type IconName } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatDate, formatWhen } from "@/lib/v2/copy";
import type { ConditionLine, MemoryRecord } from "@/lib/v2/data/memories";
import { reachMeta } from "@/lib/v2/reads-copy";
import { TargetList } from "../../_components/target-list";
import type { RecordsView } from "../records-view";
import { useMemoryScope } from "./use-memory-scope";
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

/**
 * Memory.png's side: where it applies (a personal fact tied to
 * repositories, with Apply everywhere), Sources, Reaches (the compiled
 * files) and "Stays true while".
 */
export function MemorySide({
  view,
  record,
}: {
  view: RecordsView;
  record: MemoryRecord;
}) {
  const { l, rc, copy, now, timeZone, locale } = view;
  const p = l.memory.page;
  const reach = reachMeta(p, record.reach, locale);
  return (
    <aside className={styles.side}>
      {record.repos?.length ? <AppliesIn view={view} record={record} /> : null}
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
          {reach ? <span className="mx-meta">{reach}</span> : null}
        </header>
        {record.reaches?.length ? (
          <TargetList space={view.space} targets={record.reaches} />
        ) : (
          // Nothing compiles it yet, or the targets didn't load.
          <p className={styles.note}>{p.reachesLater}</p>
        )}
      </section>

      {record.conditions.length > 0 ? (
        <section className="mx-panel" aria-labelledby="memory-stays-true">
          <header className="mx-panel-head">
            <h2 id="memory-stays-true" className="mx-panel-title">
              {p.staysTrue}
            </h2>
          </header>
          <div className={styles.conditions}>
            {record.conditions.map((condition) => (
              <span key={condition.key}>{conditionWords(view, condition)}</span>
            ))}
            {record.checked ? (
              <span className={`mx-meta ${styles.checked} ${styles.prose}`}>
                {interpolate(
                  record.checked.agent
                    ? record.conditions.length > 1
                      ? p.checkedFor
                      : p.checkedForOne
                    : record.conditions.length > 1
                      ? p.checked
                      : p.checkedOne,
                  {
                    when: formatWhen(
                      copy,
                      record.checked.at,
                      now,
                      timeZone,
                      locale,
                    ),
                    branch: record.checked.branch,
                    name: record.checked.agent
                      ? view.name(record.checked.agent)
                      : "",
                  },
                )}
              </span>
            ) : record.conditions.some((c) => c.kind !== "date") ? (
              // Memax checks dates itself; the rest wait for a check.
              <span className={`mx-meta ${styles.checked} ${styles.prose}`}>
                {p.notChecked}
              </span>
            ) : null}
          </div>
        </section>
      ) : null}
    </aside>
  );
}

/** One condition in words, its paths, text and dependency in mono. */
function conditionWords(view: RecordsView, c: ConditionLine): ReactNode {
  const w = view.l.memory.page.conditions;
  const code = (s: string) => <code className="mx-code">{s}</code>;
  switch (c.kind) {
    case "path_exists":
      return c.glob
        ? formatNodes(w.glob, { glob: code(c.glob) })
        : formatNodes(w.path, { path: code(c.path ?? "") });
    case "file_contains":
      return formatNodes(c.word ? w.containsWord : w.contains, {
        path: code(c.path),
        text: code(c.text),
      });
    case "dep_present":
      return c.range
        ? formatNodes(w.depRange, {
            name: code(c.name),
            range: c.range,
            manifest: c.manifest,
          })
        : formatNodes(w.dep, { name: code(c.name), manifest: c.manifest });
    case "file_unchanged":
      return formatNodes(w.unchanged, { path: code(c.path) });
    case "pr_state":
      return interpolate(w.pr[c.state], {
        number: String(c.number),
        repo: c.repo,
      });
    case "date":
      return interpolate(w.until, {
        date: formatDate(new Date(c.at), view.timeZone, view.locale),
      });
  }
}

/** The repositories a memory applies in, and Apply everywhere. */
function AppliesIn({
  view,
  record,
}: {
  view: RecordsView;
  record: MemoryRecord;
}) {
  const p = view.l.memory.page;
  const scope = useMemoryScope(view.space, record);
  const repos = record.repos ?? [];
  return (
    <section className="mx-panel" aria-labelledby="memory-applies-in">
      <header className="mx-panel-head">
        <h2 id="memory-applies-in" className="mx-panel-title">
          {p.appliesIn}
        </h2>
        <span className="mx-meta">{repos.length}</span>
      </header>
      <div className={styles.conditions}>
        {repos.map((repo) => (
          <code key={repo} className="mx-code">
            {repo}
          </code>
        ))}
        <span className={styles.prose}>{p.appliesInNote}</span>
      </div>
      <div className={styles.scopeAction}>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void scope.everywhere()}
          pending={scope.pending}
        >
          {p.everywhere}
        </Button>
      </div>
    </section>
  );
}
