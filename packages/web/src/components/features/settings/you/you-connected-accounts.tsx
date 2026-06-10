"use client";

import { useLocale } from "@/i18n";
import { ConnectedAccountsSection } from "@/components/features/connected-accounts-section";
import { Section } from "../shared/section";

export function YouConnectedAccounts() {
  const { t } = useLocale();

  return (
    <Section title={t.userSettings.connectedAccounts}>
      <ConnectedAccountsSection />
    </Section>
  );
}
