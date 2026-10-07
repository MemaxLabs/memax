"use client";

import { useRouter } from "next/navigation";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { formatDayTitle } from "@/lib/v2/copy";
import type { TodayData } from "@/lib/v2/data/today";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { todayFooter, todayHeadline, todayLedeOf } from "@/lib/v2/today-copy";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { useTargets, useToday } from "../../_lib/compile";
import { useCompile } from "../brief/use-compile";
import { NotYetButton } from "../place";
import { useRecordsView, type RecordsView } from "../records-view";
import { EmptySpace } from "./empty-space";
import { SwitchToV2 } from "./switch-to-v2";
import {
  AgentsPanel,
  CompiledPanel,
  InFlightPanel,
  TodayDream,
  WaitingPanel,
} from "./today-panels";
import styles from "./today.module.css";

/**
 * Today (Main.png, epic 1.9) at /[space]/today: what changed while you
 * were away and what needs you. The date and a lede from real counts,
 * Hand off and Start review (R); Dream's edition, what's waiting, what's
 * in flight, the agents' day and the compiled files; the footer. A new,
 * empty space shows its three steps instead (EmptySpace.png), and a
 * phone shows the date, one headline, the edition and what's waiting
 * (MobileToday.png). A space still on V1 shows what switching it to V2
 * moves, and the switch (switch-to-v2.tsx).
 */
export function TodayPlace() {
  const view = useRecordsView();
  if (view.space.onV2 === false) return <SwitchToV2 view={view} />;
  return <TodayOnV2 view={view} />;
}

function TodayOnV2({ view }: { view: RecordsView }) {
  const { space, overview, overviewFailed, retryOverview, l, copy } = view;
  const today = useToday(space);
  const router = useRouter();
  const reviewHref = placeHref(space.slug, "review");

  // From Today, R starts Review (HANDOFF §7).
  useHotkey("today.review", () => router.push(reviewHref));

  if (!overview || today.data === undefined) {
    if (overviewFailed || today.isError) {
      return (
        <PlaceError
          space={space}
          onRetry={() => {
            retryOverview();
            void today.refetch();
          }}
        />
      );
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.today.waiting.title}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, {
            place: copy.titles.today,
          })}
        />
      </div>
    );
  }
  if (!overview.memories.any && today.data.waiting.total === 0) {
    return <EmptySpace view={view} dreamAt={today.data.dreamAt} />;
  }
  return <Today view={view} data={today.data} reviewHref={reviewHref} />;
}

function Today({
  view,
  data,
  reviewHref,
}: {
  view: RecordsView;
  data: TodayData;
  reviewHref: string;
}) {
  const { space, overview, l, copy, now, timeZone, locale, agentName } = view;
  const targets = useTargets(space);
  const compile = useCompile(space);
  const reviewKey = useKeycap("today.review");
  const date = formatDayTitle(now, timeZone, locale);
  const lede = overview
    ? todayLedeOf(copy, locale, overview, data, agentName)
    : null;
  const compiledAt =
    (targets.data ?? [])
      .map((t) => t.lastCompile?.at)
      .filter((at): at is string => Boolean(at))
      .sort()
      .at(-1) ?? null;
  const footer = todayFooter(
    l.today,
    {
      compiledAt,
      dreamAt: data.dreamAt,
      dreamWeekday: data.dreamWeekday ?? null,
      kept: overview?.memories.kept ?? null,
      space: space.name,
    },
    { now, timeZone, locale },
  );

  return (
    <div className={`mx-page ${styles.page}`}>
      <PageHeader
        className={styles.head}
        eyebrow={
          <>
            <span className={styles.desktopOnly}>{view.eyebrow}</span>
            <span className={styles.phoneOnly}>{date}</span>
          </>
        }
        title={
          <>
            <span className={styles.desktopOnly}>{date}</span>
            <span className={styles.phoneOnly}>
              {todayHeadline(copy, l.today, locale, data)}
            </span>
          </>
        }
        lede={lede ?? undefined}
        actions={
          <>
            {/* Handoffs arrive in Phase 4 (epic 4.4). */}
            <NotYetButton variant="secondary" icon="handoff">
              {copy.today.handOff}
            </NotYetButton>
            <Button variant="primary" kbd={reviewKey} href={reviewHref}>
              {copy.today.startReview}
            </Button>
          </>
        }
      />
      <div className={styles.grid}>
        <div className={styles.column}>
          <TodayDream view={view} data={data} />
          <WaitingPanel view={view} data={data} />
          <span className={styles.phoneOnly}>
            <Button
              variant="primary"
              size="lg"
              className={styles.startPhone}
              href={reviewHref}
            >
              {copy.today.startReview}
            </Button>
          </span>
        </div>
        <div className={`${styles.column} ${styles.side}`}>
          <InFlightPanel view={view} data={data} />
          <AgentsPanel view={view} data={data} />
          <CompiledPanel
            view={view}
            targets={targets.data}
            compiling={compile.pending}
            onCompile={() => void compile.run(targets.data ?? [])}
          />
        </div>
      </div>
      {footer ? <p className={`mx-meta ${styles.foot}`}>{footer}</p> : null}
    </div>
  );
}
