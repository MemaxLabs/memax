"use client";

import { useTheme } from "next-themes";
import { useLocale } from "@/i18n";
import { Section, SelectionOption } from "../shared/section";

/**
 * Plan 24 phase 4b retired the v1/v2 cookie dispatch — the shell-
 * preview toggle that used to live here would now be a broken
 * affordance. Removed in `00d0f4b4..` along with `SHELL_COOKIE`,
 * `readShellCookie`, `setShellCookieAndReload`, and the i18n strings
 * `shellPreview*` / `shellSwitchReloading`.
 */
export function YouAppearance() {
  const { theme, setTheme } = useTheme();
  const { t } = useLocale();

  const themeMode =
    theme === "light" || theme === "dark" || theme === "system"
      ? theme
      : "system";

  return (
    <Section title={t.userSettings.appearance}>
      <div className="space-y-1">
        <div className="px-1 pb-1">
          <p className="text-[15px] text-fg-2">{t.userSettings.theme}</p>
          <p className="mt-0.5 text-[13px] text-fg-3">
            {t.userSettings.themeDesc}
          </p>
        </div>
        {(
          [
            {
              value: "light",
              label: t.userSettings.light,
              desc: t.userSettings.lightDesc,
            },
            {
              value: "dark",
              label: t.userSettings.dark,
              desc: t.userSettings.darkDesc,
            },
            {
              value: "system",
              label: t.userSettings.auto,
              desc: t.userSettings.autoDesc,
            },
          ] as const
        ).map((option) => (
          <SelectionOption
            key={option.value}
            label={option.label}
            sublabel={option.desc}
            checked={themeMode === option.value}
            onSelect={() => {
              document.documentElement.classList.add("theme-transition");
              setTheme(option.value);
              window.setTimeout(() => {
                document.documentElement.classList.remove("theme-transition");
              }, 500);
            }}
          />
        ))}
      </div>
    </Section>
  );
}
