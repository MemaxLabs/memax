"use client";

import { useEffect } from "react";
import { useLocale } from "@/i18n";

/**
 * Keeps <html lang> equal to the active locale. The root layout renders
 * lang="en" because the LocaleProvider renders English on the server;
 * once it switches to the device's choice (or the account's), lang
 * follows in the same commit as the text, so assistive tech, hyphenation
 * and :lang() rules always see the language actually on screen.
 */
export function LocaleDocumentSync() {
  const { locale } = useLocale();
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  return null;
}
