"use client";

import { notFound } from "next/navigation";
import { PageHeader, useLedger } from "@memaxlabs/ledger";
import { useKeycap } from "@/lib/v2/keymap/react";
import { EmptyState } from "../_components/empty-state";
import { NotYetButton, PlaceBody, PlaceColumn, usePlace } from "./place";
import styles from "./place.module.css";

// Handoffs and Decisions: their real headers, and the empty state until
// each screen lands (epics 4.4, 4.2). Agents and Activity are in
// ./agents and ./activity.

export function HandoffsPlace() {
  const place = usePlace();
  const { copy } = place;
  const { strings } = useLedger();
  const handoffKey = useKeycap("memory.handoff");
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.handoffs.title}
        lede={copy.handoffs.lede}
        actions={
          <NotYetButton variant="primary" icon="plus" kbd={handoffKey}>
            {copy.handoffs.new}
          </NotYetButton>
        }
      />
      <PlaceBody
        place={strings.nav.places.handoffs}
        isEmpty={(o) => !o.handoffs}
        empty={
          <EmptyState
            title={copy.empty.handoffs.title}
            detail={copy.empty.handoffs.detail}
          />
        }
      />
    </PlaceColumn>
  );
}

export function DecisionsPlace() {
  const place = usePlace();
  const { space, copy } = place;
  const { strings } = useLedger();
  // A team space's place (HANDOFF §6); elsewhere there is no such page.
  if (space.kind !== "team") notFound();
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.decisions.title}
        lede={copy.decisions.lede}
        actions={
          <>
            <NotYetButton variant="secondary" icon="file">
              {copy.decisions.export}
            </NotYetButton>
            <NotYetButton variant="primary" icon="plus">
              {copy.decisions.record}
            </NotYetButton>
          </>
        }
      />
      <PlaceBody
        place={strings.nav.places.decisions}
        isEmpty={(o) => !o.decisions}
        empty={
          <EmptyState
            title={copy.empty.decisions.title}
            detail={copy.empty.decisions.detail}
          />
        }
      />
    </PlaceColumn>
  );
}
