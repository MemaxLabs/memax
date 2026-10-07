"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useLocale } from "@/i18n";
import styles from "./settings.module.css";

const ITEMS = [
  "account",
  "spaces",
  "keys",
  "plan",
  "notifications",
  "security",
  "integrations",
  "export",
] as const;

/**
 * Pages built so far; the others are listed, as drawn, and arrive later.
 * No board's nav has Security: it sits after Notifications.
 */
const BUILT = new Set<string>(["account", "keys", "notifications", "security"]);

export function SettingsFrame({ children }: { children: React.ReactNode }) {
  const { t } = useLocale();
  const copy = t.ledger.app.settings;
  const pathname = usePathname();
  return (
    <div className={styles.layout}>
      <nav aria-label={copy.nav} className={styles.nav}>
        <p className={`mx-section-label ${styles.navLabel}`}>{copy.nav}</p>
        <ul className={styles.navList}>
          {ITEMS.map((item) => {
            const href = `/settings/${item}`;
            const label = copy.items[item];
            return (
              <li key={item}>
                {BUILT.has(item) ? (
                  <Link
                    href={href}
                    className={styles.navItem}
                    aria-current={pathname === href ? "page" : undefined}
                  >
                    {label}
                  </Link>
                ) : (
                  <span
                    className={`${styles.navItem} ${styles.navLater}`}
                    title={t.ledger.app.notYet}
                    aria-disabled="true"
                  >
                    {label}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      </nav>
      <div className={styles.content}>{children}</div>
    </div>
  );
}
