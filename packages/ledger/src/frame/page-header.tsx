import type { ReactNode } from "react";
import { cx } from "../lib/cx";

export interface PageHeaderProps {
  title: ReactNode;
  /** One full sentence: what changed or what is waiting, never a tagline. */
  lede?: ReactNode;
  /** Where you are ("memax-v2 · Project space"). */
  eyebrow?: ReactNode;
  /** At most two, with the primary one last. */
  actions?: ReactNode;
  className?: string;
}

/** A page's title in the serif, an optional eyebrow, a one-sentence lede, and its actions. */
export function PageHeader({
  title,
  lede,
  eyebrow,
  actions,
  className,
}: PageHeaderProps) {
  return (
    <header className={cx("mx-page-head", className)}>
      <div className="mx-page-head-text">
        {eyebrow != null ? (
          <div className="mx-page-eyebrow">{eyebrow}</div>
        ) : null}
        <h1 className="mx-page-title">{title}</h1>
        {lede != null ? <p className="mx-page-lede">{lede}</p> : null}
      </div>
      {actions != null ? (
        <div className="mx-page-actions">{actions}</div>
      ) : null}
    </header>
  );
}
