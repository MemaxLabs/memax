"use client";

import Link from "next/link";
import { Menu } from "@base-ui/react/menu";
import { Icon, Kbd } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { spaceMeta } from "@/lib/v2/copy";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { useKeycaps } from "@/lib/v2/keymap/react";
import { switchHref, type PlaceRoute } from "@/lib/v2/places";
import { useCount } from "../_lib/frame-copy";
import styles from "./space-menu.module.css";

/**
 * The space switcher (SpaceSwitcher.png): your spaces, then teams, each
 * with its ⌘1–⌘9 key, a check on the current one and the waiting count
 * on the others. Base UI's Menu gives it roving focus, typeahead and
 * Escape; the frame's keymap adds ⌘1–⌘9 everywhere.
 */
export function SpaceMenuPopup({
  spaces,
  current,
  route,
  anchorOffset,
}: {
  spaces: SpaceSummary[];
  current: SpaceSummary | undefined;
  route: PlaceRoute | undefined;
  /** Lines the popup up with the rail's edge, as drawn. */
  anchorOffset?: { side: number; align: number };
}) {
  const { t } = useLocale();
  const copy = t.ledger.app;
  const caps = useKeycaps("space.switch");
  const waitingLabel = useCount(
    copy.frame.reviewCountOne,
    copy.frame.reviewCount,
  );
  const groups = [
    {
      label: copy.spaces.yours,
      items: spaces.filter((space) => space.kind !== "team"),
    },
    {
      label: copy.spaces.teams,
      items: spaces.filter((space) => space.kind === "team"),
    },
  ].filter((group) => group.items.length > 0);

  return (
    <Menu.Portal>
      <Menu.Positioner
        className={styles.positioner}
        side="bottom"
        align="start"
        sideOffset={anchorOffset?.side ?? 6}
        alignOffset={anchorOffset?.align ?? 0}
      >
        <Menu.Popup
          className={styles.popup}
          aria-label={copy.frame.switchSpace}
        >
          {groups.map((group, g) => (
            <Menu.Group
              key={group.label}
              className={g > 0 ? styles.groupRuled : undefined}
            >
              <Menu.GroupLabel className={`mx-section-label ${styles.label}`}>
                {group.label}
              </Menu.GroupLabel>
              {group.items.map((space) => {
                const index = spaces.indexOf(space);
                const isCurrent = space.slug === current?.slug;
                const waiting = space.waiting ?? 0;
                return (
                  <Menu.LinkItem
                    key={space.slug}
                    className={styles.item}
                    aria-current={isCurrent ? "true" : undefined}
                    render={<Link href={switchHref(space, route)} />}
                  >
                    <span className={styles.name}>
                      <span className={styles.title}>{space.name}</span>
                      <span className="mx-meta">{spaceMeta(copy, space)}</span>
                    </span>
                    {isCurrent ? (
                      <span className={styles.check}>
                        <Icon name="check" title={copy.spaces.current} />
                      </span>
                    ) : waiting > 0 ? (
                      <span className="mx-count is-pending">
                        <span aria-hidden="true">{waiting}</span>
                        <span className="mx-sr">{waitingLabel(waiting)}</span>
                      </span>
                    ) : (
                      <span />
                    )}
                    {index < 9 ? (
                      <Kbd aria-hidden="true">{caps[index]?.join(" ")}</Kbd>
                    ) : (
                      <span />
                    )}
                  </Menu.LinkItem>
                );
              })}
            </Menu.Group>
          ))}
          <div className={styles.footer}>
            <Menu.Item className={styles.action} disabled title={copy.notYet}>
              <Icon name="plus" />
              {copy.spaces.newSpace}
            </Menu.Item>
            <Menu.Item className={styles.action} disabled title={copy.notYet}>
              <Icon name="link" />
              {copy.spaces.join}
            </Menu.Item>
          </div>
        </Menu.Popup>
      </Menu.Positioner>
    </Menu.Portal>
  );
}
