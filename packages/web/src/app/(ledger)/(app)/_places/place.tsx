"use client";

import type { ReactNode } from "react";
import { Button } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import { eyebrow } from "@/lib/v2/copy";
import type { SpaceOverview } from "@/lib/v2/data/types";
import { useSource, useViewer } from "../_lib/data";
import { useSpaceView } from "../_lib/space-context";
import { PlaceSkeleton } from "../_components/skeleton";
import { PlaceError } from "../_components/status";
import styles from "./place.module.css";

/** What every place page reads: the space, its overview, the clock and copy. */
export function usePlace() {
  const view = useSpaceView();
  const viewer = useViewer();
  const source = useSource();
  const { t, locale } = useLocale();
  const copy = t.ledger.app;
  return {
    ...view,
    copy,
    locale,
    now: source.now(),
    timeZone:
      viewer?.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
    eyebrow: eyebrow(copy, view.space),
  };
}

/**
 * The body under a place's header. Phase 0 draws two of a place's
 * states (States board): the skeleton while the overview loads, and
 * the empty state when there's nothing yet. A place with records shows
 * its header only until its screen lands in Phase 1; nothing is faked.
 */
export function PlaceBody({
  place,
  isEmpty,
  empty,
}: {
  /** The place's name, for the loading panel. */
  place: string;
  isEmpty: (overview: SpaceOverview) => boolean;
  empty: ReactNode;
}) {
  const { overview, overviewFailed, retryOverview, space, copy } = usePlace();
  if (!overview) {
    if (overviewFailed) {
      return <PlaceError space={space} onRetry={retryOverview} />;
    }
    return (
      <PlaceSkeleton
        title={place}
        status={copy.empty.loadingMeta}
        label={interpolate(copy.empty.loading, { place })}
      />
    );
  }
  return isEmpty(overview) ? <>{empty}</> : null;
}

/** A header action whose feature isn't built yet: drawn, focusable, with the reason. */
export function NotYetButton(props: React.ComponentProps<typeof Button>) {
  const { t } = useLocale();
  return <Button {...props} disabled disabledReason={t.ledger.app.notYet} />;
}

/** The page column: the boards' 1160px measure, centred in the sheet. */
export function PlaceColumn({
  children,
  wide = false,
}: {
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={`mx-page ${wide ? styles.wide : styles.column}`}>
      {children}
    </div>
  );
}
