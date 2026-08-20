"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import { setTranslationsSnapshot } from "@/lib/translations-snapshot";
import { interpolate } from "./interpolate";
import { en, type Translations } from "./locales/en";
import { zh } from "./locales/zh";

// Re-export for the old import path — some call sites still use
// `import { interpolate } from "@/i18n"`. The helper itself lives in
// `./interpolate` so tests that mock the React layer don't break
// module-level callers.
export { interpolate };

// ─── Supported locales ───
export type Locale = "en" | "zh";
const LOCALES: Record<Locale, Translations> = { en, zh };
const STORAGE_KEY = "memax-locale";

// ─── Detect from browser ───

/**
 * The locale the user explicitly chose on THIS device, or null.
 * Exposed so the server-sync layer can tell "explicit local choice"
 * apart from "browser default" — only the former is worth migrating
 * up to the server-side settings.locale.
 */
export function getStoredLocale(): Locale | null {
  if (typeof window === "undefined") return null;
  const saved = localStorage.getItem(STORAGE_KEY);
  return saved && saved in LOCALES ? (saved as Locale) : null;
}

function detectLocale(): Locale {
  if (typeof window === "undefined") return "en";
  const saved = getStoredLocale();
  if (saved) return saved;
  const lang = navigator.language.toLowerCase();
  if (lang.startsWith("zh")) return "zh";
  return "en";
}

// ─── Context ───
interface LocaleContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  /**
   * Apply the server-persisted preference (settings.locale) without
   * treating it as a new user choice. Same local effect as setLocale
   * — state + localStorage — but callers of setLocale additionally
   * PATCH the preference up to the server; this path exists so the
   * server → client sync can't loop back into a server write.
   */
  applyServerLocale: (l: Locale) => void;
  t: Translations;
}

const LocaleContext = createContext<LocaleContextValue>({
  locale: "en",
  setLocale: () => {},
  applyServerLocale: () => {},
  t: en,
});

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>("en");

  useEffect(() => {
    setLocaleState(detectLocale());
  }, []);

  // Mirror the active translations into a module-level ref so code
  // running outside React (MutationCache error classifier, etc.) can
  // render locale-correct copy. This must happen on EVERY locale
  // change, not just mount, so switching from en → zh during a
  // session re-points the snapshot too.
  useEffect(() => {
    setTranslationsSnapshot(LOCALES[locale]);
  }, [locale]);

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l);
    localStorage.setItem(STORAGE_KEY, l);
  }, []);

  // Same local behavior; a separate identity so LocaleServerSync can
  // distinguish "user chose" from "server said" (see interface note).
  const applyServerLocale = setLocale;

  return (
    <LocaleContext.Provider
      value={{ locale, setLocale, applyServerLocale, t: LOCALES[locale] }}
    >
      {children}
    </LocaleContext.Provider>
  );
}

// ─── Hooks ───
export function useLocale() {
  return useContext(LocaleContext);
}

// Helper for memory count (handles pluralization)
export function useMemoryCount() {
  const { t } = useLocale();
  return (n: number) => {
    if (n === 0) return t.memory.zero;
    if (n === 1) return t.memory.one;
    return interpolate(t.memory.other, { n });
  };
}

// Helper for interpolation
export function useInterpolate() {
  return interpolate;
}

// Pick a singular or plural template and interpolate {n}. Use for any
// "{n} things" label that can legitimately receive n === 1 — prevents
// "1 memories" / "1 topics" in English. Chinese (and other no-plural
// locales) supply the same string for both forms.
export function pluralize(one: string, other: string, n: number): string {
  return n === 1 ? one : interpolate(other, { n });
}
