"use client";

import { Button } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { placeHref } from "@/lib/v2/places";
import { PlaceSkeleton } from "../../_components/skeleton";
import { StatusPage } from "../../../_components/status-page";
import { useConflict } from "../../_lib/records";
import { useRecordsView } from "../records-view";
import { Compare } from "./compare-form";
import styles from "./compare.module.css";

/**
 * Comparing a conflict (ReviewConflict.png) at /[space]/review/[ref]/compare:
 * both sides, then one answer every agent will read, kept as a decision
 * authored by the person. Only a conflict the judge linked to a kept
 * memory has two sides; anything else says so and goes back to the queue.
 */
export function ComparePlace({ memoryRef }: { memoryRef: string }) {
  const view = useRecordsView();
  const conflict = useConflict(view.space, memoryRef);
  const c = view.l.review.compare;
  const queueHref = placeHref(view.space.slug, "review");
  if (conflict.data === undefined) {
    return (
      <div className={`mx-page ${styles.page}`}>
        <PlaceSkeleton
          title={view.copy.review.title}
          status={view.copy.empty.loadingMeta}
          label={view.l.review.loading}
        />
      </div>
    );
  }
  if (conflict.data === null) {
    return (
      <StatusPage
        variant="sheet"
        receipt={memoryRef}
        title={interpolate(c.nothing.title, { ref: memoryRef })}
        description={c.nothing.detail}
        actions={
          <Button variant="secondary" size="sm" href={queueHref}>
            {c.nothing.back}
          </Button>
        }
      />
    );
  }
  return <Compare view={view} conflict={conflict.data} memoryRef={memoryRef} />;
}
