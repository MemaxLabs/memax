"use client";

/**
 * `<PageShell>` — canonical desktop+mobile content container for top-level
 * routes (`/agents`, `/topics`, `/memories`, `/inbox`, `/dreams`, etc).
 *
 * One source of truth for:
 *   - horizontal padding (`px-5 sm:px-8`)
 *   - max-width (`max-w-4xl` centered)
 *   - top padding: CONTENT_TOP on desktop, MOBILE_CHROME_CONTENT_GAP on
 *     mobile. NOT MOBILE_CONTENT_TOP — that constant includes the chrome
 *     row offset, which the mobile shell's <main> already adds. Adding
 *     both would double-count by ~64px.
 *   - bottom clearance (`pb-36 md:pb-32` to clear bar + dock)
 *   - the `animate-content-ready` opacity fade per design system §6
 *
 * Why this exists: pre-Phase-1, every page rolled its own outer-div +
 * paddingTop + max-w-4xl (with subtle drift — `px-4` on inbox, `px-5
 * sm:px-8` on agents, etc). The complaint is *inconsistency*. One
 * primitive eliminates the drift class and any future page that
 * forgets to import it stands out as the wrong-looking page.
 *
 * NOT for:
 *   - the global bar (owns its own positioning)
 *   - shell chrome (left rail, mobile top bar — fixed-position)
 *   - in-flow modal/drawer surfaces (Dialog primitives handle their own shells)
 */

import type { ReactNode } from "react";
import { useIsMobile } from "@/hooks/use-is-mobile";
import { CONTENT_TOP, MOBILE_CHROME_CONTENT_GAP } from "@/lib/layout";

interface PageShellProps {
  children: ReactNode;
  /**
   * Override outer padding when the page wants edge-to-edge content
   * (e.g., a full-bleed banner). Default `px-5 sm:px-8` matches the
   * inbox + hub-overview baseline.
   */
  outerClassName?: string;
  /**
   * Override the inner max-width container. Default `max-w-4xl`
   * (~896px). Pages that need a wider canvas can pass `max-w-6xl`.
   */
  innerClassName?: string;
}

const DEFAULT_OUTER = "animate-content-ready px-5 sm:px-8 pb-36 md:pb-32";
const DEFAULT_INNER = "mx-auto max-w-4xl";

export function PageShell({
  children,
  outerClassName = DEFAULT_OUTER,
  innerClassName = DEFAULT_INNER,
}: PageShellProps) {
  const isMobile = useIsMobile();
  // Mobile: shell-layout-mobile's <main> already pads itself to clear
  // the fixed chrome (`pt = chrome-height + safe-area-inset`). PageShell
  // adds only the small interior gap below that — adding the full chrome
  // offset would double-count.
  // Desktop: shell-layout-desktop's <main> has no top padding (no top
  // chrome), so PageShell owns the full top spacing.
  const paddingTop = isMobile ? MOBILE_CHROME_CONTENT_GAP : CONTENT_TOP;
  return (
    <div className={outerClassName} style={{ paddingTop }}>
      <div className={innerClassName}>{children}</div>
    </div>
  );
}
