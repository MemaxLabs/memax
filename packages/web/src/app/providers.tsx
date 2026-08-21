"use client";

import { useEffect } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query-client";
import { AuthProvider } from "@/lib/auth";
import { ThemeProvider } from "next-themes";
import { initPostHog } from "@/lib/posthog";
import { LocaleProvider } from "@/i18n";
import { LocaleServerSync } from "@/components/features/locale-server-sync";

export function Providers({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    // Tag the initial $pageview with `shellVersion: "v2"`. Plan 24
    // phase 4b removed the v1/v2 cookie dispatch — pre-launch, no
    // external users — so this is constant. Kept as a registered
    // super-property so the rollout-gate query reads the same key as
    // before the flip.
    initPostHog({ shellVersion: "v2" });
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
        <LocaleProvider>
          <AuthProvider>
            <LocaleServerSync />
            {children}
          </AuthProvider>
        </LocaleProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
