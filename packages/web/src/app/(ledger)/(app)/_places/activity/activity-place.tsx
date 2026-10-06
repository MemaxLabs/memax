"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { activityCsv, activityCsvName } from "@/lib/v2/activity/csv";
import type { Names } from "@/lib/v2/activity/sentence";
import { countWeek } from "@/lib/v2/activity/totals";
import type { ActivityFilter, WeeklyTotals } from "@/lib/v2/data/activity";
import { count } from "@/lib/v2/copy";
import { EmptyState } from "../../_components/empty-state";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useToast } from "../../_components/toasts";
import { useOverlays } from "../../_lib/overlays";
import { PlaceColumn, usePlace } from "../place";
import { ActivityLog } from "./activity-log";
import { useActivity } from "./queries";
import styles from "./activity.module.css";

/** The agent names, levels and surfaces the sentences need, from the Ledger. */
function useNames(): Names {
  const { locale } = useLocale();
  const { agents, strings } = useLedger();
  return useMemo(
    () => ({
      locale,
      agent: (key) => agents[key]?.name ?? key,
      level: (a) => strings.autonomy[a],
      surface: (s) => strings.surface[s],
    }),
    [agents, locale, strings],
  );
}

/** Downloads text as a file, in the browser. */
function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Activity (Activity.png, epic 1.1): every receipt in the space, newest
 * first, with this week's totals and a CSV of what's loaded. Nothing
 * changes without a line here.
 */
export function ActivityPlace() {
  const place = usePlace();
  const { space, copy, now, timeZone } = place;
  const { t } = useLocale();
  const a = t.ledger.activity;
  const names = useNames();
  const toast = useToast();
  const { openCommand } = useOverlays();
  const params = useSearchParams();
  const pathname = usePathname();
  const agentFilter = params?.get("agent") ?? null;
  const [filter, setFilter] = useState<ActivityFilter>("all");
  const query = useActivity(space);

  const pages = query.data?.pages;
  const loaded = useMemo(() => pages?.flatMap((p) => p.entries) ?? [], [pages]);
  const entries = agentFilter
    ? loaded.filter(
        (e) => e.actor.kind === "agent" && e.actor.connectionId === agentFilter,
      )
    : loaded;
  const filteredAgent = agentFilter
    ? loaded.find(
        (e) => e.actor.kind === "agent" && e.actor.connectionId === agentFilter,
      )
    : undefined;
  const hasMore = Boolean(query.hasNextPage);
  // The API's totals when it serves them; otherwise counted from what's loaded.
  const served = pages?.[0]?.totals ?? null;
  // Reads aren't receipts (plan §5.3), so the list can't count them.
  const counted = countWeek(loaded, { now, hasMore, readsListed: false });
  const week: { totals: WeeklyTotals; complete: boolean } = served
    ? { totals: served, complete: true }
    : counted;

  const exportCsv = () => {
    download(activityCsvName(space.slug, now), activityCsv(loaded));
    toast({
      state: "off",
      text: count(a.export.doneOne, a.export.done, loaded.length),
    });
  };
  const exportLabel = hasMore
    ? count(a.export.loadedOne, a.export.loaded, loaded.length)
    : copy.activity.export;
  const exportHint = hasMore
    ? count(a.export.hintLoadedOne, a.export.hintLoaded, loaded.length)
    : interpolate(a.export.hintAll, { space: space.name });

  const header = (
    <PageHeader
      className={styles.head}
      eyebrow={place.eyebrow}
      title={copy.activity.title}
      lede={interpolate(copy.activity.lede, { space: space.name })}
      actions={
        <Button
          variant="secondary"
          icon="file"
          title={exportHint}
          disabled={loaded.length === 0}
          onClick={exportCsv}
        >
          {exportLabel}
        </Button>
      }
    />
  );

  if (!pages) {
    return (
      <PlaceColumn>
        {header}
        {query.isError ? (
          <PlaceError space={space} onRetry={() => void query.refetch()} />
        ) : (
          <PlaceSkeleton
            title={copy.activity.title}
            status={copy.empty.loadingMeta}
            label={interpolate(copy.empty.loading, {
              place: copy.activity.title,
            })}
          />
        )}
      </PlaceColumn>
    );
  }

  if (loaded.length === 0 && !hasMore) {
    return (
      <PlaceColumn>
        {header}
        <EmptyState
          title={copy.empty.activity.title}
          detail={interpolate(copy.empty.activity.detail, {
            space: space.name,
          })}
          action={
            <Button
              variant="secondary"
              icon="plus"
              onClick={() => openCommand("remember")}
            >
              {copy.empty.remember}
            </Button>
          }
        />
      </PlaceColumn>
    );
  }

  const w = a.week;
  const rows: [string, number | null][] = [
    [w.reads, week.totals.reads],
    [w.proposals, week.totals.proposals],
    [w.kept, week.totals.kept],
    [w.rejected, week.totals.rejected],
    [w.forgotten, week.totals.forgotten],
    [w.compiles, week.totals.compiles],
  ];
  return (
    <PlaceColumn>
      {header}
      <div className={styles.grid}>
        <div className={styles.main}>
          {agentFilter ? (
            <p className={styles.byAgent}>
              {interpolate(a.byAgent, {
                agent:
                  filteredAgent?.actor.kind === "agent"
                    ? names.agent(filteredAgent.actor.agent)
                    : "",
              })}{" "}
              <Link href={pathname ?? ""} className={styles.everyone}>
                {a.everyone}
              </Link>
            </p>
          ) : null}
          <ActivityLog
            space={space}
            entries={entries}
            names={names}
            now={now}
            timeZone={timeZone}
            hasMore={hasMore}
            loadingMore={query.isFetchingNextPage}
            moreFailed={query.isFetchNextPageError}
            onMore={() => void query.fetchNextPage()}
            filter={filter}
            onFilter={setFilter}
          />
        </div>
        <aside className={styles.aside}>
          <section className="mx-panel" aria-labelledby="activity-week">
            <header className="mx-panel-head">
              <h2 id="activity-week" className="mx-panel-title">
                {w.title}
              </h2>
              {week.complete ? null : (
                <span className="mx-meta" title={w.partialHint}>
                  {w.partial}
                </span>
              )}
            </header>
            <dl className={styles.week}>
              {rows.map(([label, value]) => (
                <div key={label} className={styles.weekRow}>
                  <dt>{label}</dt>
                  <dd>
                    {value === null ? (
                      <>
                        <span aria-hidden="true">—</span>
                        <span className="mx-sr">{w.notRecorded}</span>
                      </>
                    ) : (
                      value.toLocaleString(
                        names.locale === "zh" ? "zh-CN" : "en-US",
                      )
                    )}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
          <p className={`mx-meta ${styles.note} ${styles.plain}`}>
            {a.retention}
          </p>
        </aside>
      </div>
    </PlaceColumn>
  );
}
