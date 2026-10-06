"use client";

import Link from "next/link";
import { Menu } from "@base-ui/react/menu";
import {
  AgentStamp,
  Icon,
  Logo,
  useLedger,
  type IconName,
} from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import type { SpaceSummary, Viewer } from "@/lib/v2/data/types";
import { placeHref, SETTINGS_HREF, type PlaceRoute } from "@/lib/v2/places";
import { useCount } from "../_lib/frame-copy";
import { SpaceMenuPopup } from "./space-menu";
import styles from "./app-frame.module.css";

/**
 * Below 640px (MobileToday.png): a top bar with the space switcher and
 * the person, and a bottom bar with Today, Review, Ask and Agents. The
 * rail is hidden there. Both bars respect the safe areas.
 */
export function MobileTopBar({
  space,
  spaces,
  route,
  viewer,
}: {
  space: SpaceSummary;
  spaces: SpaceSummary[];
  route: PlaceRoute | undefined;
  viewer: Viewer | null;
}) {
  const { t } = useLocale();
  return (
    <header className={styles.topbar}>
      <Logo variant="mark" size={20} />
      <Menu.Root>
        <Menu.Trigger
          className={styles.topbarSpace}
          aria-label={`${space.name}, ${t.ledger.app.frame.switchSpace}`}
        >
          {space.name}
          <Icon name="chevron-down" size={14} />
        </Menu.Trigger>
        <SpaceMenuPopup spaces={spaces} current={space} route={route} />
      </Menu.Root>
      {viewer ? (
        <Link
          href={SETTINGS_HREF}
          className={styles.topbarMe}
          aria-label={t.ledger.app.frame.account}
        >
          <AgentStamp person={viewer.initials} name={viewer.name} decorative />
        </Link>
      ) : null}
    </header>
  );
}

interface Tab {
  id: string;
  icon: IconName;
  label: string;
  href?: string;
  current?: boolean;
  count?: number;
}

export function MobileTabBar({
  space,
  route,
  waiting,
  onAsk,
}: {
  space: SpaceSummary;
  route: PlaceRoute | undefined;
  waiting: number;
  onAsk: () => void;
}) {
  const { t } = useLocale();
  const { strings } = useLedger();
  const waitingLabel = useCount(
    t.ledger.app.frame.reviewCountOne,
    t.ledger.app.frame.reviewCount,
  );
  const tabs: Tab[] = [
    {
      id: "today",
      icon: "today",
      label: strings.nav.places.today,
      href: placeHref(space.slug, "today"),
      current: route === "today",
    },
    {
      id: "review",
      icon: "review",
      label: strings.nav.places.review,
      href: placeHref(space.slug, "review"),
      current: route === "review",
      count: waiting,
    },
    { id: "ask", icon: "search", label: strings.command.ask },
    {
      id: "agents",
      icon: "agents",
      label: strings.nav.places.agents,
      href: placeHref(space.slug, "agents"),
      current: route === "agents",
    },
  ];
  return (
    <nav className={styles.tabbar} aria-label={t.ledger.app.frame.mobileNav}>
      {tabs.map((tab) => {
        const body = (
          <>
            <Icon name={tab.icon} size={20} />
            <span>{tab.label}</span>
            {tab.count ? (
              <span className={`mx-count is-pending ${styles.tabCount}`}>
                <span aria-hidden="true">{tab.count}</span>
                <span className="mx-sr">{waitingLabel(tab.count)}</span>
              </span>
            ) : null}
          </>
        );
        return tab.href ? (
          <Link
            key={tab.id}
            href={tab.href}
            className={styles.tab}
            aria-current={tab.current ? "page" : undefined}
          >
            {body}
          </Link>
        ) : (
          <button
            key={tab.id}
            type="button"
            className={styles.tab}
            onClick={onAsk}
            aria-haspopup="dialog"
          >
            {body}
          </button>
        );
      })}
    </nav>
  );
}
