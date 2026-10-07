import { Suspense } from "react";
import type { Metadata } from "next";
import { en } from "@/i18n/locales/en";
import { FirstRunScreen } from "../../../_onboarding/first-run";

export const metadata: Metadata = {
  title: en.ledger.onboarding.frame.titles.firstRun,
};

export default function Page() {
  return (
    <Suspense>
      <FirstRunScreen />
    </Suspense>
  );
}
