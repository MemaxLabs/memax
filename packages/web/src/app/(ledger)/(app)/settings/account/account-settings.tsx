"use client";

import { PageHeader, Segmented } from "@memaxlabs/ledger";
import { useLocale, type Locale } from "@/i18n";
import { ThemeControl } from "../../../_components/theme-control";
import styles from "../settings.module.css";

const LOCALES: Locale[] = ["en", "zh"];

/**
 * Settings › Account, the Phase 0 placeholder: the device's appearance
 * (Paper, Carbon or System) and language. Profile, sign-in methods and
 * sessions (Account.png) arrive with epic 2.6.
 */
export function AccountSettings() {
  const { t, locale, setLocale } = useLocale();
  const copy = t.ledger.app.settings.account;
  return (
    <>
      <PageHeader title={copy.title} lede={copy.lede} />
      <section className="mx-panel" aria-labelledby="appearance">
        <header className="mx-panel-head">
          <h2 id="appearance" className="mx-panel-title">
            {copy.appearance}
          </h2>
          <span className="mx-meta">{copy.appearanceMeta}</span>
        </header>
        <div className={styles.row}>
          <div className={styles.rowText}>
            <span className={styles.rowLabel}>{copy.theme}</span>
            <span className={styles.hint}>{copy.themeHint}</span>
          </div>
          <ThemeControl size="sm" />
        </div>
        <div className={styles.row}>
          <div className={styles.rowText}>
            <span className={styles.rowLabel}>{copy.language}</span>
          </div>
          <Segmented<Locale>
            size="sm"
            label={copy.language}
            value={locale}
            onChange={setLocale}
            options={LOCALES.map((value) => ({
              value,
              label: (
                <span lang={value === "zh" ? "zh-CN" : "en"}>
                  {copy.languages[value]}
                </span>
              ),
            }))}
          />
        </div>
      </section>
    </>
  );
}
