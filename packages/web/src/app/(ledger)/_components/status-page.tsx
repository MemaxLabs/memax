import styles from "./status-page.module.css";

/** Layout shared by (ledger)/not-found.tsx and (ledger)/error.tsx. */
export function StatusPage({
  receipt,
  title,
  description,
  actions,
  role,
}: {
  receipt?: string;
  title: string;
  description: string;
  actions: React.ReactNode;
  role?: "alert";
}) {
  return (
    <main className={styles.page}>
      <div className={styles.panel} role={role}>
        {receipt ? (
          <p className={`receipt ${styles.receipt}`}>{receipt}</p>
        ) : null}
        <h1 className={`heading ${styles.title}`}>{title}</h1>
        <p className={`ui ${styles.description}`}>{description}</p>
        <div className={styles.actions}>{actions}</div>
      </div>
    </main>
  );
}

export const statusActionClass = `ui-strong ${styles.action}`;
export const statusPrimaryActionClass = `ui-strong ${styles.action} ${styles.primary}`;
