"use client";

import { useRouter } from "next/navigation";
import { Button, PageHeader, useLedger } from "@memaxlabs/ledger";
import { formatDayTitle, todayLede } from "@/lib/v2/copy";
import { useHotkey, useKeycap } from "@/lib/v2/keymap/react";
import { placeHref } from "@/lib/v2/places";
import { EmptyState } from "../_components/empty-state";
import { useAgentName } from "../_lib/frame-copy";
import { useOverlays } from "../_lib/overlays";
import { PlaceBody, PlaceColumn, usePlace } from "./place";

/** Today (Main.png): what changed while you were away, and what needs you. */
export function TodayPlace() {
  const place = usePlace();
  const { space, overview, copy, locale, now, timeZone } = place;
  const router = useRouter();
  const { strings } = useLedger();
  const { openCommand } = useOverlays();
  const agentName = useAgentName();
  const reviewKey = useKeycap("today.review");
  const reviewHref = placeHref(space.slug, "review");

  // From Today, R starts Review (HANDOFF §7).
  useHotkey("today.review", () => router.push(reviewHref));

  const lede = overview ? todayLede(copy, locale, overview, agentName) : null;
  const agents = overview?.agents?.connected ?? 0;

  return (
    <PlaceColumn>
      <PageHeader
        eyebrow={place.eyebrow}
        title={formatDayTitle(now, timeZone, locale)}
        lede={lede ?? undefined}
        actions={
          <>
            <Button
              variant="secondary"
              icon="handoff"
              href={placeHref(space.slug, "handoffs")}
            >
              {copy.today.handOff}
            </Button>
            <Button variant="primary" kbd={reviewKey} href={reviewHref}>
              {copy.today.startReview}
            </Button>
          </>
        }
      />
      <PlaceBody
        place={strings.nav.places.today}
        isEmpty={(o) => o.waiting === 0 && !o.dream}
        empty={
          <EmptyState
            title={copy.empty.today.title}
            detail={
              agents > 0
                ? copy.empty.today.detail
                : copy.empty.today.detailNoAgents
            }
            action={
              agents > 0 ? (
                <Button
                  variant="secondary"
                  icon="plus"
                  onClick={() => openCommand("remember")}
                >
                  {copy.empty.remember}
                </Button>
              ) : (
                <Button
                  variant="secondary"
                  icon="plus"
                  href={placeHref(space.slug, "agents")}
                >
                  {copy.empty.connect}
                </Button>
              )
            }
          />
        }
      />
    </PlaceColumn>
  );
}
