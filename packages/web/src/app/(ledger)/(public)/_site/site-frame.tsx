"use client";

import Link from "next/link";
import { Button, Logo } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import { COPYRIGHT, LINKS } from "./links";
import styles from "./site.module.css";

export type SiteSection = "product" | "docs" | "pricing" | "trust";

/**
 * The public site's frame (Landing.png, Pricing.png, Security.png): the
 * wordmark home, the site nav, Sign in and Get started, the page, and the
 * footer. Sign in and Get started both go to /signin, which signs a new
 * person up and lands a signed-in one where they left off. A skip link
 * jumps past the header.
 */
export function SiteFrame({
  current,
  children,
}: {
  /** The nav item this page belongs to. */
  current?: SiteSection;
  children: React.ReactNode;
}) {
  const { t } = useLocale();
  const s = t.ledger.public.site;
  const here = (section: SiteSection) =>
    section === current ? ("page" as const) : undefined;
  return (
    <div className={styles.site}>
      <a className="mx-skip" href="#content">
        {s.skip}
      </a>
      <div className={styles.wrap}>
        <header className={styles.header}>
          <Link
            href={LINKS.landing}
            className={styles.home}
            aria-label={s.home}
          >
            <Logo variant="lockup" size={20} decorative />
          </Link>
          <nav aria-label={s.nav.label} className={styles.nav}>
            <ul>
              <li>
                <Link href={LINKS.landing} aria-current={here("product")}>
                  {s.nav.product}
                </Link>
              </li>
              <li>
                <Link href={LINKS.docs} aria-current={here("docs")}>
                  {s.nav.docs}
                </Link>
              </li>
              <li>
                <Link href={LINKS.pricing} aria-current={here("pricing")}>
                  {s.nav.pricing}
                </Link>
              </li>
              <li>
                <Link href={LINKS.security} aria-current={here("trust")}>
                  {s.nav.trust}
                </Link>
              </li>
              <li className={styles.wide}>
                <a href={LINKS.changelog}>{s.nav.changelog}</a>
              </li>
            </ul>
          </nav>
          <div className={styles.actions}>
            <Button variant="quiet" href={LINKS.signIn}>
              {s.signIn}
            </Button>
            <Button variant="primary" href={LINKS.signIn}>
              {s.getStarted}
            </Button>
          </div>
        </header>
        <main id="content" tabIndex={-1} className={styles.main}>
          {children}
        </main>
        <footer className={styles.footer}>
          <div className={styles.footerMark}>
            <Logo variant="mark" size={20} decorative />
            <span className="mx-meta">{COPYRIGHT}</span>
          </div>
          <nav aria-label={s.footer.label} className={styles.footerNav}>
            <ul>
              <li>
                <a href={LINKS.fullDocs}>{s.footer.docs}</a>
              </li>
              <li>
                <a href={LINKS.changelog}>{s.footer.changelog}</a>
              </li>
              <li>
                <a href={LINKS.github}>{s.footer.github}</a>
              </li>
              <li>
                <a href={LINKS.privacy}>{s.footer.privacy}</a>
              </li>
            </ul>
          </nav>
        </footer>
      </div>
    </div>
  );
}
