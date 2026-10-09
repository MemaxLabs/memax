"use client";

import Link from "next/link";
import { LedgerProvider } from "@memaxlabs/ledger";
import { useLocale } from "@/i18n";

/**
 * Gives every Ledger component the active locale (its own en/zh
 * strings, the Chinese type rules) and Next's Link, so rail items,
 * Button href and Cite href navigate on the client.
 */
export function LedgerUiProvider({ children }: { children: React.ReactNode }) {
  const { locale } = useLocale();
  return (
    <LedgerProvider locale={locale} linkComponent={Link}>
      {children}
    </LedgerProvider>
  );
}
