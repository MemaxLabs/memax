"use client";

import { useLocale } from "@/i18n";
import type { Actor } from "@/lib/v2/data/records";
import { actorName, dateTime, railTime, stampOf } from "@/lib/v2/records-copy";
import { useViewer } from "../_lib/data";
import { useAgentName } from "../_lib/frame-copy";
import { usePlace } from "./place";

/**
 * What Review, Memories and a memory's page read: the place (space,
 * overview, clock, zone) plus the Ledger catalogue and the helpers that
 * turn a record's actors and times into words.
 */
export function useRecordsView() {
  const place = usePlace();
  const { t } = useLocale();
  const agentName = useAgentName();
  const viewer = useViewer();
  const rc = t.ledger.records;
  const { now, timeZone, locale } = place;
  return {
    ...place,
    l: t.ledger,
    rc,
    viewer,
    agentName,
    /** A rail's time: "14 min ago", "03:12", "Oct 2". */
    time: (iso: string) => railTime(rc, iso, now, timeZone, locale),
    /** "Oct 2, 10:41". */
    dateTime: (iso: string) => dateTime(rc, iso, timeZone, locale),
    stamp: (actor: Actor | null) => stampOf(actor, rc, viewer ?? undefined),
    name: (actor: Actor | null) => actorName(actor, rc, agentName),
  };
}

export type RecordsView = ReturnType<typeof useRecordsView>;
