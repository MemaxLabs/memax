import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { ConnectScreen } from "../../../_onboarding/connect";

export const metadata: Metadata = {
  title: en.ledger.onboarding.frame.titles.connect,
};

export default function Page() {
  return (
    <Suspense>
      <ConnectScreen />
    </Suspense>
  );
}
