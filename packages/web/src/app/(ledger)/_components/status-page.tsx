import styles from "./status-page.module.css";

/**
 * The not-found and error layout, after the "Not found" state on the
 * States2 board: a receipt line in mono, one serif sentence, one line
 * of explanation and the actions (Ledger Buttons, primary last).
 *
 * `page` centres it on its own page ((ledger)/not-found.tsx, error.tsx);
 * `sheet` sets it inside the app frame's sheet, beside the rail.
 */
export function StatusPage({
  receipt,
  title,
  description,
  actions,
  role,
  variant = "page",
}: {
  receipt?: string;
  title: string;
  description: string;
  actions: React.ReactNode;
  role?: "alert";
  variant?: "page" | "sheet";
}) {
  const Root = variant === "page" ? "main" : "div";
  return (
    <Root className={variant === "page" ? styles.page : styles.sheet}>
      <div className={styles.panel} role={role}>
        {receipt ? (
          <p className={`receipt ${styles.receipt}`}>{receipt}</p>
        ) : null}
        <h1 className={styles.title}>{title}</h1>
        <p className={`ui ${styles.description}`}>{description}</p>
        <div className={styles.actions}>{actions}</div>
      </div>
    </Root>
  );
}
