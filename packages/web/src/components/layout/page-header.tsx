"use client";

/**
 * `<PageHeader>` — canonical title + subtitle for top-level pages.
 *
 * Inbox is the visual baseline (per user spec): 18px font-semibold
 * title, optional 13px text-fg-3 subtitle, mb-6 to body. EVERY
 * top-level page gets the same scale — uniform beats clever, and
 * the user's complaint was inconsistency.
 *
 * The optional `size="large"` (21px font-bold) is reserved for
 * surfaces with a real destination-weight need (memory detail's
 * single-record title is the existing precedent). Default to
 * `size="default"` and only escalate when there is evidence the
 * page needs to read louder than inbox does.
 *
 * Why a component instead of a class: every page importing the
 * same JSX shape (h2 + small p) drifts over time — one page adds
 * a tracking tweak, another bumps the bottom margin "just here."
 * A component freezes the contract.
 */

import type { ReactNode } from "react";

interface PageHeaderProps {
  title: ReactNode;
  /** Small descriptive line under the title. Optional. */
  subtitle?: ReactNode;
  /**
   * `default` (18px font-semibold) — matches inbox baseline. Use this
   * unless the page has been validated as needing more weight.
   * `large` (21px font-bold) — reserved for destination titles where
   * the page IS the content (e.g., a single memory's title-as-page).
   */
  size?: "default" | "large";
  /**
   * Right-aligned actions slot (filter chips, primary CTA, etc).
   * Renders inline on desktop, drops below title on mobile narrow.
   */
  actions?: ReactNode;
  /** Bottom margin to body content. Defaults to `mb-6` (matches inbox). */
  className?: string;
}

const TITLE_DEFAULT = "text-[18px] font-semibold text-foreground";
const TITLE_LARGE =
  "text-[21px] font-bold text-foreground tracking-[-0.02em] leading-snug";

export function PageHeader({
  title,
  subtitle,
  size = "default",
  actions,
  className = "mb-6",
}: PageHeaderProps) {
  const titleClass = size === "large" ? TITLE_LARGE : TITLE_DEFAULT;
  return (
    <header className={className}>
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1">
          {size === "large" ? (
            <h1 className={titleClass}>{title}</h1>
          ) : (
            <h2 className={titleClass}>{title}</h2>
          )}
          {subtitle ? (
            <p className="mt-1 text-[13px] text-fg-3">{subtitle}</p>
          ) : null}
        </div>
        {actions ? (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        ) : null}
      </div>
    </header>
  );
}
