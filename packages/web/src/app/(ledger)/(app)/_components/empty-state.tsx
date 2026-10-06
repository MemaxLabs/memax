import type { ReactNode } from "react";
import styles from "./empty-state.module.css";

/**
 * An empty place, after the States board: one serif sentence, a line on
 * what will arrive here, one action, and an optional receipt-style meta
 * line. Never a spinner, never an illustration.
 */
export function EmptyState({
  title,
  detail,
  action,
  meta,
  children,
}: {
  title: string;
  detail?: ReactNode;
  action?: ReactNode;
  meta?: string;
  /** Anything that belongs with the detail, such as a command to run. */
  children?: ReactNode;
}) {
  return (
    <section className={styles.empty}>
      <h2 className={styles.title}>{title}</h2>
      {detail ? <p className={styles.detail}>{detail}</p> : null}
      {children}
      {action ? <div className={styles.action}>{action}</div> : null}
      {meta ? <p className={`mx-meta ${styles.meta}`}>{meta}</p> : null}
    </section>
  );
}
