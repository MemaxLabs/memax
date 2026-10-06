import { useId, type ReactNode } from "react";
import { useLedger } from "../i18n/provider";
import { cx } from "../lib/cx";
import { NavRail, type NavRailProps } from "./nav-rail";

export interface ShellProps {
  nav: NavRailProps;
  /** The page. Wrap content in `.mx-page` for the standard gutters and width. */
  children?: ReactNode;
  /** The id of the `<main>` sheet, which the skip link targets. */
  mainId?: string;
  className?: string;
}

/**
 * The app frame: the rail on `paper`, and the work on an inset `sheet` with a
 * hairline edge. A skip link, visible on focus, jumps past the rail.
 */
export function Shell({ nav, children, mainId, className }: ShellProps) {
  const { strings } = useLedger();
  const autoId = useId();
  const sheetId = mainId ?? `mx-main-${autoId.replace(/[^A-Za-z0-9_-]/g, "")}`;
  return (
    <div className={cx("mx-shell", className)}>
      <a className="mx-skip" href={`#${sheetId}`}>
        {strings.shell.skip}
      </a>
      <NavRail {...nav} />
      <main id={sheetId} className="mx-sheet" tabIndex={-1}>
        {children}
      </main>
    </div>
  );
}
