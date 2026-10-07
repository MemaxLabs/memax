import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ConsentScreen } from "./consent-screen";

export const metadata: Metadata = {
  title: en.ledger.consent.pageTitle,
  robots: { index: false },
};

// OAuthConsent (OAuthConsent.png): where an outside agent's OAuth request
// (MCP) asks a person for one space. Open to every browser, like /device:
// the API sends a person with a space on the V2 record here after they
// sign in for the agent, and a browser that opted into V2 comes here from
// V1's /oauth/consent (lib/ui-gate.ts). The referrer policy stays the
// default: the consent form's post must carry this page's Origin, which
// the API checks.
export default function OAuthConsentPage() {
  return (
    <Suspense>
      <ConsentScreen />
    </Suspense>
  );
}
