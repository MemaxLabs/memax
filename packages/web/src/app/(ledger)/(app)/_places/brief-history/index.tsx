"use client";

import { useMemo, useState } from "react";
import { AgentStamp, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import type { BriefVersionView } from "@/lib/v2/data/brief";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useBrief, useBriefVersions } from "../../_lib/compile";
import { useRecordsView, type RecordsView } from "../records-view";
import { diffFromParent } from "./history-diff";
import { versionMeta, versionTitle, versionWhen } from "./history-copy";
import { VersionDetail } from "./version-detail";
import styles from "./brief-history.module.css";

/**
 * The Brief's history (BriefHistory.png) at /[space]/brief/history:
 * every version (B-), newest first, with who changed it and when; the
 * chosen one's changes against the one before it, or against now, and
 * Restore (confirmed inline), which revises the Brief to its structure.
 */
export function BriefHistoryPlace() {
  const view = useRecordsView();
  const { space, l, copy } = view;
  const versions = useBriefVersions(space);
  const brief = useBrief(space);
  const h = l.brief.history;

  const header = (
    <PageHeader
      eyebrow={interpolate(copy.brief.eyebrow, { space: space.name })}
      title={h.title}
      lede={h.lede}
    />
  );
  if (versions.data === undefined) {
    if (versions.isError) {
      return (
        <PlaceError space={space} onRetry={() => void versions.refetch()} />
      );
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={h.versions}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, { place: h.title })}
        />
      </div>
    );
  }
  const items = versions.data.items;
  return (
    <div className={`mx-page ${styles.page}`}>
      {header}
      {items.length === 0 ? (
        <p className="mx-meta">{h.empty}</p>
      ) : (
        <History
          view={view}
          versions={items}
          memories={brief.data?.memories ?? {}}
        />
      )}
    </div>
  );
}

function History({
  view,
  versions,
  memories,
}: {
  view: RecordsView;
  versions: BriefVersionView[];
  memories: NonNullable<ReturnType<typeof useBrief>["data"]>["memories"];
}) {
  const { l, now, timeZone, locale } = view;
  const b = l.brief;
  const [chosen, setChosen] = useState(versions[0]!.ref);
  const selected = versions.find((v) => v.ref === chosen) ?? versions[0]!;
  const diffs = useMemo(
    () => new Map(versions.map((v) => [v.ref, diffFromParent(v, versions)])),
    [versions],
  );

  return (
    <div className={styles.grid}>
      <nav aria-label={b.history.versions} className="mx-panel">
        <ul className={styles.versions}>
          {versions.map((version) => {
            const diff = diffs.get(version.ref)!;
            const stamp = view.stamp(version.by);
            return (
              <li key={version.ref}>
                <button
                  type="button"
                  className={styles.version}
                  aria-current={
                    version.ref === selected.ref ? "true" : undefined
                  }
                  onClick={() => setChosen(version.ref)}
                >
                  {stamp ? <AgentStamp {...stamp} size="sm" /> : <span />}
                  <span>
                    <span className={styles.versionTitle}>
                      {versionTitle(b, version, diff, view.name)}
                    </span>
                    <span className={styles.versionMeta}>
                      {versionMeta(
                        b,
                        version,
                        diff,
                        versionWhen(b, version.at, now, timeZone, locale),
                      )}
                    </span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      </nav>
      <VersionDetail
        key={selected.ref}
        view={view}
        version={selected}
        versions={versions}
        memories={memories}
      />
    </div>
  );
}
