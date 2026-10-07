import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ConsentScreen } from "./consent-screen";

export const metadata: Metadata = {
  title: en.ledger.consent.pageTitle,
  robots: { index: false },
};

// OAuthConsent (OAuthConsent.png): where an outside agent's OAuth request
// (MCP) asks a person for one space. Open to every browser, like /signin
// and /device: the API sends everyone here from GET /oauth/authorize, and
// V1's retired /oauth/consent sends its links here (lib/ui-gate.ts). The
// person signs in on the way if they need to.
export default function OAuthConsentPage() {
  return (
    <Suspense>
      <ConsentScreen />
    </Suspense>
  );
}
