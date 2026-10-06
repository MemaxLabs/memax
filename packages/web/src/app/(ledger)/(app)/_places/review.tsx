"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button, Kbd, Segmented } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatAge, formatWhen, joinSentences } from "@/lib/v2/copy";
import { placeHref } from "@/lib/v2/places";
import { useKeycaps } from "@/lib/v2/keymap/react";
import { EmptyState } from "../_components/empty-state";
import { useOverlays } from "../_lib/overlays";
import { PlaceBody, usePlace } from "./place";
import styles from "./review.module.css";

const FILTERS = ["all", "conflicts", "external", "stale"] as const;
type Filter = (typeof FILTERS)[number];

/**
 * Review (Review.png): the queue in a 400px column with its filters and
 * key legend, and the card beside it. The queue and the card arrive
 * with epic 1.4; until then the column shows the count, and an empty
 * queue shows the States board's empty state.
 */
export function ReviewPlace() {
  const place = usePlace();
  const { space, overview, copy, locale, now, timeZone } = place;
  const { setKeysOpen } = useOverlays();
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const filter = (FILTERS as readonly string[]).includes(
    params.get("filter") ?? "",
  )
    ? (params.get("filter") as Filter)
    : "all";
  const move = useKeycaps("review.move");
  const [keep] = useKeycaps("review.keep");
  const [edit] = useKeycaps("review.edit");
  const [reject] = useKeycaps("review.reject");
  const [help] = useKeycaps("help.keys");

  const waiting = overview?.waiting ?? 0;
  let meta = "";
  if (overview && waiting === 0) meta = copy.review.metaNone;
  else if (overview?.oldestWaitingAt) {
    meta = interpolate(copy.review.meta, {
      n: waiting,
      age: formatAge(copy, overview.oldestWaitingAt, now),
    });
  } else if (overview) {
    meta = interpolate(copy.review.metaCount, { n: waiting });
  }
  const filters = overview?.reviewFilters;

  const lastReview = overview?.lastReview;
  const inSync =
    overview?.targets &&
    overview.targets.total > 0 &&
    overview.targets.inSync === overview.targets.total
      ? count(
          copy.empty.review.inSyncOne,
          copy.empty.review.inSync,
          overview.targets.total,
        )
      : null;

  return (
    <div className={styles.layout}>
      <aside className={styles.queue}>
        <div className={styles.queueHead}>
          <div className={styles.titleRow}>
            <h1 className="mx-page-title">{copy.review.title}</h1>
            <span className="mx-meta">{meta}</span>
          </div>
          {filters && waiting > 0 ? (
            <Segmented<Filter>
              size="sm"
              label={copy.review.filterLabel}
              value={filter}
              onChange={(next) => {
                const query = next === "all" ? "" : `?filter=${next}`;
                router.replace(`${pathname}${query}`, { scroll: false });
              }}
              options={[
                {
                  value: "all",
                  label: interpolate(copy.review.filters.all, { n: waiting }),
                },
                {
                  value: "conflicts",
                  label: interpolate(copy.review.filters.conflicts, {
                    n: filters.conflicts,
                  }),
                },
                {
                  value: "external",
                  label: interpolate(copy.review.filters.external, {
                    n: filters.external,
                  }),
                },
                {
                  value: "stale",
                  label: interpolate(copy.review.filters.stale, {
                    n: filters.stale,
                  }),
                },
              ]}
            />
          ) : null}
        </div>
        <div className={styles.list} />
        <div
          className={styles.legend}
          aria-label={copy.review.legendLabel}
          role="group"
        >
          <span className={styles.key}>
            {move.map((caps) => (
              <Kbd key={caps.join()}>{caps.join(" ")}</Kbd>
            ))}{" "}
            {copy.review.legend.move}
          </span>
          <span className={styles.key}>
            <Kbd>{keep?.join(" ")}</Kbd> {copy.review.legend.keep}
          </span>
          <span className={styles.key}>
            <Kbd>{edit?.join(" ")}</Kbd> {copy.review.legend.edit}
          </span>
          <span className={styles.key}>
            <Kbd>{reject?.join(" ")}</Kbd> {copy.review.legend.reject}
          </span>
          <button
            type="button"
            className={styles.allKeys}
            onClick={() => setKeysOpen(true)}
            aria-haspopup="dialog"
          >
            <Kbd>{help?.join(" ")}</Kbd> {copy.review.legend.allKeys}
          </button>
        </div>
      </aside>
      <section className={styles.card} aria-label={copy.review.title}>
        <div className={styles.cardColumn}>
          <PlaceBody
            place={copy.review.title}
            isEmpty={(o) => o.waiting === 0}
            empty={
              <EmptyState
                title={copy.empty.review.title}
                detail={joinSentences(
                  [copy.empty.review.detail, inSync],
                  locale,
                )}
                action={
                  <Button
                    variant="secondary"
                    icon="plus"
                    href={placeHref(space.slug, "agents")}
                  >
                    {copy.empty.connect}
                  </Button>
                }
                meta={
                  lastReview
                    ? interpolate(copy.empty.review.lastReview, {
                        when: formatWhen(
                          copy,
                          lastReview.at,
                          now,
                          timeZone,
                          locale,
                        ),
                        kept: interpolate(copy.empty.review.kept, {
                          n: lastReview.kept,
                        }),
                        rejected: interpolate(copy.empty.review.rejected, {
                          n: lastReview.rejected,
                        }),
                      })
                    : undefined
                }
              />
            }
          />
        </div>
      </section>
    </div>
  );
}
