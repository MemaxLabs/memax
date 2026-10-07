import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { CliAuthScreen } from "../../_onboarding/cli-auth";

export const metadata: Metadata = {
  title: en.ledger.onboarding.cliAuth.title,
};

// CliAuth (CliAuth.png): /device?code=WQRT-4821, from `memax login` or
// `npx memax-cli init` where no browser could open (RFC 8628).
export default function DevicePage() {
  return (
    <Suspense>
      <CliAuthScreen />
    </Suspense>
  );
}
