import type { ReactNode } from "react";
import { LedgerProvider, type LedgerLocale } from "../i18n/provider";
import { cx } from "../lib/cx";

export type PreviewTheme = "light" | "dark";

export interface PreviewProps {
  /** Paper (light) or Carbon (dark). */
  theme?: PreviewTheme;
  /** The interface strings. The demo data stays as drawn (English). */
  locale?: LedgerLocale;
}

export interface PreviewFrameProps extends PreviewProps {
  /** Scopes the preview's own layout helpers in previews.css. */
  name: string;
  children: ReactNode;
}

const LANG: Record<LedgerLocale, string> = { en: "en", zh: "zh-CN" };

/**
 * Renders a preview the way the handoff's preview pages do: the body's base
 * styles under one theme, so a dark preview is dark edge to edge.
 */
export function PreviewFrame({
  name,
  theme = "light",
  locale = "en",
  children,
}: PreviewFrameProps) {
  return (
    <LedgerProvider locale={locale}>
      <div
        className={cx("pv-frame", `pv-${name}`)}
        data-theme={theme}
        lang={LANG[locale]}
      >
        {children}
      </div>
    </LedgerProvider>
  );
}
