"use client";

import { Button } from "@memaxlabs/ledger";
import { interpolate, useLocale } from "@/i18n";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { placeHref } from "@/lib/v2/places";
import styles from "./command-center.module.css";

/** Whether a space is still on V1: its V2 record takes no new words or decisions (policy space_on_v1). */
export function isOnV1(space: SpaceSummary): boolean {
  return space.onV2 === false;
}

/**
 * ⌘K in a space still on V1: Ask answers only from the V2 record and
 * Remember would write to it, so neither is offered. Says why, and opens
 * the space's Today, where its owner switches it to V2.
 */
export function OnV1Notice({
  space,
  onOpen,
}: {
  space: SpaceSummary;
  /** Today was opened: the overlay closes. */
  onOpen: () => void;
}) {
  const { t } = useLocale();
  const copy = t.ledger.app.command.onV1;
  return (
    <div className={styles.quiet} role="status">
      <p className={styles.quietTitle}>
        {interpolate(copy.title, { space: space.name })}
      </p>
      <p className={styles.quietDetail}>
        {space.role === "owner" ? copy.owner : copy.member}
      </p>
      <div>
        <Button
          variant="secondary"
          size="sm"
          href={placeHref(space.slug, "today")}
          onClick={onOpen}
        >
          {copy.open}
        </Button>
      </div>
    </div>
  );
}
