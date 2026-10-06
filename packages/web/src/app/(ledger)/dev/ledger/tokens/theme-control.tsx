"use client";

import { useEffect, useState } from "react";
import { useLocale } from "@/i18n";
import {
  getThemePreference,
  setThemePreference,
  type ThemePreference,
} from "../../../_lib/theme";
import styles from "./specimen.module.css";

const PREFERENCES: ThemePreference[] = ["light", "dark", "system"];

/**
 * Paper / Carbon / System for the specimen page. A plain button group
 * with aria-pressed until @memaxlabs/ledger's Segmented lands; the
 * Shell work package moves the Carbon toggle into settings.
 */
export function ThemeControl() {
  const { t } = useLocale();
  // "system" on the server and on the first client render, then the
  // stored cookie once mounted (the server can't know it).
  const [preference, setPreference] = useState<ThemePreference>("system");
  useEffect(() => {
    setPreference(getThemePreference());
  }, []);

  return (
    <div
      role="group"
      aria-label={t.ledger.theme.label}
      className={styles.segmented}
    >
      {PREFERENCES.map((value) => (
        <button
          key={value}
          type="button"
          aria-pressed={preference === value}
          className={`ui-sm ${styles.segment}`}
          onClick={() => {
            setThemePreference(value);
            setPreference(value);
          }}
        >
          {t.ledger.theme[value]}
        </button>
      ))}
    </div>
  );
}
