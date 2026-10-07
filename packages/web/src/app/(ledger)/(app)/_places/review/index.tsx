"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { count, formatWhen, joinSentences } from "@/lib/v2/copy";
import { waitingOnYou } from "@/lib/v2/data/types";
import { KeyScopeBoundary } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { EmptyState } from "../../_components/empty-state";
import { PlaceError } from "../../_components/status";
import { useRecordsView, type RecordsView } from "../records-view";
import { ReviewImportPlace } from "../review-import";
import { CardColumn } from "./card-column";
import { Queue } from "./queue";
import { useReviewKeys } from "./review-keys";
import { REVIEW_FILTERS, useReview, type ReviewFilter } from "./use-review";
import styles from "./review.module.css";

/**
 * Review (Review.png): the queue in a 400px column with its filters and
 * key legend, and the card beside it. Every decision is a key away (the
 * review.* bindings), Keep stamps the seal, and an empty queue is one
 * panel across the sheet (States board).
 */
export function ReviewPlace() {
  const view = useRecordsView();
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const asked = params.get("filter") ?? "";
  // What `memax init` imported, kept in bulk (ReviewImport.png).
  if (asked === "import") {
    return (
      <KeyScopeBoundary name="review">
        <ReviewImportPlace />
      </KeyScopeBoundary>
    );
  }
  const filter = (REVIEW_FILTERS as readonly string[]).includes(asked)
    ? (asked as ReviewFilter)
    : "all";
  // A decision gate to open (gateHref, from Today and Activity).
  const gate = params.get("gate") || null;
  const onFilter = (next: ReviewFilter) => {
    const query = next === "all" ? "" : `?filter=${next}`;
    router.replace(`${pathname}${query}`, { scroll: false });
  };
  return (
    <KeyScopeBoundary name="review">
      <ReviewScreen
        view={view}
        filter={filter}
        gate={gate}
        onFilter={onFilter}
      />
    </KeyScopeBoundary>
  );
}

function ReviewScreen({
  view,
  filter,
  gate,
  onFilter,
}: {
  view: RecordsView;
  filter: ReviewFilter;
  gate: string | null;
  onFilter: (filter: ReviewFilter) => void;
}) {
  const review = useReview(view.space, filter, gate);
  useReviewKeys(review, view.space);
  const { queue } = review;
  const gates = review.gateCards.query;
  const loaded = queue.data !== undefined;
  // Unknown until the gates answer (a failed read counts as none: the
  // queue says so).
  const gatesKnown = gates.data !== undefined || gates.isError;

  if (!loaded && queue.isError) {
    return (
      <PlaceError space={view.space} onRetry={() => void queue.refetch()} />
    );
  }
  const empty = loaded
    ? review.all.length === 0 &&
      !queue.hasNextPage &&
      gatesKnown &&
      review.gateCards.gates.length === 0 &&
      !review.gateCards.finding
    : view.overview !== undefined && waitingOnYou(view.overview) === 0;
  if (empty) return <ReviewEmpty view={view} />;
  return (
    <div className={styles.layout}>
      <Queue view={view} review={review} filter={filter} onFilter={onFilter} />
      <CardColumn view={view} review={review} />
    </div>
  );
}

/** Nothing waiting: the States board's panel, across the whole sheet. */
function ReviewEmpty({ view }: { view: RecordsView }) {
  const { copy, overview, space, now, timeZone, locale } = view;
  const lastReview = overview?.lastReview;
  const targets = overview?.targets;
  const inSync =
    targets && targets.total > 0 && targets.inSync === targets.total
      ? count(
          copy.empty.review.inSyncOne,
          copy.empty.review.inSync,
          targets.total,
        )
      : null;
  return (
    <div className={styles.emptySheet}>
      <h1 className="mx-sr">{copy.review.title}</h1>
      <EmptyState
        title={copy.empty.review.title}
        detail={joinSentences([copy.empty.review.detail, inSync], locale)}
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
                when: formatWhen(copy, lastReview.at, now, timeZone, locale),
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
    </div>
  );
}
