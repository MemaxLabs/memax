"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Menu } from "@base-ui/react/menu";
import { Icon, Kbd } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { spaceMeta } from "@/lib/v2/copy";
import type { SpaceSummary } from "@/lib/v2/data/types";
import { KeyScopeBoundary, useHotkey, useKeycaps } from "@/lib/v2/keymap/react";
import { switchHref, type PlaceRoute } from "@/lib/v2/places";
import { useCount } from "../_lib/frame-copy";
import styles from "./space-menu.module.css";

interface SpaceMenuProps {
  spaces: SpaceSummary[];
  current: SpaceSummary | undefined;
  route: PlaceRoute | undefined;
  /** Lines the popup up with the rail's edge, as drawn. */
  anchorOffset?: { side: number; align: number };
}

/**
 * The space switcher (SpaceSwitcher.png) around its trigger: `children`
 * render a `<Menu.Trigger />` somewhere inside (the rail's space name,
 * or the phone's top bar). While it's open it is a modal key layer, so
 * typeahead letters stay the menu's and ⌘1–⌘9 still switch.
 */
export function SpaceMenu({
  children,
  ...popup
}: SpaceMenuProps & { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Menu.Root open={open} onOpenChange={setOpen}>
      {children}
      <SpaceMenuPopup {...popup} onSwitched={() => setOpen(false)} />
    </Menu.Root>
  );
}

/** ⌘1–⌘9 while the menu is open: switch, then close it. */
function MenuKeys({
  spaces,
  current,
  route,
  onSwitched,
}: Pick<SpaceMenuProps, "spaces" | "current" | "route"> & {
  onSwitched: () => void;
}) {
  const router = useRouter();
  useHotkey("space.switch", (_, { index }) => {
    const target = spaces[index];
    if (!target) return false;
    onSwitched();
    if (target.slug !== current?.slug) router.push(switchHref(target, route));
  });
  return null;
}

/**
 * Your spaces, then teams, each with its ⌘1–⌘9 key, a check on the
 * current one and the waiting count on the others. Base UI's Menu gives
 * it roving focus, typeahead and Escape.
 */
function SpaceMenuPopup({
  spaces,
  current,
  route,
  anchorOffset,
  onSwitched,
}: SpaceMenuProps & { onSwitched: () => void }) {
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
          <KeyScopeBoundary name="spaces" modal>
            <MenuKeys
              spaces={spaces}
              current={current}
              route={route}
              onSwitched={onSwitched}
            />
          </KeyScopeBoundary>
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
                    // Navigation is client-side: the page (and the menu) stay.
                    closeOnClick
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
