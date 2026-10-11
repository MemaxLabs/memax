"use client";

import type { ReactNode } from "react";
import { Button, PageHeader } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import {
  placeHref,
  spaceHasPlace,
  v1SpaceHref,
  type PlaceRoute,
} from "@/lib/v2/places";
import { EmptyState } from "../_components/empty-state";
import { useFrameLocation } from "../_lib/location";
import { useSpaceView } from "../_lib/space-context";
import { PlaceColumn, usePlace } from "./place";
import styles from "./place.module.css";

/** Every place a space has but Today, whose page is the switch itself. */
export type PlaceOnV1 = Exclude<PlaceRoute, "today">;

/**
 * A space's places (the [space] layout's body). A space still on V1 has
 * no V2 record, so each of its places but Today shows SpaceOnV1 instead
 * of a page that would read an empty record and call it empty; Today is
 * the switch to V2 (today/switch-to-v2.tsx). A place the space doesn't
 * have (Decisions outside a team) still answers 404.
 */
export function SpacePlaces({ children }: { children: ReactNode }) {
  const { space } = useSpaceView();
  const { route } = useFrameLocation();
  if (
    space.onV2 === false &&
    route !== undefined &&
    route !== "today" &&
    spaceHasPlace(space.kind, route)
  ) {
    return <SpaceOnV1 route={route} />;
  }
  return <>{children}</>;
}

/**
 * A place of a space still on V1: the place's header, so the person
 * knows where they are, then one calm panel in the States board's Empty
 * language. It says the space is still on V1 and what this place holds
 * once it's on V2; its owner gets the switch (a link to Today, which says
 * what moves before anything changes), and anyone can open it in V1,
 * where its memories still are. Only the owner switches a space
 * (policy.DecideSwitchSpace), so a member or viewer is told that instead.
 * No header actions: none of this place's commands has a record to act on.
 */
export function SpaceOnV1({ route }: { route: PlaceOnV1 }) {
  const place = usePlace();
  const { space, copy } = place;
  const v1 = copy.onV1;
  const owner = space.role === "owner";
  return (
    <PlaceColumn>
      <PageHeader
        className={styles.tightHead}
        eyebrow={place.eyebrow}
        title={copy.titles[route]}
      />
      <EmptyState
        title={interpolate(v1.title, { space: space.name })}
        detail={v1.places[route]}
        action={
          <span className={styles.actions}>
            {owner ? (
              <Button variant="secondary" href={placeHref(space.slug, "today")}>
                {v1.switch}
              </Button>
            ) : null}
            <Button
              variant={owner ? "quiet" : "secondary"}
              href={v1SpaceHref(space)}
            >
              {v1.openInV1}
            </Button>
          </span>
        }
        meta={owner ? v1.ownerMeta : v1.memberMeta}
      />
    </PlaceColumn>
  );
}
