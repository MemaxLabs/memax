"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { activityCsv, activityCsvName } from "@/lib/v2/activity/csv";
import { mergeLog, type StreamName } from "@/lib/v2/activity/merge";
import { sealSentences } from "@/lib/v2/activity/seal";
import type { Names } from "@/lib/v2/activity/sentence";
import { countWeek } from "@/lib/v2/activity/totals";
import type {
  ActivityEntry,
  ActivityFilter,
  WeeklyTotals,
} from "@/lib/v2/data/activity";
import { count } from "@/lib/v2/copy";
import { EmptyState } from "../../_components/empty-state";
import { PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useToast } from "../../_components/toasts";
import { useOverlays } from "../../_lib/overlays";
import { PlaceColumn, usePlace } from "../place";
import { ActivityLog, type OlderRows } from "./activity-log";
import { useActivity, useReads, useSeal } from "./queries";
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

const EMPTY: ActivityEntry[] = [];

/**
 * Activity (Activity.png, epic 1.1): every receipt in the space and the
 * reads beside them, newest first, with this week's totals, how far the
 * receipts are sealed and a CSV of the receipts loaded. Nothing changes
 * without a line here.
 */
export function ActivityPlace() {
  const place = usePlace();
  const { space, copy, now, timeZone, locale } = place;
  const { t } = useLocale();
  const a = t.ledger.activity;
  const names = useNames();
  const toast = useToast();
  const { openCommand } = useOverlays();
  const params = useSearchParams();
  const pathname = usePathname();
  const agentFilter = params?.get("agent") ?? null;
  const [filter, setFilter] = useState<ActivityFilter>("all");
  const receiptsQuery = useActivity(space);
  const readsQuery = useReads(space);
  const seal = useSeal(space).data;

  const pages = receiptsQuery.data?.pages;
  const receipts = useMemo(
    () => pages?.flatMap((p) => p.entries) ?? EMPTY,
    [pages],
  );
  const readPages = readsQuery.data?.pages;
  const reads = useMemo(
    () => readPages?.flatMap((p) => p.entries) ?? EMPTY,
    [readPages],
  );
  const receiptsMore = Boolean(receiptsQuery.hasNextPage);
  const readsMore = Boolean(readsQuery.hasNextPage);
  // Without the reads (they didn't load), "All" is the receipts alone.
  const merged = useMemo(
    () =>
      mergeLog(
        { entries: receipts, hasMore: receiptsMore },
        readPages ? { entries: reads, hasMore: readsMore } : null,
      ),
    [receipts, receiptsMore, readPages, reads, readsMore],
  );

  // What the filter shows, and what "older" loads for it.
  const shown: { entries: ActivityEntry[]; next: StreamName[] } =
    filter === "all"
      ? merged
      : filter === "reads"
        ? { entries: reads, next: readsMore ? ["reads"] : [] }
        : { entries: receipts, next: receiptsMore ? ["receipts"] : [] };
  const byAgent = (e: ActivityEntry) =>
    e.actor.kind === "agent" && e.actor.connectionId === agentFilter;
  const entries = agentFilter ? shown.entries.filter(byAgent) : shown.entries;
  const filteredAgent = agentFilter
    ? (receipts.find(byAgent) ?? reads.find(byAgent))
    : undefined;
  const streams = { receipts: receiptsQuery, reads: readsQuery };
  const older: OlderRows = {
    stream:
      filter === "all" ? "all" : filter === "reads" ? "reads" : "receipts",
    more: shown.next.length > 0,
    loading: shown.next.some((s) => streams[s].isFetchingNextPage),
    failed: shown.next.some((s) => streams[s].isFetchNextPageError),
    load: () => {
      for (const s of shown.next) void streams[s].fetchNextPage();
    },
  };

  // The API's totals when it serves them; otherwise counted from the
  // receipts loaded. The week's reads are always the API's (reads_7d).
  const served = pages?.[0]?.totals ?? null;
  const counted = countWeek(receipts, {
    now,
    hasMore: receiptsMore,
    readsListed: false,
  });
  const base = served ?? counted.totals;
  const week: { totals: WeeklyTotals; complete: boolean } = {
    totals: { ...base, reads: readPages?.[0]?.week ?? base.reads },
    complete: served ? true : counted.complete,
  };
  const sealed = seal
    ? sealSentences(a, copy, seal, { now, timeZone, locale })
    : [];

  const exportCsv = () => {
    download(activityCsvName(space.slug, now), activityCsv(receipts));
    toast({ text: count(a.export.doneOne, a.export.done, receipts.length) });
  };
  const exportLabel = receiptsMore
    ? count(a.export.loadedOne, a.export.loaded, receipts.length)
    : copy.activity.export;
  const exportHint = receiptsMore
    ? count(a.export.hintLoadedOne, a.export.hintLoaded, receipts.length)
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
          disabled={receipts.length === 0}
          onClick={exportCsv}
        >
          {exportLabel}
        </Button>
      }
    />
  );

  // The reads load beside the receipts; wait for both, unless they failed.
  if (!pages || (!readPages && readsQuery.isPending)) {
    return (
      <PlaceColumn>
        {header}
        {receiptsQuery.isError ? (
          <PlaceError
            space={space}
            onRetry={() => void receiptsQuery.refetch()}
          />
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

  if (
    receipts.length === 0 &&
    !receiptsMore &&
    reads.length === 0 &&
    !readsMore
  ) {
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
            older={older}
            readsFailed={
              filter === "reads" && !readPages
                ? () => void readsQuery.refetch()
                : undefined
            }
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
          {sealed.length > 0 ? (
            <p
              className={`mx-meta ${styles.note} ${styles.plain}`}
              title={a.seal.hint}
            >
              {sealed.map((s, i) => (
                <span
                  key={i}
                  className={s.problem ? styles.problem : undefined}
                >
                  {i > 0 && locale !== "zh" ? " " : null}
                  {s.text}
                </span>
              ))}
            </p>
          ) : null}
          <p className={`mx-meta ${styles.note} ${styles.plain}`}>
            {a.retention}
          </p>
        </aside>
      </div>
    </PlaceColumn>
  );
}
