"use client";

import { useEffect } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";
import { AuthProvider } from "@/lib/auth";
import { initPostHog } from "@/lib/posthog";
import { LocaleProvider } from "@/i18n";
import { LedgerUiProvider } from "./_lib/ledger-ui-provider";
import { LocaleDocumentSync } from "./_lib/locale-document-sync";

// V2 providers. They reuse the V1 data and auth plumbing that carries
// over (plan §6.5): the TanStack Query client, AuthProvider (which talks
// to the API through memax-client) and the i18n LocaleProvider, plus
// LedgerProvider for the components' locale and links. There is no
// next-themes: the theme is data-theme on <html> (see _lib/theme.ts).
// No V1 UI is imported here: the app frame ((app)/layout.tsx) brings
// its own keymap, toasts and ⌘K.

export function LedgerProviders({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    // Same super-property V1 uses for its rollout queries, with a value
    // that tells the Ledger UI apart.
    initPostHog({ shellVersion: "ledger" });
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <LocaleProvider>
        <AuthProvider>
          <LocaleDocumentSync />
          <LedgerUiProvider>{children}</LedgerUiProvider>
        </AuthProvider>
      </LocaleProvider>
    </QueryClientProvider>
  );
}
