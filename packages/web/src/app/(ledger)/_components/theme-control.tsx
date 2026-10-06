"use client";

import { useEffect, useState } from "react";
import { Segmented } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";
import {
  getThemePreference,
  setThemePreference,
  type ThemePreference,
} from "../_lib/theme";

const PREFERENCES: ThemePreference[] = ["light", "dark", "system"];

/**
 * Paper / Carbon / System, as a Ledger Segmented (a radio group with
 * arrow keys). It writes the memax_theme cookie and applies data-theme
 * at once; the head script applies it before first paint on the next
 * load, so there is no flash.
 */
export function ThemeControl({ size = "md" }: { size?: "sm" | "md" }) {
  const { t } = useLocale();
  // "system" on the server and the first client render, then the stored
  // cookie once mounted (the server can't know it).
  const [preference, setPreference] = useState<ThemePreference>("system");
  useEffect(() => {
    setPreference(getThemePreference());
  }, []);

  return (
    <Segmented<ThemePreference>
      size={size}
      label={t.ledger.theme.label}
      value={preference}
      onChange={(value) => {
        setThemePreference(value);
        setPreference(value);
      }}
      options={PREFERENCES.map((value) => ({
        value,
        label: t.ledger.theme[value],
      }))}
    />
  );
}
