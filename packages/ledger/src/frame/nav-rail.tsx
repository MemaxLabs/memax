import type { MouseEventHandler, ReactElement } from "react";
import { useRender } from "@base-ui/react/use-render";
import { Icon, type IconName } from "../brand/icon";
import { Logo } from "../brand/logo";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { toAriaKeyshortcuts } from "../lib/keyshortcuts";
import type { MarkState, NavPlace } from "../lib/types";
import { StateMark } from "../memory/state-mark";
import { Kbd } from "../primitives/kbd";
import { AgentStamp } from "../provenance/agent-stamp";

export interface NavItem {
  /** One of the places, or any other id (then pass `label` and `icon`). */
  id: NavPlace | (string & {});
  href: string;
  /** Defaults to the locale's name for a known place. */
  label?: string;
  /** Defaults to the place's icon. */
  icon?: IconName;
  count?: number;
  /** `pending` is the ochre count: things waiting on the person. Only Review uses it. */
  tone?: "pending" | "plain";
  /** Read instead of the bare number ("5 waiting on you"). */
  countLabel?: string;
}

export interface NavRailStatus {
  /** `kept` for in sync, `proposed` when a person must act ("Cursor file drifted"). */
  state: MarkState;
  label: string;
}

export interface NavRailProps {
  /** The places, in order. */
  items: NavItem[];
  /** The id of the current place. */
  active?: string;
  /** The current space's name ("memax-v2"). */
  space: string;
  /** Its kind ("Project", "Personal", "Team"), already translated. */
  spaceKind?: string;
  /** Opens the space switcher. */
  onSpaceClick?: MouseEventHandler<HTMLElement>;
  /** Renders the switcher button as another element, e.g. `<Menu.Trigger />`. */
  spaceRender?: ReactElement;
  /** Opens Ask or remember (⌘K). */
  onAskClick?: MouseEventHandler<HTMLElement>;
  /** Renders the Ask button as another element, e.g. `<Dialog.Trigger />`. */
  askRender?: ReactElement;
  /** The keycap on the Ask button. */
  askShortcut?: string;
  settingsHref: string;
  settingsActive?: boolean;
  /** The signed-in person's initials. */
  person: string;
  /** Their label beside the stamp. Defaults to the locale's "You". */
  personName?: string;
  /** The status line; announced politely when it changes. */
  status: NavRailStatus;
  /** The navigation landmark's name. Defaults to the locale's "Main". */
  label?: string;
  className?: string;
}

const PLACE_ICONS: Record<NavPlace, IconName> = {
  today: "today",
  review: "review",
  briefs: "brief",
  memories: "memories",
  handoffs: "handoff",
  agents: "agents",
  // Not drawn in the handoff; a designer should confirm.
  decisions: "receipt",
};

function isPlace(id: string): id is NavPlace {
  return id in PLACE_ICONS;
}

/** The navigation rail: space switcher, ⌘K, the places, settings and the status line. */
export function NavRail({
  items,
  active,
  space,
  spaceKind,
  onSpaceClick,
  spaceRender,
  onAskClick,
  askRender,
  askShortcut = "⌘K",
  settingsHref,
  settingsActive = false,
  person,
  personName,
  status,
  label,
  className,
}: NavRailProps) {
  const { strings, Link } = useLedger();
  const n = strings.nav;

  const spaceButton = useRender({
    defaultTagName: "button",
    render: spaceRender,
    props: {
      type: spaceRender ? undefined : "button",
      className: "mx-rail-space",
      onClick: onSpaceClick,
      children: (
        <>
          <span className="mx-rail-space-name">{space}</span>
          {spaceKind ? (
            <span className="mx-rail-space-kind">{spaceKind}</span>
          ) : null}
          <Icon name="chevron-down" size={14} />
        </>
      ),
    },
  });

  const askButton = useRender({
    defaultTagName: "button",
    render: askRender,
    props: {
      type: askRender ? undefined : "button",
      className: "mx-rail-ask",
      onClick: onAskClick,
      "aria-keyshortcuts": toAriaKeyshortcuts(askShortcut),
      children: (
        <>
          <Icon name="search" size={16} />
          <span>{n.ask}</span>
          <Kbd aria-hidden="true">{askShortcut}</Kbd>
        </>
      ),
    },
  });

  return (
    <nav className={cx("mx-rail", className)} aria-label={label ?? n.label}>
      <div className="mx-rail-top">
        <Logo variant="mark" size={20} />
        {spaceButton}
      </div>
      {askButton}
      <ul className="mx-rail-list">
        {items.map((item) => {
          const current = item.id === active;
          const text =
            item.label ?? (isPlace(item.id) ? n.places[item.id] : item.id);
          const icon =
            item.icon ?? (isPlace(item.id) ? PLACE_ICONS[item.id] : "memories");
          return (
            <li key={item.id}>
              <Link
                href={item.href}
                className={cx("mx-rail-item", current && "is-active")}
                aria-current={current ? "page" : undefined}
              >
                <Icon name={icon} size={16} />
                <span className="mx-rail-label">{text}</span>
                {item.count ? (
                  <span
                    className={cx(
                      "mx-count",
                      item.tone === "pending" && "is-pending",
                    )}
                  >
                    {item.countLabel ? (
                      <>
                        <span aria-hidden="true">{item.count}</span>
                        <span className="mx-sr">{item.countLabel}</span>
                      </>
                    ) : (
                      item.count
                    )}
                  </span>
                ) : null}
              </Link>
            </li>
          );
        })}
      </ul>
      <div className="mx-rail-foot">
        <Link
          href={settingsHref}
          className={cx("mx-rail-item", settingsActive && "is-active")}
          aria-current={settingsActive ? "page" : undefined}
        >
          <Icon name="settings" size={16} />
          <span className="mx-rail-label">{n.settings}</span>
        </Link>
        <div className="mx-rail-me">
          <AgentStamp
            person={person}
            name={personName ?? strings.agent.you}
            showName
          />
          <span className="mx-rail-sync" role="status">
            <StateMark state={status.state} label={status.label} size={8} />
          </span>
        </div>
      </div>
    </nav>
  );
}
